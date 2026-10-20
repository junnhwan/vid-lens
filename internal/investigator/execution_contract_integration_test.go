package investigator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"
	"vidlens/internal/domain"
	"vidlens/internal/store"
)

type executionWireMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	ToolCallID string          `json:"tool_call_id"`
}

func wireText(t *testing.T, m executionWireMessage) string {
	t.Helper()
	var text string
	if err := json.Unmarshal(m.Content, &text); err != nil {
		t.Fatal(err)
	}
	return text
}

func frozenPromptHash(t *testing.T, revision int, question bool) string {
	t.Helper()
	raw, err := os.ReadFile("testdata/protocol-golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Kind     string `json:"kind"`
		Level    string `json:"level"`
		Revision int    `json:"revision"`
		Question bool   `json:"question"`
		SHA256   string `json:"sha256"`
	}
	if err = json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		if (c.Kind == "prompt" || c.Kind == "initial") && c.Level == "standard" && c.Revision == revision && c.Question == question {
			return c.SHA256
		}
	}
	t.Fatal("no frozen prompt fixture", revision, question)
	return ""
}

func TestEngineRejectsIncompatibleExecutionBeforeSending(t *testing.T) {
	for _, fault := range []string{"future_adapter", "another_run", "future_harness", "future_schema", "invalid_json_type", "wrong_context_kind", "missing_frozen_messages", "null_checkpoint"} {
		t.Run(fault, func(t *testing.T) {
			s := planSession(t)
			ctx := context.Background()
			var sent atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				sent.Add(1)
				w.WriteHeader(http.StatusBadRequest)
			}))
			defer server.Close()
			s.profile.ModelBaseURL = server.URL
			if _, err := s.engine.Store.Pool.Exec(ctx, "UPDATE runtime_config_revisions SET profile=$2 WHERE id=$1", s.claim.Run.ConfigID, domain.JSON(s.profile)); err != nil {
				t.Fatal(err)
			}
			identity := map[string]any{"version": "vidlens-execution-2026-10-20-v1", "run_id": s.claim.Run.ID}
			cp := map[string]any{"schema_version": 1, "execution_contract": identity, "layout_version": draftLayout, "harness_revision": 14, "protocol": domain.ArticleDraftProtocol, "ready": true, "messages": []*schema.Message{schema.SystemMessage("already frozen"), schema.UserMessage("source")}}
			switch fault {
			case "future_adapter":
				identity["version"] = "future-unsupported-adapter"
			case "another_run":
				identity["run_id"] = "ancestor-run"
			case "future_harness":
				cp["harness_revision"] = 999
			case "future_schema":
				cp["schema_version"] = 999
			case "invalid_json_type":
				cp["harness_revision"] = "not-an-integer"
			case "wrong_context_kind":
				cp["layout_version"] = "source-context-v2"
			case "missing_frozen_messages":
				cp["messages"] = []*schema.Message{}
			}
			var input any = cp
			if fault == "null_checkpoint" {
				input = nil
			}
			if err := s.engine.Store.Checkpoint(ctx, s.claim, input); err != nil {
				t.Fatal(err)
			}
			var err error
			s.claim.Run, err = s.engine.Store.Run(ctx, s.claim.Run.ID)
			if err != nil {
				t.Fatal(err)
			}
			before := store.Hash(json.RawMessage(s.claim.Run.Checkpoint))
			err = s.engine.Run(ctx, s.claim, s.profile)
			var problem *domain.Problem
			if !errors.As(err, &problem) || problem.Code != "INTERNAL_FAILURE" || sent.Load() != 0 {
				t.Fatalf("incompatible execution was silently reinterpreted: sends=%d err=%v", sent.Load(), err)
			}
			after, err := s.engine.Store.Run(ctx, s.claim.Run.ID)
			if err != nil || store.Hash(json.RawMessage(after.Checkpoint)) != before {
				t.Fatal("rejection erased the original checkpoint", err)
			}
		})
	}
}

