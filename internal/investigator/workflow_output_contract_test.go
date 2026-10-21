package investigator

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"vidlens/internal/domain"
	"vidlens/internal/store"
)

func TestWorkflowAuthorInitialContractMatchesToolAndAssignedSections(t *testing.T) {
	for _, name := range []string{"write_video_chapters", "revise_video_chapters"} {
		t.Run(name, func(t *testing.T) {
			var sends atomic.Int32
			s := boundedReviewSession(t, func(w http.ResponseWriter, r *http.Request) {
				sends.Add(1)
				var in struct {
					Messages []struct{ Role, Content string }
					Tools    []struct {
						Function struct{ Parameters json.RawMessage }
					}
				}
				if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
					t.Error(err)
				}
				found := false
				for _, message := range in.Messages {
					_, raw, ok := strings.Cut(message.Content, "OUTPUT_CONTRACT_JSON\n")
					if !ok || message.Role != "system" {
						continue
					}
					found = true
					var contract struct {
						Schema  json.RawMessage `json:"json_schema"`
						Example json.RawMessage `json:"shape_example_only"`
					}
					if err := json.Unmarshal([]byte(raw), &contract); err != nil {
						t.Error(err)
					}
					var a, b any
					_ = json.Unmarshal(contract.Schema, &a)
					_ = json.Unmarshal(in.Tools[0].Function.Parameters, &b)
					// JSON Schema required is a set; the SDK sorts its wire copy.
					var normalize func(any)
					normalize = func(v any) {
						switch x := v.(type) {
						case map[string]any:
							if required, ok := x["required"].([]any); ok {
								slices.SortFunc(required, func(a, b any) int { return strings.Compare(a.(string), b.(string)) })
							}
							for _, child := range x {
								normalize(child)
							}
						case []any:
							for _, child := range x {
								normalize(child)
							}
						}
					}
					normalize(a)
					normalize(b)
					if a == nil || store.Hash(a) != store.Hash(b) {
						t.Error("model-visible schema differs from the actual tool schema")
					}
					var example workflowBody
					if err := workflowDecode(string(contract.Example), &example); err != nil || len(example.Sections) != 2 || example.Sections[0].ID != "s2" || example.Sections[1].ID != "s3" || example.Limitations == nil {
						t.Error("initial author example lacks real assigned IDs or typed arrays", string(contract.Example), err)
					}
				}
				if !found {
					t.Error("first submission has no model-visible output schema and concrete author example")
				}
				writeReviewWireArgs(w, name, workflowBody{Sections: []workflowSection{}, Limitations: []domain.Limitation{}})
			})
			workflowFixtureState(s)
			phase := "write"
			if name == "revise_video_chapters" {
				phase = "revision"
			}
			_, err := workflowStage[workflowBody](context.Background(), s, name, phase, name, workflowWriteInstructions,
				map[string]any{"snapshot_id": s.snapshot.ID, "assigned_chapters": []sectionPlan{{ID: "s2"}, {ID: "s3"}}}, nil, nil)
			if err != nil || sends.Load() != 1 || s.cp.Calls != 1 {
				t.Fatal("initial contract required another call", err, sends.Load())
			}
		})
	}
}

func TestWorkflowFormatBudgetRetainsLastRejectedField(t *testing.T) {
	var sends atomic.Int32
	s := boundedReviewSession(t, func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		// Synthetic minimal shape of the recorded failure: a chapter ID inside a block.
		writeReviewWireArgs(w, "write_video_chapters", map[string]any{
			"sections":    []any{map[string]any{"id": "s2", "blocks": []any{map[string]any{"id": "s2", "type": "paragraph", "origin": "video", "text": "来源事实", "citation_ids": []string{"t1"}}}}},
			"limitations": []any{},
		})
	})
	workflowFixtureState(s)
	s.cp.Workflow.Calls["format"] = 1
	_, err := workflowStage[workflowBody](context.Background(), s, "write:later", "write", "write_video_chapters", workflowWriteInstructions,
		map[string]any{"snapshot_id": s.snapshot.ID}, nil, nil)
	var problem *domain.Problem
	if !errors.As(err, &problem) || problem.Code != "WORKFLOW_RESULT_INVALID" || !strings.Contains(problem.Detail, "/sections/0/blocks/0/id") || problem.Meta["recovery_exhausted"] != true {
		t.Fatal("exhausted recovery hid the last validation failure", err)
	}
	if sends.Load() != 1 || s.cp.Calls != 1 || s.cp.Workflow.Calls["format"] != 1 {
		t.Fatal("invalid body gained a second recovery or another send", sends.Load(), s.cp)
	}
	if _, _, err := s.loadDraft(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestWorkflowV2RetainsOriginalInitialPrompt(t *testing.T) {
	s := boundedReviewSession(t, func(w http.ResponseWriter, r *http.Request) {
		var wire reviewWireRequest
		_ = json.NewDecoder(r.Body).Decode(&wire)
		if strings.Contains(string(domain.JSON(wire.Messages)), "OUTPUT_CONTRACT_JSON") {
			t.Error("admitted v2 stage received the new v3 prompt")
		}
		writeReviewWireArgs(w, "write_video_chapters", workflowBody{Sections: []workflowSection{}, Limitations: []domain.Limitation{}})
	})
	workflowFixtureState(s)
	s.cp.ExecutionContract.Version = domain.ExecutionContractV11
	_, err := workflowStage[workflowBody](context.Background(), s, "legacy-v2", "write", "write_video_chapters", workflowWriteInstructions,
		map[string]any{"snapshot_id": s.snapshot.ID}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
}
