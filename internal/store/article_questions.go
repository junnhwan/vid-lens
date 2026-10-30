package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"vidlens/internal/domain"
)

const ArticleQuestionsTimeout = 90 * time.Second

type ArticleQuestions struct {
	ArticleID   string                     `json:"article_id"`
	ConfigID    string                     `json:"config_id"`
	Policy      string                     `json:"policy"`
	Status      string                     `json:"status"`
	Epoch       int64                      `json:"epoch"`
	Submissions int                        `json:"submissions"`
	Result      *domain.SuggestedQuestions `json:"result"`
	DeadlineAt  time.Time                  `json:"deadline_at"`
	UpdatedAt   time.Time                  `json:"updated_at"`
}

func queueArticleQuestions(ctx context.Context, tx pgx.Tx, c domain.Claim, article string) error {
	_, err := tx.Exec(ctx, `INSERT INTO article_questions(article_id,config_id,policy)
 SELECT $1,config_id,$3 FROM job_runs WHERE id=$2 AND checkpoint->>'article_questions_policy'=$3 ON CONFLICT DO NOTHING`, article, c.Run.ID, domain.ArticleQuestionsPolicy)
	return err
}

func (s *Store) PendingArticleQuestions(ctx context.Context) ([]ArticleQuestions, error) {
	if _, err := s.Pool.Exec(ctx, "UPDATE article_questions SET status='incomplete',failure_code='QUESTIONS_UNFINISHED',updated_at=now() WHERE status IN ('queued','running') AND deadline_at<=now()"); err != nil {
		return nil, err
	}
	return many[ArticleQuestions](ctx, s.Pool, "SELECT to_jsonb(q) FROM article_questions q WHERE status='queued' ORDER BY created_at LIMIT 20")
}

func (s *Store) ClaimArticleQuestions(ctx context.Context, id string) (*ArticleQuestions, error) {
	var out *ArticleQuestions
	err := s.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext('vidlens-article-questions'))"); err != nil {
			return err
		}
		q, err := one[ArticleQuestions](ctx, tx, "SELECT to_jsonb(q) FROM article_questions q WHERE article_id=$1 FOR UPDATE", id)
		if err != nil {
			return err
		}
		if q.Status != "queued" || !q.DeadlineAt.After(time.Now()) {
			return nil
		}
		var busy bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM article_questions WHERE status='running' AND deadline_at>now())").Scan(&busy); err != nil {
			return err
		}
		if busy {
			return ErrExecutionBusy
		}
		q, err = one[ArticleQuestions](ctx, tx, "UPDATE article_questions SET status='running',epoch=epoch+1,started_at=now(),deadline_at=now()+$2*interval '1 second',updated_at=now() WHERE article_id=$1 RETURNING to_jsonb(article_questions)", id, int(ArticleQuestionsTimeout.Seconds()))
		if err == nil {
			out = &q
		}
		return err
	})
	return out, err
}

func (s *Store) BeginArticleQuestionsSubmission(ctx context.Context, q ArticleQuestions, inputHash string) error {
	return s.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := checkAvailable(ctx, tx, q.ConfigID); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, "UPDATE article_questions SET submissions=1,input_hash=$3,updated_at=now() WHERE article_id=$1 AND epoch=$2 AND status='running' AND deadline_at>now() AND submissions=0", q.ArticleID, q.Epoch, inputHash)
		if err == nil && tag.RowsAffected() != 1 {
			return domain.ErrFenced
		}
		return err
	})
}

func (s *Store) FinishArticleQuestions(ctx context.Context, q ArticleQuestions, result *domain.SuggestedQuestions, failure string, reporting any) error {
	status := "completed"
	if failure != "" || result == nil {
		status = "incomplete"
	}
	tag, err := s.Pool.Exec(ctx, "UPDATE article_questions SET status=$3,result=$4,failure_code=NULLIF($5,''),reporting=$6,updated_at=now() WHERE article_id=$1 AND epoch=$2 AND status='running'", q.ArticleID, q.Epoch, status, domain.JSON(result), failure, domain.JSON(reporting))
	if err == nil && tag.RowsAffected() != 1 {
		return domain.ErrFenced
	}
	return err
}

func articleQuestionsView(ctx context.Context, q Querier, article string) (map[string]any, error) {
	r, err := one[ArticleQuestions](ctx, q, "SELECT to_jsonb(q) FROM article_questions q WHERE article_id=$1", article)
	if errors.Is(err, pgx.ErrNoRows) {
		return map[string]any{"status": "unavailable", "items": []domain.SuggestedQuestion{}}, nil
	}
	if err != nil {
		return nil, err
	}
	status := r.Status
	if (status == "queued" || status == "running") && !r.DeadlineAt.After(time.Now()) {
		status = "incomplete"
	}
	items := []domain.SuggestedQuestion{}
	if status == "completed" && r.Result != nil {
		items = r.Result.Items
	}
	return map[string]any{"status": status, "items": items}, nil
}

func (s *Store) ArticleQuestionSuggestions(ctx context.Context, session, request string) (map[string]any, error) {
	var out map[string]any
	err := s.read(ctx, func(ctx context.Context, q pgx.Tx) error {
		r, err := loadRequest(ctx, q, request)
		if err != nil || r.SessionID != session || r.ArticleID == nil {
			return domain.Err(404, "RESOURCE_NOT_FOUND", "资源不存在。")
		}
		out, err = articleQuestionsView(ctx, q, *r.ArticleID)
		return err
	})
	return out, err
}
