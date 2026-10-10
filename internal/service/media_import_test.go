package service

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/minio/minio-go/v7"
	"github.com/redis/go-redis/v9"
	"vid-lens/internal/ai"
	"vid-lens/internal/artifact"
	"vid-lens/internal/config"
	"vid-lens/internal/model"
	"vid-lens/internal/processing"
)

type importProfileFixture struct {
	profile ai.Profile
	calls   int
	err     error
}

func (p *importProfileFixture) GetDefaultAIProfile(int64) (*ai.Profile, error) {
	p.calls++
	return &p.profile, p.err
}
func (p *importProfileFixture) GetConversationProfile(owner, id int64) (*ResolvedConversationProfile, error) {
	p.calls++
	if p.err != nil {
		return nil, p.err
	}
	if id != 0 && id != p.profile.ID {
		return nil, ErrAIProfileNotFound
	}
	budget, _ := config.DefaultAgentBudgetConfig().Resolve(nil)
	return &ResolvedConversationProfile{Profile: &p.profile, ProfileID: p.profile.ID, EffectiveAgentBudget: budget}, nil
}

type importProducer struct {
	recordingMediaProducer
	sourceTasks []int64
	sourceErr   error
}

func (p *importProducer) EnqueueTextSource(ctx context.Context, id int64, md5 string) error {
	p.sourceTasks = append(p.sourceTasks, id)
	return p.sourceErr
}

func importFixture(t *testing.T) (*MediaService, *importProfileFixture, *importProducer) {
	t.Helper()
	profile := &importProfileFixture{profile: ai.Profile{ID: 19, LLMProvider: "openai_compatible", LLMBaseURL: "https://example.com/v1", LLMModel: "summary-first", LLMAPIKey: "never-persist-this-key", LLMContextTokens: 32000}}
	producer := &importProducer{}
	svc := &MediaService{repo: newMediaTestRepositories(t), profiles: profile, mq: producer, tools: config.ToolsConfig{AllowedVideoHosts: []string{"bilibili.com", "b23.tv", "youtube.com"}}, remoteURLResolver: fakeRemoteURLResolver{"www.bilibili.com": {net.ParseIP("8.8.8.8")}, "www.youtube.com": {net.ParseIP("8.8.8.8")}}}
	return svc, profile, producer
}
func importOpts(key string) ImportOptions {
	return ImportOptions{Options: processing.Options{AutoSummary: true, AutoTagsEnabled: true}, IdempotencyKey: key}
}
func assertImportStatus(t *testing.T, err error, status int) {
	t.Helper()
	var typed *artifact.Error
	if !errors.As(err, &typed) || typed.Status != status {
		t.Fatalf("error=%v,want HTTP %d", err, status)
	}
}