// Exercise the Engine boundary and the request actually delivered to the
// provider. A local 400 ends the run after capture, without a real model call.
func TestDraftRetrySendsCompleteFrozenAuthoringContract(t *testing.T) {
	for _, revision := range []int{5, 12, 13, 14, 15, 16} {
		t.Run(fmt.Sprint(revision), func(t *testing.T) {
			s := planSession(t)
			ctx := context.Background()
			requests := make(chan []executionWireMessage, 4)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var input struct {
					Messages []executionWireMessage `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
					t.Error(err)
				}
				requests <- input.Messages
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":{"message":"fixture stops after input capture"}}`))
			}))
			defer server.Close()
			s.profile.ModelBaseURL = server.URL
			if _, err := s.engine.Store.Pool.Exec(ctx, "UPDATE runtime_config_revisions SET profile=$2 WHERE id=$1", s.claim.Run.ConfigID, domain.JSON(s.profile)); err != nil {
				t.Fatal(err)
			}
			spec, err := s.engine.Store.GenerationForRun(ctx, s.claim.Run.ID)
			if err != nil {
				t.Fatal(err)
			}
			checkpoint := map[string]any{
				"schema_version": 1, "protocol": domain.ArticleDraftProtocol,
				"layout_version": draftLayout, "harness_revision": revision,
				"ready": true, "draft_resume": true,
				"execution_contract": map[string]any{"version": "vidlens-execution-2026-10-20-v1", "run_id": s.claim.Run.ID},
			}
			if err = s.engine.Store.Checkpoint(ctx, s.claim, checkpoint); err != nil {
				t.Fatal(err)
			}
			s.claim.Run, err = s.engine.Store.Run(ctx, s.claim.Run.ID)
			if err != nil {
				t.Fatal(err)
			}
			if err = s.engine.Run(ctx, s.claim, s.profile); err == nil {
				t.Fatal("capture provider should end the run with an error")
			}
			select {
			case messages := <-requests:
				if len(messages) == 0 || messages[0].Role != "system" {
					t.Fatal("missing system contract")
				}
				hash := sha256.Sum256([]byte(wireText(t, messages[0])))
				if hex.EncodeToString(hash[:]) != frozenPromptHash(t, revision, false) {
					t.Fatal("draft retry omitted rules from the frozen authoring contract")
				}
			default:
				t.Fatal("engine did not deliver a request to the capture provider", err)
			}
			after, err := s.engine.Store.GenerationForRun(ctx, s.claim.Run.ID)
			if err != nil || store.Hash(after) != store.Hash(spec) {
				t.Fatal("execution setup rewrote the user's generation specification", err)
			}
		})
	}
}

