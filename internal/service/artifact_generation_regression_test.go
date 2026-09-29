package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"vid-lens/internal/ai"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
)

func TestStudyChineseTokenAdmissionDoesNotChargeUTF8Bytes(t *testing.T) {
	svc, db, calls := artifactFixture(t, func(w http.ResponseWriter, r *http.Request) { artifactStreamResponse(w, `{"ok":true}`, "stop") })
	ctx := context.Background()
	view, err := svc.Submit(ctx, 7, "chinese-admission", artifactRequest(), nil)
	if err != nil {
		t.Fatal(err)
	}
	run, err := svc.repos.Artifact.Claim(ctx, view.ID, "worker", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Model(run).Updates(map[string]any{"prompt_tokens_used": 40000, "max_prompt_tokens": 65536}).Error; err != nil {
		t.Fatal(err)
	}
	_, req, err := svc.repos.Artifact.Run(ctx, 7, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	client, err := svc.artifactClient(7, req)
	if err != nil {
		t.Fatal(err)
	}
	messages := []ai.ChatMessage{{Role: "system", Content: "只返回JSON"}, {Role: "user", Content: strings.Repeat("视频学习内容", 1600)}}
	_, _, err = svc.callStudyProvider(ctx, run, "worker", "segment", messages, 4096, client)
	if err != nil || calls.Load() != 1 {
		t.Fatalf("Chinese prompt falsely rejected: calls=%d err=%v", calls.Load(), err)
	}
	var step model.AgentStep
	if err = db.Where("run_id=?", run.ID).First(&step).Error; err != nil {
		t.Fatal(err)
	}
	if step.EstimatedPromptTokens >= 20000 {
		t.Fatalf("UTF8 bytes charged as tokens: %d", step.EstimatedPromptTokens)
	}
}

func TestStudyShortCitationIDsPersistCanonicalEvidence(t *testing.T) {
	svc, _, _ := artifactFixture(t, func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []ai.ChatMessage `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if !isStudyOrganizationPrompt(req.Messages[0].Content) {
			var input struct {
				Evidence []studyEvidence `json:"evidence"`
			}
			_ = json.Unmarshal([]byte(req.Messages[1].Content), &input)
			if len(input.Evidence) == 0 || input.Evidence[0].ID != "e1" {
				http.Error(w, "long citation ID", 400)
				return
			}
		}
		r.Body = io.NopCloser(strings.NewReader(artifact.JSON(req)))
		artifactModelResponse(w, r)
	})
	ctx := context.Background()
	view, err := svc.Submit(ctx, 7, "short-citations", artifactRequest(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.ExecuteArtifact(ctx, view.ID); err != nil {
		t.Fatal(err)
	}
	final, err := svc.Run(ctx, 7, view.ID)
	if err != nil || final.Status != "completed" {
		t.Fatalf("short citation workflow failed: %+v %v", final, err)
	}
	detail, err := svc.Get(ctx, 7, final.ArtifactID)
	if err != nil {
		t.Fatal(err)
	}
	ref := detail.Version.Body.Blocks[0].EvidenceRefs[0].EvidenceID
	if len(ref) != 36 {
		t.Fatalf("alias escaped into persisted result: %q", ref)
	}
	if _, err = svc.Evidence(ctx, 7, detail.Version.ManifestID, ref); err != nil {
		t.Fatal(err)
	}
}

func TestStudyQwenUsesBoundedNonThinkingJSONRequest(t *testing.T) {
	var missing bool
	svc, db, _ := artifactFixture(t, func(w http.ResponseWriter, r *http.Request) {
		var req map[string]json.RawMessage
		_ = json.NewDecoder(r.Body).Decode(&req)
		missing = string(req["enable_thinking"]) != "false" || string(req["response_format"]) != `{"type":"json_object"}`
		if missing {
			http.Error(w, "structured request missing", 400)
			return
		}
		// Keep the real service request available to the existing provider fixture.
		raw, _ := json.Marshal(req)
		r.Body = io.NopCloser(strings.NewReader(string(raw)))
		artifactModelResponse(w, r)
	})
	if err := db.Model(&model.UserAIProfile{}).Where("user_id=?", 7).Update("llm_model", "qwen3.6-flash").Error; err != nil {
		t.Fatal(err)
	}
	view, err := svc.Submit(context.Background(), 7, "qwen-json", artifactRequest(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.ExecuteArtifact(context.Background(), view.ID); err != nil {
		t.Fatal(err)
	}
	final, err := svc.Run(context.Background(), 7, view.ID)
	if err != nil || missing || final.Status != "completed" {
		t.Fatalf("unbounded JSON generation: missing=%t run=%+v err=%v", missing, final, err)
	}
}

func TestStudyUnknownShortCitationCannotPublish(t *testing.T) {
	svc, db, _ := artifactFixture(t, func(w http.ResponseWriter, r *http.Request) {
		body := artifact.Body{SchemaVersion: 1, Kind: "study", Title: "事务", Blocks: []artifact.Block{{BlockID: "a", Type: "concept", Title: "提交", Content: "提交生效", ClaimOrigin: "source", EvidenceRefs: []artifact.Ref{{EvidenceID: "e999", Relation: "supports"}}}}, Warnings: []string{}}
		artifactStreamResponse(w, artifact.JSON(body), "stop")
	})
	ctx := context.Background()
	view, err := svc.Submit(ctx, 7, "unknown-citation", artifactRequest(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.ExecuteArtifact(ctx, view.ID); err != nil {
		t.Fatal(err)
	}
	final, err := svc.Run(ctx, 7, view.ID)
	if err != nil || final.Status != "failed" || final.Result != nil {
		t.Fatalf("invented citation published: %+v %v", final, err)
	}
	var steps []model.AgentStep
	if err = db.Where("run_id=?", view.ID).Find(&steps).Error; err != nil {
		t.Fatal(err)
	}
	if len(steps) != 2 || steps[0].ErrorMessage != "citation_id" {
		t.Fatalf("missing validation diagnostic: %+v", steps)
	}
}

func TestStudyBudgetFailurePreservesProgressAndDimension(t *testing.T) {
	svc, db, _ := artifactFixture(t, artifactModelResponse)
	ctx := context.Background()
	view, err := svc.Submit(ctx, 7, "budget-diagnostic", artifactRequest(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Model(&model.AgentRun{}).Where("id=?", view.ID).Updates(map[string]any{"max_completion_tokens": 1, "completion_tokens_used": 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err = svc.ExecuteArtifact(ctx, view.ID); err != nil {
		t.Fatal(err)
	}
	final, err := svc.Run(ctx, 7, view.ID)
	if err != nil || final.Status != "budget_exhausted" || final.Budget.StopReason != "output_tokens" || final.Progress.Stage != "generating" || final.Progress.CoveredSegments != 0 || final.Progress.TotalSegments != 1 {
		t.Fatalf("budget diagnostic lost: %+v %v", final, err)
	}
}

func TestStudyOrganizationFailureSavesAllValidatedSegmentsWithWarning(t *testing.T) {
	svc, _, _ := artifactFixture(t, func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []ai.ChatMessage `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Messages[0].Content == studyIndexSystem {
			// Schema-valid but unusable: the model collapsed every concept and
			// chapter into one block, as observed in the real source replay.
			artifactStreamResponse(w, `{"groups":[{"block_indices":[1,2,3],"title_from":1}]}`, "stop")
			return
		}
		var input struct {
			Evidence []studyEvidence `json:"evidence"`
		}
		_ = json.Unmarshal([]byte(req.Messages[1].Content), &input)
		parent := "chapter"
		refs := []artifact.Ref{{EvidenceID: input.Evidence[0].ID, Relation: "supports"}}
		body := artifact.Body{SchemaVersion: 1, Kind: "study", Title: "事务", Blocks: []artifact.Block{
			{BlockID: parent, Type: "section", Title: "事务操作", Content: "提交与回滚。", ClaimOrigin: "source", EvidenceRefs: refs},
			{BlockID: "commit", ParentID: &parent, Type: "concept", Title: "提交", Content: "提交生效。", ClaimOrigin: "source", EvidenceRefs: refs},
			{BlockID: "rollback", ParentID: &parent, Type: "concept", Title: "回滚", Content: "回滚撤销。", ClaimOrigin: "source", EvidenceRefs: refs},
		}, Warnings: []string{}}
		for i := 0; i < 100; i++ {
			body.Warnings = append(body.Warnings, "需结合原视频核对")
		}
		artifactStreamResponse(w, artifact.JSON(body), "stop")
	})
	ctx := context.Background()
	view, err := svc.Submit(ctx, 7, "organization-fallback", artifactRequest(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.ExecuteArtifact(ctx, view.ID); err != nil {
		t.Fatal(err)
	}
	final, err := svc.Run(ctx, 7, view.ID)
	if err != nil || final.Status != "completed" || final.Progress.CoveredSegments != final.Progress.TotalSegments {
		t.Fatalf("complete notes discarded: %+v %v", final, err)
	}
	detail, err := svc.Get(ctx, 7, final.ArtifactID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(artifact.JSON(detail.Version.Body.Warnings), "organization_kept_segment_structure") || len(detail.Version.Body.Blocks[0].SourceBlockIDs) != 1 {
		t.Fatalf("fallback not explicit: %+v", detail.Version.Body)
	}
	if len(detail.Version.Body.Blocks) != 3 || detail.Version.Body.Blocks[1].ParentID == nil || *detail.Version.Body.Blocks[1].ParentID != detail.Version.Body.Blocks[0].BlockID {
		t.Fatalf("fallback lost chapter/concept structure: %+v", detail.Version.Body.Blocks)
	}
	if _, err = svc.Evidence(ctx, 7, detail.Version.ManifestID, detail.Version.Body.Blocks[0].EvidenceRefs[0].EvidenceID); err != nil {
		t.Fatal(err)
	}
}

func TestStudyOptionalOrganizationDoesNotBlockCompleteSourceCoverage(t *testing.T) {
	svc, db, calls := artifactFixture(t, artifactModelResponse)
	ctx := context.Background()
	view, err := svc.Submit(ctx, 7, "source-only-budget", artifactRequest(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Model(&model.AgentRun{}).Where("id=?", view.ID).Update("max_llm_calls", 1).Error; err != nil {
		t.Fatal(err)
	}
	if err = svc.ExecuteArtifact(ctx, view.ID); err != nil {
		t.Fatal(err)
	}
	final, err := svc.Run(ctx, 7, view.ID)
	if err != nil || final.Status != "completed" || calls.Load() != 1 {
		t.Fatalf("optional call blocked valid notes: %+v calls=%d err=%v", final, calls.Load(), err)
	}
}

func TestStudyV2StillUsesItsOriginalCitationAndCheckpointContract(t *testing.T) {
	svc, db, _ := artifactFixture(t, artifactModelResponse)
	ctx := context.Background()
	view, err := svc.Submit(ctx, 7, "legacy-v2", artifactRequest(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Model(&model.AgentRun{}).Where("id=?", view.ID).Update("recipe_version", artifact.RecipeV2).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Model(&model.GenerationRequest{}).Where("run_id=?", view.ID).Update("recipe", artifact.RecipeV2).Error; err != nil {
		t.Fatal(err)
	}
	if err = svc.ExecuteArtifact(ctx, view.ID); err != nil {
		t.Fatal(err)
	}
	final, err := svc.Run(ctx, 7, view.ID)
	if err != nil || final.Status != "completed" {
		t.Fatalf("legacy v2 failed: %+v %v", final, err)
	}
	var step model.AgentStep
	if err = db.Where("run_id=? AND step_id=?", view.ID, "study-v2.global.0").First(&step).Error; err != nil {
		t.Fatal(err)
	}
}

func TestStudyFreezesReferenceDateAndLimitsClaimsToVideoMaterial(t *testing.T) {
	var received string
	svc, db, _ := artifactFixture(t, func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []ai.ChatMessage `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if !isStudyOrganizationPrompt(req.Messages[0].Content) {
			received = req.Messages[0].Content
		}
		r.Body = io.NopCloser(strings.NewReader(artifact.JSON(req)))
		artifactModelResponse(w, r)
	})
	view, err := svc.Submit(context.Background(), 7, "frozen-reference-date", artifactRequest(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var run model.AgentRun
	if err = db.First(&run, "id=?", view.ID).Error; err != nil {
		t.Fatal(err)
	}
	var policy map[string]any
	_ = json.Unmarshal([]byte(run.PolicySnapshot), &policy)
	if policy["reference_date"] != run.CreatedAt.UTC().Format("2006-01-02") {
		t.Fatalf("request date was not frozen: %+v", policy)
	}
	// Recovery must reuse this frozen date even on a later calendar day.
	policy["reference_date"] = "2026-09-29"
	if err = db.Model(&run).Update("policy_snapshot", artifact.JSON(policy)).Error; err != nil {
		t.Fatal(err)
	}
	if err = svc.ExecuteArtifact(context.Background(), view.ID); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(received, "生成请求日期：2026-09-29。") || !strings.Contains(received, "如果来源自身未声称这些结论，不得加入这样的结论") {
		t.Fatalf("missing source-only date context: %s", received)
	}
}

func TestStudyRepairsUncitedReleaseAndFictionJudgments(t *testing.T) {
	extractions := 0
	svc, db, _ := artifactFixture(t, func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []ai.ChatMessage `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if !isStudyOrganizationPrompt(req.Messages[0].Content) {
			extractions++
			if extractions == 1 {
				var input struct {
					Evidence []studyEvidence `json:"evidence"`
				}
				_ = json.Unmarshal([]byte(req.Messages[1].Content), &input)
				body := artifact.Body{SchemaVersion: 1, Kind: "study", Title: "事务", Blocks: []artifact.Block{{BlockID: "judgment", Type: "note", Title: "真实性警示", Content: "这些模型尚未发布，因此是虚构场景。", ClaimOrigin: "source", EvidenceRefs: []artifact.Ref{{EvidenceID: input.Evidence[0].ID, Relation: "supports"}}}}, Warnings: []string{}}
				artifactStreamResponse(w, artifact.JSON(body), "stop")
				return
			}
			if !strings.Contains(req.Messages[len(req.Messages)-1].Content, "外部判断必须删去") {
				t.Error("repair did not identify unsupported external judgments")
			}
		}
		r.Body = io.NopCloser(strings.NewReader(artifact.JSON(req)))
		artifactModelResponse(w, r)
	})
	view, err := svc.Submit(context.Background(), 7, "source-attribution", artifactRequest(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.ExecuteArtifact(context.Background(), view.ID); err != nil {
		t.Fatal(err)
	}
	final, err := svc.Run(context.Background(), 7, view.ID)
	if err != nil || final.Status != "completed" || extractions != 2 {
		t.Fatalf("unrelated citation authorized external assertion: %+v extractions=%d err=%v", final, extractions, err)
	}
	var step model.AgentStep
	if err = db.Where("run_id=? AND status=?", view.ID, "failed").First(&step).Error; err != nil || step.ErrorMessage != "source_attribution" {
		t.Fatalf("source attribution diagnostic missing: %+v %v", step, err)
	}
}
