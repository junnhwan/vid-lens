package service

import (
	"context"
	"strings"
	"testing"
	"time"
	"vid-lens/internal/ai"
	"vid-lens/internal/artifact"
	"vid-lens/internal/config"
	"vid-lens/internal/model"
	"vid-lens/internal/processing"
	"vid-lens/internal/repository"
	"vid-lens/internal/summarydoc"
	"vid-lens/internal/textsource"
)

type visualRetryProfiles struct{ manualGenerationProfiles }

func (p *visualRetryProfiles) GetAIProfileByID(owner, id int64) (*ai.Profile, error) {
	if owner != 7 || id != p.profile.ID {
		return nil, ErrAIProfileNotFound
	}
	return &p.profile, nil
}

func visualRetryFixture(t *testing.T) (*generationFixture, *MediaService, *summaryVisualFixture, *recordingMediaProducer, *model.AISummary) {
	t.Helper()
	f := newGenerationFixture(t, false)
	v := enableGenerationVisualFixture(t, f)
	v.foreignSelection = true
	if err := f.svc.Generate(context.Background(), f.task, f.job, f.job.ProcessingToken); err != nil {
		t.Fatal(err)
	}
	if _, err := f.repos.CompleteTaskProcessing(repository.TaskProcessingCompleteRequest{TaskID: f.task.ID, JobType: model.TaskJobTypeSummary, JobStage: model.TaskStageSummarizing, Token: f.job.ProcessingToken, Now: time.Now()}); err != nil {
		t.Fatal(err)
	}
	old, err := f.repos.Summary.FindByTaskID(f.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	// The original inspector fixture uses a sentinel hash. Reuse acceptance
	// needs the real immutable raw hash, as production observations provide.
	var observation model.VideoVisualObservation
	if err = f.db.First(&observation, "id=?", v.selectedID).Error; err != nil {
		t.Fatal(err)
	}
	if err = f.db.Model(&observation).Update("raw_response_hash", artifact.Hash(observation.Observation)).Error; err != nil {
		t.Fatal(err)
	}
	profiles := &visualRetryProfiles{manualGenerationProfiles{importProfileFixture: importProfileFixture{profile: f.profiles.profile}}}
	producer := &recordingMediaProducer{}
	return f, &MediaService{repo: f.repos, profiles: profiles, mq: producer}, v, producer, old
}
func visualRetryInput(old *model.AISummary) SummaryVisualRetryRequest {
	return SummaryVisualRetryRequest{ExpectedGenerationID: old.GenerationID, ExpectedGeneratedVersion: old.GeneratedVersion, ExpectedContentDigest: old.ContentDigest, ExpectedSourceID: old.SourceID, ExpectedSourceDigest: old.SourceDigest, AuthorizeNewVisualBudget: true}
}
func claimVisualRetry(t *testing.T, f *generationFixture) (*model.VideoTask, *model.TaskJob) {
	t.Helper()
	job, err := f.repos.TaskJob.FindByTaskAndType(f.task.ID, model.TaskJobTypeSummary)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	claim, err := f.repos.ClaimTaskProcessing(repository.TaskProcessingClaimRequest{TaskID: f.task.ID, JobType: model.TaskJobTypeSummary, Stage: model.TaskStageSummarizing, MessageToken: job.ProcessingToken, NewToken: "visual-only-worker", Now: now, LeaseUntil: now.Add(time.Hour)})
	if err != nil || claim.Outcome != repository.TaskLeaseAcquired {
		t.Fatalf("claim %+v %v", claim, err)
	}
	job, _ = f.repos.TaskJob.FindByTaskAndType(f.task.ID, model.TaskJobTypeSummary)
	task, _ := f.repos.Task.FindByID(f.task.ID)
	return task, job
}

func TestSummaryVisualRetryReusesTextAndInspectedCheckpointWithoutAnotherVLM(t *testing.T) {
	f, svc, v, producer, old := visualRetryFixture(t)
	ctx := context.Background()
	parentStore := repository.NewSummaryGenerationExecutionStore(f.repos, 7, f.task.ID, old.GenerationID)
	parent, _ := parentStore.GetExecution(ctx, 7, old.GenerationID)
	parentBytes := artifact.JSON(parent)
	head := model.SummaryRevision{ID: "user-kept-visual-retry", UserID: 7, TaskID: f.task.ID, Version: 1, Content: "用户保留的正文", ContentHashKind: model.SummaryHashMarkdown, BaseGeneratedHash: old.ContentDigest, BaseGeneratedHashKind: old.ContentHashKind, Origin: "manual"}
	if err := f.db.Create(&head).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Create(&model.SummaryRevisionHead{UserID: 7, TaskID: f.task.ID, CurrentRevisionID: head.ID, Version: 1}).Error; err != nil {
		t.Fatal(err)
	}
	var headBefore model.SummaryRevisionHead
	f.db.First(&headBefore, "user_id=? AND task_id=?", 7, f.task.ID)
	f.db.First(&head, "id=?", head.ID)
	headRowBytes := artifact.JSON(head)
	headPointerBytes := artifact.JSON(headBefore)
	accepted, err := svc.RequestSummaryVisualRetry(ctx, 7, f.task.ID, "visual-once", visualRetryInput(old))
	if err != nil {
		t.Fatal(err)
	}
	replay, err := svc.RequestSummaryVisualRetry(ctx, 7, f.task.ID, "visual-once", visualRetryInput(old))
	if err != nil || replay.GenerationID != accepted.GenerationID || len(producer.analyzes) != 1 {
		t.Fatalf("replay %+v %v", replay, err)
	}
	view, err := NewSummaryGenerationReadService(f.repos).Latest(ctx, 7, f.task.ID)
	if err != nil || view.TextState != "ready" || view.ResultGenerationID != old.GenerationID || view.GeneratedContentDigest != old.ContentDigest || view.Operation != processing.OperationVisualRetry {
		t.Fatalf("queued reuse %+v %v", view, err)
	}
	task, job := claimVisualRetry(t, f)
	v.foreignSelection = false
	beforeText, beforeVision := len(f.chat.calls), v.inspectCalls
	if err = f.svc.Generate(ctx, task, job, job.ProcessingToken); err != nil {
		t.Fatal(err)
	}
	current, _ := f.repos.Summary.FindByTaskID(f.task.ID)
	if current.GenerationID != accepted.GenerationID || current.GeneratedVersion != old.GeneratedVersion+1 || current.DocumentJSON == old.DocumentJSON || len(f.chat.calls) != beforeText || v.inspectCalls != beforeVision {
		t.Fatalf("not visual-only / repeated VLM text=%d/%d vision=%d/%d", beforeText, len(f.chat.calls), beforeVision, v.inspectCalls)
	}
	view, err = NewSummaryGenerationReadService(f.repos).Latest(ctx, 7, f.task.ID)
	if err != nil || view.VisualState != "complete" || view.ResultGenerationID != accepted.GenerationID || view.VisualRetryAvailable {
		t.Fatalf("finished %+v %v", view, err)
	}
	effective, err := f.repos.SummaryRevision.Effective(ctx, 7, f.task.ID)
	if err != nil || effective.Content != head.Content || effective.SourceStatus != "needs_merge" {
		t.Fatalf("user head %+v %v", effective, err)
	}
	var headAfter model.SummaryRevisionHead
	f.db.First(&headAfter, "user_id=? AND task_id=?", 7, f.task.ID)
	var revisionAfter model.SummaryRevision
	f.db.First(&revisionAfter, "id=?", head.ID)
	if artifact.JSON(headAfter) != headPointerBytes || artifact.JSON(revisionAfter) != headRowBytes {
		t.Fatal("user head/revision bytes changed")
	}
	var run model.AgentRun
	if err = f.db.First(&run, "id=?", accepted.GenerationID).Error; err != nil {
		t.Fatal(err)
	}
	if run.VisionCallsUsed != 0 || run.FramesUsed != 0 || run.LLMCallsUsed != 1 {
		t.Fatalf("reuse budget %+v", run)
	}
	newEvents, _ := NewSummaryGenerationReadService(f.repos).Events(ctx, 7, f.task.ID, accepted.GenerationID, 0, 100)
	textReused := false
	for _, event := range newEvents.Events {
		if event.Type == "run.text_ready" || event.Data["activity_id"] == "publish" {
			t.Fatal("invented text publication")
		}
		if event.Type == "run.text_reused" {
			textReused = true
		}
	}
	if !textReused {
		t.Fatal("missing actual reuse receipt")
	}
	if err = f.svc.Generate(ctx, task, job, job.ProcessingToken); err != nil || len(f.chat.calls) != beforeText || v.inspectCalls != beforeVision {
		t.Fatal("completed redelivery spent calls")
	}
	parentAfter, _ := parentStore.GetExecution(ctx, 7, old.GenerationID)
	if artifact.JSON(parentAfter) != parentBytes {
		t.Fatal("old run/steps/calls/events changed")
	}
}

func TestSummaryVisualRetrySourceCancelProfileAndOldLeaseFence(t *testing.T) {
	for _, kind := range []string{"source", "cancel", "profile", "old_lease", "deleted"} {
		t.Run(kind, func(t *testing.T) {
			f, svc, _, _, old := visualRetryFixture(t)
			ctx := context.Background()
			body := artifact.JSON(old)
			if _, err := svc.RequestSummaryVisualRetry(ctx, 7, f.task.ID, "fenced-"+kind, visualRetryInput(old)); err != nil {
				t.Fatal(err)
			}
			task, job := claimVisualRetry(t, f)
			calls := 0
			f.svc.WithVisualEnricher(generationFixtureVisual(func(context.Context, *model.AISummary) error {
				calls++
				if kind == "cancel" {
					f.db.Model(&model.AgentRun{}).Where("id=?", job.GenerationID).Update("cancel_requested_at", time.Now())
				}
				return artifact.Err("visual_not_beneficial", 422)
			}))
			switch kind {
			case "source":
				next := *f.source
				next.Cues = append([]textsource.Cue(nil), f.source.Cues...)
				end := *next.Cues[0].EndMS + 50
				next.Cues[0].EndMS = &end
				if _, err := f.repos.PublishTextSource(ctx, repository.PublishTextSourceRequest{UserID: 7, TaskID: f.task.ID, ExpectedActiveSourceID: f.source.ID, Snapshot: next}, nil); err != nil {
					t.Fatal(err)
				}
			case "deleted":
				if err := f.db.Delete(&model.VideoTask{}, task.ID).Error; err != nil {
					t.Fatal(err)
				}
			case "profile":
				f.profiles.profile.VisionModel = "changed-after-acceptance"
			}
			token := job.ProcessingToken
			if kind == "old_lease" {
				token = f.job.ProcessingToken
			}
			if err := f.svc.Generate(ctx, task, job, token); err == nil {
				t.Fatal("fence allowed execution/completion")
			}
			if kind != "cancel" && calls != 0 {
				t.Fatal("stale attempt invoked visual hook")
			}
			current, _ := f.repos.Summary.FindByTaskID(f.task.ID)
			if artifact.JSON(current) != body || len(f.chat.calls) != 1 {
				t.Fatal("fence rewrote/re-generated old body")
			}
		})
	}
}

func TestSummaryVisualRetryDeploymentSwitchesAndReplay(t *testing.T) {
	f, svc, v, _, old := visualRetryFixture(t)
	ctx := context.Background()
	input := visualRetryInput(old)
	svc.WithSummaryExperience(disabledSummaryPolicy())
	_, err := svc.RequestSummaryVisualRetry(ctx, 7, f.task.ID, "blocked-new", input)
	assertSummaryDisabled(t, err)
	disabled := false
	svc.WithSummaryExperience(config.SummaryExperienceConfig{VisualEnrichmentEnabled: &disabled})
	if _, err = svc.RequestSummaryVisualRetry(ctx, 7, f.task.ID, "blocked-visual", input); err == nil {
		t.Fatal("disabled visual accepted new attempt")
	}
	svc.WithSummaryExperience(config.SummaryExperienceConfig{})
	accepted, err := svc.RequestSummaryVisualRetry(ctx, 7, f.task.ID, "accepted-before-switch", input)
	if err != nil {
		t.Fatal(err)
	}
	svc.WithSummaryExperience(disabledSummaryPolicy())
	replay, err := svc.RequestSummaryVisualRetry(ctx, 7, f.task.ID, "accepted-before-switch", input)
	if err != nil || replay.GenerationID != accepted.GenerationID {
		t.Fatal("switch blocked immutable replay")
	}
	task, job := claimVisualRetry(t, f)
	f.svc.WithVisualEnricher(WithSummaryVisualPolicy(v, disabledSummaryPolicy()))
	if err = f.svc.Generate(ctx, task, job, job.ProcessingToken); err != nil {
		t.Fatal(err)
	}
	view, err := NewSummaryGenerationReadService(f.repos).Latest(ctx, 7, f.task.ID)
	if err != nil || view.VisualState != "skipped" || view.FallbackReason != "visual_disabled" || view.TextState != "ready" {
		t.Fatalf("disabled accepted %+v %v", view, err)
	}
	current, _ := f.repos.Summary.FindByTaskID(f.task.ID)
	if artifact.JSON(current) != artifact.JSON(old) || v.inspectCalls != 1 || v.chatCalls != 2 {
		t.Fatal("switch spent visual calls or changed text")
	}
}

type visualRetryChatHook struct {
	v               *summaryVisualFixture
	beforeSelection func()
	readSelection   func([]ai.ChatMessage)
}

func (c *visualRetryChatHook) Chat(ctx context.Context, m []ai.ChatMessage) (string, error) {
	if strings.Contains(m[0].Content, "已实际看图") && c.readSelection != nil {
		c.readSelection(m)
	}
	if strings.Contains(m[0].Content, "已实际看图") && c.beforeSelection != nil {
		fn := c.beforeSelection
		c.beforeSelection = nil
		fn()
	}
	return c.v.Chat(ctx, m)
}

func TestSummaryVisualRetryReparsesImmutableFencedObservationWithoutVLM(t *testing.T) {
	f, svc, v, _, old := visualRetryFixture(t)
	ctx := context.Background()
	raw := "```json\n{\"facts\":[\"最大连接数是十\"],\"gaps\":[\"未显示超时参数\"]}\n```"
	var observation model.VideoVisualObservation
	if err := f.db.First(&observation, "id=?", v.selectedID).Error; err != nil {
		t.Fatal(err)
	}
	observation.Observation = raw
	observation.RawResponseHash = artifact.Hash(raw)
	if err := f.db.Save(&observation).Error; err != nil {
		t.Fatal(err)
	}
	var step model.AgentStep
	if err := f.db.Where("run_id=? AND step_id LIKE ?", old.GenerationID, "visual-inspect-%").First(&step).Error; err != nil {
		t.Fatal(err)
	}
	var candidates []summaryVisualCandidate
	if err := artifact.Decode([]byte(step.ResultCheckpoint), &candidates); err != nil || len(candidates) != 1 {
		t.Fatalf("checkpoint %v", err)
	}
	candidates[0].Observation = raw
	candidates[0].Facts = []string{raw}
	candidates[0].Gaps = nil
	step.ResultCheckpoint = artifact.JSON(candidates)
	if err := f.db.Save(&step).Error; err != nil {
		t.Fatal(err)
	}
	before := artifact.JSON(observation)
	if _, err := svc.RequestSummaryVisualRetry(ctx, 7, f.task.ID, "reparse-fenced", visualRetryInput(old)); err != nil {
		t.Fatal(err)
	}
	task, job := claimVisualRetry(t, f)
	v.foreignSelection = false
	seen := false
	chat := &visualRetryChatHook{v: v, readSelection: func(messages []ai.ChatMessage) {
		joined := artifact.JSON(messages)
		if !strings.Contains(joined, `\"facts\":[\"最大连接数是十\"]`) || !strings.Contains(joined, `\"gaps\":[\"未显示超时参数\"]`) {
			t.Fatalf("unparsed candidate: %s", joined)
		}
		seen = true
	}}
	f.svc.WithVisualEnricher(NewSummaryVisualService(f.repos, visualRetryFactoryHook{v, chat}, v))
	if err := f.svc.Generate(ctx, task, job, job.ProcessingToken); err != nil {
		t.Fatal(err)
	}
	var after model.VideoVisualObservation
	f.db.First(&after, "id=?", observation.ID)
	if !seen || v.inspectCalls != 1 || artifact.JSON(after) != before {
		t.Fatal("raw observation changed or another VLM spent")
	}
}

type visualRetryFactoryHook struct {
	v    *summaryVisualFixture
	chat *visualRetryChatHook
}

func (f visualRetryFactoryHook) NewChatClient(ai.Profile) (ai.ChatClient, error) { return f.chat, nil }
func (f visualRetryFactoryHook) NewVisionClient(p ai.Profile) (ai.VisionClient, error) {
	return f.v.NewVisionClient(p)
}

func TestSummaryVisualRetryPublicationCASRollsBackNewScreenshot(t *testing.T) {
	f, svc, v, _, old := visualRetryFixture(t)
	ctx := context.Background()
	v.foreignSelection = false
	if _, err := svc.RequestSummaryVisualRetry(ctx, 7, f.task.ID, "cas-race", visualRetryInput(old)); err != nil {
		t.Fatal(err)
	}
	task, job := claimVisualRetry(t, f)
	chat := &visualRetryChatHook{v: v, beforeSelection: func() {
		doc, _ := summarydoc.Parse([]byte(old.DocumentJSON))
		doc.Title = "并发新原稿"
		raw, _ := summarydoc.CanonicalJSON(doc)
		digest, _ := summarydoc.Digest(doc)
		markdown, _ := summarydoc.Markdown(doc)
		if err := f.db.Model(&model.AISummary{}).Where("task_id=?", task.ID).Updates(map[string]any{"document_json": string(raw), "content_digest": digest, "content": markdown, "generated_version": old.GeneratedVersion + 1}).Error; err != nil {
			t.Fatal(err)
		}
	}}
	f.svc.WithVisualEnricher(NewSummaryVisualService(f.repos, visualRetryFactoryHook{v, chat}, v))
	if err := f.svc.Generate(ctx, task, job, job.ProcessingToken); err == nil {
		t.Fatal("stale CAS attempt completed")
	}
	var refs int64
	f.db.Model(&model.SummaryScreenshotRef{}).Where("generation_id=?", job.GenerationID).Count(&refs)
	if refs != 0 {
		t.Fatal("screenshot escaped failed CAS transaction")
	}
	current, _ := f.repos.Summary.FindByTaskID(task.ID)
	if current.GenerationID != old.GenerationID || current.GeneratedVersion != old.GeneratedVersion+1 || !strings.Contains(current.Content, "并发新原稿") {
		t.Fatal("retry overwrote raced base")
	}
}

func TestSummaryVisualRetryFailureRetainsExactBodyAndDoesNotRepeatTextOrTags(t *testing.T) {
	f, svc, v, _, old := visualRetryFixture(t)
	ctx := context.Background()
	before := artifact.JSON(old)
	_, err := svc.RequestSummaryVisualRetry(ctx, 7, f.task.ID, "failed-visual-once", visualRetryInput(old))
	if err != nil {
		t.Fatal(err)
	}
	task, job := claimVisualRetry(t, f)
	if err = f.svc.Generate(ctx, task, job, job.ProcessingToken); err != nil {
		t.Fatal(err)
	}
	after, _ := f.repos.Summary.FindByTaskID(f.task.ID)
	if artifact.JSON(after) != before || len(f.chat.calls) != 1 || v.inspectCalls != 1 {
		t.Fatal("visual failure changed/re-generated existing text")
	}
	view, err := NewSummaryGenerationReadService(f.repos).Latest(ctx, 7, f.task.ID)
	if err != nil || view.TextState != "ready" || view.VisualState != "failed" || view.ResultGenerationID != old.GenerationID || view.FallbackReason != "invalid_visual_selection" {
		t.Fatalf("failure state %+v %v", view, err)
	}
	var tags int64
	f.db.Model(&model.SummaryTagIntent{}).Where("generation_id=?", job.GenerationID).Count(&tags)
	if tags != 0 {
		t.Fatal("retry ran tag recipe")
	}
}

func TestSummaryVisualRetryRejectsStaleOwnerAndUnauthorizedBudget(t *testing.T) {
	f, svc, _, producer, old := visualRetryFixture(t)
	ctx := context.Background()
	input := visualRetryInput(old)
	input.AuthorizeNewVisualBudget = false
	if _, err := svc.RequestSummaryVisualRetry(ctx, 7, f.task.ID, "no-budget", input); err == nil {
		t.Fatal("unacknowledged new attempt accepted")
	}
	input = visualRetryInput(old)
	if _, err := svc.RequestSummaryVisualRetry(ctx, 8, f.task.ID, "other-owner", input); err == nil {
		t.Fatal("cross-owner accepted")
	}
	input.ExpectedGeneratedVersion++
	if _, err := svc.RequestSummaryVisualRetry(ctx, 7, f.task.ID, "stale-version", input); err == nil {
		t.Fatal("stale version accepted")
	}
	if len(producer.analyzes) != 0 {
		t.Fatal("invalid requests dispatched")
	}
	var receipts int64
	f.db.Model(&model.ImportRequest{}).Where("action=?", "summary_visual_retry").Count(&receipts)
	if receipts != 0 {
		t.Fatal("receipt escaped acceptance rollback")
	}
}

func TestSummaryVisualRetrySecondExplicitAttemptKeepsBaseEvidenceAndPreviousSpend(t *testing.T) {
	f, svc, v, _, old := visualRetryFixture(t)
	ctx := context.Background()
	first, err := svc.RequestSummaryVisualRetry(ctx, 7, f.task.ID, "first-failed-attempt", visualRetryInput(old))
	if err != nil {
		t.Fatal(err)
	}
	task, job := claimVisualRetry(t, f)
	if err = f.svc.Generate(ctx, task, job, job.ProcessingToken); err != nil {
		t.Fatal(err)
	}
	store := repository.NewSummaryGenerationExecutionStore(f.repos, 7, task.ID, first.GenerationID)
	before, _ := store.GetExecution(ctx, 7, first.GenerationID)
	if _, err = f.repos.CompleteTaskProcessing(repository.TaskProcessingCompleteRequest{TaskID: task.ID, JobType: model.TaskJobTypeSummary, JobStage: model.TaskStageSummarizing, Token: job.ProcessingToken, Now: time.Now()}); err != nil {
		t.Fatal(err)
	}
	second, err := svc.RequestSummaryVisualRetry(ctx, 7, task.ID, "second-explicit-attempt", visualRetryInput(old))
	if err != nil || second.GenerationID == first.GenerationID {
		t.Fatalf("new explicit attempt %+v %v", second, err)
	}
	task, job = claimVisualRetry(t, f)
	var frozen processing.GenerationSnapshot
	if err = artifact.Decode([]byte(job.InputSnapshotJSON), &frozen); err != nil {
		t.Fatal(err)
	}
	if frozen.VisualRetry.CheckpointGenerationID != old.GenerationID || !strings.Contains(frozen.VisualRetry.PreviousSpendJSON, first.GenerationID) {
		t.Fatal("reused evidence lost or previous attempt spend misidentified")
	}
	v.foreignSelection = false
	if err = f.svc.Generate(ctx, task, job, job.ProcessingToken); err != nil {
		t.Fatal(err)
	}
	after, _ := store.GetExecution(ctx, 7, first.GenerationID)
	if v.inspectCalls != 1 || len(f.chat.calls) != 1 || artifact.JSON(before) != artifact.JSON(after) {
		t.Fatal("second user attempt repeated VLM/text or rewrote previous failed attempt")
	}
}

func TestSummaryVisualRetryRedeliveryDoesNotResetPersistedBudgetOrDeadline(t *testing.T) {
	f, svc, _, _, old := visualRetryFixture(t)
	ctx := context.Background()
	if _, err := svc.RequestSummaryVisualRetry(ctx, 7, f.task.ID, "duration-bound-attempt", visualRetryInput(old)); err != nil {
		t.Fatal(err)
	}
	task, job := claimVisualRetry(t, f)
	var frozen processing.GenerationSnapshot
	if err := artifact.Decode([]byte(job.InputSnapshotJSON), &frozen); err != nil {
		t.Fatal(err)
	}
	lease := repository.SummaryGenerationLease{UserID: 7, TaskID: task.ID, GenerationID: job.GenerationID, SourceID: frozen.SourceID, SourceDigest: frozen.SourceDigest, LeaseToken: job.ProcessingToken}
	run := &model.AgentRun{ID: job.GenerationID, UserID: 7, TaskID: task.ID, SubjectKind: model.AgentRunSubjectSummaryGeneration, SubjectID: job.GenerationID, ExecutionKind: "artifact", ScopeType: model.ChatScopeVideo, RecipeVersion: processing.Recipe, ProfileSnapshot: artifact.JSON(map[string]any{"profile_id": f.profiles.profile.ID, "fingerprint": frozen.Intent.ProfileFingerprint}), PolicySnapshot: artifact.JSON(map[string]any{"recipe": processing.Recipe, "options": frozen.Intent.Options, "source_id": f.source.ID, "source_digest": f.source.SourceDigest, "operation": processing.OperationVisualRetry, "parent_generation_id": frozen.VisualRetry.ParentGenerationID, "new_budget_authorized": true, "previous_spend": frozen.VisualRetry.PreviousSpendJSON}), BudgetSnapshot: frozen.Intent.BudgetJSON, Status: model.AgentRunStatusRunning, Stage: "visual_retry", MaxSteps: 10, MaxToolCalls: 8, MaxLLMCalls: 8, MaxVisionCalls: 2, MaxFrames: 2, MaxAttemptsPerStep: 2, MaxDurationMs: 1, LLMCallsUsed: 2, VisionCallsUsed: 1, FramesUsed: 1}
	if _, err := f.repos.StartSummaryGeneration(ctx, lease, run); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Model(run).Update("created_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	f.db.First(run, "id=?", run.ID)
	calls := 0
	f.svc.WithVisualEnricher(generationFixtureVisual(func(context.Context, *model.AISummary) error { calls++; return nil }))
	for i := 0; i < 2; i++ {
		if err := f.svc.Generate(ctx, task, job, job.ProcessingToken); err == nil || err.Error() != "duration_limit" {
			t.Fatalf("redelivery reset elapsed duration or changed authority: %v", err)
		}
	}
	var after model.AgentRun
	f.db.First(&after, "id=?", job.GenerationID)
	if after.LLMCallsUsed != 2 || after.VisionCallsUsed != 1 || after.FramesUsed != 1 || after.MaxDurationMs != 1 || !after.CreatedAt.Equal(run.CreatedAt) || calls != 0 {
		t.Fatal("accepted attempt budget/deadline reset on redelivery")
	}
}
