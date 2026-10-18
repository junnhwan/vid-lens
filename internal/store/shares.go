package store

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"vidlens/internal/domain"
)

type RequestShare struct {
	SchemaVersion int        `json:"schema_version"`
	Enabled       bool       `json:"enabled"`
	URL           *string    `json:"url"`
	CreatedAt     *time.Time `json:"created_at"`
}

type shareRow struct {
	ID        string    `json:"id"`
	RequestID string    `json:"request_id"`
	CreatedAt time.Time `json:"created_at"`
}

type SharedRecord struct {
	SchemaVersion int            `json:"schema_version"`
	View          map[string]any `json:"view"`
	Article       map[string]any `json:"article"`
	Steps         []domain.Step  `json:"steps"`
}

// Only the random share id is stored. The URL is a separate, domain-separated
// capability; it never contains a visitor session token or a private cursor.
func (s *Store) shareToken(id string) string {
	h := hmac.New(sha256.New, s.signing)
	h.Write([]byte("request-share:" + id))
	return id + "." + base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

func (s *Store) shareID(token string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 || len(parts[0]) != 32 || !exampleArticleID.MatchString(parts[0]) || !hmac.Equal([]byte(token), []byte(s.shareToken(parts[0]))) {
		return "", shareNotFound()
	}
	return parts[0], nil
}

func shareNotFound() error {
	return domain.Err(404, "RESOURCE_NOT_FOUND", "分享链接不存在或已取消。")
}

func (s *Store) requestShare(ctx context.Context, q Querier, request string) (RequestShare, error) {
	out := RequestShare{SchemaVersion: 1}
	row, err := one[shareRow](ctx, q, "SELECT to_jsonb(s) FROM request_shares s WHERE request_id=$1", request)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, nil
	}
	if err == nil {
		out.Enabled = true
		out.URL = domain.Ptr("/s/" + s.shareToken(row.ID))
		out.CreatedAt = &row.CreatedAt
	}
	return out, err
}

func (s *Store) RequestShare(ctx context.Context, session, request string) (RequestShare, error) {
	var out RequestShare
	err := s.read(ctx, func(tx pgx.Tx) error {
		r, err := loadRequest(ctx, tx, request)
		if err != nil || r.SessionID != session {
			return domain.Err(404, "RESOURCE_NOT_FOUND", "资源不存在。")
		}
		out, err = s.requestShare(ctx, tx, request)
		return err
	})
	return out, err
}

func (s *Store) SetRequestShare(ctx context.Context, session, request string, enabled bool) (RequestShare, error) {
	var out RequestShare
	err := s.Tx(ctx, func(tx pgx.Tx) error {
		r, err := loadRequest(ctx, tx, request)
		if err != nil || r.SessionID != session {
			return domain.Err(404, "RESOURCE_NOT_FOUND", "资源不存在。")
		}
		if enabled {
			_, err = tx.Exec(ctx, "INSERT INTO request_shares(request_id,id) VALUES($1,$2) ON CONFLICT(request_id) DO NOTHING", request, domain.ID())
		} else {
			_, err = tx.Exec(ctx, "DELETE FROM request_shares WHERE request_id=$1", request)
		}
		if err != nil {
			return err
		}
		out, err = s.requestShare(ctx, tx, request)
		return err
	})
	return out, err
}

func (s *Store) sharedRequest(ctx context.Context, q Querier, token string) (domain.Request, error) {
	id, err := s.shareID(token)
	if err != nil {
		return domain.Request{}, err
	}
	row, err := one[domain.Request](ctx, q, "SELECT to_jsonb(r) FROM generation_requests r JOIN request_shares s ON s.request_id=r.id WHERE s.id=$1", id)
	if errors.Is(err, pgx.ErrNoRows) {
		return row, shareNotFound()
	}
	return row, err
}

// Retry history belongs to this record only when both the source and owner
// match. Never follow Q&A runs or another browser's request lineage.
func sharedSegments(ctx context.Context, q Querier, current domain.Request) ([]domain.ExecutionSegment, error) {
	chain := []domain.Request{current}
	seen := map[string]bool{current.ID: true}
	for len(chain) < 40 {
		last := chain[len(chain)-1]
		if last.RetryOfID == nil || seen[*last.RetryOfID] {
			break
		}
		prior, err := loadRequest(ctx, q, *last.RetryOfID)
		if err != nil {
			return nil, err
		}
		if prior.SessionID != current.SessionID || prior.SourceID != current.SourceID {
			break
		}
		chain = append(chain, prior)
		seen[prior.ID] = true
	}
	slices.Reverse(chain)
	segments := []domain.ExecutionSegment{}
	indices := map[string]int{}
	for _, req := range chain {
		for _, seg := range req.Segments {
			run, err := loadRun(ctx, q, seg.RunID)
			if err != nil {
				return nil, err
			}
			if run.Kind != "source.prepare" && run.Kind != "article.generate" {
				continue
			}
			if i, ok := indices[seg.RunID]; ok {
				// A later retry may see more of the same shared source run.
				segments[i].ThroughSeq = seg.ThroughSeq
				continue
			}
			seg.SegmentNo = len(segments)
			indices[seg.RunID] = len(segments)
			segments = append(segments, seg)
		}
	}
	return segments, nil
}

func sharedFrame(frame domain.Frame, token string) domain.Frame {
	frame.SnapshotID = ""
	frame.ReplacesFrameID = nil
	frame.URLs = map[string]*string{"full": nil, "thumbnail": nil}
	if frame.Availability == "ready" {
		path := "/api/v1/shares/" + url.PathEscape(token) + "/frames/" + url.PathEscape(frame.ID) + "/content"
		frame.URLs["full"] = &path
		frame.URLs["thumbnail"] = &path
	}
	return frame
}

