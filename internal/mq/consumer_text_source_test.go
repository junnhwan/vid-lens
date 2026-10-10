package mq

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
	"vid-lens/internal/pkg/ytdlp"
	"vid-lens/internal/processing"
	"vid-lens/internal/repository"
	"vid-lens/internal/textsource"
)

type sourceFixtureAdapter struct {
	probes      int
	fetches     int
	probeError  error
	raw         []byte
	tracks      []ytdlp.SubtitleTrack
	beforeFetch func()
	wrongCID    bool
}

func (a *sourceFixtureAdapter) ResolveIdentity(context.Context, string) (ytdlp.BilibiliIdentity, error) {
	a.probes++
	if a.probeError != nil {
		return ytdlp.BilibiliIdentity{}, a.probeError
	}
	cid := int64(222)
	if a.wrongCID {
		cid = 333
	}
	return ytdlp.BilibiliIdentity{Platform: "bilibili", BVID: "BV1xx411c7mD", AID: 42, CID: cid, PartIndex: 2, PartCount: 2, CanonicalURL: "https://www.bilibili.com/video/BV1xx411c7mD?p=2", DurationMS: 10000}, nil
}
func (a *sourceFixtureAdapter) ListSubtitleTracks(context.Context, ytdlp.BilibiliIdentity) ([]ytdlp.SubtitleTrack, error) {
	return a.tracks, nil
}
func (a *sourceFixtureAdapter) FetchSelectedSubtitle(context.Context, ytdlp.BilibiliIdentity, ytdlp.SubtitleTrack) ([]byte, error) {
	a.fetches++
	if a.beforeFetch != nil {
		a.beforeFetch()
	}
	return a.raw, nil
}

type sourceFixtureProducer struct {
	summaryCalls, asrCalls int
	token, budget          string
	err                    error
}

func (p *sourceFixtureProducer) EnqueueSummary(ctx context.Context, _ int64, _ string) error {
	p.summaryCalls++
	p.token = ClaimTokenFromContext(ctx)
	p.budget = RetryBudgetIDFromContext(ctx)
	return p.err
}
func (p *sourceFixtureProducer) EnqueueTranscribe(ctx context.Context, _ int64, _ string) error {
	p.asrCalls++
	p.token = ClaimTokenFromContext(ctx)
	p.budget = RetryBudgetIDFromContext(ctx)
	return p.err
}
func (p *sourceFixtureProducer) EnqueueTextSource(ctx context.Context, _ int64, _ string) error {
	p.token = ClaimTokenFromContext(ctx)
	return p.err
}

// Satisfy scheduler's legacy producer surface while exercising explicit source routing.
func (p *sourceFixtureProducer) EnqueueAnalyze(context.Context, int64, string) error {
	return errors.New("unexpected analyze route")
}
func (p *sourceFixtureProducer) EnqueueDownload(context.Context, int64, string) error {
	return errors.New("unexpected download route")
}
func (p *sourceFixtureProducer) EnqueueRAGIndex(context.Context, int64) error {
	return errors.New("unexpected RAG route")
}