func TestWorkerRecoveryKeepsFrozenContractAndFinishesPendingAction(t *testing.T) {
	s := planSession(t)
	ctx := context.Background()
	requests := make(chan []executionWireMessage, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Messages []executionWireMessage `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
		}
		requests <- input.Messages
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()
	s.profile.ModelBaseURL = server.URL
	if _, err := s.engine.Store.Pool.Exec(ctx, "UPDATE runtime_config_revisions SET profile=$2 WHERE id=$1", s.claim.Run.ConfigID, domain.JSON(s.profile)); err != nil {
		t.Fatal(err)
	}
	cp := map[string]any{
		"schema_version": 1, "execution_contract": map[string]any{"version": "vidlens-execution-2026-10-20-v1", "run_id": s.claim.Run.ID},
		"protocol": domain.ArticleDraftProtocol, "layout_version": draftLayout, "harness_revision": 14, "ready": true, "calls": 7, "repairs": 2,
		"progress": map[string]any{"total": 5, "stalled": 2},
		"messages": []*schema.Message{schema.SystemMessage("frozen historical system input"), schema.UserMessage("complete original source input"),
			{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{ID: "pending-source-read", Type: "function", Function: schema.FunctionCall{Name: "find_evidence", Arguments: `{"query":"来源事实"}`}}}}},
	}
	if err := s.engine.Store.Checkpoint(ctx, s.claim, cp); err != nil {
		t.Fatal(err)
	}
	var err error
	s.claim.Run, err = s.engine.Store.Run(ctx, s.claim.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.engine.Run(ctx, s.claim, s.profile); err == nil {
		t.Fatal("capture provider should stop the run")
	}
	select {
	case messages := <-requests:
		if wireText(t, messages[0]) != "frozen historical system input" {
			t.Fatal("recovery rebuilt an already frozen prompt")
		}
		paired := false
		for _, m := range messages {
			if m.Role == "tool" && m.ToolCallID == "pending-source-read" {
				paired = true
			}
		}
		if !paired {
			t.Fatal("a new request was sent before finishing the pending tool")
		}
	default:
		t.Fatal("provider was not reached after pending recovery", err)
	}
	run, err := s.engine.Store.Run(ctx, s.claim.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	var restored struct {
		HarnessRevision int `json:"harness_revision"`
		Calls           int `json:"calls"`
		Repairs         int `json:"repairs"`
		Progress        struct {
			Total int `json:"total"`
		} `json:"progress"`
		Contract *domain.ExecutionContract `json:"execution_contract"`
	}
	if err = json.Unmarshal(run.Checkpoint, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.HarnessRevision != 14 || restored.Calls != 7 || restored.Repairs != 2 || restored.Progress.Total < 5 || restored.Contract == nil || restored.Contract.RunID != run.ID {
		t.Fatalf("worker recovery reset execution state: %s", run.Checkpoint)
	}
}

func TestExplicitDraftRetryResolvesItsOwnCompatibleExecution(t *testing.T) {
	for _, tc := range []struct {
		name, policy       string
		previous, expected int
	}{
		{"current", "", 14, 16},
		{"recovery", recoveryPolicy, 13, 13},
		{"paced", "interpretation-2026-10-17-v5", 5, 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := planSession(t)
			ctx := context.Background()
			if tc.policy != "" {
				s.spec.DetailPolicy.Revision = tc.policy
				s.spec.DetailPolicyRevision = tc.policy
				if _, err := s.engine.Store.Pool.Exec(ctx, "UPDATE generation_requests SET generation_spec=$2 WHERE active_run_id=$1", s.claim.Run.ID, domain.JSON(s.spec)); err != nil {
					t.Fatal(err)
				}
			}
			var owner, request string
			if err := s.engine.Store.Pool.QueryRow(ctx, "SELECT session_id,id FROM generation_requests WHERE active_run_id=$1", s.claim.Run.ID).Scan(&owner, &request); err != nil {
				t.Fatal(err)
			}
			s.cp.HarnessRevision = tc.previous
			s.cp.ExecutionContract = &domain.ExecutionContract{Version: domain.ExecutionContractV1, RunID: s.claim.Run.ID}
			s.cp.Messages = []*schema.Message{schema.SystemMessage("ancestor's frozen prompt")}
			if err := s.engine.Store.Checkpoint(ctx, s.claim, s.cp); err != nil {
				t.Fatal(err)
			}
			draft := articleDraft{Title: "保留的草稿", Plan: []sectionPlan{{ID: "one", Title: "来源事实", KeyPoints: []string{"关键事实"}, EvidenceIDs: []string{"t1"}}}, Sections: map[string]domain.Section{}}
			if _, err := s.engine.Store.SaveArticleDraft(ctx, s.claim, "before-retry", nil, 0, draft); err != nil {
				t.Fatal(err)
			}
			if _, err := s.engine.Store.Fail(ctx, s.claim, domain.Failure{Code: "MODEL_RESULT_INVALID", Message: "fixture", CanRetry: true}, false, 0); err != nil {
				t.Fatal(err)
			}
			ancestor, err := s.engine.Store.Run(ctx, s.claim.Run.ID)
			if err != nil {
				t.Fatal(err)
			}
			retry, err := s.engine.Store.CreateRequest(ctx, owner, domain.ID(), "", request, false)
			if err != nil {
				t.Fatal(err)
			}
			next, err := s.engine.Store.Request(ctx, owner, retry.ID)
			if err != nil || next.ActiveRunID == nil {
				t.Fatal(err)
			}
			claim, err := s.engine.Store.Claim(ctx, *next.ActiveRunID, 1, "explicit-retry")
			if err != nil || claim == nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadRequest) }))
			defer server.Close()
			s.profile.ModelBaseURL = server.URL
			if _, err = s.engine.Store.Pool.Exec(ctx, "UPDATE runtime_config_revisions SET profile=$2 WHERE id=$1", claim.Run.ConfigID, domain.JSON(s.profile)); err != nil {
				t.Fatal(err)
			}
			if err = s.engine.Run(ctx, *claim, s.profile); err == nil {
				t.Fatal("fixture should stop after the request")
			}
			run, err := s.engine.Store.Run(ctx, claim.Run.ID)
			if err != nil {
				t.Fatal(err)
			}
			var cp checkpoint
			if err = json.Unmarshal(run.Checkpoint, &cp); err != nil {
				t.Fatal(err)
			}
			if cp.ExecutionContract == nil || cp.ExecutionContract.Version != domain.CurrentExecutionContract || cp.ExecutionContract.RunID != run.ID || cp.HarnessRevision != tc.expected || cp.Protocol != domain.ArticleDraftProtocol || cp.DraftResume || cp.Calls != 0 || cp.Repairs != 0 {
				t.Fatalf("explicit retry did not resolve and persist its own execution: %s", run.Checkpoint)
			}
			text := cp.Messages[0].Content
			if cp.ExecutionContract.Version == domain.ExecutionContractV4 {
				if !strings.HasSuffix(text, "\n"+sourceContextInstructions) {
					t.Fatal("new execution omitted bounded source contract")
				}
				text = strings.TrimSuffix(text, "\n"+sourceContextInstructions)
			}
			hash := sha256.Sum256([]byte(text))
			if hex.EncodeToString(hash[:]) != frozenPromptHash(t, tc.expected, false) {
				t.Fatal("new retry did not use the complete compatible prompt")
			}
			if store.Hash(next.GenerationSpec) != store.Hash(s.spec) {
				t.Fatal("choosing an execution protocol rewrote immutable generation policy")
			}
			old, err := s.engine.Store.Run(ctx, s.claim.Run.ID)
			if err != nil || store.Hash(old.Checkpoint) != store.Hash(ancestor.Checkpoint) {
				t.Fatal("new execution changed its ancestor", err)
			}
			inherited, err := s.engine.Store.ArticleDraft(ctx, run.ID)
			if err != nil || inherited.Version != 1 {
				t.Fatal("execution bootstrap changed inherited draft version", err)
			}
		})
	}
}

func TestNewQuestionFreezesExecutionIndependentlyOfArticlePolicy(t *testing.T) {
	s := planSession(t)
	ctx := context.Background()
	var owner, request string
	if err := s.engine.Store.Pool.QueryRow(ctx, "SELECT session_id,id FROM generation_requests WHERE active_run_id=$1", s.claim.Run.ID).Scan(&owner, &request); err != nil {
		t.Fatal(err)
	}
	s.spec.DetailPolicy.Revision = recoveryPolicy
	s.spec.DetailPolicyRevision = recoveryPolicy
	s.spec.Purpose = "理解来源范围"
	if _, err := s.engine.Store.Pool.Exec(ctx, "UPDATE generation_requests SET generation_spec=$2 WHERE id=$1", request, domain.JSON(s.spec)); err != nil {
		t.Fatal(err)
	}
	article := domain.Article{SchemaVersion: 1, SnapshotID: s.snapshot.ID, Title: "原来的文章", Sections: []domain.Section{}, Citations: map[string]domain.Citation{}, Limitations: []domain.Limitation{}, Quality: "standard"}
	if err := s.engine.Store.Publish(ctx, s.claim, article); err != nil {
		t.Fatal(err)
	}
	conversation, _, err := s.engine.Store.EnsureConversation(ctx, owner, request)
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.engine.Store.CreateTurn(ctx, owner, conversation.ID, domain.ID(), domain.QuestionInput{Question: "解释来源的关键事实"}, "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := s.engine.Store.Turn(ctx, owner, conversation.ID, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	claim, err := s.engine.Store.Claim(ctx, turn.RunID, 1, "new-question")
	if err != nil || claim == nil {
		t.Fatal(err)
	}
	requests := make(chan []executionWireMessage, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Messages []executionWireMessage `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
		}
		requests <- input.Messages
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()
	s.profile.ModelBaseURL = server.URL
	if _, err = s.engine.Store.Pool.Exec(ctx, "UPDATE runtime_config_revisions SET profile=$2 WHERE id=$1", claim.Run.ConfigID, domain.JSON(s.profile)); err != nil {
		t.Fatal(err)
	}
	if err = s.engine.Run(ctx, *claim, s.profile); err == nil {
		t.Fatal("capture provider should stop the run")
	}
	select {
	case messages := <-requests:
		text := wireText(t, messages[0])
		if !strings.HasSuffix(text, "\n"+sourceContextInstructions) {
			t.Fatal("new question omitted bounded source contract")
		}
		hash := sha256.Sum256([]byte(strings.TrimSuffix(text, "\n"+sourceContextInstructions)))
		if hex.EncodeToString(hash[:]) != frozenPromptHash(t, 16, true) {
			t.Fatal("new question inherited its article's old authoring protocol")
		}
	case <-time.After(time.Second):
		t.Fatal("new question did not reach the provider", err)
	}
	spec, err := s.engine.Store.GenerationForRun(ctx, claim.Run.ID)
	if err != nil || store.Hash(spec) != store.Hash(s.spec) {
		t.Fatal("question execution changed its article's immutable intent", err)
	}
	run, err := s.engine.Store.Run(ctx, claim.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	var cp checkpoint
	if err = json.Unmarshal(run.Checkpoint, &cp); err != nil {
		t.Fatal(err)
	}
	if cp.ExecutionContract == nil || cp.ExecutionContract.Version != domain.CurrentExecutionContract || cp.ExecutionContract.RunID != run.ID || cp.HarnessRevision != 16 || cp.Protocol != protocol {
		t.Fatal("question did not persist its own execution contract")
	}
}
