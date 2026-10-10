package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"vid-lens/internal/ai"
	"vid-lens/internal/artifact"
	"vid-lens/internal/config"
	"vid-lens/internal/model"
	"vid-lens/internal/processing"
	"vid-lens/internal/repository"
	"vid-lens/internal/summarydoc"
	"vid-lens/internal/textsource"
)

type generationFixtureProfiles struct {
	profile   ai.Profile
	requested []int64
}

func (p *generationFixtureProfiles) GetAIProfileByID(owner, id int64) (*ai.Profile, error) {
	p.requested = append(p.requested, id)
	if owner != 7 || id != p.profile.ID {
		return nil, errors.New("profile not owned")
	}
	return &p.profile, nil
}

type generationFixtureChat struct {
	source     *textsource.Snapshot
	generation string
	calls      []string
	failedOnce bool
	failAt     int
	invalid    int
	afterCall  func()
	covered    []summaryGenerationCue
	candidates []repository.TagCandidate
}

func (c *generationFixtureChat) Chat(ctx context.Context, messages []ai.ChatMessage) (string, error) {
	input := messages[len(messages)-1].Content
	c.calls = append(c.calls, input)
	if c.failAt == len(c.calls) && !c.failedOnce {
		c.failedOnce = true
		return "", errors.New("retryable fixture interruption")
	}
	if c.invalid > 0 {
		c.invalid--
		return `{"document":{"content":"Markdown is not the canonical document"}}`, nil
	}
	if rows, ok := decodeSummaryGenerationCues(input); ok {
		c.covered = append(c.covered, rows...)
	}
	doc := summarydoc.Document{SchemaVersion: summarydoc.SchemaVersion, DocumentID: c.generation, SourceID: c.source.ID, SourceDigest: c.source.SourceDigest, MediaRevision: c.source.Identity.MediaFingerprint, PresentationMode: "text", Title: "来源摘要", Overview: "概括配置方法与限制。"}
	for _, cue := range c.source.Cues {
		if !strings.Contains(input, cue.ID) {
			continue
		}
		doc.Blocks = append(doc.Blocks, summarydoc.Block{ID: "block-" + cue.ID, Order: len(doc.Blocks), Title: "章节", BodyMarkdown: "验证结论 " + cue.ID, SourceRefs: []summarydoc.SourceRef{{SourceID: c.source.ID, CueIDs: []string{cue.ID}, StartMS: cue.StartMS, EndMS: cue.EndMS, TimingMethod: cue.TimingMethod}}})
	}
	if c.afterCall != nil {
		fn := c.afterCall
		c.afterCall = nil
		fn()
	}
	return artifact.JSON(map[string]any{"public_title": "核对来源并整理主要结论", "public_summary": "已保留章节的来源依据。", "document": doc, "tag_candidates": c.candidates}), nil
}

type generationFixtureFactory struct{ client *generationFixtureChat }

func (f generationFixtureFactory) NewChatClient(ai.Profile) (ai.ChatClient, error) {
	return f.client, nil
}

type generationFixture struct {
	db       *gorm.DB
	repos    *repository.Repositories
	task     *model.VideoTask
	job      *model.TaskJob
	source   *textsource.Snapshot
	profiles *generationFixtureProfiles
	chat     *generationFixtureChat
	svc      *SummaryGenerationService
}

