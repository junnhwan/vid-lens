package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"vidlens/internal/domain"
)

const ArticleReviewTimeout = 120 * time.Second

type ArticleReview struct {
	ID          string                `json:"id"`
	ArticleID   string                `json:"article_id"`
	ConfigID    string                `json:"config_id"`
	Trigger     string                `json:"trigger"`
	SessionID   *string               `json:"session_id"`
	Status      string                `json:"status"`
	Epoch       int64                 `json:"epoch"`
	Submissions int                   `json:"submissions"`
	Result      *domain.ArticleAdvice `json:"result"`
	FailureCode *string               `json:"failure_code"`
	CreatedAt   time.Time             `json:"created_at"`
	StartedAt   *time.Time            `json:"started_at"`
	DeadlineAt  time.Time             `json:"deadline_at"`
	UpdatedAt   time.Time             `json:"updated_at"`
}

func queueAutomaticArticleReview(ctx context.Context, tx pgx.Tx, run, article, config string) error {
	_, err := tx.Exec(ctx, `INSERT INTO article_reviews(id,article_id,config_id,trigger)
 SELECT $1,$2,$3,'automatic' FROM job_runs WHERE id=$4 AND checkpoint->>'article_delivery_policy'=ANY($5::text[])
 ON CONFLICT DO NOTHING`, domain.ID(), article, config, run, []string{domain.ArticleDeliveryV1, domain.ArticleDeliveryV2, domain.ArticleDeliveryV3, domain.ArticleDeliveryV4, domain.ArticleDeliveryV5, domain.ArticleDeliveryV6})
	return err
}

func expireArticleReviews(ctx context.Context, q interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}) error {
	_, err := q.Exec(ctx, `UPDATE article_reviews SET status='incomplete',failure_code='REVIEW_UNFINISHED',updated_at=now() WHERE status IN ('queued','running') AND deadline_at<=now()`)
	return err
}

func (s *Store) PendingArticleReviews(ctx context.Context) ([]ArticleReview, error) {
	if err := expireArticleReviews(ctx, s.Pool); err != nil {
		return nil, err
	}
	return many[ArticleReview](ctx, s.Pool, "SELECT to_jsonb(r) FROM article_reviews r WHERE status='queued' ORDER BY created_at LIMIT 20")
}

func (s *Store) ClaimArticleReview(ctx context.Context, id string) (*ArticleReview, error) {
	var out *ArticleReview
	err := s.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext('vidlens-article-review'))"); err != nil {
			return err
		}
		if err := expireArticleReviews(ctx, tx); err != nil {
			return err
		}
		r, err := one[ArticleReview](ctx, tx, "SELECT to_jsonb(r) FROM article_reviews r WHERE id=$1 FOR UPDATE", id)
		if err != nil {
			return err
		}
		if r.Status != "queued" {
			return nil
		}
		var busy bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM article_reviews WHERE status='running')").Scan(&busy); err != nil {
			return err
		}
		if busy {
			return ErrExecutionBusy
		}
		r, err = one[ArticleReview](ctx, tx, `UPDATE article_reviews SET status='running',epoch=epoch+1,started_at=now(),deadline_at=now()+$2*interval '1 second',updated_at=now() WHERE id=$1 RETURNING to_jsonb(article_reviews)`, id, int(ArticleReviewTimeout.Seconds()))
		if err == nil {
			out = &r
		}
		return err
	})
	return out, err
}

func (s *Store) BeginArticleReviewSubmission(ctx context.Context, r ArticleReview, inputHash string) error {
	tag, err := s.Pool.Exec(ctx, `UPDATE article_reviews SET submissions=1,input_hash=$3,updated_at=now() WHERE id=$1 AND epoch=$2 AND status='running' AND deadline_at>now() AND submissions=0`, r.ID, r.Epoch, inputHash)
	if err == nil && tag.RowsAffected() != 1 {
		return domain.ErrFenced
	}
	return err
}

