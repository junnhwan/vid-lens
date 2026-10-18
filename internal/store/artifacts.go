package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"vidlens/internal/config"
	"vidlens/internal/domain"
)

func (s *Store) Snapshot(ctx context.Context, id string) (domain.Snapshot, error) {
	return one[domain.Snapshot](ctx, s.Pool, "SELECT to_jsonb(s) FROM source_snapshots s WHERE id=$1", id)
}
func (s *Store) SourceIdentity(ctx context.Context, id string) (domain.SourceIdentity, error) {
	return one[domain.SourceIdentity](ctx, s.Pool, "SELECT jsonb_build_object('Platform',platform,'CanonicalID',canonical_id,'PartKey',part_key,'URL',canonical_url) FROM video_sources WHERE id=$1", id)
}
func (s *Store) Transcript(ctx context.Context, snapshot string) ([]domain.Segment, error) {
	return many[domain.Segment](ctx, s.Pool, "SELECT to_jsonb(t) FROM transcript_segments t WHERE snapshot_id=$1 ORDER BY ordinal", snapshot)
}

// The caller must verify that the fingerprint includes the ASR audio input.
// Reuse only completed ASR for the same source, fully reverified media bytes,
// duration and frozen provider configuration. Titles/durations alone are insufficient.
func (s *Store) ReusableASR(ctx context.Context, source, videoHash, pipeline string, duration int64, p config.Profile) (domain.Snapshot, bool, error) {
	v, e := one[domain.Snapshot](ctx, s.Pool, `SELECT to_jsonb(s) FROM source_snapshots s
 WHERE s.source_id=$1 AND s.video_hash=$2 AND s.duration_ms=$3
 AND s.source->'text_provenance'->>'origin'='asr'
 AND COALESCE(s.metadata->>'asr_pipeline_version','60s-mp3-48k16k-v1')=$4
 AND EXISTS(SELECT 1 FROM job_runs r JOIN runtime_config_revisions c ON c.id=r.config_id
   WHERE r.kind='source.prepare' AND r.status='succeeded' AND r.result_id=s.id
   AND c.profile->>'asr_base_url'=$5 AND c.profile->>'asr_model'=$6)
 ORDER BY s.created_at DESC LIMIT 1`, source, videoHash, duration, pipeline, p.ASRBaseURL, p.ASRModel)
	if errors.Is(e, pgx.ErrNoRows) {
		return v, false, nil
	}
	return v, e == nil, e
}
func (s *Store) Asset(ctx context.Context, id string) (domain.Asset, error) {
	return one[domain.Asset](ctx, s.Pool, "SELECT to_jsonb(a) FROM media_assets a WHERE id=$1", id)
}
func (s *Store) Video(ctx context.Context, snapshot string) (domain.Asset, error) {
	return one[domain.Asset](ctx, s.Pool, "SELECT to_jsonb(a) FROM media_assets a WHERE snapshot_id=$1 AND kind='video' AND state='ready' AND (expires_at IS NULL OR expires_at>now() OR EXISTS(SELECT 1 FROM asset_refs WHERE asset_id=a.id AND (expires_at IS NULL OR expires_at>now()))) ORDER BY created_at DESC LIMIT 1", snapshot)
}
func (s *Store) AddAsset(ctx context.Context, c domain.Claim, a domain.Asset) error {
	return s.Tx(ctx, func(tx pgx.Tx) error {
		if e := fence(ctx, tx, c); e != nil {
			return e
		}
		if a.RunID != c.Run.ID || a.SourceID != c.Run.SourceID {
			return errors.New("asset scope mismatch")
		}
		if c.Run.SnapshotID != nil && !sameID(a.SnapshotID, c.Run.SnapshotID) {
			return errors.New("asset snapshot mismatch")
		}
		if a.Kind == "video" || a.Kind == "audio" {
			a.ExpiresAt = nil
		}
		if _, e := tx.Exec(ctx, "INSERT INTO media_assets(id,snapshot_id,source_id,run_id,kind,storage_key,sha256,mime,bytes,time_ms,requested_time_ms,width,height,caption,crop,replaces_frame_id,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)", a.ID, a.SnapshotID, a.SourceID, a.RunID, a.Kind, a.StorageKey, a.SHA256, a.MIME, a.Bytes, a.TimeMS, a.RequestedTimeMS, a.Width, a.Height, a.Caption, nullableCrop(a.Crop), a.ReplacesFrameID, a.ExpiresAt); e != nil {
			return e
		}
		_, e := tx.Exec(ctx, "INSERT INTO asset_refs(asset_id,owner_type,owner_id,expires_at) VALUES($1,'run',$2,$3)", a.ID, c.Run.ID, c.Run.DeadlineAt.Add(5*time.Minute))
		return e
	})
}
func nullableCrop(v *domain.Crop) any {
	if v == nil {
		return nil
	}
	return domain.JSON(v)
}
func (s *Store) Pin(ctx context.Context, c domain.Claim, asset string) error {
	return s.Tx(ctx, func(tx pgx.Tx) error {
		if e := fence(ctx, tx, c); e != nil {
			return e
		}
		a, e := one[domain.Asset](ctx, tx, "SELECT to_jsonb(a) FROM media_assets a WHERE id=$1 FOR UPDATE", asset)
		if e != nil {
			return e
		}
		if a.State != "ready" {
			return errors.New("asset not ready")
		}
		if !sameID(a.SnapshotID, c.Run.SnapshotID) {
			return errors.New("asset scope mismatch")
		}
		_, e = tx.Exec(ctx, "INSERT INTO asset_refs(asset_id,owner_type,owner_id,expires_at) VALUES($1,'run',$2,$3) ON CONFLICT(asset_id,owner_type,owner_id) DO UPDATE SET expires_at=EXCLUDED.expires_at", asset, c.Run.ID, c.Run.DeadlineAt.Add(5*time.Minute))
		return e
	})
}
func (s *Store) TouchVideo(ctx context.Context, c domain.Claim, id string) error {
	return s.Pin(ctx, c, id)
}
func (s *Store) FinishSource(ctx context.Context, c domain.Claim, snap domain.Snapshot, segments []domain.Segment, videoID string) (string, error) {
	id := snap.ID
	e := s.Tx(ctx, func(tx pgx.Tx) error {
		if e := fence(ctx, tx, c); e != nil {
			return e
		}
		if c.Run.Kind == "source.restore" {
			old, e := one[domain.Snapshot](ctx, tx, "SELECT to_jsonb(s) FROM source_snapshots s WHERE id=$1", *c.Run.SnapshotID)
			if e != nil {
				return e
			}
			if old.VideoHash != snap.VideoHash {
				return domain.Err(422, "MEDIA_UNAVAILABLE", "原视频内容已变化，无法作为同一来源恢复。")
			}
			id = old.ID
		} else {
			var exists bool
			if e := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM source_snapshots WHERE source_id=$1 AND video_hash=$2 AND text_hash=$3)", snap.SourceID, snap.VideoHash, snap.TextHash).Scan(&exists); e != nil {
				return e
			}
			if exists {
				if e := tx.QueryRow(ctx, "SELECT id FROM source_snapshots WHERE source_id=$1 AND video_hash=$2 AND text_hash=$3", snap.SourceID, snap.VideoHash, snap.TextHash).Scan(&id); e != nil {
					return e
				}
			} else {
				snap.Source.SnapshotID = &id
				if _, e := tx.Exec(ctx, "INSERT INTO source_snapshots(id,source_id,video_hash,text_hash,duration_ms,source,metadata) VALUES($1,$2,$3,$4,$5,$6,$7)", id, snap.SourceID, snap.VideoHash, snap.TextHash, snap.DurationMS, domain.JSON(snap.Source), snap.Metadata); e != nil {
					return e
				}
				for _, t := range segments {
					if _, e := tx.Exec(ctx, "INSERT INTO transcript_segments(id,snapshot_id,ordinal,start_ms,end_ms,text,origin) VALUES($1,$2,$3,$4,$5,$6,$7)", id+"_"+stringOrdinal(t.Ordinal), id, t.Ordinal, t.StartMS, t.EndMS, t.Text, t.Origin); e != nil {
						return e
					}
				}
			}
		}
		if _, e := tx.Exec(ctx, "UPDATE media_assets SET snapshot_id=$2 WHERE id=$1 AND run_id=$3 AND state='ready'", videoID, id, c.Run.ID); e != nil {
			return e
		}
		if e := finishRun(ctx, tx, c, id); e != nil {
			return e
		}
		if _, e := tx.Exec(ctx, "UPDATE job_runs SET snapshot_id=$2 WHERE id=$1", c.Run.ID, id); e != nil {
			return e
		}
		if c.Run.Kind == "source.restore" {
			return nil
		}
		return bindPrepared(ctx, tx, c.Run.ID, id)
	})
	return id, e
}
func stringOrdinal(n int) string { b, _ := json.Marshal(n); return string(b) }
func finishRun(ctx context.Context, tx pgx.Tx, c domain.Claim, result string) error {
	if e := charge(ctx, tx, c.Run.ID); e != nil {
		return e
	}
	if _, e := tx.Exec(ctx, "UPDATE job_runs SET status='succeeded',result_id=$2,lease_owner=NULL,lease_expires_at=NULL,updated_at=now() WHERE id=$1", c.Run.ID, result); e != nil {
		return e
	}
	if _, e := tx.Exec(ctx, "UPDATE job_attempts SET ended_at=now(),outcome='succeeded' WHERE id=$1", c.AttemptID); e != nil {
		return e
	}
	if _, e := tx.Exec(ctx, "DELETE FROM asset_refs WHERE owner_type='run' AND owner_id=$1", c.Run.ID); e != nil {
		return e
	}
	if _, e := tx.Exec(ctx, "DELETE FROM storage_reservations WHERE run_id=$1", c.Run.ID); e != nil {
		return e
	}
	return event(ctx, tx, c.Run.ID, nil)
}
func bindPrepared(ctx context.Context, tx pgx.Tx, prepare, snapshot string) error {
	rs, e := many[domain.Request](ctx, tx, "SELECT to_jsonb(r) FROM generation_requests r WHERE active_run_id=$1 AND status='waiting_source'", prepare)
	if e != nil {
		return e
	}
	for _, r := range rs {
		work := snapshot + ":" + r.ConfigID
		if r.GenerationSpec != nil {
			work = "article-v2:" + snapshot + ":" + Hash(r.GenerationSpec)
			if r.GenerationSpec.Version >= 2 {
				work = "article-v3:" + snapshot + ":" + Hash(r.GenerationSpec)
			}
		}
		var aid string
		e = tx.QueryRow(ctx, "SELECT id FROM articles a WHERE reuse_key=$1 AND NOT EXISTS(SELECT 1 FROM asset_refs ar JOIN media_assets m ON m.id=ar.asset_id WHERE ar.owner_type='article' AND ar.owner_id=a.id AND m.state<>'ready') ORDER BY published_at DESC LIMIT 1", work).Scan(&aid)
		var active *string
		reuse := "none"
		status := "generating"
		var articleID *string
		if e == nil {
			status = "completed"
			reuse = "published_article"
			articleID = &aid
		} else if !errors.Is(e, pgx.ErrNoRows) {
			return e
		} else {
			var rid, st string
			e = tx.QueryRow(ctx, "SELECT id,status FROM job_runs WHERE kind='article.generate' AND work_key=$1 AND status IN ('queued','running','retry_wait','waiting_dependency','cancel_requested')", work).Scan(&rid, &st)
			if errors.Is(e, pgx.ErrNoRows) {
				rid, e = newRun(ctx, tx, "article.generate", work, r.SourceID, r.ConfigID, &snapshot, r.DeadlineAt)
				if e != nil {
					return e
				}
			} else if e != nil {
				return e
			} else if st == "cancel_requested" {
				return errors.New("matching generation is stopping")
			} else {
				reuse = "active_work"
			}
			if reuse == "none" && r.RetryOfID != nil {
				old, err := loadRequest(ctx, tx, *r.RetryOfID)
				if err != nil {
					return err
				}
				if old.SessionID != r.SessionID {
					return domain.ErrFenced
				}
				if err = reuseArticleDraft(ctx, tx, old, rid, snapshot); err != nil {
					return err
				}
			}
			active = &rid
			var seq int64
			if e = tx.QueryRow(ctx, "SELECT last_event_seq FROM job_runs WHERE id=$1", prepare).Scan(&seq); e != nil {
				return e
			}
			for i := range r.Segments {
				if r.Segments[i].RunID == prepare {
					r.Segments[i].ThroughSeq = &seq
				}
			}
			r.Segments = append(r.Segments, domain.ExecutionSegment{RunID: rid, SegmentNo: len(r.Segments)})
			_, e = tx.Exec(ctx, "INSERT INTO asset_refs(asset_id,owner_type,owner_id,expires_at) SELECT id,'run',$2,$3 FROM media_assets WHERE snapshot_id=$1 AND kind='video' AND state='ready' ON CONFLICT DO NOTHING", snapshot, rid, r.DeadlineAt.Add(5*time.Minute))
			if e != nil {
				return e
			}
		}
		if _, e = tx.Exec(ctx, "UPDATE generation_requests SET snapshot_id=$2,active_run_id=$3,article_id=$4,status=$5,reuse=$6,segments=$7,revision=revision+1,updated_at=now() WHERE id=$1", r.ID, snapshot, active, articleID, status, reuse, domain.JSON(r.Segments)); e != nil {
			return e
		}
		if articleID != nil {
			if e = receipt(ctx, tx, r.SessionID, *articleID); e != nil {
				return e
			}
		}
	}
	return nil
}
func (s *Store) ArticleForRun(ctx context.Context, run string) (domain.Article, error) {
	return one[domain.Article](ctx, s.Pool, "SELECT a.content FROM articles a JOIN qa_turns t ON t.article_id=a.id WHERE t.run_id=$1", run)
}
func (s *Store) TurnForRun(ctx context.Context, run string) (domain.Turn, error) {
	return one[domain.Turn](ctx, s.Pool, "SELECT to_jsonb(t) FROM qa_turns t WHERE run_id=$1", run)
}
func (s *Store) PriorTurns(ctx context.Context, t domain.Turn) ([]domain.Turn, error) {
	return many[domain.Turn](ctx, s.Pool, "SELECT to_jsonb(t) FROM qa_turns t WHERE conversation_id=$1 AND turn_no<$2 AND status='completed' AND expires_at>now() ORDER BY turn_no DESC LIMIT 6", t.ConversationID, t.TurnNo)
}
func (s *Store) PublishedArticle(ctx context.Context, id string) (domain.Article, error) {
	return article(ctx, s.Pool, id)
}
func (s *Store) Publish(ctx context.Context, c domain.Claim, a domain.Article) error {
	return s.Tx(ctx, func(tx pgx.Tx) error {
		if e := fence(ctx, tx, c); e != nil {
			return e
		}
		if c.Run.SnapshotID == nil || a.SnapshotID != *c.Run.SnapshotID {
			return errors.New("publication snapshot mismatch")
		}
		ids := ContentFrameIDs(a.Sections, a.Citations)
		var bytes int64
		for _, id := range ids {
			var state, snap string
			var size int64
			if e := tx.QueryRow(ctx, "SELECT state,snapshot_id,bytes FROM media_assets WHERE id=$1 AND kind='frame' FOR UPDATE", id).Scan(&state, &snap, &size); e != nil {
				return e
			}
			if state != "ready" || snap != a.SnapshotID {
				return errors.New("publication image unavailable")
			}
			bytes += size
		}
		if bytes > 20<<20 {
			return domain.Err(422, "RESOURCE_LIMIT_EXCEEDED", "文章配图超过预算。")
		}
		a.ID = domain.ID()
		a.SchemaVersion = 1
		a.PublishedAt = time.Now().UTC()
		model, e := modelIdentity(ctx, tx, c.Run.ConfigID)
		if e != nil {
			return e
		}
		frozen, err := one[domain.GenerationSpec](ctx, tx, `SELECT generation_spec FROM generation_requests WHERE active_run_id=$1 AND generation_spec IS NOT NULL ORDER BY created_at LIMIT 1`, c.Run.ID)
		if err == nil && frozen.ConfigID == c.Run.ConfigID && frozen.Model.ID != "" {
			model = frozen.Model
		} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if _, e := tx.Exec(ctx, "INSERT INTO articles(id,generation_run_id,snapshot_id,config_id,reuse_key,content,generation_model) VALUES($1,$2,$3,$4,$5,$6,$7)", a.ID, c.Run.ID, a.SnapshotID, c.Run.ConfigID, c.Run.WorkKey, domain.JSON(a), domain.JSON(model)); e != nil {
			return e
		}
		for _, id := range ids {
			if _, e := tx.Exec(ctx, "INSERT INTO asset_refs(asset_id,owner_type,owner_id) VALUES($1,'article',$2)", id, a.ID); e != nil {
				return e
			}
		}
		if _, e := tx.Exec(ctx, "UPDATE generation_requests SET status='completed',article_id=$2,revision=revision+1,updated_at=now() WHERE active_run_id=$1 AND status='generating'", c.Run.ID, a.ID); e != nil {
			return e
		}
		rs, e := many[domain.Request](ctx, tx, "SELECT to_jsonb(r) FROM generation_requests r WHERE active_run_id=$1 AND article_id=$2 AND status='completed'", c.Run.ID, a.ID)
		if e != nil {
			return e
		}
		for _, r := range rs {
			if e = receipt(ctx, tx, r.SessionID, a.ID); e != nil {
				return e
			}
		}
		return finishRun(ctx, tx, c, a.ID)
	})
}
func (s *Store) CompleteAnswer(ctx context.Context, c domain.Claim, a domain.Answer) error {
	return s.Tx(ctx, func(tx pgx.Tx) error {
		if e := fence(ctx, tx, c); e != nil {
			return e
		}
		t, e := one[domain.Turn](ctx, tx, "SELECT to_jsonb(t) FROM qa_turns t WHERE run_id=$1", c.Run.ID)
		if e != nil {
			return e
		}
		for _, f := range a.Frames {
			var state string
			if e = tx.QueryRow(ctx, "SELECT state FROM media_assets WHERE id=$1 AND snapshot_id=$2 FOR UPDATE", f.ID, t.SnapshotID).Scan(&state); e != nil {
				return e
			}
			if state != "ready" {
				return errors.New("answer image unavailable")
			}
			if _, e = tx.Exec(ctx, "INSERT INTO asset_refs(asset_id,owner_type,owner_id,expires_at) VALUES($1,'answer',$2,now()+interval '30 days') ON CONFLICT DO NOTHING", f.ID, t.ID); e != nil {
				return e
			}
		}
		if _, e = tx.Exec(ctx, "UPDATE qa_turns SET status='completed',answer=$2,revision=revision+1,updated_at=now(),expires_at=now()+interval '30 days' WHERE id=$1", t.ID, domain.JSON(a)); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, "UPDATE qa_conversations SET active_turn_id=NULL WHERE id=$1 AND active_turn_id=$2", t.ConversationID, t.ID); e != nil {
			return e
		}
		return finishRun(ctx, tx, c, t.ID)
	})
}
func (s *Store) WaitForRestore(ctx context.Context, c domain.Claim) error {
	return s.Tx(ctx, func(tx pgx.Tx) error {
		if e := fence(ctx, tx, c); e != nil {
			return e
		}
		r, e := loadRun(ctx, tx, c.Run.ID)
		if e != nil {
			return e
		}
		if r.Kind != "qa.answer" || r.SnapshotID == nil || r.RestoreUsed {
			return domain.Err(422, "MEDIA_UNAVAILABLE", "所需视频画面无法恢复。")
		}
		var ordinary int
		if e = tx.QueryRow(ctx, "SELECT count(*) FROM job_runs WHERE kind<>'qa.answer' AND status IN ('queued','running','retry_wait','waiting_dependency','cancel_requested')").Scan(&ordinary); e != nil {
			return e
		}
		if ordinary >= 3 {
			return domain.Err(422, "MEDIA_UNAVAILABLE", "当前没有视频恢复容量。")
		}
		p, e := one[config.Profile](ctx, tx, "SELECT profile FROM runtime_config_revisions WHERE id=$1", r.ConfigID)
		if e != nil {
			return e
		}
		deadline := r.CreatedAt.Add(time.Duration(p.RestoreAnswerSeconds) * time.Second)
		var dep string
		e = tx.QueryRow(ctx, "SELECT id FROM job_runs WHERE kind='source.restore' AND work_key=$1 AND status IN ('queued','running','retry_wait')", *r.SnapshotID).Scan(&dep)
		if errors.Is(e, pgx.ErrNoRows) {
			dep, e = newRun(ctx, tx, "source.restore", *r.SnapshotID, r.SourceID, r.ConfigID, r.SnapshotID, deadline)
			if e != nil {
				return e
			}
		} else if e != nil {
			return e
		}
		if e = charge(ctx, tx, r.ID); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, "UPDATE job_attempts SET ended_at=now(),outcome='waiting_dependency' WHERE id=$1", c.AttemptID); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, "UPDATE job_runs SET status='waiting_dependency',dependency_run_id=$2,restore_used=true,deadline_at=$3,lease_owner=NULL,lease_expires_at=NULL,updated_at=now() WHERE id=$1", r.ID, dep, deadline); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, "UPDATE qa_turns SET status='waiting_dependency',revision=revision+1,updated_at=now() WHERE run_id=$1", r.ID); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, "UPDATE asset_refs SET expires_at=$2 WHERE owner_type='run' AND owner_id=$1", r.ID, deadline.Add(5*time.Minute)); e != nil {
			return e
		}
		return event(ctx, tx, r.ID, nil)
	})
}