func newGenerationFixture(t *testing.T, long bool) *generationFixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	repos := repository.NewRepositories(db)
	profile := ai.Profile{ID: 19, LLMProvider: "openai_compatible", LLMBaseURL: "https://example.com/v1", LLMModel: "fixture-summary", LLMAPIKey: "fixture-secret", LLMContextTokens: 8192}
	if long {
		profile.LLMContextTokens = 4096
	}
	budget, _ := config.DefaultAgentBudgetConfig().Resolve(nil)
	opts, _ := processing.Normalize(processing.Options{AutoSummary: true, AutoTagsEnabled: true}, false)
	intent := processing.Intent{ID: uuid.NewString(), Version: 1, GenerationID: uuid.NewString(), Options: opts, ProfileID: 19, ProfileFingerprint: processing.FingerprintProfile(profile), SummaryPreference: "只总结来源支持的结论", RecipeVersion: processing.Recipe, BudgetJSON: artifact.JSON(budget), PolicyJSON: artifact.JSON(struct {
		Recipe  string             `json:"recipe"`
		Options processing.Options `json:"options"`
	}{processing.Recipe, opts})}
	task := &model.VideoTask{UserID: 7, FileMD5: "summary-media", Filename: "lesson.mp4", FileURL: "videos/lesson.mp4", Status: model.TaskStatusCompleted, Stage: model.TaskStageUploaded, ProcessingIntentJSON: artifact.JSON(intent), MediaIdentityJSON: `{"platform":"bilibili","bvid":"BV-source","cid":123,"part_index":2,"media_fingerprint":"summary-media"}`}
	if err = db.Create(task).Error; err != nil {
		t.Fatal(err)
	}
	var cues []textsource.Cue
	count := 2
	if long {
		count = 9
	}
	for index := 0; index < count; index++ {
		text := "解释配置的条件与限制。"
		if long {
			text = strings.Repeat("来源说明配置条件与限制，", 180)
		}
		start, end := int64(index*3000), int64(index*3000+2000)
		cues = append(cues, textsource.Cue{ID: "cue-" + string(rune('a'+index)), Order: index, Text: text, RawText: text, StartMS: &start, EndMS: &end, TimingMethod: "subtitle_cue"})
	}
	canonical, err := textsource.Canonicalize(textsource.Snapshot{Kind: textsource.KindSubtitle, Identity: textsource.Identity{Platform: "bilibili", BVID: "BV-source", CID: 123, PartIndex: 2, MediaFingerprint: task.FileMD5}, TrackKey: "zh", Language: "zh", SubtitleKind: "unknown", KindBasis: "fixture", ParserVersion: "fixture-v1", Cues: cues}, textsource.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	published, err := repos.PublishTextSource(context.Background(), repository.PublishTextSourceRequest{UserID: task.UserID, TaskID: task.ID, Snapshot: canonical}, nil)
	if err != nil {
		t.Fatal(err)
	}
	source, err := repos.TextSource.Read(context.Background(), task.UserID, task.ID, published.ID)
	if err != nil {
		t.Fatal(err)
	}
	task, err = repos.Task.FindByID(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	frozen := processing.GenerationSnapshot{Intent: intent, SourceID: source.ID, SourceDigest: source.SourceDigest, ExpectedGeneratedHashKind: model.SummaryHashMarkdown}
	until := time.Now().Add(time.Hour)
	job := &model.TaskJob{TaskID: task.ID, UserID: task.UserID, JobType: model.TaskJobTypeSummary, GenerationID: intent.GenerationID, InputSourceID: source.ID, InputText: source.CanonicalText, InputSnapshotJSON: artifact.JSON(frozen), Status: model.TaskStatusRunning, Stage: model.TaskStageSummarizing, ProcessingToken: "generation-worker", LeaseKind: model.TaskLeaseKindProcessing, LeaseExpiresAt: &until, MaxRetries: 3}
	if err = db.Create(job).Error; err != nil {
		t.Fatal(err)
	}
	profiles := &generationFixtureProfiles{profile: profile}
	chat := &generationFixtureChat{source: source, generation: intent.GenerationID}
	return &generationFixture{db: db, repos: repos, task: task, job: job, source: source, profiles: profiles, chat: chat, svc: NewSummaryGenerationService(repos, profiles, generationFixtureFactory{chat})}
}
func TestSummaryGenerationShortPublishesCanonicalDocumentAndReplaysWithoutCalls(t *testing.T) {
	f := newGenerationFixture(t, false)
	ctx := context.Background()
	if err := f.svc.Generate(ctx, f.task, f.job, f.job.ProcessingToken); err != nil {
		t.Fatal(err)
	}
	if len(f.chat.calls) != 1 || len(f.profiles.requested) != 1 || f.profiles.requested[0] != 19 {
		t.Fatalf("not one frozen-profile call: %d %+v", len(f.chat.calls), f.profiles.requested)
	}
	summary, err := f.repos.Summary.FindByTaskID(f.task.ID)
	if err != nil || summary == nil {
		t.Fatal(err)
	}
	doc, err := summarydoc.Parse([]byte(summary.DocumentJSON))
	if err != nil {
		t.Fatal(err)
	}
	markdown, _ := summarydoc.Markdown(doc)
	if summary.Content != markdown || summary.GeneratedVersion != 1 || summary.GenerationID != f.job.GenerationID {
		t.Fatal("canonical JSON and projection diverged")
	}
	store := repository.NewSummaryGenerationExecutionStore(f.repos, 7, f.task.ID, f.job.GenerationID)
	run, err := store.GetRun(ctx, 7, f.job.GenerationID)
	if err != nil || run == nil || run.SessionID != 0 || run.SubjectKind != model.AgentRunSubjectSummaryGeneration || run.Status != model.AgentRunStatusCompleted || run.LLMCallsUsed != 1 {
		t.Fatalf("run=%+v %v", run, err)
	}
	if strings.Contains(run.ProfileSnapshot, "fixture-secret") {
		t.Fatal("run persisted credentials")
	}
	if legacy, _ := f.repos.AgentExecution.GetRun(ctx, 7, f.job.GenerationID); legacy != nil {
		t.Fatal("generation exposed as a chat run")
	}
	if other, err := store.GetRun(ctx, 8, f.job.GenerationID); err == nil || other != nil {
		t.Fatal("cross-owner generation read")
	}
	var events []model.RunEvent
	if err = f.db.Where("run_id=?", run.ID).Order("seq").Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	if len(events) < 4 {
		t.Fatal("missing real execution events")
	}
	for index, event := range events {
		if event.Seq != int64(index+1) {
			t.Fatal("event sequence gap")
		}
	}
	f.profiles.profile.LLMModel = "default changed"
	if err = f.svc.Generate(ctx, f.task, f.job, f.job.ProcessingToken); err != nil || len(f.chat.calls) != 1 {
		t.Fatalf("publication replay spent new call: %v", err)
	}
}
func TestSummaryGenerationLongCoverageAndRecoverableCheckpoints(t *testing.T) {
	f := newGenerationFixture(t, true)
	f.chat.failAt = 2
	if err := f.svc.Generate(context.Background(), f.task, f.job, f.job.ProcessingToken); err == nil {
		t.Fatal("fixture interruption was not exercised")
	}
	if summary, _ := f.repos.Summary.FindByTaskID(f.task.ID); summary != nil {
		t.Fatal("partial leaf published as complete")
	}
	if err := f.svc.Generate(context.Background(), f.task, f.job, f.job.ProcessingToken); err != nil {
		t.Fatal(err)
	}
	var covered strings.Builder
	for _, cue := range f.chat.covered {
		covered.WriteString(cue.Text)
	}
	var expected strings.Builder
	for _, cue := range f.source.Cues {
		expected.WriteString(cue.Text)
	}
	if covered.String() != expected.String() {
		t.Fatalf("long-source coverage duplicated or omitted: got%d want%d", covered.Len(), expected.Len())
	}
	if len(f.chat.calls) < 4 {
		t.Fatal("long source did not segment and reduce")
	}
	summary, _ := f.repos.Summary.FindByTaskID(f.task.ID)
	doc, err := summarydoc.Parse([]byte(summary.DocumentJSON))
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Blocks) != len(f.source.Cues) {
		t.Fatalf("reducer lost source sections: %d", len(doc.Blocks))
	}
}
func TestSummaryGenerationRepairIsBoundedAndPrivateMetadataCannotPublish(t *testing.T) {
	for _, invalid := range []int{1, 2} {
		t.Run(string(rune('0'+invalid)), func(t *testing.T) {
			f := newGenerationFixture(t, false)
			f.chat.invalid = invalid
			err := f.svc.Generate(context.Background(), f.task, f.job, f.job.ProcessingToken)
			if (invalid == 1 && err != nil) || (invalid == 2 && err == nil) {
				t.Fatalf("repair result=%v", err)
			}
			if len(f.chat.calls) != 2 {
				t.Fatal("repair did not stay bounded")
			}
			summary, _ := f.repos.Summary.FindByTaskID(f.task.ID)
			if invalid == 2 && summary != nil {
				t.Fatal("invalid model Markdown fixture published")
			}
		})
	}
}
func TestSummaryGenerationChangedProfileOrSourceCannotPublish(t *testing.T) {
	f := newGenerationFixture(t, false)
	f.profiles.profile.LLMModel = "changed"
	if err := f.svc.Generate(context.Background(), f.task, f.job, f.job.ProcessingToken); err == nil || len(f.chat.calls) != 0 {
		t.Fatal("changed frozen profile used")
	}
	f = newGenerationFixture(t, false)
	f.chat.afterCall = func() { f.task = publishReadFixture(t, f.repos, f.task, 12) }
	if err := f.svc.Generate(context.Background(), f.task, f.job, f.job.ProcessingToken); err == nil {
		t.Fatal("source refresh during model call published stale generation")
	}
	if summary, _ := f.repos.Summary.FindByTaskID(f.task.ID); summary != nil {
		t.Fatal("stale-source result survived")
	}
}

