package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"vidlens/internal/domain"
)

func retainedASR(t *testing.T, s *Store, owner string, model CatalogModel) (domain.Request, domain.Claim) {
	t.Helper()
	ctx := context.Background()
	in := domain.RequestInput{VideoURL: "https://youtube.com/watch?v=abcdefghijk", ModelID: model.ID, ModelRevisionID: model.RevisionID}
	a, err := s.CreateRequestWithInput(ctx, owner, domain.ID(), in, "", false)
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.Request(ctx, owner, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Claim(ctx, *r.ActiveRunID, 1, "retained-source")
	if err != nil || c == nil {
		t.Fatal(err)
	}
	video := domain.Asset{ID: domain.ID(), SourceID: c.Run.SourceID, RunID: c.Run.ID, Kind: "video", StorageKey: domain.ID() + ".mp4", SHA256: "retained-video", MIME: "video/mp4", Bytes: 72}
	if err := s.AddAsset(ctx, *c, video); err != nil {
		t.Fatal(err)
	}
	snapshot := domain.Snapshot{ID: domain.ID(), SourceID: c.Run.SourceID, VideoHash: video.SHA256, TextHash: "retained-text", DurationMS: 1000, Source: domain.Source{SourceID: c.Run.SourceID, CanonicalURL: in.VideoURL, TextProvenance: &domain.Provenance{Origin: "asr", Coverage: "complete"}}, Metadata: domain.JSON(map[string]any{"access_policy": "anonymous", "asr_pipeline_version": "60s-mp3-48k16k-v1"})}
	segments := []domain.Segment{{Ordinal: 0, StartMS: 0, EndMS: 1000, Text: "The complete original transcript stays immutable.", Origin: "asr"}}
	if _, err := s.FinishSource(ctx, *c, snapshot, segments, video.ID); err != nil {
		t.Fatal(err)
	}
	r, err = s.Request(ctx, owner, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	return r, *c
}

func sourceReuseModels(t *testing.T, s *Store) (CatalogModel, CatalogModel) {
	t.Helper()
	a, b := enabledModel(t, s, "source-model-a"), enabledModel(t, s, "source-model-b")
	for i, m := range []CatalogModel{a, b} {
		limits := map[string]any{"max_video_ms": int64((90 + i*30) * 60 * 1000), "asr_base_url": "https://asr.example.test/v1", "asr_model": "retained-asr"}
		if _, err := s.Pool.Exec(context.Background(), "UPDATE runtime_config_revisions SET profile=profile || $2::jsonb WHERE id=(SELECT config_id FROM model_revisions WHERE id=$1)", m.RevisionID, domain.JSON(limits)); err != nil {
			t.Fatal(err)
		}
	}
	return a, b
}

func TestCompletedSourceReuseSurvivesModelAndAdmissionChanges(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a, b := sourceReuseModels(t, s)
	owner := session(t, s)
	old, prepare := retainedASR(t, s, owner, a)
	before, err := s.Transcript(ctx, *old.SnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := s.CreateRequestWithInput(ctx, session(t, s), domain.ID(), domain.RequestInput{VideoURL: old.InputURL, ModelID: b.ID, ModelRevisionID: b.RevisionID, DetailLevel: domain.Ptr("detailed")}, "", false)
	if err != nil {
		t.Fatal(err)
	}
	next, err := loadRequest(ctx, s.Pool, accepted.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !sameID(next.SnapshotID, old.SnapshotID) || next.Status != "generating" {
		t.Fatalf("changing only interpretation model and fitting duration limit repeated source/ASR: status=%s snapshot=%v want=%s", next.Status, next.SnapshotID, *old.SnapshotID)
	}
	run, err := s.Run(ctx, *next.ActiveRunID)
	if err != nil || run.Kind != "article.generate" || run.ConfigID != next.ConfigID || next.GenerationSpec.Model.ID != b.ID {
		t.Fatalf("source reuse lost the new frozen interpretation: %+v %v", run, err)
	}
	var sources int
	if err := s.Pool.QueryRow(ctx, "SELECT count(*) FROM job_runs WHERE kind IN ('source.prepare','source.restore')").Scan(&sources); err != nil || sources != 1 {
		t.Fatalf("repeated source work: %d %v", sources, err)
	}
	after, err := s.Transcript(ctx, *old.SnapshotID)
	if err != nil || Hash(after) != Hash(before) {
		t.Fatal("reusing the source changed the original transcript", err)
	}
	finished, err := s.Run(ctx, prepare.Run.ID)
	if err != nil || finished.Status != "succeeded" || finished.ResultID == nil || *finished.ResultID != *old.SnapshotID {
		t.Fatal("completed source identity changed", err)
	}
}

func TestRetainedTranscriptRestoresOnlyExpiredVideo(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a, b := sourceReuseModels(t, s)
	owner := session(t, s)
	old, _ := retainedASR(t, s, owner, a)
	if _, err := s.Pool.Exec(ctx, "UPDATE media_assets SET state='deleted' WHERE snapshot_id=$1 AND kind='video'", *old.SnapshotID); err != nil {
		t.Fatal(err)
	}
	nextOwner := session(t, s)
	accepted, err := s.CreateRequestWithInput(ctx, nextOwner, domain.ID(), domain.RequestInput{VideoURL: old.InputURL, ModelID: b.ID, ModelRevisionID: b.RevisionID}, "", false)
	if err != nil {
		t.Fatal(err)
	}
	next, err := s.Request(ctx, nextOwner, accepted.ID)
	if err != nil {
		t.Fatal(err)
	}
	run, err := s.Run(ctx, *next.ActiveRunID)
	if err != nil || run.Kind != "source.restore" || !sameID(run.SnapshotID, old.SnapshotID) {
		t.Fatalf("expired video discarded retained ASR instead of restoring media: kind=%s snapshot=%v err=%v", run.Kind, run.SnapshotID, err)
	}
	restore, err := s.Claim(ctx, run.ID, 1, "media-only-restore")
	if err != nil || restore == nil {
		t.Fatal(err)
	}
	video := domain.Asset{ID: domain.ID(), SourceID: run.SourceID, SnapshotID: old.SnapshotID, RunID: run.ID, Kind: "video", StorageKey: domain.ID() + ".mp4", SHA256: "retained-video", MIME: "video/mp4", Bytes: 72}
	if err := s.AddAsset(ctx, *restore, video); err != nil {
		t.Fatal(err)
	}
	snapshot := domain.Snapshot{ID: domain.ID(), SourceID: run.SourceID, VideoHash: video.SHA256, DurationMS: 1000}
	if _, err := s.FinishSource(ctx, *restore, snapshot, []domain.Segment{{Text: "must not replace retained original"}}, video.ID); err != nil {
		t.Fatal(err)
	}
	next, err = s.Request(ctx, nextOwner, accepted.ID)
	if err != nil || next.Status != "generating" || !sameID(next.SnapshotID, old.SnapshotID) {
		t.Fatalf("media restoration did not resume interpretation on retained original: %+v %v", next, err)
	}
	text, err := s.Transcript(ctx, *old.SnapshotID)
	if err != nil || len(text) != 1 || text[0].Text != "The complete original transcript stays immutable." {
		t.Fatal("media restore replaced the original transcript", err)
	}
}

func TestRetainedSourceDoesNotBypassSourceBoundaries(t *testing.T) {
	for _, mutation := range []string{"duration_limit", "download_limit", "private_ingestion", "partial_text", "missing_text", "different_pipeline"} {
		t.Run(mutation, func(t *testing.T) {
			s := testStore(t)
			ctx := context.Background()
			a, b := sourceReuseModels(t, s)
			old, prepare := retainedASR(t, s, session(t, s), a)
			var err error
			switch mutation {
			case "duration_limit":
				_, err = s.Pool.Exec(ctx, "UPDATE runtime_config_revisions SET profile=jsonb_set(profile,'{max_video_ms}','500') WHERE id=(SELECT config_id FROM model_revisions WHERE id=$1)", b.RevisionID)
			case "download_limit":
				_, err = s.Pool.Exec(ctx, "UPDATE runtime_config_revisions SET profile=jsonb_set(profile,'{max_download_bytes}','64') WHERE id=(SELECT config_id FROM model_revisions WHERE id=$1)", b.RevisionID)
			case "private_ingestion":
				_, err = s.Pool.Exec(ctx, "UPDATE source_snapshots SET metadata=jsonb_set(metadata,'{access_policy}','\"authenticated\"') WHERE id=$1", *old.SnapshotID)
			case "partial_text":
				_, err = s.Pool.Exec(ctx, "UPDATE source_snapshots SET source=jsonb_set(source,'{text_provenance,coverage}','\"partial\"') WHERE id=$1", *old.SnapshotID)
			case "missing_text":
				_, err = s.Pool.Exec(ctx, "DELETE FROM transcript_segments WHERE snapshot_id=$1", *old.SnapshotID)
			case "different_pipeline":
				_, err = s.Pool.Exec(ctx, "UPDATE job_runs SET work_key=work_key||':old-pipeline' WHERE id=$1", prepare.Run.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			nextOwner := session(t, s)
			accepted, err := s.CreateRequestWithInput(ctx, nextOwner, domain.ID(), domain.RequestInput{VideoURL: old.InputURL, ModelID: b.ID, ModelRevisionID: b.RevisionID}, "", false)
			if err != nil {
				t.Fatal(err)
			}
			next, err := s.Request(ctx, nextOwner, accepted.ID)
			if err != nil || next.SnapshotID != nil {
				t.Fatalf("reused an incompatible source: %+v %v", next, err)
			}
		})
	}
}

func TestRetainedTranscriptIgnoresASRCredentialRotation(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a, b := sourceReuseModels(t, s)
	old, _ := retainedASR(t, s, session(t, s), a)
	if _, err := s.Pool.Exec(ctx, "UPDATE runtime_config_revisions SET profile=profile || $2::jsonb WHERE id=(SELECT config_id FROM model_revisions WHERE id=$1)", b.RevisionID, domain.JSON(map[string]any{"asr_key_ref": "rotated-asr", "asr_base_url": "https://new-asr.example.test/v1", "asr_model": "new-transcriber"})); err != nil {
		t.Fatal(err)
	}
	owner := session(t, s)
	x, err := s.CreateRequestWithInput(ctx, owner, domain.ID(), domain.RequestInput{VideoURL: old.InputURL, ModelID: b.ID, ModelRevisionID: b.RevisionID}, "", false)
	if err != nil {
		t.Fatal(err)
	}
	next, err := s.Request(ctx, owner, x.ID)
	if err != nil || !sameID(next.SnapshotID, old.SnapshotID) {
		t.Fatal("changing an ASR credential/provider discarded an already saved original", err)
	}
}

func TestSourceRetryPinsOriginalSnapshotWhenNewerTextExists(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a, _ := sourceReuseModels(t, s)
	owner := session(t, s)
	old, original := retainedASR(t, s, owner, a)
	if err := s.CancelRequest(ctx, owner, old.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Claim(ctx, *old.ActiveRunID, 1, "cancel-original"); err != nil {
		t.Fatal(err)
	}
	var fresh string
	if err := s.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		fresh, err = newRun(ctx, tx, "source.prepare", original.Run.WorkKey, original.Run.SourceID, original.Run.ConfigID, nil, time.Now().Add(time.Hour))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	claim, err := s.Claim(ctx, fresh, 1, "newer-text-fixture")
	if err != nil || claim == nil {
		t.Fatal(err)
	}
	asset := domain.Asset{ID: domain.ID(), SourceID: original.Run.SourceID, RunID: fresh, Kind: "video", StorageKey: domain.ID() + ".mp4", SHA256: "retained-video", MIME: "video/mp4", Bytes: 72}
	if err := s.AddAsset(ctx, *claim, asset); err != nil {
		t.Fatal(err)
	}
	snapshot := domain.Snapshot{ID: domain.ID(), SourceID: original.Run.SourceID, VideoHash: asset.SHA256, TextHash: "newer-text", DurationMS: 1000, Source: domain.Source{TextProvenance: &domain.Provenance{Origin: "asr", Coverage: "complete"}}, Metadata: domain.JSON(map[string]any{"access_policy": "anonymous"})}
	if _, err := s.FinishSource(ctx, *claim, snapshot, []domain.Segment{{Ordinal: 0, StartMS: 0, EndMS: 1000, Text: "A newer transcription.", Origin: "asr"}}, asset.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pool.Exec(ctx, "UPDATE media_assets SET state='deleted' WHERE snapshot_id=$1 AND kind='video'", *old.SnapshotID); err != nil {
		t.Fatal(err)
	}
	retry, err := s.CreateRequest(ctx, owner, domain.ID(), "", old.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	next, err := s.Request(ctx, owner, retry.ID)
	if err != nil || !sameID(next.SnapshotID, old.SnapshotID) || Hash(next.GenerationSpec) != Hash(old.GenerationSpec) {
		t.Fatalf("retry changed its frozen original/spec: %+v %v", next, err)
	}
}

func TestMediaRestoreSubscribersReserveArticleCapacity(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a, b := sourceReuseModels(t, s)
	old, _ := retainedASR(t, s, session(t, s), a)
	if _, err := s.Pool.Exec(ctx, "UPDATE media_assets SET state='deleted' WHERE snapshot_id=$1 AND kind='video'", *old.SnapshotID); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxPendingVideoTasks-1; i++ {
		_, err := s.CreateRequestWithInput(ctx, session(t, s), domain.ID(), domain.RequestInput{VideoURL: old.InputURL, ModelID: b.ID, ModelRevisionID: b.RevisionID, Purpose: fmt.Sprintf("restored article %d", i)}, "", false)
		if err != nil {
			t.Fatalf("subscriber %d: %v", i, err)
		}
	}
	_, err := s.CreateRequestWithInput(ctx, session(t, s), domain.ID(), domain.RequestInput{VideoURL: old.InputURL, ModelID: b.ID, ModelRevisionID: b.RevisionID, Purpose: "over restored article capacity"}, "", false)
	if err == nil {
		t.Fatal("shared restore bypassed future article admission")
	}
}