func (s *Store) FinishArticleReview(ctx context.Context, r ArticleReview, result *domain.ArticleAdvice, failure string, reporting any) error {
	status := "completed"
	if failure != "" || result == nil {
		status = "incomplete"
	}
	tag, err := s.Pool.Exec(ctx, `UPDATE article_reviews SET status=$3,result=$4,failure_code=NULLIF($5,''),reporting=$6,updated_at=now() WHERE id=$1 AND epoch=$2 AND status='running'`, r.ID, r.Epoch, status, domain.JSON(result), failure, domain.JSON(reporting))
	if err == nil && tag.RowsAffected() != 1 {
		return domain.ErrFenced
	}
	return err
}

func (s *Store) ArticleReview(ctx context.Context, id string) (ArticleReview, error) {
	return one[ArticleReview](ctx, s.Pool, "SELECT to_jsonb(r) FROM article_reviews r WHERE id=$1", id)
}

func articleReviewAccess(ctx context.Context, tx pgx.Tx, session, request string) (domain.Request, error) {
	r, err := loadRequest(ctx, tx, request)
	if err != nil || r.SessionID != session {
		return r, domain.Err(404, "RESOURCE_NOT_FOUND", "资源不存在。")
	}
	if r.ArticleID == nil {
		return r, domain.Err(409, "ARTICLE_NOT_READY", "文章尚未就绪。")
	}
	return r, nil
}

func (s *Store) LatestArticleReview(ctx context.Context, session, request string) (*ArticleReview, error) {
	var out *ArticleReview
	err := s.read(ctx, func(ctx context.Context, tx pgx.Tx) error {
		r, err := articleReviewAccess(ctx, tx, session, request)
		if err != nil {
			return err
		}
		v, err := one[ArticleReview](ctx, tx, `SELECT to_jsonb(r) FROM article_reviews r WHERE article_id=$1 AND (trigger='automatic' OR session_id=$2) ORDER BY created_at DESC,id DESC LIMIT 1`, *r.ArticleID, session)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err == nil {
			out = &v
		}
		return err
	})
	return out, err
}

func (s *Store) CreateArticleReview(ctx context.Context, session, request, key string) (Accepted, error) {
	var a Accepted
	err := s.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		r, err := articleReviewAccess(ctx, tx, session, request)
		if err != nil {
			return err
		}
		scope, h := "article-review:"+session+":"+request, Hash(map[string]string{"article": *r.ArticleID})
		var found bool
		if a, found, err = replay(ctx, tx, scope, key, h); err != nil || found {
			return err
		}
		if _, err = tx.Exec(ctx, "SELECT id FROM articles WHERE id=$1 FOR UPDATE", *r.ArticleID); err != nil {
			return err
		}
		if err = expireArticleReviews(ctx, tx); err != nil {
			return err
		}
		var id string
		err = tx.QueryRow(ctx, `SELECT id FROM article_reviews WHERE article_id=$1 AND (trigger='automatic' OR session_id=$2) AND status IN ('queued','running') ORDER BY created_at LIMIT 1`, *r.ArticleID, session).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			var pending int
			if err = tx.QueryRow(ctx, "SELECT count(*) FROM article_reviews WHERE status IN ('queued','running')").Scan(&pending); err != nil {
				return err
			}
			if pending >= 20 {
				return domain.Err(429, "ADMISSION_LIMITED", "审核队列暂时已满，文章仍可阅读和导出。")
			}
			if err = checkSubmission(ctx, "question", session); err != nil {
				return err
			}
			id = domain.ID()
			_, err = tx.Exec(ctx, `INSERT INTO article_reviews(id,article_id,config_id,trigger,session_id) SELECT $1,id,config_id,'manual',$3 FROM articles WHERE id=$2`, id, *r.ArticleID, session)
		}
		if err != nil {
			return err
		}
		a, err = remember(ctx, tx, scope, key, h, id, 86400)
		return err
	})
	return a, err
}

func ArticleReviewView(r *ArticleReview) any {
	if r == nil {
		return map[string]any{"review": nil}
	}
	return map[string]any{"review": map[string]any{"id": r.ID, "article_id": r.ArticleID, "trigger": r.Trigger, "status": r.Status, "result": r.Result, "created_at": r.CreatedAt, "updated_at": r.UpdatedAt}}
}
