//go:build real_llm

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"vid-lens/internal/ai"
	"vid-lens/internal/artifact"
	"vid-lens/internal/config"
	appdb "vid-lens/internal/database"
	"vid-lens/internal/model"
	"vid-lens/internal/pkg/secret"
	"vid-lens/internal/repository"
)

// Reads the source database only. All generation, checkpoints and versions are
// written to artifactFixture's isolated database, never to the user's records.
func TestArtifactRealSourceReplay(t *testing.T) {
	path, runID := os.Getenv("VIDLENS_ARTIFACT_REPLAY_CONFIG"), os.Getenv("VIDLENS_ARTIFACT_REPLAY_RUN")
	if path == "" || runID == "" {
		t.Skip("set replay config and run ID to opt into real model calls")
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	connection, err := appdb.OpenPostgres(context.Background(), cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	prod := connection.GORM
	// Make the production connection read only, including accidental writes.
	connection.SQL.SetMaxOpenConns(1)
	if err = prod.Exec("SET default_transaction_read_only=on").Error; err != nil {
		t.Fatal(err)
	}
	var original model.AgentRun
	if err = prod.First(&original, "id=?", runID).Error; err != nil {
		t.Fatal(err)
	}
	repos := repository.NewRepositories(prod)
	passphrase := cfg.Security.APIKeySecret
	if passphrase == "" {
		passphrase = cfg.JWT.Secret
	}
	codec, err := secret.NewCodecFromPassphrase(passphrase)
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("VIDLENS_ARTIFACT_REPLAY_POSTGRES") == "1" {
		isolated := cfg.Database
		isolated.DBName = "vidlens_artifact_test"
		dsn, err := appdb.PostgresDSN(isolated)
		if err != nil {
			t.Fatal(err)
		}
		t.Setenv("VIDLENS_ARTIFACT_TEST_DSN", dsn)
	}
	profiles := NewAIProfileService(repos.AIProfile, codec, nil)
	resolved, err := profiles.GetDefaultConversationProfile(original.UserID)
	if err != nil {
		t.Fatal(err)
	}
	realClient, err := ai.NewFactory().NewChatClient(*resolved.Profile)
	if err != nil {
		t.Fatal(err)
	}
	var number atomic.Int32
	allowedEvidence := map[string]bool{}
	svc, db, _ := artifactFixture(t, func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages       []ai.ChatMessage `json:"messages"`
			MaxTokens      int64            `json:"max_tokens"`
			EnableThinking *bool            `json:"enable_thinking"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			w.WriteHeader(500)
			return
		}
		index := number.Add(1)
		if index == 1 && os.Getenv("VIDLENS_ARTIFACT_REPLAY_COMPARE") == "1" {
			var baseline ai.ChatUsage
			beforeCtx := ai.WithChatBudget(r.Context(), request.MaxTokens, func(u ai.ChatUsage) { baseline = u })
			before, beforeErr := collectStudyResponse(beforeCtx, realClient, request.Messages)
			t.Logf("baseline answer_bytes=%d completion=%d reasoning=%d error=%v", len(before), baseline.CompletionTokens, baseline.ReasoningTokens, beforeErr)
		}
		var usage ai.ChatUsage
		ctx := ai.WithChatBudget(r.Context(), request.MaxTokens, func(u ai.ChatUsage) { usage = u })
		if request.EnableThinking != nil && !*request.EnableThinking {
			ctx = ai.WithStructuredJSON(ctx)
		}
		raw, callErr := collectStudyResponse(ctx, realClient, request.Messages)
		t.Logf("call=%d input_bytes=%d answer_bytes=%d requested_output=%d prompt=%d completion=%d reasoning=%d error=%v", index, len(artifact.JSON(request.Messages)), len(raw), request.MaxTokens, usage.PromptTokens, usage.CompletionTokens, usage.ReasoningTokens, callErr)
		if dir := os.Getenv("VIDLENS_ARTIFACT_REPLAY_CAPTURE"); dir != "" {
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Error(err)
			}
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("call-%d.json", index)), []byte(raw), 0600); err != nil {
				t.Error(err)
			}
		}
		// Expose precise syntax issues without retaining secrets or reasoning text.
		if !isStudyOrganizationPrompt(request.Messages[0].Content) {
			var body artifact.Body
			decoder := json.NewDecoder(strings.NewReader(raw))
			decoder.DisallowUnknownFields()
			decodeErr := decoder.Decode(&body)
			t.Logf("call=%d strict_decode=%v blocks=%d", index, decodeErr, len(body.Blocks))
			if decodeErr == nil {
				t.Logf("call=%d validation=%v warnings_nil=%t", index, body.Validate(allowedEvidence), body.Warnings == nil)
				for _, block := range body.Blocks {
					for _, ref := range block.EvidenceRefs {
						if !allowedEvidence[ref.EvidenceID] {
							t.Logf("call=%d unknown_evidence=%s", index, ref.EvidenceID)
						}
					}
					if block.EvidenceRefs == nil {
						t.Logf("call=%d block=%s refs_nil", index, block.BlockID)
					}
				}
			}
		}
		if callErr != nil {
			w.WriteHeader(502)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\n", artifact.JSON(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": raw}}}}))
		fmt.Fprintf(w, "data: %s\n\n", artifact.JSON(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "delta": map[string]any{}}}, "usage": map[string]int64{"prompt_tokens": usage.PromptTokens, "completion_tokens": usage.CompletionTokens}}))
		fmt.Fprint(w, "data: [DONE]\n\n")
	})
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	task, err := repos.Task.FindByID(original.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	transcription, chunks, err := taskTranscriptSource(repos, task)
	if err != nil {
		t.Fatal(err)
	}
	frames, err := repos.VisualFrame.ListByTaskID(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Where("task_id=?", 42).Delete(&model.VideoTranscription{}).Error; err != nil {
		t.Fatal(err)
	}
	task.ID, task.UserID = 42, 7
	task.AssetID, task.Asset = nil, nil
	if err = db.Save(task).Error; err != nil {
		t.Fatal(err)
	}
	if transcription != nil {
		transcription.ID, transcription.TaskID = 0, 42
		if err = db.Create(transcription).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, chunk := range chunks {
		chunk.ID, chunk.TaskID = 0, 42
		if err = db.Create(&chunk).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, frame := range frames {
		frame.ID, frame.TaskID = 0, 42
		if err = db.Create(&frame).Error; err != nil {
			t.Fatal(err)
		}
	}
	request := artifactRequest()
	request.Goal = original.Goal
	if err = db.Model(&model.UserAIProfile{}).Where("user_id=?", 7).Updates(map[string]any{"llm_model": resolved.Profile.LLMModel, "llm_context_tokens": resolved.Profile.LLMContextTokens}).Error; err != nil {
		t.Fatal(err)
	}
	svc.profiles.WithAgentBudgetConfig(cfg.AgentBudget)
	view, err := svc.Submit(ctx, 7, "real-replay", request, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, frozenRequest, err := svc.repos.Artifact.Run(ctx, 7, view.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, frozenItems, err := svc.repos.Artifact.Snapshot(ctx, 7, frozenRequest.ManifestID)
	if err != nil {
		t.Fatal(err)
	}
	_, sourceRequest, err := repos.Artifact.Run(ctx, original.UserID, original.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, originalItems, err := repos.Artifact.Snapshot(ctx, original.UserID, sourceRequest.ManifestID)
	if err != nil {
		t.Fatal(err)
	}
	material := func(items []model.SourceSnapshotItem) string {
		rows := make([]map[string]any, 0, len(items))
		for _, item := range items {
			rows = append(rows, map[string]any{"content": item.Content, "modality": item.Modality, "start_ms": item.StartMS, "end_ms": item.EndMS, "time_range_status": item.TimeRangeStatus})
		}
		return artifact.JSON(rows)
	}
	if material(frozenItems) != material(originalItems) {
		t.Fatal("current source differs from the reported failed run's frozen material")
	}
	t.Logf("source items=%d segments=%d model=%s", len(frozenItems), len(studySegments(frozenItems)), resolved.Profile.LLMModel)
	for i, item := range frozenItems {
		allowedEvidence[item.ID] = true
		allowedEvidence[fmt.Sprintf("e%d", i+1)] = true
	}
	if err = svc.ExecuteArtifact(ctx, view.ID); err != nil {
		t.Fatal(err)
	}
	final, err := svc.Run(ctx, 7, view.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("result status=%s stage=%s usage=%+v", final.Status, final.Stage, final.Usage)
	if final.Status != "completed" || final.Result == nil {
		t.Fatalf("real source failed: %+v", final)
	}
	detail, err := svc.Get(ctx, 7, final.ArtifactID)
	if err != nil || detail.Version == nil || len(detail.Version.Body.Blocks) == 0 {
		t.Fatalf("missing persisted body: %v", err)
	}
	if detail.HeadVersion != 1 || detail.Version.ID != final.Result.VersionID || final.Progress.CoveredSegments != final.Progress.TotalSegments {
		t.Fatal("generation did not publish one complete immutable version")
	}
	var checkpoints []model.AgentStep
	if err = db.Where("run_id=? AND status=? AND step_id LIKE ?", view.ID, "completed", artifact.Recipe+".segment.%").Find(&checkpoints).Error; err != nil {
		t.Fatal(err)
	}
	originalBlocks := map[string]artifact.Block{}
	for _, step := range checkpoints {
		parts := strings.Split(step.StepID, ".")
		segment, err := strconv.Atoi(parts[2])
		if err != nil {
			t.Fatal(err)
		}
		var body artifact.Body
		if err = artifact.Decode([]byte(step.ResultCheckpoint), &body); err != nil {
			t.Fatal(err)
		}
		for _, block := range body.Blocks {
			originalBlocks[fmt.Sprintf("s%d-%s", segment+1, block.BlockID)] = block
		}
	}
	seenOriginal := map[string]bool{}
	for _, block := range detail.Version.Body.Blocks {
		for _, id := range block.SourceBlockIDs {
			original, ok := originalBlocks[id]
			if !ok || seenOriginal[id] || original.Type != block.Type || !strings.EqualFold(strings.TrimSpace(original.Title), strings.TrimSpace(block.Title)) || original.Type == "section" && len(block.SourceBlockIDs) != 1 {
				t.Fatalf("organization lost chapter/concept identity: block=%s source=%s", block.BlockID, id)
			}
			seenOriginal[id] = true
		}
	}
	if len(seenOriginal) != len(originalBlocks) {
		t.Fatalf("source blocks omitted: saved=%d generated=%d", len(seenOriginal), len(originalBlocks))
	}
	// Specific regression observed in this reported source: an older model
	// called a past September 2026 date "future" and inferred fictional data.
	for _, phrase := range []string{"未来时间戳", "未来日期", "未来模型", "未来命名", "尚未发布", "虚构场景"} {
		if !strings.Contains(material(originalItems), phrase) && strings.Contains(artifact.JSON(detail.Version.Body), phrase) {
			t.Fatalf("reproduced unsupported temporal claim: %s", phrase)
		}
	}
	refs := 0
	for _, block := range detail.Version.Body.Blocks {
		for _, ref := range block.EvidenceRefs {
			if len(ref.EvidenceID) != 36 {
				t.Fatalf("prompt alias persisted: %s", ref.EvidenceID)
			}
			if _, err := svc.Evidence(ctx, 7, detail.Version.ManifestID, ref.EvidenceID); err != nil {
				t.Fatal(err)
			}
			refs++
		}
	}
	t.Logf("saved version verified: head=%d blocks=%d generated_blocks=%d canonical_refs=%d covered=%d/%d", detail.HeadVersion, len(detail.Version.Body.Blocks), len(originalBlocks), refs, final.Progress.CoveredSegments, final.Progress.TotalSegments)
	if dir := os.Getenv("VIDLENS_ARTIFACT_REPLAY_CAPTURE"); dir != "" {
		if err := os.WriteFile(filepath.Join(dir, "persisted-body.json"), []byte(artifact.JSON(detail.Version.Body)), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("persisted blocks=%d warnings=%v", len(detail.Version.Body.Blocks), detail.Version.Body.Warnings)
}
