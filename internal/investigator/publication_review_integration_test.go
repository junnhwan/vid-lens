package investigator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/cloudwego/eino/schema"
	"vidlens/internal/domain"
	"vidlens/internal/media"
	"vidlens/internal/store"
	"vidlens/internal/testsupport"
)

func publicationReviewFixture(t *testing.T, handler http.HandlerFunc) (*session, domain.Article) {
	t.Helper()
	s := boundedReviewSession(t, handler)
	ctx := context.Background()
	var pixels bytes.Buffer
	if err := jpeg.Encode(&pixels, image.NewRGBA(image.Rect(0, 0, 4, 4)), nil); err != nil {
		t.Fatal(err)
	}
	id := domain.ID()
	hash := sha256.Sum256(pixels.Bytes())
	if err := os.WriteFile(filepath.Join(s.engine.Config.DataDir, id+".jpg"), pixels.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	asset := domain.Asset{ID: id, SnapshotID: &s.snapshot.ID, SourceID: s.claim.Run.SourceID, RunID: s.claim.Run.ID, Kind: "frame", StorageKey: id + ".jpg", SHA256: hex.EncodeToString(hash[:]), MIME: "image/jpeg", Bytes: int64(pixels.Len()), TimeMS: domain.Ptr(int64(1000)), RequestedTimeMS: domain.Ptr(int64(1000))}
	if err := s.engine.Store.AddAsset(ctx, s.claim, asset); err != nil {
		t.Fatal(err)
	}
	s.cp.OverviewIDs, s.cp.CitationFrames = []string{id}, []string{id}
	s.cp.Messages = []*schema.Message{schema.SystemMessage("frozen publication fixture")}
	if err := s.engine.Store.RecordObservation(ctx, s.claim, "publication-observation", "overview", observation{InspectionID: "overview", FrameIDs: []string{id}, VisibleFacts: []string{"合成黑色画面。"}, Status: "supported"}); err != nil {
		t.Fatal(err)
	}
	ms, err := media.New(s.engine.Store, s.engine.Resources, s.engine.Config)
	if err != nil {
		t.Fatal(err)
	}
	s.engine.Media = ms
	a := domain.Article{SchemaVersion: 1, SnapshotID: s.snapshot.ID, Source: s.snapshot.Source, Title: "测试来源的完整内容", Quality: "with_limitations", Sections: []domain.Section{{ID: "main", Title: "来源说明", Blocks: []domain.Block{
		{ID: "prose", Type: "paragraph", Origin: "background_explanation", Content: []domain.Inline{{Type: "text", Text: "这是一段用于校验发布恢复流程的合成说明，测试只判断审核记录与候选及来源身份是否保持一致。"}}},
		{ID: "picture", Type: "figure", Origin: "video", FrameIDs: []string{id}, Layout: "single", Caption: "合成黑色画面。", CitationIDs: []string{"f1"}},
	}}}, Citations: map[string]domain.Citation{"f1": {ID: "f1", Kind: "video_frame", Label: "合成图像", SnapshotID: s.snapshot.ID, FrameID: id, TimeMS: domain.Ptr(int64(1000))}}, Limitations: []domain.Limitation{{Code: "partial_text", Message: "测试来源文字不完整。", SectionIDs: []string{}, CitationIDs: []string{}}}}
	if err := s.validateArticle(ctx, a); err != nil {
		t.Fatal("fixture candidate must pass structural validation", err)
	}
	return s, a
}

func persistPublicationCandidate(t *testing.T, s *session, a domain.Article) {
	t.Helper()
	ctx := context.Background()
	if _, err := s.engine.Store.BeginOperation(ctx, s.claim, "candidate", "article_candidate", a, false); err != nil {
		t.Fatal(err)
	}
	if err := s.engine.Store.FinishOperation(ctx, s.claim, "candidate", a, "", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.engine.Store.Checkpoint(ctx, s.claim, s.cp); err != nil {
		t.Fatal(err)
	}
	var err error
	s.claim.Run, err = s.engine.Store.Run(ctx, s.claim.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
}

func TestEngineRejectsUnreviewedCompletedCandidateWithoutSending(t *testing.T) {
	var calls atomic.Int32
	s, a := publicationReviewFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	})
	persistPublicationCandidate(t, s, a)
	err := s.engine.Run(context.Background(), s.claim, s.profile)
	var problem *domain.Problem
	if !errors.As(err, &problem) || problem.Code != "REVIEW_COVERAGE_INCOMPLETE" || calls.Load() != 0 {
		t.Fatalf("unreviewed persisted candidate crossed publication: calls=%d err=%v", calls.Load(), err)
	}
	var published int
	if err := s.engine.Store.Pool.QueryRow(context.Background(), "SELECT count(*) FROM articles WHERE generation_run_id=$1", s.claim.Run.ID).Scan(&published); err != nil || published != 0 {
		t.Fatal("unreviewed candidate became an immutable article", published, err)
	}
	op, err := s.engine.Store.Operation(context.Background(), s.claim.Run.ID, "candidate")
	var retained domain.Article
	if err != nil || json.Unmarshal(op.Result, &retained) != nil || store.Hash(retained) != store.Hash(a) {
		t.Fatal("rejection did not retain the original candidate", err)
	}
}

func TestEnginePublicationRequiresCurrentCompleteReview(t *testing.T) {
	for _, fault := range []string{"approved", "candidate_changed", "source_text_changed", "source_video_changed", "frame_changed", "missing_visual", "not_assessed", "duplicate_result", "wrong_request", "wrong_operation_kind", "wrong_protocol_args", "another_run", "split_complete", "split_missing_leaf", "legacy_v1"} {
		t.Run(fault, func(t *testing.T) {
			var calls atomic.Int32
			s, a := publicationReviewFixture(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var in reviewWireRequest
				if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
					t.Error(err)
				}
				reviewWireReply(t, w, in, false)
			})
			ctx := context.Background()
			if fault == "legacy_v1" {
				s.cp.ExecutionContract.Version = domain.ExecutionContractV1
			} else if err := s.reviewArticle(ctx, a); err != nil {
				t.Fatal(err)
			}
			before := calls.Load()
			exec := func(query string, args ...any) {
				t.Helper()
				if _, err := s.engine.Store.Pool.Exec(ctx, query, args...); err != nil {
					t.Fatal(err)
				}
			}
			switch fault {
			case "candidate_changed":
				a.Title = "来源说明中已改变的新标题"
			case "source_text_changed":
				exec("UPDATE source_snapshots SET text_hash='different-text' WHERE id=$1", s.snapshot.ID)
			case "source_video_changed":
				exec("UPDATE source_snapshots SET video_hash='different-video' WHERE id=$1", s.snapshot.ID)
			case "frame_changed":
				id := a.Citations["f1"].FrameID
				path := filepath.Join(s.engine.Config.DataDir, id+".jpg")
				pixels, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				pixels = append(pixels, '\n')
				if err := os.WriteFile(path, pixels, 0600); err != nil {
					t.Fatal(err)
				}
				hash := sha256.Sum256(pixels)
				exec("UPDATE media_assets SET sha256=$2,bytes=$3 WHERE id=$1", id, hex.EncodeToString(hash[:]), len(pixels))
			case "missing_visual":
				exec("DELETE FROM run_operations WHERE run_id=$1 AND kind='review_batch' AND result->>'mode'='visual'", s.claim.Run.ID)
			case "not_assessed":
				exec("UPDATE run_operations SET result=jsonb_set(result,'{report,results,0,verdict}','\"not_assessed\"') WHERE run_id=$1 AND kind='review_batch'", s.claim.Run.ID)
			case "duplicate_result":
				exec("UPDATE run_operations SET result=jsonb_set(result,'{report,results}',jsonb_build_array(result#>'{report,results,0}',result#>'{report,results,0}')) WHERE run_id=$1 AND kind='review_batch'", s.claim.Run.ID)
			case "wrong_request":
				exec("UPDATE run_operations SET result=jsonb_set(result,'{request_id}','\"wrong-request\"') WHERE run_id=$1 AND kind='review_batch'", s.claim.Run.ID)
			case "wrong_operation_kind":
				exec("UPDATE run_operations SET kind='unrelated' WHERE run_id=$1 AND kind='review_batch'", s.claim.Run.ID)
			case "wrong_protocol_args":
				exec("UPDATE run_operations SET args_hash='old-review-protocol' WHERE run_id=$1 AND kind='review_batch'", s.claim.Run.ID)
			case "another_run":
				exec("UPDATE run_operations SET run_id=(SELECT id FROM job_runs WHERE kind='source.prepare' LIMIT 1) WHERE run_id=$1 AND kind='review_batch'", s.claim.Run.ID)
			case "split_complete", "split_missing_leaf":
				candidate := map[string]any{"title": a.Title, "sections": a.Sections, "limitations": a.Limitations}
				hash, catalog, _, _, err := s.contentReviewIdentity(ctx, candidate, a.Citations)
				if err != nil {
					t.Fatal(err)
				}
				units, err := candidateReviewUnits(candidate, catalog)
				if err != nil {
					t.Fatal(err)
				}
				tasks, err := planReviewTasks(units, s.profile.MaxOutputTokens)
				if err != nil || len(tasks[0].Units) < 2 {
					t.Fatal("fixture must have a splittable text task", err)
				}
				task := tasks[0]
				ids := []string{}
				for _, unit := range task.Units {
					ids = append(ids, unit.ID)
				}
				key := "review-units:" + store.Hash(map[string]any{"candidate_hash": hash, "mode": task.Mode, "unit_ids": ids})
				exec("UPDATE run_operations SET result=jsonb_set(result,'{state}','\"split\"') WHERE run_id=$1 AND operation_key=$2", s.claim.Run.ID, key)
				middle := len(task.Units) / 2
				for i, children := range [][]reviewUnit{task.Units[:middle], task.Units[middle:]} {
					if fault == "split_missing_leaf" && i == 1 {
						continue
					}
					childIDs := []string{}
					for _, unit := range children {
						childIDs = append(childIDs, unit.ID)
					}
					childKey := "review-units:" + store.Hash(map[string]any{"candidate_hash": hash, "mode": task.Mode, "unit_ids": childIDs})
					id := "r_" + childKey[len("review-units:"):][:24]
					report := &reviewUnitReport{RequestID: id, Results: []reviewUnitResult{}}
					for _, unit := range children {
						report.Results = append(report.Results, reviewUnitResult{UnitID: unit.ID, Verdict: "supported", Issues: []reviewUnitIssue{}})
					}
					if _, err := s.engine.Store.BeginOperation(ctx, s.claim, childKey, "review_batch", map[string]any{"request_id": id, "unit_ids": childIDs, "mode": task.Mode, "protocol": boundedReviewProtocol}, false); err != nil {
						t.Fatal(err)
					}
					if err := s.engine.Store.FinishOperation(ctx, s.claim, childKey, reviewBatchOutcome{State: "complete", Mode: task.Mode, RequestID: id, UnitIDs: childIDs, Report: report}, "", nil); err != nil {
						t.Fatal(err)
					}
				}
			}
			persistPublicationCandidate(t, s, a)
			err := s.engine.Run(ctx, s.claim, s.profile)
			accepted := fault == "approved" || fault == "split_complete" || fault == "legacy_v1"
			var problem *domain.Problem
			if accepted && err != nil || !accepted && (!errors.As(err, &problem) || problem.Code != "REVIEW_COVERAGE_INCOMPLETE") {
				t.Fatalf("publication did not enforce current complete review: accepted=%t err=%v", accepted, err)
			}
			if calls.Load() != before {
				t.Fatalf("publication takeover sent a provider request: before=%d after=%d", before, calls.Load())
			}
			var published int
			if err := s.engine.Store.Pool.QueryRow(ctx, "SELECT count(*) FROM articles WHERE generation_run_id=$1", s.claim.Run.ID).Scan(&published); err != nil || accepted != (published == 1) {
				t.Fatal("immutable publication disagrees with review gate", published, err)
			}
		})
	}
}