func sourceWorkerFixture(t *testing.T, auto bool, policy string) (*Consumer, *repository.Repositories, *gorm.DB, model.VideoTask, AnalyzePayload, *sourceFixtureAdapter, *sourceFixtureProducer, *int) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { sqlDB.Close() })
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	repos := repository.NewRepositories(db)
	options, err := processing.Normalize(processing.Options{AutoSummary: auto, TextSourcePolicy: policy, PreferredLanguage: "zh", OutputMode: "text"}, false)
	if err != nil {
		t.Fatal(err)
	}
	intent := processing.Intent{ID: uuid.NewString(), Version: 1, GenerationID: uuid.NewString(), Options: options, RecipeVersion: processing.Recipe}
	intentJSON, _ := json.Marshal(intent)
	identity := textsource.Identity{Platform: "bilibili", BVID: "BV1xx411c7mD", AID: 42, CID: 222, PartIndex: 2, MediaFingerprint: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	identityJSON, _ := json.Marshal(identity)
	task := model.VideoTask{UserID: 1, FileMD5: identity.MediaFingerprint, FileURL: "videos/owner/source.mp4", Filename: "video.mp4", SourceType: model.TaskSourceTypeURL, SourceURL: "https://www.bilibili.com/video/BV1xx411c7mD?p=2", Stage: model.TaskStageUploaded, ProcessingIntentJSON: string(intentJSON), MediaIdentityJSON: string(identityJSON)}
	now := time.Now()
	dispatch, err := repos.PrepareInitialTaskDispatch(repository.InitialTaskDispatchRequest{Task: &task, CreateTask: true, JobType: model.TaskJobTypeTextSource, Stage: model.TaskStageTextSource, Now: now, LeaseUntil: now.Add(time.Minute), Token: "source-dispatch"})
	if err != nil {
		t.Fatal(err)
	}
	adapter := &sourceFixtureAdapter{raw: []byte("1\n00:00:01,000 --> 00:00:03,000\n真实来源的字幕文字\n"), tracks: []ytdlp.SubtitleTrack{{TrackKey: "zh:1", Language: "zh", Format: "srt", SubtitleKind: "unknown", KindBasis: "extractor lacks type metadata", BVID: identity.BVID, CID: identity.CID, PartIndex: 2}}}
	producer := &sourceFixtureProducer{}
	uploads := new(int)
	consumer := NewConsumer(repos, nil, nil, nil, "ffmpeg")
	consumer.SetTextSourceAdapter(adapter)
	consumer.SetSourceDispatchProducer(producer)
	consumer.uploadLocalFile = func(_ context.Context, path, key, contentType string) error {
		*uploads++
		if !strings.HasPrefix(key, "sources/1/") || !strings.HasSuffix(key, ".srt") || contentType != "application/x-subrip" {
			t.Fatalf("invalid private raw source storage %s %s", key, contentType)
		}
		raw, err := os.ReadFile(path)
		if err != nil || string(raw) != string(adapter.raw) {
			t.Fatal("raw source bytes lost")
		}
		return nil
	}
	return consumer, repos, db, task, AnalyzePayload{TaskID: task.ID, MD5: task.FileMD5, JobType: model.TaskJobTypeTextSource, ClaimToken: dispatch.Token, BudgetID: dispatch.RetryBudgetID}, adapter, producer, uploads
}

func TestTextSourcePublicationSummaryHandoffAndRepeatedDelivery(t *testing.T) {
	c, repos, db, task, payload, adapter, producer, uploads := sourceWorkerFixture(t, true, "prefer_platform")
	producer.err = errors.New("RabbitMQ unavailable")
	if err := c.handleTextSource(context.Background(), payload); err != nil {
		t.Fatal(err)
	}
	current, _ := repos.Task.FindByID(task.ID)
	if current.Status != model.TaskStatusCompleted || current.ActiveTextSourceID == "" || current.ProcessingToken != "" {
		t.Fatalf("source not completed: %+v", current)
	}
	source, err := repos.TextSource.Active(context.Background(), 1, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if source.Kind != textsource.KindSubtitle || source.Identity.CID != 222 || source.RawObjectKey == "" {
		t.Fatalf("source=%+v", source)
	}
	transcription, _ := repos.Transcription.FindByTaskID(task.ID)
	if transcription == nil || transcription.SourceID != source.ID || transcription.Content != source.CanonicalText {
		t.Fatalf("projection=%+v", transcription)
	}
	job, _ := repos.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeSummary)
	var frozen processing.GenerationSnapshot
	if job == nil || json.Unmarshal([]byte(job.InputSnapshotJSON), &frozen) != nil || job.InputSourceID != source.ID || job.GenerationID != frozen.Intent.GenerationID || frozen.SourceDigest != source.SourceDigest || job.Status != model.TaskStatusQueued || job.ProcessingToken == "" || job.LeaseExpiresAt == nil {
		t.Fatalf("summary snapshot/dispatch not durable: %+v", job)
	}
	if producer.summaryCalls != 1 || producer.asrCalls != 0 || producer.token != job.ProcessingToken || producer.budget != job.RetryBudgetID {
		t.Fatalf("producer=%+v job=%+v", producer, job)
	}
	var asrChunks int64
	db.Model(&model.VideoTranscriptionChunk{}).Where("task_id = ?", task.ID).Count(&asrChunks)
	if asrChunks != 0 {
		t.Fatal("subtitle fabricated ASR chunks")
	}
	if err := c.handleTextSource(context.Background(), payload); err != nil {
		t.Fatal(err)
	}
	if adapter.probes != 1 || adapter.fetches != 1 || *uploads != 1 || producer.summaryCalls != 1 {
		t.Fatal("repeated source delivery repeated side effects")
	}
}