func sharedSteps(steps []domain.Step, token string) []domain.Step {
	for i := range steps {
		steps[i].RunID = "attempt-" + strconv.Itoa(steps[i].SegmentNo)
		for j := range steps[i].Frames {
			steps[i].Frames[j] = sharedFrame(steps[i].Frames[j], token)
		}
	}
	return steps
}

func (s *Store) SharedRecord(ctx context.Context, token string) (SharedRecord, error) {
	out := SharedRecord{SchemaVersion: 1, Steps: []domain.Step{}}
	if _, err := s.shareID(token); err != nil {
		return out, err
	}
	err := s.read(ctx, func(tx pgx.Tx) error {
		r, err := s.sharedRequest(ctx, tx, token)
		if err != nil {
			return err
		}
		segments, err := sharedSegments(ctx, tx, r)
		if err != nil {
			return err
		}
		steps, history, expiry, err := stepsAt(ctx, tx, segments, r.ID)
		if err != nil {
			return err
		}
		out.Steps = sharedSteps(steps, token)
		src, err := source(ctx, tx, r)
		if err != nil {
			return err
		}
		src.SourceID, src.SnapshotID = "", nil
		var run *domain.Run
		if r.ActiveRunID != nil {
			v, err := loadRun(ctx, tx, *r.ActiveRunID)
			if err != nil {
				return err
			}
			v.ID = ""
			run = &v
		}
		var failure *domain.Failure
		if r.Failure != nil {
			failure = &domain.Failure{Code: r.Failure.Code, Message: "这次处理未能完成。"}
		}
		activity := activitySummary(out.Steps, history, expiry, "", !domain.Terminal(r.Status))
		activity["recent_steps"], activity["has_more"], activity["history_url"] = []domain.Step{}, false, ""
		request := map[string]any{"id": "", "revision": r.Revision, "input_url": src.CanonicalURL, "status": r.Status, "reuse": r.Reuse, "retry_of_id": nil, "article_id": r.ArticleID, "created_at": r.CreatedAt, "updated_at": r.UpdatedAt}
		out.View = map[string]any{"schema_version": 1, "server_now": time.Now().UTC(), "request": request, "source": src, "execution": execution(run), "activity": activity, "failure": failure, "actions": map[string]bool{"can_cancel": false, "can_retry": false, "can_read_article": r.ArticleID != nil}}
		if r.ArticleID != nil {
			a, err := article(ctx, tx, *r.ArticleID)
			if err != nil {
				return err
			}
			fs, err := frames(ctx, tx, r.ID, ContentFrameIDs(a.Sections, a.Citations))
			if err != nil {
				return err
			}
			for i := range fs {
				fs[i] = sharedFrame(fs[i], token)
			}
			out.Article = map[string]any{"schema_version": 1, "article": scrubArticle(a), "frames": fs, "media": publicExampleMedia{Status: "unavailable"}, "qa_available": false, "detail_level": a.DetailLevel}
			model, err := one[domain.GenerationModel](ctx, tx, "SELECT generation_model FROM articles WHERE id=$1", a.ID)
			if err != nil {
				return err
			}
			model.RevisionID, model.Description = "", ""
			out.Article["generation_model"] = model
		}
		if r.GenerationSpec != nil {
			model := r.GenerationSpec.Model
			model.RevisionID, model.Description = "", ""
			request["generation_model"], request["detail_level"] = model, r.GenerationSpec.DetailLevel
			if out.Article != nil {
				out.Article["generation_model"] = model
			}
		}
		return nil
	})
	return out, err
}

func (s *Store) SharedFrame(ctx context.Context, token, id string) (domain.Asset, error) {
	var asset domain.Asset
	if _, err := s.shareID(token); err != nil {
		return asset, err
	}
	err := s.read(ctx, func(tx pgx.Tx) error {
		r, err := s.sharedRequest(ctx, tx, token)
		if err != nil {
			return err
		}
		allowed := false
		if r.ArticleID != nil {
			a, err := article(ctx, tx, *r.ArticleID)
			if err != nil {
				return err
			}
			allowed = slices.Contains(ContentFrameIDs(a.Sections, a.Citations), id)
		}
		if !allowed {
			segments, err := sharedSegments(ctx, tx, r)
			if err != nil {
				return err
			}
			for _, segment := range segments {
				err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM (
					SELECT DISTINCT ON (step_id) step FROM run_events
					WHERE run_id=$1 AND step_id IS NOT NULL AND ($2::bigint IS NULL OR seq<=$2)
					ORDER BY step_id,seq DESC
				) latest WHERE step->'frame_ids' ? $3)`, segment.RunID, segment.ThroughSeq, id).Scan(&allowed)
				if err != nil {
					return err
				}
				if allowed {
					break
				}
			}
		}
		if !allowed {
			return shareNotFound()
		}
		asset, err = one[domain.Asset](ctx, tx, "SELECT to_jsonb(a) FROM media_assets a WHERE id=$1 AND kind='frame' AND state='ready' AND (expires_at IS NULL OR expires_at>now() OR EXISTS(SELECT 1 FROM asset_refs WHERE asset_id=a.id AND (expires_at IS NULL OR expires_at>now())))", id)
		if errors.Is(err, pgx.ErrNoRows) {
			return shareNotFound()
		}
		return err
	})
	return asset, err
}
