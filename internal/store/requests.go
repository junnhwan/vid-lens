package store

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"vidlens/internal/config"
	"vidlens/internal/domain"
)

type Accepted struct {
	ID       string
	Replayed bool
	Expires  time.Time
}

func replay(ctx context.Context, tx pgx.Tx, scope, key, hash string) (Accepted, bool, error) {
	var a Accepted
	var h string
	e := tx.QueryRow(ctx, "SELECT resource_id,hash,expires_at FROM idempotency WHERE scope=$1 AND key=$2", scope, key).Scan(&a.ID, &h, &a.Expires)
	if errors.Is(e, pgx.ErrNoRows) {
		return a, false, nil
	}
	if e != nil {
		return a, false, e
	}
	if !a.Expires.After(time.Now()) {
		return a, false, domain.Err(410, "IDEMPOTENCY_EXPIRED", "这次操作的幂等保证窗口已结束，请先读取已知资源。")
	}
	if h != hash {
		return a, false, domain.Err(409, "IDEMPOTENCY_CONFLICT", "同一操作键已使用不同内容。")
	}
	a.Replayed = true
	return a, true, nil
}
func remember(ctx context.Context, tx pgx.Tx, scope, key, hash, id string, seconds int64) (Accepted, error) {
	a := Accepted{ID: id}
	e := tx.QueryRow(ctx, "INSERT INTO idempotency(scope,key,hash,resource_id,expires_at) VALUES($1,$2,$3,$4,now()+$5*interval '1 second') RETURNING expires_at", scope, key, hash, id, seconds).Scan(&a.Expires)
	return a, e
}
func (s *Store) CreateRequest(ctx context.Context, session, key, input, retryID string, local bool) (Accepted, error) {
	var a Accepted
	e := s.Tx(ctx, func(tx pgx.Tx) error {
		input = strings.TrimSpace(input)
		scope := "request:" + session
		if retryID != "" {
			scope += "/retry/" + retryID
		}
		h := Hash(map[string]any{"video_url": input})
		if retryID != "" {
			h = Hash(map[string]any{})
		}
		var found bool
		var e error
		if a, found, e = replay(ctx, tx, scope, key, h); e != nil || found {
			return e
		}
		if retryID != "" {
			old, e := loadRequest(ctx, tx, retryID)
			if e != nil || old.SessionID != session {
				return domain.Err(404, "RESOURCE_NOT_FOUND", "资源不存在。")
			}
			if old.Status != "failed" && old.Status != "canceled" {
				return domain.Err(409, "RETRY_NOT_ALLOWED", "当前请求不能重新发起。")
			}
			if old.Failure != nil && !old.Failure.CanRetry {
				return domain.Err(409, "RETRY_NOT_ALLOWED", "请先修正来源或配置。")
			}
			input = old.InputURL
		}
		identity, e := domain.ParseSource(input, local)
		if e != nil {
			return e
		}
		var cid string
		if e = tx.QueryRow(ctx, "SELECT config_id FROM runtime_settings").Scan(&cid); e != nil {
			return e
		}
		p, e := one[config.Profile](ctx, tx, "SELECT profile FROM runtime_config_revisions WHERE id=$1", cid)
		if e != nil {
			return e
		}
		if p.Validate() != nil {
			return domain.Err(503, "SERVICE_UNAVAILABLE", "服务尚未配置可用模型。")
		}
		supported := identity.Platform == "local"
		for _, v := range p.Platforms {
			supported = supported || v == identity.Platform
		}
		if !supported {
			return domain.Err(422, "UNSUPPORTED_SOURCE", "暂不支持这个视频来源。")
		}
		var source string
		e = tx.QueryRow(ctx, "INSERT INTO video_sources(id,platform,canonical_id,part_key,canonical_url) VALUES($1,$2,$3,$4,$5) ON CONFLICT(platform,canonical_id,part_key) DO UPDATE SET canonical_url=EXCLUDED.canonical_url RETURNING id", domain.ID(), identity.Platform, identity.CanonicalID, identity.PartKey, identity.URL).Scan(&source)
		if e != nil {
			return e
		}
		work := source + ":" + cid
		var runID, status string
		e = tx.QueryRow(ctx, "SELECT id,status FROM job_runs WHERE kind='source.prepare' AND work_key=$1 AND status IN ('queued','running','retry_wait','waiting_dependency','cancel_requested')", work).Scan(&runID, &status)
		reuse := "pending"
		deadline := time.Now().Add(time.Duration(p.GenerationSeconds) * time.Second)
		if errors.Is(e, pgx.ErrNoRows) {
			var count int
			if e = tx.QueryRow(ctx, "SELECT count(*) FROM job_runs WHERE kind<>'qa.answer' AND status IN ('queued','running','retry_wait','waiting_dependency','cancel_requested')").Scan(&count); e != nil {
				return e
			}
			if count >= 3 {
				return domain.Err(429, "ADMISSION_LIMITED", "当前视频处理容量已满，请稍后再试。")
			}
			runID, e = newRun(ctx, tx, "source.prepare", work, source, cid, nil, deadline)
			if e != nil {
				return e
			}
		} else if e != nil {
			return e
		} else if status == "cancel_requested" {
			return domain.Err(429, "ADMISSION_LIMITED", "同一来源正在停止，请稍后再试。")
		}
		id := domain.ID()
		var retry *string
		if retryID != "" {
			retry = &retryID
		}
		segs := []domain.ExecutionSegment{{RunID: runID, SegmentNo: 0}}
		_, e = tx.Exec(ctx, "INSERT INTO generation_requests(id,session_id,input_url,source_id,active_run_id,config_id,status,reuse,retry_of_id,segments,deadline_at) VALUES($1,$2,$3,$4,$5,$6,'waiting_source',$7,$8,$9,$10)", id, session, input, source, runID, cid, reuse, retry, domain.JSON(segs), deadline)
		if e != nil {
			return e
		}
		a, e = remember(ctx, tx, scope, key, h, id, p.IdempotencySeconds)
		return e
	})
	return a, e
}
func (s *Store) CancelRequest(ctx context.Context, session, id string) error {
	return s.Tx(ctx, func(tx pgx.Tx) error {
		r, e := loadRequest(ctx, tx, id)
		if e != nil || r.SessionID != session {
			return domain.Err(404, "RESOURCE_NOT_FOUND", "资源不存在。")
		}
		if domain.Terminal(r.Status) {
			return nil
		}
		run := r.ActiveRunID
		for i := range r.Segments {
			if run != nil && r.Segments[i].RunID == *run {
				rr, e := loadRun(ctx, tx, *run)
				if e != nil {
					return e
				}
				r.Segments[i].ThroughSeq = &rr.LastEventSeq
			}
		}
		if _, e = tx.Exec(ctx, "UPDATE generation_requests SET status='canceled',active_run_id=NULL,segments=$2,revision=revision+1,updated_at=now() WHERE id=$1", id, domain.JSON(r.Segments)); e != nil {
			return e
		}
		if run != nil {
			return cancelUnused(ctx, tx, *run)
		}
		return nil
	})
}
func cancelUnused(ctx context.Context, tx pgx.Tx, id string) error {
	var needed bool
	e := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM generation_requests WHERE active_run_id=$1 AND status IN ('waiting_source','generating')) OR EXISTS(SELECT 1 FROM job_runs WHERE dependency_run_id=$1 AND status='waiting_dependency')", id).Scan(&needed)
	if e != nil || needed {
		return e
	}
	_, e = tx.Exec(ctx, "UPDATE job_runs SET status='cancel_requested',updated_at=now() WHERE id=$1 AND status NOT IN ('succeeded','failed','canceled')", id)
	if e == nil {
		e = event(ctx, tx, id, nil)
	}
	return e
}
func (s *Store) EnsureConversation(ctx context.Context, session, request string) (domain.Conversation, bool, error) {
	var v domain.Conversation
	created := false
	e := s.Tx(ctx, func(tx pgx.Tx) error {
		r, e := loadRequest(ctx, tx, request)
		if e != nil || r.SessionID != session {
			return domain.Err(404, "RESOURCE_NOT_FOUND", "资源不存在。")
		}
		if r.Status != "completed" || r.ArticleID == nil {
			return domain.Err(409, "ARTICLE_NOT_READY", "文章尚未发布。")
		}
		v, e = one[domain.Conversation](ctx, tx, "SELECT to_jsonb(c) FROM qa_conversations c WHERE session_id=$1 AND request_id=$2", session, request)
		if e == nil {
			return nil
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		created = true
		v, e = one[domain.Conversation](ctx, tx, "INSERT INTO qa_conversations(id,session_id,request_id,article_id,snapshot_id) VALUES($1,$2,$3,$4,$5) RETURNING to_jsonb(qa_conversations)", domain.ID(), session, request, *r.ArticleID, *r.SnapshotID)
		return e
	})
	return v, created, e
}
func (s *Store) Conversation(ctx context.Context, session, id string) (domain.Conversation, error) {
	v, e := one[domain.Conversation](ctx, s.Pool, "SELECT to_jsonb(c) FROM qa_conversations c WHERE id=$1 AND session_id=$2", id, session)
	return v, NotFound(e)
}
func sameID(a, b *string) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }
func (s *Store) CreateTurn(ctx context.Context, session, conversation, key string, in domain.QuestionInput, retry string) (Accepted, error) {
	var a Accepted
	e := s.Tx(ctx, func(tx pgx.Tx) error {
		c, e := one[domain.Conversation](ctx, tx, "SELECT to_jsonb(c) FROM qa_conversations c WHERE id=$1 AND session_id=$2", conversation, session)
		if e != nil {
			return NotFound(e)
		}
		in.Question = strings.TrimSpace(in.Question)
		if in.Context.Passage != nil {
			in.Context.Passage.Quote = domain.NormalizeQuote(in.Context.Passage.Quote)
		}
		scope := "turn:" + session + ":" + conversation
		if retry != "" {
			scope += "/retry/" + retry
		}
		h := Hash(in)
		if retry != "" {
			h = Hash(map[string]any{"expected_last_turn_id": in.ExpectedLastTurnID})
		}
		var found bool
		if a, found, e = replay(ctx, tx, scope, key, h); e != nil || found {
			return e
		}
		if !sameID(c.LastTurnID, in.ExpectedLastTurnID) {
			return domain.Err(409, "TURN_HISTORY_CHANGED", "对话已更新，请刷新后发送。")
		}
		if c.ActiveTurnID != nil {
			return &domain.Problem{Status: 409, Code: "TURN_IN_PROGRESS", Detail: "上一轮回答仍在进行。", Meta: map[string]any{"active_turn_id": *c.ActiveTurnID}}
		}
		if retry != "" {
			old, e := loadTurn(ctx, tx, retry)
			if e != nil || old.ConversationID != conversation {
				return domain.Err(404, "RESOURCE_NOT_FOUND", "资源不存在。")
			}
			if c.LastTurnID == nil || *c.LastTurnID != retry {
				return domain.Err(409, "TURN_HISTORY_CHANGED", "只能重新回答最后一轮。")
			}
			if old.Status != "failed" && old.Status != "canceled" || old.Failure != nil && !old.Failure.CanRetry {
				return domain.Err(409, "RETRY_NOT_ALLOWED", "当前回答不能重试。")
			}
			in.Question = old.Question
			in.Context = old.Context
		}
		var configID string
		if e = tx.QueryRow(ctx, "SELECT config_id FROM runtime_settings").Scan(&configID); e != nil {
			return e
		}
		p, e := one[config.Profile](ctx, tx, "SELECT profile FROM runtime_config_revisions WHERE id=$1", configID)
		if e != nil {
			return e
		}
		if p.Validate() != nil {
			return domain.Err(503, "SERVICE_UNAVAILABLE", "服务尚未配置可用模型。")
		}
		if utf8.RuneCountInString(in.Question) > p.QuestionCharsMax || strings.TrimSpace(in.Question) == "" {
			return domain.Err(422, "INPUT_LIMIT_EXCEEDED", "问题为空或超过长度限制。")
		}
		if in.Context.Passage != nil {
			if utf8.RuneCountInString(in.Context.Passage.Quote) > p.SelectionCharsMax || in.Context.Passage.Quote == "" {
				return domain.Err(422, "INVALID_CONTEXT", "选段为空或过长。")
			}
			art, e := article(ctx, tx, c.ArticleID)
			if e != nil {
				return e
			}
			found := false
			for _, sec := range art.Sections {
				if sec.ID != in.Context.Passage.SectionID {
					continue
				}
				for _, b := range sec.Blocks {
					if b.ID == in.Context.Passage.BlockID && strings.Contains(domain.NormalizeQuote(domain.BlockText(b)), in.Context.Passage.Quote) {
						found = true
					}
				}
			}
			if !found {
				return domain.Err(422, "INVALID_CONTEXT", "选段不属于这篇文章。")
			}
		}
		if in.Context.FrameID != nil {
			if e = visibleFrame(ctx, tx, session, c.RequestID, *in.Context.FrameID); e != nil {
				return domain.Err(422, "INVALID_CONTEXT", "无法引用这张图片。")
			}
		}
		var count int
		if e = tx.QueryRow(ctx, "SELECT count(*) FROM job_runs WHERE kind='qa.answer' AND status IN ('queued','running','waiting_dependency','retry_wait','cancel_requested')").Scan(&count); e != nil {
			return e
		}
		if count >= 5 {
			return domain.Err(429, "ADMISSION_LIMITED", "当前问答容量已满。")
		}
		id := domain.ID()
		source, e := one[domain.Snapshot](ctx, tx, "SELECT to_jsonb(s) FROM source_snapshots s WHERE id=$1", c.SnapshotID)
		if e != nil {
			return e
		}
		run, e := newRun(ctx, tx, "qa.answer", id, source.SourceID, configID, &c.SnapshotID, time.Now().Add(time.Duration(p.AnswerSeconds)*time.Second))
		if e != nil {
			return e
		}
		var retryPtr *string
		if retry != "" {
			retryPtr = &retry
		}
		_, e = tx.Exec(ctx, "INSERT INTO qa_turns(id,conversation_id,request_id,article_id,snapshot_id,run_id,turn_no,status,question,context,retry_of_id) VALUES($1,$2,$3,$4,$5,$6,$7,'queued',$8,$9,$10)", id, c.ID, c.RequestID, c.ArticleID, c.SnapshotID, run, c.NextTurnNo, in.Question, domain.JSON(in.Context), retryPtr)
		if e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, "UPDATE qa_conversations SET last_turn_id=$2,active_turn_id=$2,next_turn_no=next_turn_no+1 WHERE id=$1", c.ID, id); e != nil {
			return e
		}
		a, e = remember(ctx, tx, scope, key, h, id, p.IdempotencySeconds)
		return e
	})
	return a, e
}
func loadTurn(ctx context.Context, q Querier, id string) (domain.Turn, error) {
	return one[domain.Turn](ctx, q, "SELECT to_jsonb(t) FROM qa_turns t WHERE id=$1", id)
}
func (s *Store) Turn(ctx context.Context, session, conversation, id string) (domain.Turn, error) {
	v, e := one[domain.Turn](ctx, s.Pool, "SELECT to_jsonb(t) FROM qa_turns t JOIN qa_conversations c ON c.id=t.conversation_id WHERE t.id=$1 AND c.id=$2 AND c.session_id=$3", id, conversation, session)
	if e != nil {
		return v, NotFound(e)
	}
	if v.ExpiresAt != nil && !v.ExpiresAt.After(time.Now()) {
		return v, domain.Err(410, "RESOURCE_EXPIRED", "这轮私人问答已过期。")
	}
	return v, nil
}
func (s *Store) CancelTurn(ctx context.Context, session, conversation, id string) error {
	return s.Tx(ctx, func(tx pgx.Tx) error {
		c, e := one[domain.Conversation](ctx, tx, "SELECT to_jsonb(c) FROM qa_conversations c WHERE id=$1 AND session_id=$2", conversation, session)
		if e != nil {
			return NotFound(e)
		}
		t, e := loadTurn(ctx, tx, id)
		if e != nil || t.ConversationID != c.ID {
			return domain.Err(404, "RESOURCE_NOT_FOUND", "资源不存在。")
		}
		if domain.Terminal(t.Status) || t.Status == "cancel_requested" {
			return nil
		}
		if _, e = tx.Exec(ctx, "UPDATE qa_turns SET status='cancel_requested',revision=revision+1,updated_at=now() WHERE id=$1", id); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, "UPDATE job_runs SET status='cancel_requested',updated_at=now() WHERE id=$1", t.RunID); e != nil {
			return e
		}
		return event(ctx, tx, t.RunID, nil)
	})
}