type generationFixtureVisual func(context.Context, *model.AISummary) error

func (fn generationFixtureVisual) Enrich(ctx context.Context, _ *model.VideoTask, _ *model.TaskJob, _ processing.GenerationSnapshot, _ ai.Profile, _ *textsource.Snapshot, base *model.AISummary, _ string) error {
	return fn(ctx, base)
}

func (f *generationFixture) options(t *testing.T, update func(*processing.Options)) {
	t.Helper()
	var frozen processing.GenerationSnapshot
	if err := json.Unmarshal([]byte(f.job.InputSnapshotJSON), &frozen); err != nil {
		t.Fatal(err)
	}
	update(&frozen.Intent.Options)
	frozen.Intent.PolicyJSON = artifact.JSON(struct {
		Recipe  string             `json:"recipe"`
		Options processing.Options `json:"options"`
	}{processing.Recipe, frozen.Intent.Options})
	f.task.ProcessingIntentJSON = artifact.JSON(frozen.Intent)
	f.job.InputSnapshotJSON = artifact.JSON(frozen)
	if err := f.db.Model(f.task).Update("processing_intent_json", f.task.ProcessingIntentJSON).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Model(f.job).Update("input_snapshot_json", f.job.InputSnapshotJSON).Error; err != nil {
		t.Fatal(err)
	}
}