func TestEngineAnswerPublicationRequiresCurrentReview(t *testing.T) {
	for _, fault := range []string{"approved", "empty_parts", "missing_review", "changed_summary", "changed_parts", "mixed_empty_parts_coverage"} {
		t.Run(fault, func(t *testing.T) {
			db, resources, cfg := testsupport.Services(t)
			ctx := context.Background()
			turn, claim := testsupport.Question(t, db)
			snap, err := db.Snapshot(ctx, turn.SnapshotID)
			if err != nil {
				t.Fatal(err)
			}
			transcript, err := db.Transcript(ctx, turn.SnapshotID)
			if err != nil {
				t.Fatal(err)
			}
			article, err := db.PublishedArticle(ctx, turn.ArticleID)
			if err != nil {
				t.Fatal(err)
			}
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var in reviewWireRequest
				if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
					t.Error(err)
				}
				reviewWireReply(t, w, in, false)
			}))
			defer server.Close()
			profile := cfg.Profile
			profile.ModelBaseURL = server.URL
			profile.MaxOutputTokens, profile.ContextTokens, profile.InputCharsLimit = 393216, 1048576, 2000000
			if _, err := db.Pool.Exec(ctx, "UPDATE runtime_config_revisions SET profile=$2 WHERE id=$1", claim.Run.ConfigID, domain.JSON(profile)); err != nil {
				t.Fatal(err)
			}
			s := &session{engine: &Engine{Store: db, Resources: resources, Config: cfg}, claim: claim, profile: profile, snapshot: snap, transcript: transcript, turn: &turn, article: &article, cp: checkpoint{SchemaVersion: 1, Ready: true, Protocol: protocol, LayoutVersion: "source-context-v2", HarnessRevision: 16, ExecutionContract: &domain.ExecutionContract{Version: domain.ExecutionContractV2, RunID: claim.Run.ID}, Inspections: map[string][]string{}, Repeated: map[string]int{}, Messages: []*schema.Message{schema.SystemMessage("frozen question fixture")}}}
			if _, err := s.makeTools(); err != nil {
				t.Fatal(err)
			}
			a := domain.Answer{Kind: "insufficient_evidence", Summary: "当前来源不足以核实。", Blocks: []domain.Block{{ID: "answer", Type: "paragraph", Origin: "background_explanation", Content: []domain.Inline{{Type: "text", Text: "现有来源尚未提供足够信息，因此保留未知。"}}}}, Citations: map[string]domain.Citation{}, Limitations: []domain.Limitation{}}
			if fault == "empty_parts" || fault == "mixed_empty_parts_coverage" {
				a.Parts = []domain.AnswerPart{}
			}
			if fault == "mixed_empty_parts_coverage" {
				for i := 0; i < 9; i++ {
					a.Blocks = append(a.Blocks, domain.Block{ID: fmt.Sprintf("extra-%d", i), Type: "paragraph", Origin: "background_explanation", Content: []domain.Inline{{Type: "text", Text: fmt.Sprintf("第%d个独立测试说明。", i)}}})
				}
			}
			if fault != "missing_review" {
				if err := s.reviewAnswer(ctx, a); err != nil {
					t.Fatal(err)
				}
			}
			if fault == "changed_summary" {
				a.Summary = "已经被替换而尚未复核的新结论。"
			}
			if fault == "changed_parts" {
				a.Parts = []domain.AnswerPart{{Title: "新增但未复核的回答方面", BlockIDs: []string{"answer"}}}
			}
			if fault == "mixed_empty_parts_coverage" {
				candidate := map[string]any{"kind": a.Kind, "summary": a.Summary, "blocks": a.Blocks, "parts": a.Parts, "limitations": a.Limitations}
				emptyHash, catalog, _, _, err := s.contentReviewIdentity(ctx, candidate, a.Citations)
				if err != nil {
					t.Fatal(err)
				}
				units, err := candidateReviewUnits(candidate, catalog)
				if err != nil {
					t.Fatal(err)
				}
				tasks, err := planReviewTasks(units, profile.MaxOutputTokens)
				if err != nil || len(tasks) < 2 {
					t.Fatal("fixture needs at least two independently approved tasks", err)
				}
				a.Parts = nil
				if err := s.reviewAnswer(ctx, a); err != nil {
					t.Fatal(err)
				}
				candidate["parts"] = a.Parts
				nilHash, _, _, _, err := s.contentReviewIdentity(ctx, candidate, a.Citations)
				if err != nil {
					t.Fatal(err)
				}
				// The union covers every task, but neither exact review hash is
				// complete. The compatibility gate must not assemble a new proof.
				for i, hash := range []string{emptyHash, nilHash} {
					ids := []string{}
					for _, unit := range tasks[i].Units {
						ids = append(ids, unit.ID)
					}
					key := "review-units:" + store.Hash(map[string]any{"candidate_hash": hash, "mode": tasks[i].Mode, "unit_ids": ids})
					if _, err := db.Pool.Exec(ctx, "DELETE FROM run_operations WHERE run_id=$1 AND operation_key=$2", claim.Run.ID, key); err != nil {
						t.Fatal(err)
					}
				}
			}
			before := calls.Load()
			if _, err := db.BeginOperation(ctx, claim, "candidate", "answer_candidate", a, false); err != nil {
				t.Fatal(err)
			}
			if err := db.FinishOperation(ctx, claim, "candidate", a, "", nil); err != nil {
				t.Fatal(err)
			}
			if err := db.Checkpoint(ctx, claim, s.cp); err != nil {
				t.Fatal(err)
			}
			claim.Run, err = db.Run(ctx, claim.Run.ID)
			if err != nil {
				t.Fatal(err)
			}
			err = s.engine.Run(ctx, claim, profile)
			accepted := fault == "approved" || fault == "empty_parts"
			var problem *domain.Problem
			if accepted && err != nil || !accepted && (!errors.As(err, &problem) || problem.Code != "REVIEW_COVERAGE_INCOMPLETE") {
				t.Fatalf("answer crossed publication without its exact approved review: %v", err)
			}
			if calls.Load() != before {
				t.Fatal("answer publication automatically sent another model request")
			}
			var status string
			if err := db.Pool.QueryRow(ctx, "SELECT status FROM qa_turns WHERE id=$1", turn.ID).Scan(&status); err != nil || accepted != (status == "completed") {
				t.Fatal("answer persistence disagrees with review gate", status, err)
			}
		})
	}
}