func TestAutoImportURLFreezesProfileWithoutASRAndReplaysWithoutResolvingDefault(t *testing.T) {
	svc, profile, producer := importFixture(t)
	ctx := context.Background()
	if err := svc.repo.AIProfile.SetPromptPreference(7, "summary", "focus on verified conclusions"); err != nil {
		t.Fatal(err)
	}
	url := "https://www.bilibili.com/video/BV1xx411c7mD?p=2&tracking=private"
	result, err := svc.UploadByURLWithOptions(ctx, 7, url, importOpts("first-url"))
	if err != nil {
		t.Fatal(err)
	}
	task, err := svc.repo.Task.FindByID(result.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	intent, err := processing.Decode(task.ProcessingIntentJSON)
	if err != nil {
		t.Fatal(err)
	}
	if result.GenerationID != intent.GenerationID || intent.ProfileID != 19 || intent.ProfileFingerprint != processing.FingerprintProfile(profile.profile) || intent.Options.OutputMode != "auto" || intent.Options.TextSourcePolicy != "prefer_platform" || !intent.Options.AutoTagsEnabled || intent.PolicyJSON == "" || intent.BudgetJSON == "" || intent.GenerationID == "" || intent.SummaryPreference != "focus on verified conclusions" {
		t.Fatalf("missing frozen acceptance: %+v", intent)
	}
	if strings.Contains(task.ProcessingIntentJSON, profile.profile.LLMAPIKey) || strings.Contains(task.SourceURL, "private") {
		t.Fatal("sensitive configuration persisted")
	}
	if task.SourceURL != "https://www.bilibili.com/video/BV1xx411c7mD?p=2" || len(producer.downloads) != 1 || producer.downloads[0].claimToken == "" || producer.downloads[0].budgetID == "" {
		t.Fatalf("dispatch=%+v task=%+v", producer.downloads, task)
	}
	lease := task.LeaseVersion
	frozen := task.ProcessingIntentJSON
	if err := svc.repo.AIProfile.SetPromptPreference(7, "summary", "changed preference"); err != nil {
		t.Fatal(err)
	}
	profile.err = errors.New("default changed or unavailable")
	svc.remoteURLResolver = fakeRemoteURLResolver{} // replay must not depend on external DNS.
	replay, err := svc.UploadByURLWithOptions(ctx, 7, url, importOpts("first-url"))
	if err != nil || replay.TaskID != result.TaskID || profile.calls != 1 || len(producer.downloads) != 1 {
		t.Fatalf("replay=%+v err=%v calls=%d dispatch=%d", replay, err, profile.calls, len(producer.downloads))
	}
	after, _ := svc.repo.Task.FindByID(result.TaskID)
	if after.ProcessingIntentJSON != frozen || after.LeaseVersion != lease {
		t.Fatal("replay mutated frozen import or lease")
	}
	changed := importOpts("first-url")
	changed.SummaryInstruction = "different instruction"
	_, err = svc.UploadByURLWithOptions(ctx, 7, url, changed)
	assertImportStatus(t, err, 409)
	_, err = svc.UploadByURLWithOptions(ctx, 7, "https://www.youtube.com/watch?v=changed", importOpts("first-url"))
	assertImportStatus(t, err, 409)
}

func TestAutoImportURLValidationAndOwnerIsolation(t *testing.T) {
	svc, _, producer := importFixture(t)
	ctx := context.Background()
	url := "https://www.bilibili.com/video/BV1xx411c7mD"
	_, err := svc.UploadByURLWithOptions(ctx, 7, url, importOpts(""))
	assertImportStatus(t, err, 400)
	_, err = svc.UploadByURLWithOptions(ctx, 7, "https://www.youtube.com/watch?v=123", importOpts("unsupported"))
	assertImportStatus(t, err, 400)
	one, err := svc.UploadByURLWithOptions(ctx, 7, url, importOpts("scoped"))
	if err != nil {
		t.Fatal(err)
	}
	two, err := svc.UploadByURLWithOptions(ctx, 8, url, importOpts("scoped"))
	if err != nil {
		t.Fatal(err)
	}
	if one.TaskID == two.TaskID || len(producer.downloads) != 2 {
		t.Fatal("idempotency crossed owner boundary")
	}
	legacy, err := svc.UploadByURLWithOptions(ctx, 7, "https://www.youtube.com/watch?v=123", ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	task, _ := svc.repo.Task.FindByID(legacy.TaskID)
	if task.ProcessingIntentJSON != "" {
		t.Fatal("legacy upload started new automatic flow")
	}
}

func TestAutoImportUnavailableProfileDoesNotCreateTaskOrIdempotencyRecord(t *testing.T) {
	svc, profile, producer := importFixture(t)
	ctx := context.Background()
	url := "https://www.bilibili.com/video/BV1xx411c7mD"
	profile.profile.LLMAPIKey = ""
	_, err := svc.UploadByURLWithOptions(ctx, 7, url, importOpts("profile-retry"))
	assertImportStatus(t, err, 422)
	profile.profile.LLMAPIKey = "restored-key"
	result, err := svc.UploadByURLWithOptions(ctx, 7, url, importOpts("profile-retry"))
	if err != nil || result == nil || len(producer.downloads) != 1 {
		t.Fatalf("failed acceptance consumed identity: %+v %v", result, err)
	}
}

func TestAutoImportPublishFailureKeepsAcceptedTaskAndDoesNotDoubleEnqueueOnReplay(t *testing.T) {
	svc, _, producer := importFixture(t)
	ctx := context.Background()
	url := "https://www.bilibili.com/video/BV1xx411c7mD"
	producer.downloadErr = errors.New("transport unavailable")
	_, err := svc.UploadByURLWithOptions(ctx, 7, url, importOpts("publish-failure"))
	if !errors.Is(err, ErrTaskDispatchUnavailable) {
		t.Fatal(err)
	}
	producer.downloadErr = nil
	result, err := svc.UploadByURLWithOptions(ctx, 7, url, importOpts("publish-failure"))
	if err != nil || result == nil || len(producer.downloads) != 1 {
		t.Fatalf("replay double enqueue: %+v %v", result, err)
	}
	task, _ := svc.repo.Task.FindByID(result.TaskID)
	if task.NextRetryAt == nil {
		t.Fatal("failed publish lost durable retry intent")
	}
}

func TestAutoImportChunkAssetHitDoesNotReuseLegacySummaryAndForcesASR(t *testing.T) {
	svc, profile, producer := importFixture(t)
	ctx := context.Background()
	server := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: server.Addr()})
	svc.rdb = rdb
	t.Cleanup(func() { _ = rdb.Close() })
	svc.storage = &stubObjectStore{}
	md5 := "abababababababababababababababab"
	asset := &model.VideoAsset{FileMD5: md5, ObjectName: "videos/source.mp4", FileSize: 11, ContentType: "video/mp4"}
	if err := svc.repo.Asset.CreateOrRestore(asset); err != nil {
		t.Fatal(err)
	}
	old := &model.VideoTask{UserID: 90, AssetID: &asset.ID, FileMD5: md5, Filename: "old.mp4", Status: model.TaskStatusCompleted}
	if err := svc.repo.Task.Create(old); err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.Transcription.Upsert(&model.VideoTranscription{TaskID: old.ID, FileMD5: md5, Content: "legacy transcription"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.Summary.Upsert(&model.AISummary{TaskID: old.ID, FileMD5: md5, Content: "legacy summary"}); err != nil {
		t.Fatal(err)
	}
	options := importOpts("local-hit")
	options.TextSourcePolicy = "prefer_platform"
	result, err := svc.MergeChunksWithOptions(ctx, 7, md5, "lesson.mp4", 3, 11, 5, options)
	if err != nil {
		t.Fatal(err)
	}
	task, _ := svc.repo.Task.FindByID(result.TaskID)
	intent, err := processing.Decode(task.ProcessingIntentJSON)
	if err != nil || intent.Options.TextSourcePolicy != "force_asr" || task.Status != model.TaskStatusQueued || len(producer.sourceTasks) != 1 || len(producer.downloads) != 0 {
		t.Fatalf("local=%+v intent=%+v err=%v", task, intent, err)
	}
	detail, err := svc.GetTaskDetail(ctx, 7, task.ID)
	if err != nil || detail.Summary != nil || detail.Transcription != nil {
		t.Fatalf("private task inherited shared results: %+v %v", detail, err)
	}
	profile.err = errors.New("profile changed")
	replay, err := svc.MergeChunksWithOptions(ctx, 7, md5, "lesson.mp4", 3, 11, 5, options)
	if err != nil || replay.TaskID != task.ID || len(producer.sourceTasks) != 1 {
		t.Fatalf("local replay: %+v %v", replay, err)
	}
}

type importComposeStore struct {
	stubObjectStore
	composeCount int
}

func (s *importComposeStore) ComposeObject(context.Context, string, []minio.CopySrcOptions) (int64, error) {
	s.composeCount++
	return 11, nil
}
func TestAutoImportNewChunkMergeAcceptsOnceAndRollsBackInvalidAssetTask(t *testing.T) {
	svc, _, producer := importFixture(t)
	ctx := context.Background()
	server := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	svc.rdb = rdb
	store := &importComposeStore{}
	svc.storage = store
	md5 := "bcbcbcbcbcbcbcbcbcbcbcbcbcbcbcbc"
	if _, err := svc.CheckUploadProgress(ctx, md5, 11, 5, 3); err != nil {
		t.Fatal(err)
	}
	for index, size := range []int{5, 5, 1} {
		if err := svc.UploadChunk(ctx, md5, index, make([]byte, size), int64(size)); err != nil {
			t.Fatal(err)
		}
	}
	result, err := svc.MergeChunksWithOptions(ctx, 7, md5, "lesson.mp4", 3, 11, 5, importOpts("compose"))
	if err != nil || result == nil || store.composeCount != 1 || len(producer.sourceTasks) != 1 {
		t.Fatalf("merge=%+v err=%v composed=%d", result, err, store.composeCount)
	}
	// Redis chunks are gone after successful merge; replay depends only on DB identity.
	replay, err := svc.MergeChunksWithOptions(ctx, 7, md5, "lesson.mp4", 3, 11, 5, importOpts("compose"))
	if err != nil || replay.TaskID != result.TaskID || store.composeCount != 1 || len(producer.sourceTasks) != 1 {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	request, _, err := svc.lookupImport(ctx, 7, "merge_chunks", "invalid-asset", importOpts("rollback"), true)
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.freezeImport(7, request); err != nil {
		t.Fatal(err)
	}
	_, err = svc.createTaskFromAssetWithImport(ctx, 7, "lesson.mp4", &model.VideoAsset{ID: 999999}, request)
	if err == nil {
		t.Fatal("expected invalid asset to roll back acceptance")
	}
	prior, err := svc.repo.ImportRequest.Lookup(ctx, 7, request.action, request.key, request.hash)
	if err != nil || prior != nil || len(producer.sourceTasks) != 1 {
		t.Fatalf("rolled-back acceptance persisted: %+v %v", prior, err)
	}
}