func TestSummaryGenerationTextReadyBeforeVisualAndTruthfulFallback(t *testing.T) {
	for _, failure := range []struct{ code, state string }{{"vision_unavailable", "skipped"}, {"invalid_visual_plan", "failed"}, {"private-provider-secret", "failed"}} {
		t.Run(failure.code, func(t *testing.T) {
			f := newGenerationFixture(t, false)
			f.options(t, func(options *processing.Options) { options.SummaryVisualEnabled = true })
			f.svc.WithVisualEnricher(generationFixtureVisual(func(ctx context.Context, base *model.AISummary) error {
				persisted, err := f.repos.Summary.FindByTaskID(f.task.ID)
				if err != nil || persisted == nil || persisted.ID != base.ID || base.DocumentJSON == "" {
					t.Fatal("visual started before canonical text commit")
				}
				store := repository.NewSummaryGenerationExecutionStore(f.repos, 7, f.task.ID, f.job.GenerationID)
				run, err := store.GetRun(ctx, 7, f.job.GenerationID)
				if err != nil || run.Status != "running" || run.Stage != "visual_enrichment" || run.MaxFrames != 8 || run.MaxVisionCalls != 8 || run.MaxVisualCalls != 3 {
					t.Fatalf("visual run budgets/stage %+v %v", run, err)
				}
				return artifact.Err(failure.code, 422)
			}))
			if err := f.svc.Generate(context.Background(), f.task, f.job, f.job.ProcessingToken); err != nil {
				t.Fatal(err)
			}
			var events []model.RunEvent
			if err := f.db.Where("run_id=? AND type=?", f.job.GenerationID, "run.completed").Find(&events).Error; err != nil {
				t.Fatal(err)
			}
			if len(events) != 1 {
				t.Fatal("missing terminal receipt")
			}
			var receipt map[string]any
			if err := json.Unmarshal([]byte(events[0].DataJSON), &receipt); err != nil {
				t.Fatal(err)
			}
			wantReason := failure.code
			if failure.code == "private-provider-secret" {
				wantReason = "visual_enrichment_failed"
			}
			if receipt["text_state"] != "ready" || receipt["visual_state"] != failure.state || receipt["fallback_reason"] != wantReason {
				t.Fatalf("receipt=%+v", receipt)
			}
			if len(f.chat.calls) != 1 {
				t.Fatal("fallback generated text twice")
			}
		})
	}
}

