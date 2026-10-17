package store

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"vidlens/internal/domain"
)

type Position struct {
	Session       string                    `json:"s"`
	Type          string                    `json:"t"`
	ID            string                    `json:"i"`
	Revision      int64                     `json:"v"`
	RunID         *string                   `json:"r"`
	Seq           int64                     `json:"q"`
	Segments      []domain.ExecutionSegment `json:"g,omitempty"`
	BeforeSegment int                       `json:"b,omitempty"`
	BeforeSeq     int64                     `json:"c,omitempty"`
	Page          bool                      `json:"p,omitempty"`
}

func (s *Store) Cursor(p Position) string {
	b := domain.JSON(p)
	h := hmac.New(sha256.New, s.signing)
	h.Write(b)
	return base64.RawURLEncoding.EncodeToString(b) + "." + base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}
func (s *Store) ParseCursor(raw, session, typ, id string) (Position, error) {
	var p Position
	v := strings.Split(raw, ".")
	if len(v) != 2 || len(raw) > 16384 {
		return p, domain.Err(400, "INVALID_CURSOR", "游标格式无效。")
	}
	b, e := base64.RawURLEncoding.DecodeString(v[0])
	if e != nil {
		return p, domain.Err(400, "INVALID_CURSOR", "游标格式无效。")
	}
	sig, e := base64.RawURLEncoding.DecodeString(v[1])
	h := hmac.New(sha256.New, s.signing)
	h.Write(b)
	if e != nil || !hmac.Equal(sig, h.Sum(nil)) || json.Unmarshal(b, &p) != nil {
		return p, domain.Err(400, "INVALID_CURSOR", "游标格式无效。")
	}
	if p.Session != session || p.Type != typ || p.ID != id {
		return p, domain.Err(404, "RESOURCE_NOT_FOUND", "资源不存在。")
	}
	return p, nil
}
func (s *Store) read(ctx context.Context, f func(pgx.Tx) error) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, e := s.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return e
	}
	defer tx.Rollback(context.Background())
	if e = f(tx); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func source(ctx context.Context, q Querier, r domain.Request) (*domain.Source, error) {
	if r.SnapshotID != nil {
		snap, e := one[domain.Snapshot](ctx, q, "SELECT to_jsonb(s) FROM source_snapshots s WHERE id=$1", *r.SnapshotID)
		return &snap.Source, e
	}
	v, e := one[struct {
		ID       string `json:"id"`
		Platform string `json:"platform"`
		URL      string `json:"canonical_url"`
		Part     string `json:"part_key"`
	}](ctx, q, "SELECT to_jsonb(s) FROM video_sources s WHERE id=$1", r.SourceID)
	if e != nil {
		return nil, e
	}
	src := &domain.Source{SourceID: v.ID, Platform: v.Platform, CanonicalURL: v.URL}
	var info struct {
		Title      string `json:"title"`
		DurationMS int64  `json:"duration_ms"`
	}
	err := q.QueryRow(ctx, `SELECT result->>'title',(result->>'duration_ms')::bigint FROM run_operations WHERE operation_key='source-metadata' AND status='completed' AND run_id IN (SELECT e->>'run_id' FROM jsonb_array_elements($1::jsonb) e) ORDER BY updated_at DESC LIMIT 1`, domain.JSON(r.Segments)).Scan(&info.Title, &info.DurationMS)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if err == nil {
		src.Title = &info.Title
		src.DurationMS = &info.DurationMS
	}
	if v.Platform == "bilibili" {
		src.Part = &domain.Part{Key: v.Part, Label: "第 " + v.Part + " P"}
	}
	return src, nil
}
func execution(r *domain.Run) any {
	if r == nil {
		return nil
	}
	kind := r.Kind
	if kind == "qa.answer" {
		kind = "answer.generate"
	}
	var wait *string
	if r.Status == "queued" {
		wait = domain.Ptr("dispatch")
	}
	if r.Status == "waiting_dependency" {
		wait = domain.Ptr("media_restore")
	}
	return map[string]any{"id": r.ID, "kind": kind, "status": r.Status, "phase": r.Phase, "retry_at": r.RetryAt, "wait_reason": wait}
}
func article(ctx context.Context, q Querier, id string) (domain.Article, error) {
	return one[domain.Article](ctx, q, "SELECT content FROM articles WHERE id=$1", id)
}
func (s *Store) Article(ctx context.Context, session, requestID string) (map[string]any, error) {
	var out map[string]any
	e := s.read(ctx, func(tx pgx.Tx) error {
		r, e := loadRequest(ctx, tx, requestID)
		if e != nil || r.SessionID != session {
			return domain.Err(404, "RESOURCE_NOT_FOUND", "资源不存在。")
		}
		if r.ArticleID == nil {
			return domain.Err(409, "ARTICLE_NOT_READY", "文章尚未发布。")
		}
		a, e := article(ctx, tx, *r.ArticleID)
		if e != nil {
			return e
		}
		ids := ContentFrameIDs(a.Sections, a.Citations)
		frames, e := frames(ctx, tx, requestID, ids)
		if e != nil {
			return e
		}
		var ready bool
		if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM media_assets WHERE snapshot_id=$1 AND kind='video' AND state='ready' AND (expires_at IS NULL OR expires_at>now() OR EXISTS(SELECT 1 FROM asset_refs WHERE asset_id=media_assets.id AND (expires_at IS NULL OR expires_at>now()))))", a.SnapshotID).Scan(&ready); e != nil {
			return e
		}
		var restoring bool
		if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM job_runs WHERE snapshot_id=$1 AND kind='source.restore' AND status IN ('queued','running','retry_wait'))", a.SnapshotID).Scan(&restoring); e != nil {
			return e
		}
		status := "expired"
		if ready {
			status = "ready"
		} else if restoring {
			status = "restoring"
		}
		out = map[string]any{"schema_version": 1, "article": a, "frames": frames, "media": map[string]any{"status": status, "can_inspect_now": ready, "can_restore_for_question": true}}
		m, e := one[domain.GenerationModel](ctx, tx, "SELECT generation_model FROM articles WHERE id=$1", a.ID)
		if e != nil {
			return e
		}
		out["generation_model"] = m
		out["purpose"] = ""
		if r.GenerationSpec != nil {
			out["generation_model"] = r.GenerationSpec.Model
			out["purpose"] = r.GenerationSpec.Purpose
			out["detail_level"] = r.GenerationSpec.DetailLevel
			out["detail_policy_revision"] = r.GenerationSpec.DetailPolicyRevision
		}
		out["qa_available"] = checkAvailable(ctx, tx, r.ConfigID) == nil
		return nil
	})
	return out, e
}
func ContentFrameIDs(sections []domain.Section, citations map[string]domain.Citation) []string {
	ids := []string{}
	for _, sec := range sections {
		for _, b := range sec.Blocks {
			for _, id := range b.FrameIDs {
				if !slices.Contains(ids, id) {
					ids = append(ids, id)
				}
			}
		}
	}
	keys := make([]string, 0, len(citations))
	for id := range citations {
		keys = append(keys, id)
	}
	slices.Sort(keys)
	for _, key := range keys {
		c := citations[key]
		if c.FrameID != "" && !slices.Contains(ids, c.FrameID) {
			ids = append(ids, c.FrameID)
		}
	}
	return ids
}
func assetFrame(a domain.Asset, request string, pinned bool) domain.Frame {
	f := domain.Frame{ID: a.ID, Width: a.Width, Height: a.Height, MIMEType: a.MIME, Caption: a.Caption, Crop: a.Crop, ReplacesFrameID: a.ReplacesFrameID, Availability: "ready", ExpiresAt: a.ExpiresAt, URLs: map[string]*string{"full": nil, "thumbnail": nil}}
	if a.SnapshotID != nil {
		f.SnapshotID = *a.SnapshotID
	}
	if a.TimeMS != nil {
		f.TimeMS = *a.TimeMS
	}
	if a.RequestedTimeMS != nil {
		f.RequestedTimeMS = *a.RequestedTimeMS
	}
	if pinned {
		f.ExpiresAt = nil
	}
	if a.State == "deleted" || a.State == "deleting" || !pinned && a.ExpiresAt != nil && !a.ExpiresAt.After(time.Now()) {
		f.Availability = "expired"
		f.UnavailableReason = domain.Ptr("expired")
	} else if a.State != "ready" {
		f.Availability = "unavailable"
		f.UnavailableReason = domain.Ptr("temporarily_unavailable")
	}
	if f.Availability == "ready" {
		base := "/api/v1/requests/" + url.PathEscape(request) + "/frames/" + url.PathEscape(a.ID) + "/content?variant="
		f.URLs["full"] = domain.Ptr(base + "full")
		f.URLs["thumbnail"] = domain.Ptr(base + "thumbnail")
	}
	return f
}
func frames(ctx context.Context, q Querier, request string, ids []string) ([]domain.Frame, error) {
	out := []domain.Frame{}
	for _, id := range ids {
		a, e := one[domain.Asset](ctx, q, "SELECT to_jsonb(a) FROM media_assets a WHERE id=$1 AND kind='frame'", id)
		if e != nil {
			return nil, e
		}
		var pinned bool
		if e = q.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM asset_refs WHERE asset_id=$1 AND (expires_at IS NULL OR expires_at>now()))", id).Scan(&pinned); e != nil {
			return nil, e
		}
		out = append(out, assetFrame(a, request, pinned))
	}
	return out, nil
}
func visibleFrame(ctx context.Context, q Querier, session, request, frame string) error {
	r, e := loadRequest(ctx, q, request)
	if e != nil || r.SessionID != session {
		return domain.Err(404, "RESOURCE_NOT_FOUND", "资源不存在。")
	}
	var snapshot *string
	if e = q.QueryRow(ctx, "SELECT snapshot_id FROM media_assets WHERE id=$1 AND kind='frame'", frame).Scan(&snapshot); e != nil || !sameID(snapshot, r.SnapshotID) {
		return domain.Err(404, "RESOURCE_NOT_FOUND", "资源不存在。")
	}
	if r.ArticleID != nil {
		a, e := article(ctx, q, *r.ArticleID)
		if e != nil {
			return e
		}
		if slices.Contains(ContentFrameIDs(a.Sections, a.Citations), frame) {
			return nil
		}
	}
	for _, seg := range r.Segments {
		var ok bool
		e = q.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM run_events WHERE run_id=$1 AND ($2::bigint IS NULL OR seq<=$2) AND step->'frame_ids' ? $3)", seg.RunID, seg.ThroughSeq, frame).Scan(&ok)
		if e != nil {
			return e
		}
		if ok {
			return nil
		}
	}
	var own bool
	e = q.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM qa_turns t JOIN qa_conversations c ON c.id=t.conversation_id JOIN asset_refs ar ON (ar.owner_id=t.run_id AND ar.owner_type='run') OR (ar.owner_id=t.id AND ar.owner_type='answer') WHERE c.session_id=$1 AND c.request_id=$2 AND ar.asset_id=$3 AND (t.expires_at IS NULL OR t.expires_at>now()))", session, request, frame).Scan(&own)
	if e != nil {
		return e
	}
	if own {
		return nil
	}
	if e = q.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM qa_turns t JOIN qa_conversations c ON c.id=t.conversation_id JOIN run_events e ON e.run_id=t.run_id WHERE c.session_id=$1 AND c.request_id=$2 AND e.step->'frame_ids' ? $3 AND (t.expires_at IS NULL OR t.expires_at>now()))", session, request, frame).Scan(&own); e != nil {
		return e
	}
	if own {
		return nil
	}
	return domain.Err(404, "RESOURCE_NOT_FOUND", "资源不存在。")
}
func (s *Store) Frame(ctx context.Context, session, request, id string) (domain.Frame, domain.Asset, error) {
	var f domain.Frame
	var a domain.Asset
	e := s.read(ctx, func(tx pgx.Tx) error {
		if e := visibleFrame(ctx, tx, session, request, id); e != nil {
			return e
		}
		var e error
		a, e = one[domain.Asset](ctx, tx, "SELECT to_jsonb(a) FROM media_assets a WHERE id=$1", id)
		if e != nil {
			return e
		}
		ff, e := frames(ctx, tx, request, []string{id})
		if e != nil {
			return e
		}
		f = ff[0]
		return nil
	})
	return f, a, e
}
func stepsAt(ctx context.Context, q Querier, segs []domain.ExecutionSegment, request string) ([]domain.Step, string, *time.Time, error) {
	all := []domain.Step{}
	expected, retained := 0, 0
	var expiry *time.Time
	for _, seg := range segs {
		var had bool
		var status string
		var updated time.Time
		if e := q.QueryRow(ctx, "SELECT had_activity,status,updated_at FROM job_runs WHERE id=$1", seg.RunID).Scan(&had, &status, &updated); e != nil {
			return nil, "", nil, e
		}
		if had {
			expected++
		}
		if had && domain.Terminal(status) {
			ex := updated.Add(7 * 24 * time.Hour)
			if expiry == nil || ex.Before(*expiry) {
				expiry = &ex
			}
		}
		list, e := many[domain.Step](ctx, q, "SELECT step FROM (SELECT DISTINCT ON (step_id) step,seq FROM run_events WHERE run_id=$1 AND step_id IS NOT NULL AND ($2::bigint IS NULL OR seq<=$2) ORDER BY step_id,seq DESC) x ORDER BY (step->>'first_seq')::bigint", seg.RunID, seg.ThroughSeq)
		if e != nil {
			return nil, "", nil, e
		}
		if len(list) > 0 {
			retained++
		}
		for i := range list {
			list[i].SegmentNo = seg.SegmentNo
			fs, e := frames(ctx, q, request, list[i].FrameIDs)
			if e != nil {
				return nil, "", nil, e
			}
			list[i].Frames = fs
		}
		all = append(all, list...)
	}
	history := "none"
	if expected > 0 {
		history = "available"
		if retained == 0 {
			history = "expired"
		} else if retained < expected {
			history = "partial"
		}
	}
	return all, history, expiry, nil
}
func activitySummary(steps []domain.Step, history string, expiry *time.Time, base string, active bool) map[string]any {
	var current *string
	if active {
		for i := len(steps) - 1; i >= 0; i-- {
			if steps[i].Status == "running" && steps[i].Kind != "model_submission" {
				current = &steps[i].ID
				break
			}
		}
	}
	n := len(steps)
	recent := steps
	if n > 8 {
		recent = steps[n-8:]
		if current != nil {
			for _, x := range steps[:n-8] {
				if x.ID == *current {
					recent = append([]domain.Step{x}, recent...)
					break
				}
			}
		}
	}
	modelCalls, submissions := 0, 0
	var plan *domain.ExecutionPlan
	for _, step := range steps {
		if step.Kind == "model_response" {
			modelCalls++
		}
		if step.Kind == "model_submission" {
			submissions++
		}
		if step.Plan != nil {
			plan = step.Plan
		}
	}
	var sends any
	if submissions > 0 {
		sends = submissions
	}
	return map[string]any{"current_step_id": current, "recent_steps": recent, "has_more": len(recent) < n, "history_url": base + "/activity", "history_status": history, "history_expires_at": expiry, "model_calls": modelCalls, "model_submissions": sends, "plan": plan}
}
func (s *Store) requestView(ctx context.Context, q Querier, session, id string) (map[string]any, Position, error) {
	var pos Position
	r, e := loadRequest(ctx, q, id)
	if e != nil || r.SessionID != session {
		return nil, pos, domain.Err(404, "RESOURCE_NOT_FOUND", "资源不存在。")
	}
	src, e := source(ctx, q, r)
	if e != nil {
		return nil, pos, e
	}
	var run *domain.Run
	if r.ActiveRunID != nil {
		v, e := loadRun(ctx, q, *r.ActiveRunID)
		if e != nil {
			return nil, pos, e
		}
		run = &v
	}
	base := "/api/v1/requests/" + id
	pos = Position{Session: session, Type: "request", ID: id, Revision: r.Revision, RunID: r.ActiveRunID}
	if run != nil {
		pos.Seq = run.LastEventSeq
	}
	steps, h, ex, e := stepsAt(ctx, q, r.Segments, id)
	if e != nil {
		return nil, pos, e
	}
	canRetry := r.Status == "canceled" || r.Status == "failed" && domain.RetryableFailure(r.Failure)
	view := map[string]any{"server_now": time.Now().UTC(), "schema_version": 1, "request": map[string]any{"id": r.ID, "revision": r.Revision, "input_url": r.InputURL, "status": r.Status, "reuse": r.Reuse, "retry_of_id": r.RetryOfID, "article_id": r.ArticleID, "created_at": r.CreatedAt, "updated_at": r.UpdatedAt}, "source": src, "execution": execution(run), "activity": activitySummary(steps, h, ex, base, !domain.Terminal(r.Status)), "failure": r.Failure, "actions": map[string]bool{"can_cancel": !domain.Terminal(r.Status), "can_retry": canRetry, "can_read_article": r.ArticleID != nil}, "stream": s.stream(pos, base)}
	attempts, e := requestAttempts(ctx, q, r)
	if e != nil {
		return nil, pos, e
	}
	activity := view["activity"].(map[string]any)
	activity["attempts"] = attempts
	totalCalls, totalSends := 0, 0
	for _, attempt := range attempts {
		totalCalls += attempt["model_calls"].(int)
		totalSends += attempt["model_submissions"].(int)
	}
	activity["total_model_calls"] = totalCalls
	activity["total_model_submissions"] = totalSends
	if r.GenerationSpec != nil {
		v := view["request"].(map[string]any)
		v["purpose"] = r.GenerationSpec.Purpose
		v["generation_model"] = r.GenerationSpec.Model
		v["detail_level"] = r.GenerationSpec.DetailLevel
		v["detail_policy_revision"] = r.GenerationSpec.DetailPolicyRevision
	}
	return view, pos, nil
}
func (s *Store) stream(p Position, base string) map[string]any {
	return map[string]any{"cursor": s.Cursor(p), "revision": p.Revision, "run_id": p.RunID, "seq": p.Seq, "url": base + "/events"}
}
func (s *Store) RequestView(ctx context.Context, session, id string) (map[string]any, Position, error) {
	var v map[string]any
	var p Position
	e := s.read(ctx, func(tx pgx.Tx) error { var e error; v, p, e = s.requestView(ctx, tx, session, id); return e })
	return v, p, e
}
func (s *Store) turnView(ctx context.Context, q Querier, session, conversation, id string) (map[string]any, Position, error) {
	var pos Position
	c, e := one[domain.Conversation](ctx, q, "SELECT to_jsonb(c) FROM qa_conversations c WHERE id=$1 AND session_id=$2", conversation, session)
	if e != nil {
		return nil, pos, NotFound(e)
	}
	t, e := loadTurn(ctx, q, id)
	if e != nil || t.ConversationID != c.ID {
		return nil, pos, domain.Err(404, "RESOURCE_NOT_FOUND", "资源不存在。")
	}
	if t.ExpiresAt != nil && !t.ExpiresAt.After(time.Now()) {
		return nil, pos, domain.Err(410, "RESOURCE_EXPIRED", "这轮私人问答已过期。")
	}
	r, e := loadRun(ctx, q, t.RunID)
	if e != nil {
		return nil, pos, e
	}
	base := "/api/v1/conversations/" + conversation + "/turns/" + id
	pos = Position{Session: session, Type: "turn", ID: id, Revision: t.Revision, RunID: &t.RunID, Seq: r.LastEventSeq}
	steps, h, ex, e := stepsAt(ctx, q, []domain.ExecutionSegment{{RunID: r.ID}}, t.RequestID)
	if e != nil {
		return nil, pos, e
	}
	if t.Answer != nil {
		ids := []string{}
		for _, f := range t.Answer.Frames {
			ids = append(ids, f.ID)
		}
		t.Answer.Frames, e = frames(ctx, q, t.RequestID, ids)
		if e != nil {
			return nil, pos, e
		}
	}
	b := domain.JSON(t)
	var turn map[string]any
	_ = json.Unmarshal(b, &turn)
	delete(turn, "run_id")
	delete(turn, "failure")
	delete(turn, "expires_at")
	latest := c.LastTurnID != nil && *c.LastTurnID == id
	canRetry := latest && (t.Status == "canceled" || t.Status == "failed" && t.Failure != nil && t.Failure.CanRetry)
	v := map[string]any{"server_now": time.Now().UTC(), "schema_version": 1, "turn": turn, "execution": execution(&r), "activity": activitySummary(steps, h, ex, base, !domain.Terminal(t.Status)), "failure": t.Failure, "actions": map[string]bool{"can_cancel": !domain.Terminal(t.Status) && t.Status != "cancel_requested", "can_retry": canRetry, "can_submit_next": latest && domain.Terminal(t.Status)}, "stream": s.stream(pos, base)}
	return v, pos, nil
}
func (s *Store) TurnView(ctx context.Context, session, conversation, id string) (map[string]any, Position, error) {
	var v map[string]any
	var p Position
	e := s.read(ctx, func(tx pgx.Tx) error { var e error; v, p, e = s.turnView(ctx, tx, session, conversation, id); return e })
	return v, p, e
}
func (s *Store) TurnPage(ctx context.Context, session, conversation string, before int64, limit int) (map[string]any, error) {
	items := []map[string]any{}
	var next *int64
	has := false
	e := s.read(ctx, func(tx pgx.Tx) error {
		var valid bool
		if e := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM qa_conversations WHERE id=$1 AND session_id=$2)", conversation, session).Scan(&valid); e != nil {
			return e
		}
		if !valid {
			return domain.Err(404, "RESOURCE_NOT_FOUND", "资源不存在。")
		}
		ts, e := many[domain.Turn](ctx, tx, "SELECT to_jsonb(t) FROM qa_turns t WHERE conversation_id=$1 AND ($2=0 OR turn_no<$2) AND (expires_at IS NULL OR expires_at>now()) ORDER BY turn_no DESC LIMIT $3", conversation, before, limit+1)
		if e != nil {
			return e
		}
		if len(ts) > limit {
			has = true
			ts = ts[:limit]
		}
		slices.Reverse(ts)
		for _, t := range ts {
			v, _, e := s.turnView(ctx, tx, session, conversation, t.ID)
			if e != nil {
				return e
			}
			items = append(items, v)
		}
		if has && len(ts) > 0 {
			next = &ts[0].TurnNo
		}
		return nil
	})
	return map[string]any{"schema_version": 1, "items": items, "has_more": has, "next_before_turn_no": next}, e
}
func (s *Store) ActivityPage(ctx context.Context, session, typ, id, conversation, before string, limit int) (map[string]any, error) {
	var out map[string]any
	e := s.read(ctx, func(tx pgx.Tx) error {
		var segs []domain.ExecutionSegment
		var pos Position
		var req string
		var base string
		if typ == "request" {
			r, e := loadRequest(ctx, tx, id)
			if e != nil || r.SessionID != session {
				return domain.Err(404, "RESOURCE_NOT_FOUND", "资源不存在。")
			}
			segs = r.Segments
			req = id
			base = "/api/v1/requests/" + id
			_, pos, e = s.requestView(ctx, tx, session, id)
			if e != nil {
				return e
			}
		} else {
			v, e := loadTurn(ctx, tx, id)
			if e != nil {
				return NotFound(e)
			}
			var err error
			_, pos, err = s.turnView(ctx, tx, session, conversation, id)
			if err != nil {
				return err
			}
			req = v.RequestID
			segs = []domain.ExecutionSegment{{RunID: v.RunID}}
			base = "/api/v1/conversations/" + conversation + "/turns/" + id
		}
		for i := range segs {
			if segs[i].ThroughSeq == nil {
				r, e := loadRun(ctx, tx, segs[i].RunID)
				if e != nil {
					return e
				}
				segs[i].ThroughSeq = &r.LastEventSeq
			}
		}
		var prior *Position
		if before != "" {
			p, e := s.ParseCursor(before, session, typ, id)
			if e != nil {
				return e
			}
			if !p.Page {
				return domain.Err(400, "INVALID_CURSOR", "游标类型无效。")
			}
			if p.Revision != pos.Revision {
				return domain.Err(410, "ACTIVITY_CURSOR_EXPIRED", "活动绑定已变化，请重新读取第一页。")
			}
			prior = &p
			segs = p.Segments
		}
		all, h, ex, e := stepsAt(ctx, tx, segs, req)
		if e != nil {
			return e
		}
		if prior != nil && (h == "expired" || h == "partial") {
			return &domain.Problem{Status: 410, Code: "ACTIVITY_CURSOR_EXPIRED", Detail: "活动记录已过期，请重新读取第一页。", Meta: map[string]any{"restart_url": base + "/activity"}}
		}
		filtered := []domain.Step{}
		for _, x := range all {
			if prior == nil || x.SegmentNo < prior.BeforeSegment || x.SegmentNo == prior.BeforeSegment && x.FirstSeq < prior.BeforeSeq {
				filtered = append(filtered, x)
			}
		}
		has := len(filtered) > limit
		if has {
			filtered = filtered[len(filtered)-limit:]
		}
		var next *string
		if has {
			p := pos
			p.Page = true
			p.Segments = segs
			p.BeforeSegment = filtered[0].SegmentNo
			p.BeforeSeq = filtered[0].FirstSeq
			next = domain.Ptr(s.Cursor(p))
		}
		out = map[string]any{"schema_version": 1, "items": filtered, "has_more": has, "next_before": next, "through_cursor": s.Cursor(pos), "history_status": h, "history_expires_at": ex}
		return nil
	})
	return out, e
}
func (s *Store) CursorAvailable(ctx context.Context, p Position) (bool, error) {
	if p.RunID == nil {
		return true, nil
	}
	var min *int64
	e := s.Pool.QueryRow(ctx, "SELECT min(seq) FROM run_events WHERE run_id=$1", *p.RunID).Scan(&min)
	if e != nil {
		return false, e
	}
	return min != nil && p.Seq >= *min-1, nil
}
func (s *Store) ProjectionStep(ctx context.Context, session, request, run string, seq int64, segment int) (*domain.Step, error) {
	step, e := one[domain.Step](ctx, s.Pool, "SELECT step FROM run_events WHERE run_id=$1 AND seq=$2 AND step_id IS NOT NULL", run, seq)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	step.SegmentNo = segment
	step.Frames, e = frames(ctx, s.Pool, request, step.FrameIDs)
	return &step, e
}
func FormatPosition(p Position) string { return fmt.Sprintf("%d:%v:%d", p.Revision, p.RunID, p.Seq) }