func TestTextSourceFallbackFinishesOwnLeaseAndDurablyPreparesASR(t *testing.T) {
	for _, test := range []struct {
		name, policy, reason string
		change               func(*sourceFixtureAdapter)
	}{
		{"forced", "force_asr", "force_asr", nil},
		{"probe_failed", "prefer_platform", "subtitle_probe_failed", func(a *sourceFixtureAdapter) { a.probeError = errors.New("provider signed url and cookie secret") }},
		{"no_subtitles", "prefer_platform", "no_matching_subtitle", func(a *sourceFixtureAdapter) { a.tracks = nil }},
		{"damaged", "prefer_platform", "subtitle_unusable", func(a *sourceFixtureAdapter) { a.raw = []byte("<i><d>danmaku is not subtitle</d></i>") }},
		{"changed_cid", "prefer_platform", "source_identity_mismatch", func(a *sourceFixtureAdapter) { a.wrongCID = true }},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, repos, _, task, payload, adapter, producer, uploads := sourceWorkerFixture(t, true, test.policy)
			if test.change != nil {
				test.change(adapter)
			}
			producer.err = errors.New("RabbitMQ unavailable")
			if err := c.handleTextSource(context.Background(), payload); err != nil {
				t.Fatal(err)
			}
			current, _ := repos.Task.FindByID(task.ID)
			sourceJob, _ := repos.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeTextSource)
			asrJob, _ := repos.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeTranscribe)
			if current.LastJobType != model.TaskJobTypeTranscribe || current.Status != model.TaskStatusQueued || current.ActiveTextSourceID != "" || asrJob == nil || asrJob.ProcessingToken == "" || asrJob.LeaseKind != model.TaskLeaseKindDispatch || sourceJob.Status != model.TaskStatusCompleted || sourceJob.ProcessingToken != "" || sourceJob.LastErrorCode != test.reason {
				t.Fatalf("handoff not atomic: task=%+v source=%+v asr=%+v", current, sourceJob, asrJob)
			}
			if producer.asrCalls != 1 || *uploads != 0 || strings.Contains(sourceJob.LastErrorMsg, "secret") {
				t.Fatal("fallback uploaded invalid source or exposed provider error")
			}
			if test.policy == "force_asr" && adapter.probes != 0 {
				t.Fatal("force_asr performed a subtitle probe")
			}
			if err := c.handleTextSource(context.Background(), payload); err != nil {
				t.Fatal(err)
			}
			if producer.asrCalls != 1 {
				t.Fatal("duplicate delegation")
			}
		})
	}
}

func TestTextSourceRollbackCannotPublishSourceWithoutSummaryDispatch(t *testing.T) {
	c, repos, db, task, payload, _, producer, _ := sourceWorkerFixture(t, true, "prefer_platform")
	cleaned := 0
	c.deleteRawSubtitle = func(_ context.Context, key string) error {
		if !strings.HasSuffix(key, ".srt") {
			t.Fatal("wrong raw cleanup target")
		}
		cleaned++
		return nil
	}
	if err := db.Migrator().DropTable(&model.SummaryPart{}); err != nil {
		t.Fatal(err)
	}
	if err := c.handleTextSource(context.Background(), payload); err != nil {
		t.Fatal(err)
	}
	current, _ := repos.Task.FindByID(task.ID)
	var sourceCount, transcriptionCount int64
	db.Model(&model.VideoTextSource{}).Where("task_id = ?", task.ID).Count(&sourceCount)
	db.Model(&model.VideoTranscription{}).Where("task_id = ?", task.ID).Count(&transcriptionCount)
	if current.ActiveTextSourceID != "" || sourceCount != 0 || transcriptionCount != 0 || producer.summaryCalls != 0 || cleaned != 1 {
		t.Fatalf("partial publication leaked: task=%+v source=%d projection=%d", current, sourceCount, transcriptionCount)
	}
	job, _ := repos.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeSummary)
	if job != nil {
		t.Fatalf("partial summary dispatch leaked: %+v", job)
	}
}