type Operation struct {
	Key               string          `json:"operation_key"`
	Kind              string          `json:"kind"`
	ArgsHash          string          `json:"args_hash"`
	Status            string          `json:"status"`
	Submissions       int             `json:"submissions"`
	Result            json.RawMessage `json:"result"`
	ProviderRequestID *string         `json:"provider_request_id"`
}

func (s *Store) Operation(ctx context.Context, run, key string) (Operation, error) {
	return one[Operation](ctx, s.Pool, "SELECT to_jsonb(o) FROM run_operations o WHERE run_id=$1 AND operation_key=$2", run, key)
}
func (s *Store) BeginOperation(ctx context.Context, c domain.Claim, key, kind string, args any, submit bool) (Operation, error) {
	return s.beginOperation(ctx, c, key, kind, args, submit, "")
}

func (s *Store) BeginModelSubmission(ctx context.Context, c domain.Claim, key string, args any, routeConfigID string) (Operation, error) {
	return s.beginOperation(ctx, c, key, "model", args, true, routeConfigID)
}

func (s *Store) beginOperation(ctx context.Context, c domain.Claim, key, kind string, args any, submit bool, routeConfigID string) (Operation, error) {
	var op Operation
	e := s.Tx(ctx, func(tx pgx.Tx) error {
		if e := fence(ctx, tx, c); e != nil {
			return e
		}
		h := Hash(args)
		_, e := tx.Exec(ctx, "INSERT INTO run_operations(run_id,operation_key,attempt_id,kind,args_hash,status) VALUES($1,$2,$3,$4,$5,'started') ON CONFLICT DO NOTHING", c.Run.ID, key, c.AttemptID, kind, h)
		if e != nil {
			return e
		}
		op, e = one[Operation](ctx, tx, "SELECT to_jsonb(o) FROM run_operations o WHERE run_id=$1 AND operation_key=$2 FOR UPDATE", c.Run.ID, key)
		if e != nil {
			return e
		}
		if op.ArgsHash != h || op.Kind != kind {
			return errors.New("operation replay conflict")
		}
		if op.Status == "completed" {
			return nil
		}
		if submit {
			if kind == "web.read" {
				if op.Submissions > 0 {
					return domain.Err(409, "PROVIDER_OUTCOME_UNKNOWN", "原网页读取结果未知，不重复发送。")
				}
				var reads int
				if e = tx.QueryRow(ctx, "SELECT count(*) FROM run_operations WHERE run_id=$1 AND kind='web.read' AND submissions>0", c.Run.ID).Scan(&reads); e != nil {
					return e
				}
				if reads >= 3 {
					return domain.Err(429, "REFERENCE_LIMIT_EXCEEDED", "本轮最多读取三个网页。")
				}
			}
			limit := 3
			recoveryGranted := false
			reporting := map[string]any{"schema": "unobserved", "stream_status": "incomplete"}
			if kind == "model" {
				routes, err := modelRoutes(ctx, tx, c.Run.ConfigID)
				if err != nil {
					return err
				}
				if routeConfigID == "" {
					routeConfigID = c.Run.ConfigID
				}
				found := false
				for _, route := range routes {
					if route.ConfigID == routeConfigID {
						found = true
						reporting["route_config_id"] = route.ConfigID
						reporting["model_revision_id"] = route.RevisionID
						reporting["provider_id"] = route.ProviderID
						reporting["provider_revision_id"] = route.ProviderRevisionID
					}
				}
				if !found {
					return domain.Err(503, "MODEL_UNAVAILABLE", "这条模型线路已不可用。")
				}
				limit = max(limit, len(routes))
				baseLimit := limit
				limit, err = recoveredModelLimit(ctx, tx, c.Run.ID, key, limit)
				if err != nil {
					return err
				}
				recoveryGranted = limit > baseLimit
			}
			if op.Submissions >= limit {
				if kind == "model" {
					if recoveryGranted {
						return &domain.Problem{Status: 422, Code: "PROVIDER_UNAVAILABLE", Detail: "本轮模型调用的自动恢复额度已用完；来源、观察和已保存章节保留，可手动继续。", Meta: map[string]any{"can_retry": true, "retry_action": "resume_checkpoint", "submissions": op.Submissions}}
					}
					return modelSubmissionLimit(op.Submissions)
				}
				return domain.Err(422, "PROVIDER_UNAVAILABLE", "同一远端操作已达到提交上限。")
			}
			if _, e = tx.Exec(ctx, "UPDATE run_operations SET submissions=submissions+1,updated_at=now() WHERE run_id=$1 AND operation_key=$2", c.Run.ID, key); e != nil {
				return e
			}
			op.Submissions++
			if kind == "model" {
				if _, e = tx.Exec(ctx, "INSERT INTO model_submissions(run_id,operation_key,submission,attempt_id,reporting) VALUES($1,$2,$3,$4,$5)", c.Run.ID, key, op.Submissions, c.AttemptID, domain.JSON(reporting)); e != nil {
					return e
				}
			}
		}
		return nil
	})
	return op, e
}
func (s *Store) FinishOperation(ctx context.Context, c domain.Claim, key string, result any, provider string, usage any) error {
	return s.Tx(ctx, func(tx pgx.Tx) error {
		if e := fence(ctx, tx, c); e != nil {
			return e
		}
		changed, e := tx.Exec(ctx, "UPDATE run_operations SET status='completed',result=$3,provider_request_id=NULLIF($4,''),usage=$5,updated_at=now(),completed_at=now() WHERE run_id=$1 AND operation_key=$2 AND status<>'completed'", c.Run.ID, key, domain.JSON(result), provider, nullableJSON(usage))
		if e != nil || changed.RowsAffected() == 0 {
			return e
		}
		// Persisted operation output is business progress, even when a long
		// source-preparation phase has not yet produced its final checkpoint.
		// Replaying an already completed operation must not refresh this clock.
		changed, e = tx.Exec(ctx, "UPDATE job_runs SET updated_at=now() WHERE id=$1 AND status='running' AND execution_epoch=$2 AND lease_owner=$3", c.Run.ID, c.Epoch, c.Owner)
		if e != nil {
			return e
		}
		if changed.RowsAffected() != 1 {
			return domain.ErrFenced
		}
		return nil
	})
}
func (s *Store) Observations(ctx context.Context, run string) ([]json.RawMessage, error) {
	return many[json.RawMessage](ctx, s.Pool, "SELECT data FROM observations WHERE run_id=$1 ORDER BY created_at,id", run)
}
func (s *Store) RecordObservation(ctx context.Context, c domain.Claim, key, inspection string, data any) error {
	return s.Tx(ctx, func(tx pgx.Tx) error {
		if e := fence(ctx, tx, c); e != nil {
			return e
		}
		var existing []byte
		e := tx.QueryRow(ctx, "SELECT data FROM observations WHERE run_id=$1 AND operation_key=$2", c.Run.ID, key).Scan(&existing)
		if e == nil {
			if Hash(json.RawMessage(existing)) != Hash(data) {
				return errors.New("observation replay conflict")
			}
			return nil
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		_, e = tx.Exec(ctx, "INSERT INTO observations(id,run_id,snapshot_id,operation_key,inspection_id,data) VALUES($1,$2,$3,$4,$5,$6)", domain.ID(), c.Run.ID, c.Run.SnapshotID, key, inspection, domain.JSON(data))
		return e
	})
}
func (s *Store) RunFrames(ctx context.Context, run string) ([]domain.Asset, error) {
	return many[domain.Asset](ctx, s.Pool, "SELECT to_jsonb(a) FROM media_assets a JOIN asset_refs ar ON ar.asset_id=a.id WHERE ar.owner_type='run' AND ar.owner_id=$1 AND a.kind='frame'", run)
}
func (s *Store) InternalFrame(ctx context.Context, c domain.Claim, id string) (domain.Asset, error) {
	a, e := s.Asset(ctx, id)
	if e != nil {
		return a, e
	}
	if !sameID(a.SnapshotID, c.Run.SnapshotID) || a.Kind != "frame" {
		return a, errors.New("frame scope mismatch")
	}
	if a.RunID == c.Run.ID {
		return a, nil
	}
	var pinned bool
	if err := s.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM asset_refs WHERE asset_id=$1 AND owner_type='run' AND owner_id=$2 AND expires_at>now())", id, c.Run.ID).Scan(&pinned); err != nil {
		return a, err
	}
	if pinned {
		return a, nil
	}
	var ok bool
	e = s.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM asset_refs ar JOIN articles a ON ar.owner_type='article' AND ar.owner_id=a.id WHERE ar.asset_id=$1 AND a.snapshot_id=$2)", id, c.Run.SnapshotID).Scan(&ok)
	if e != nil {
		return a, e
	}
	if !ok && c.Run.Kind == "qa.answer" {
		t, e := s.TurnForRun(ctx, c.Run.ID)
		if e != nil {
			return a, e
		}
		var sess string
		if e = s.Pool.QueryRow(ctx, "SELECT session_id FROM qa_conversations WHERE id=$1", t.ConversationID).Scan(&sess); e != nil {
			return a, e
		}
		e = visibleFrame(ctx, s.Pool, sess, t.RequestID, id)
		ok = e == nil
	}
	if !ok {
		return a, errors.New("private frame not accessible")
	}
	return a, nil
}
func (s *Store) PublicFrames(ctx context.Context, request string, ids []string) ([]domain.Frame, error) {
	return frames(ctx, s.Pool, request, ids)
}

// A missing owned file changes availability, never the immutable article or its
// frame identity. This observation cannot resurrect deleting/deleted assets.
func (s *Store) MarkMissingFrame(ctx context.Context, id string) error {
	return s.Tx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE media_assets SET state='unavailable' WHERE id=$1 AND kind='frame' AND state='ready'", id)
		return err
	})
}
func (s *Store) HasProtection(ctx context.Context, id string) (bool, error) {
	var ok bool
	e := s.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM asset_refs WHERE asset_id=$1 AND (expires_at IS NULL OR expires_at>now()))", id).Scan(&ok)
	return ok, e
}
func (s *Store) MarkReplacement(ctx context.Context, c domain.Claim, fresh, old string) error {
	return s.Tx(ctx, func(tx pgx.Tx) error {
		if e := fence(ctx, tx, c); e != nil {
			return e
		}
		if fresh == old {
			return nil
		}
		tag, e := tx.Exec(ctx, "UPDATE media_assets SET replaces_frame_id=$3 WHERE id=$1 AND snapshot_id=$2 AND state='ready'", fresh, c.Run.SnapshotID, old)
		if e != nil {
			return e
		}
		if tag.RowsAffected() != 1 {
			return errors.New("replacement not available")
		}
		return nil
	})
}
