package investigator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cloudwego/eino/schema"
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
	var sends atomic.Int32
	s := boundedReviewSession(t, func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
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
		var operations string
		queryErr := s.engine.Store.Pool.QueryRow(context.Background(), "SELECT COALESCE(string_agg(operation_key||':'||status,',' ORDER BY started_at),'') FROM run_operations WHERE run_id=$1", s.claim.Run.ID).Scan(&operations)
		t.Fatalf("legacy stage sends=%d operations=%s query=%v: %v", sends.Load(), operations, queryErr, err)
	}
}

func TestWorkflowCompleteObjectWithClosingEchoNeedsNoPaidRecovery(t *testing.T) {
	var sends atomic.Int32
	s := boundedReviewSession(t, func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		raw := `{"sections":[{"id":"s1","blocks":[{"type":"paragraph","origin":"video","text":"来源事实。","citation_ids":["t1"]}]}],"limitations":[]}}`
		chunk := map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": "body", "type": "function", "function": map[string]any{"name": "write_video_chapters", "arguments": raw}}}}, "finish_reason": "tool_calls"}}}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", domain.JSON(chunk))
	})
	workflowFixtureState(s)
	draft := articleDraft{Title: "主题", Plan: []sectionPlan{{ID: "s1", Title: "首章"}}, Sections: map[string]domain.Section{}}
	catalog, err := s.citationCatalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	body, err := workflowStage[workflowBody](context.Background(), s, "closing-echo", "write", "write_video_chapters", workflowWriteInstructions,
		map[string]any{"snapshot_id": s.snapshot.ID, "assigned_chapters": draft.Plan}, nil, func(body *workflowBody) error {
			_, err := workflowApplyBody(draft, *body, draft.Plan, catalog, false, false)
			return err
		})
	if err != nil || len(body.Sections) != 1 || sends.Load() != 1 || s.cp.Workflow.Calls["format"] != 0 {
		t.Fatal("a complete typed object spent paid recovery on one redundant closing brace", err, sends.Load())
	}
	op, err := s.engine.Store.Operation(context.Background(), s.claim.Run.ID, "workflow-envelope:closing-echo:0")
	if err != nil || op.Status != "completed" || !strings.Contains(string(op.Result), "one_redundant_closing_brace") {
		t.Fatal("framing normalization lost its audit record", op, err)
	}
}

func TestWorkflowClosingEchoNeverRepairsFieldsOrMissingContent(t *testing.T) {
	valid := `{"sections":[],"limitations":[]}`
	for _, test := range []struct {
		name, raw                   string
		allow, accepted, normalized bool
	}{
		{"valid", valid, true, true, false},
		{"one_closing_brace", valid + " } ", true, true, true},
		{"legacy", valid + "}", false, false, false},
		{"two_closing_braces", valid + "}}", true, false, false},
		{"another_object", valid + valid, true, false, false},
		{"prose", valid + " finished", true, false, false},
		{"fence", "```json\n" + valid + "\n```", true, false, false},
		{"truncated", `{"sections":[{"id":"s1","blocks":[`, true, false, false},
		{"trailing_comma", `{"sections":[],"limitations":[],}}`, true, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, native := range []bool{false, true} {
				response := schema.AssistantMessage(test.raw, nil)
				if native {
					response.Content = ""
					response.ToolCalls = []schema.ToolCall{{Function: schema.FunctionCall{Name: "write_video_chapters", Arguments: test.raw}}}
				}
				before := store.Hash(response)
				raw, normalized, err := workflowStageArguments(response, "write_video_chapters", test.allow)
				if (err == nil) != test.accepted || normalized != test.normalized || before != store.Hash(response) {
					t.Fatal("changed framing scope or original response", raw, normalized, err)
				}
			}
		})
	}
	for _, raw := range []string{
		`{"sections":"[]","limitations":[]}}`,
		`{"sections":[{"id":"s1","blocks":[{"id":"s1","type":"paragraph","origin":"video"}]}],"limitations":[]}}`,
		`{"sections":[],"limitations":[],"invented":true}}`,
	} {
		framed, normalized, err := workflowStageArguments(schema.AssistantMessage(raw, nil), "write_video_chapters", true)
		var body workflowBody
		if err != nil || !normalized || workflowDecode(framed, &body) == nil {
			t.Fatal("framing normalization accepted an invalid field or coerced an array", raw, err)
		}
	}
}

func TestWorkflowNullSectionPlaceholdersNeverEraseUncertainty(t *testing.T) {
	raw := `{"sections":[{"id":"s1","blocks":[{"type":"paragraph","origin":"video","text":"专名不确定。","citation_ids":["t1"]}],"limitations_placeholder":null}],"limitations":[{"code":"partial_text","message":"原文专名不清楚。","section_ids":["s1"],"citation_ids":["t1"]}]}`
	projected, paths := workflowNullSectionPlaceholders(raw)
	var body workflowBody
	if err := workflowDecode(projected, &body); err != nil || len(paths) != 1 || paths[0] != "/sections/0/limitations_placeholder" || len(body.Limitations) != 1 || body.Limitations[0].Message != "原文专名不清楚。" || body.Sections[0].Blocks[0].CitationIDs[0] != "t1" {
		t.Fatal("empty annotation removed real uncertainty or evidence", projected, paths, err)
	}
	for _, invalid := range []string{
		strings.Replace(raw, `"limitations_placeholder":null`, `"limitations_placeholder":[{"message":"有实际局限"}]`, 1),
		strings.Replace(raw, `"limitations_placeholder":null`, `"limitations_placeholder":{}`, 1),
		strings.Replace(raw, `"limitations_placeholder":null`, `"limitations_placeholder":[]`, 1),
		strings.Replace(raw, `"limitations_placeholder":null`, `"other_placeholder":null`, 1),
		strings.Replace(raw, `"text":"专名不确定。"`, `"text":"专名不确定。","limitations_placeholder":null`, 1),
		strings.Replace(raw, `"limitations_placeholder":null`, `"limitations":null`, 1),
		strings.Replace(raw, `"limitations_placeholder":null`, `"limitations_placeholder":{"message":"不能丢失"},"limitations_placeholder":null`, 1),
		strings.Replace(raw, `"id":"s1"`, `"id":"s2","id":"s1"`, 1),
	} {
		projected, _ := workflowNullSectionPlaceholders(invalid)
		if workflowDecode(projected, &body) == nil {
			t.Fatal("non-empty or misplaced metadata bypassed strict validation", projected)
		}
	}
}