func TestSummaryGenerationInterruptedVisualResumesWithoutRepeatingTextOrInvalidTags(t *testing.T) {
	f := newGenerationFixture(t, false)
	f.options(t, func(options *processing.Options) { options.SummaryVisualEnabled = true })
	for index := 0; index < 6; index++ {
		f.chat.candidates = append(f.chat.candidates, repository.TagCandidate{Name: "配置", Reason: "主题"})
	}
	ctx, cancel := context.WithCancel(context.Background())
	f.svc.WithVisualEnricher(generationFixtureVisual(func(context.Context, *model.AISummary) error { cancel(); return context.Canceled }))
	if err := f.svc.Generate(ctx, f.task, f.job, f.job.ProcessingToken); !errors.Is(err, context.Canceled) {
		t.Fatalf("interruption=%v", err)
	}
	f.svc.WithVisualEnricher(generationFixtureVisual(func(context.Context, *model.AISummary) error { return artifact.Err("visual_not_beneficial", 422) }))
	if err := f.svc.Generate(context.Background(), f.task, f.job, f.job.ProcessingToken); err != nil {
		t.Fatal(err)
	}
	if len(f.chat.calls) != 1 {
		t.Fatal("resume repeated text model call")
	}
	summary, _ := f.repos.Summary.FindByTaskID(f.task.ID)
	intent, err := f.repos.UserTag.ReadGenerationIntent(context.Background(), 7, f.task.ID, f.job.GenerationID, summary.GeneratedVersion)
	if err != nil || intent == nil || intent.Status != "failed" || intent.ErrorCode != "invalid_tag_candidates" {
		t.Fatalf("classification failure lost on resume: %+v %v", intent, err)
	}
	if err = f.svc.Generate(context.Background(), f.task, f.job, f.job.ProcessingToken); err != nil {
		t.Fatal(err)
	}
	intent, _ = f.repos.UserTag.ReadGenerationIntent(context.Background(), 7, f.task.ID, f.job.GenerationID, summary.GeneratedVersion)
	if intent.Status != "failed" || intent.ErrorCode != "invalid_tag_candidates" {
		t.Fatal("completed replay erased classification failure")
	}
}

func TestSummaryGenerationCancellationBlocksPublicationAndBoundStoreWrites(t *testing.T) {
	f := newGenerationFixture(t, false)
	f.chat.afterCall = func() {
		if err := f.db.Model(&model.AgentRun{}).Where("id=?", f.job.GenerationID).Update("cancel_requested_at", time.Now()).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := f.svc.Generate(context.Background(), f.task, f.job, f.job.ProcessingToken); err == nil {
		t.Fatal("cancelled generation published")
	}
	if summary, _ := f.repos.Summary.FindByTaskID(f.task.ID); summary != nil {
		t.Fatal("cancelled output survived")
	}
	store := repository.NewSummaryGenerationExecutionStore(f.repos, 7, f.task.ID+1, f.job.GenerationID)
	if _, err := store.ClaimStep(context.Background(), repository.AgentStepClaimRequest{UserID: 7, RunID: f.job.GenerationID, StepID: "foreign"}); err == nil {
		t.Fatal("store wrote arbitrary same-owner task run")
	}
	if _, err := store.CompleteStep(context.Background(), repository.AgentStepCompletion{UserID: 7, RunID: f.job.GenerationID}); err == nil {
		t.Fatal("store completed arbitrary run")
	}
	if _, err := store.FailStep(context.Background(), repository.AgentStepFailure{UserID: 7, RunID: f.job.GenerationID}); err == nil {
		t.Fatal("store failed arbitrary run")
	}
	if err := store.MarkFinalEvidenceRefs(context.Background(), 7, f.job.GenerationID, nil); err == nil {
		t.Fatal("store marked arbitrary evidence")
	}
}

func TestSummaryGenerationClassificationIndependentFromTextPublication(t *testing.T) {
	for _, test := range []struct {
		name       string
		enabled    bool
		candidates []repository.TagCandidate
		status     string
	}{
		{"domain", true, []repository.TagCandidate{{Name: "配置", Reason: "来源主题"}}, "completed"},
		{"disabled", false, []repository.TagCandidate{{Name: "配置", Reason: "来源主题"}}, "completed"},
		{"invalid-name", true, []repository.TagCandidate{{Name: "", Reason: "来源主题"}}, "failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newGenerationFixture(t, false)
			f.options(t, func(options *processing.Options) { options.AutoTagsEnabled = test.enabled })
			f.chat.candidates = test.candidates
			if err := f.svc.Generate(context.Background(), f.task, f.job, f.job.ProcessingToken); err != nil {
				t.Fatal(err)
			}
			summary, _ := f.repos.Summary.FindByTaskID(f.task.ID)
			if summary == nil || summary.DocumentJSON == "" {
				t.Fatal("classification invalidated text")
			}
			intent, err := f.repos.UserTag.ReadGenerationIntent(context.Background(), 7, f.task.ID, f.job.GenerationID, summary.GeneratedVersion)
			if err != nil || intent == nil || intent.Status != test.status {
				t.Fatalf("intent=%+v %v", intent, err)
			}
			state, err := f.repos.UserTag.TaskState(context.Background(), 7, f.task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !test.enabled && (len(state.Assignments) != 0 || len(state.Suggestions) != 0) {
				t.Fatal("disabled tags classified")
			}
			if test.name == "domain" && len(state.Assignments) != 1 {
				t.Fatalf("missing frozen candidate: %+v", state)
			}
		})
	}
}