func TestTextSourceStaleWorkerCannotUploadOrPublish(t *testing.T) {
	c, repos, _, task, payload, adapter, producer, uploads := sourceWorkerFixture(t, true, "prefer_platform")
	adapter.beforeFetch = func() {
		current, _ := repos.Task.FindByID(task.ID)
		now := time.Now()
		_, err := repos.PrepareInitialTaskDispatch(repository.InitialTaskDispatchRequest{Task: current, AllowedStatuses: []int8{model.TaskStatusRunning}, JobType: model.TaskJobTypeTextSource, Stage: model.TaskStageTextSource, Now: now, LeaseUntil: now.Add(time.Minute), Token: "replacement-dispatch"})
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := c.handleTextSource(context.Background(), payload); err != nil {
		t.Fatal(err)
	}
	current, _ := repos.Task.FindByID(task.ID)
	if current.ProcessingToken != "replacement-dispatch" || current.ActiveTextSourceID != "" || *uploads != 0 || producer.summaryCalls != 0 {
		t.Fatalf("stale worker wrote: %+v", current)
	}
}

func TestTextSourceDoesNotEnableLegacyImportsAndExplicitRetryRouting(t *testing.T) {
	c, repos, db, task, payload, adapter, _, uploads := sourceWorkerFixture(t, true, "prefer_platform")
	if err := db.Model(&model.VideoTask{}).Where("id = ?", task.ID).Update("processing_intent_json", "").Error; err != nil {
		t.Fatal(err)
	}
	if err := c.handleTextSource(context.Background(), payload); err != nil {
		t.Fatal(err)
	}
	current, _ := repos.Task.FindByID(task.ID)
	if current.ProcessingToken != "source-dispatch" || adapter.probes != 0 || *uploads != 0 {
		t.Fatal("legacy task started new automatic source flow")
	}
	status, stage := retryDispatchState(model.TaskJobTypeTextSource, model.TaskStageUploaded)
	if status != model.TaskStatusQueued || stage != model.TaskStageTextSource {
		t.Fatal("source retry stage not explicitly registered")
	}
	producer := &sourceFixtureProducer{}
	scheduler := NewRetryScheduler(repos, producer, RetrySchedulerConfig{})
	if err := scheduler.enqueueRetry(ContextWithClaimToken(context.Background(), "source-retry"), model.VideoTask{ID: task.ID, FileMD5: task.FileMD5, LastJobType: model.TaskJobTypeTextSource}); err != nil || producer.token != "source-retry" {
		t.Fatalf("source retry not explicitly routed: %v", err)
	}
}

func TestTextSourceManualImportPublishesWithoutStartingSummary(t *testing.T) {
	c, repos, _, task, payload, _, producer, _ := sourceWorkerFixture(t, false, "prefer_platform")
	if err := c.handleTextSource(context.Background(), payload); err != nil {
		t.Fatal(err)
	}
	current, _ := repos.Task.FindByID(task.ID)
	job, _ := repos.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeSummary)
	if current.ActiveTextSourceID == "" || current.Status != model.TaskStatusCompleted || job != nil || producer.summaryCalls != 0 || producer.asrCalls != 0 {
		t.Fatal("manual source import triggered unrequested summary")
	}
}

func TestTextSourceCancellationDoesNotDelegatePaidASR(t *testing.T) {
	c, repos, _, task, payload, adapter, producer, uploads := sourceWorkerFixture(t, true, "prefer_platform")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	adapter.beforeFetch = cancel
	if err := c.handleTextSource(ctx, payload); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation=%v", err)
	}
	current, _ := repos.Task.FindByID(task.ID)
	job, _ := repos.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeTranscribe)
	if current.LastJobType != model.TaskJobTypeTextSource || current.LeaseKind != model.TaskLeaseKindProcessing || job != nil || *uploads != 0 || producer.asrCalls != 0 {
		t.Fatal("cancelled source worker started another paid job")
	}
}

func TestTextSourceProviderLanguageAliasPublishesWithoutASRFallback(t *testing.T) {
	for _, preferred := range []string{"", "zh-CN"} {
		t.Run(map[string]string{"": "default", "zh-CN": "explicit-Chinese"}[preferred], func(t *testing.T) {
			c, repos, db, task, payload, adapter, producer, _ := sourceWorkerFixture(t, true, "prefer_platform")
			intent, err := processing.Decode(task.ProcessingIntentJSON)
			if err != nil {
				t.Fatal(err)
			}
			intent.Options.PreferredLanguage = preferred
			if err = db.Model(&model.VideoTask{}).Where("id=?", task.ID).Update("processing_intent_json", artifact.JSON(intent)).Error; err != nil {
				t.Fatal(err)
			}
			adapter.tracks[0].Language = "ai-zh"
			adapter.tracks[0].TrackKey = "ai-zh:1"
			adapter.tracks[0].SubtitleKind = "unknown"
			if err = c.handleTextSource(context.Background(), payload); err != nil {
				t.Fatal(err)
			}
			source, err := repos.TextSource.Active(context.Background(), task.UserID, task.ID)
			if err != nil || source == nil || source.Language != "ai-zh" || source.TrackKey != "ai-zh:1" || source.SubtitleKind != "unknown" || producer.summaryCalls != 1 || producer.asrCalls != 0 {
				t.Fatalf("source=%+v producer=%+v err=%v", source, producer, err)
			}
		})
	}
}
