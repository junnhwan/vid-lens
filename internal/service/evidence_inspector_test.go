package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

type inspectorChatFunc func(context.Context, []ai.ChatMessage) (string, error)

func (f inspectorChatFunc) Chat(ctx context.Context, messages []ai.ChatMessage) (string, error) {
	return f(ctx, messages)
}

func inspectorFixture(t *testing.T) (*repository.Repositories, EvidenceLedgerRecordRequest, Citation) {
	t.Helper()
	repos, task, session := newVideoAgentTestSession(t)
	refs, _ := MarshalChunkSourceRefs([]ChunkSourceRef{{SourceType: "transcript", StableID: "atom-price", StartMS: 1000, EndMS: 2000, TimeRangeStatus: "exact"}})
	chunks := []model.VideoChunk{
		{UserID: 7, TaskID: task.ID, ChunkIndex: 0, Content: "价格是十元。", EmbeddingModel: "embed", EmbeddingDim: 3, VectorID: "price-ten", Modality: "transcript", StartMS: 1000, EndMS: 2000, TimeRangeStatus: "exact", SourceMappingStatus: "mapped", SourceRefs: refs},
		{UserID: 7, TaskID: task.ID, ChunkIndex: 1, Content: "更正：价格不是十元，而是二十元。", EmbeddingModel: "embed", EmbeddingDim: 3, VectorID: "price-correction", Modality: "visual_ocr", StartMS: 3000, EndMS: 4000, TimeRangeStatus: "exact", SourceMappingStatus: "mapped", SourceRefs: refs},
	}
	if err := repos.VideoChunk.ReplaceTaskChunks(task.ID, "embed", chunks); err != nil {
		t.Fatal(err)
	}
	stored, err := repos.VideoChunk.ListByTaskID(7, task.ID, "embed")
	if err != nil {
		t.Fatal(err)
	}
	citations := make([]Citation, len(stored))
	for i, chunk := range stored {
		citations[i] = Citation{TaskID: task.ID, CitationID: fmt.Sprintf("C%d", i+1), EvidenceID: chunk.VectorID, ChunkID: chunk.ID, Content: chunk.Content}
	}
	req := EvidenceLedgerRecordRequest{UserID: 7, TaskID: task.ID, SessionID: session.ID, MessageID: 10, RunID: "inspector-fixture", RawAnswer: "价格是十元。[C1]", Evidence: citations[:1]}
	policy, budget := defaultTemplateAgentPolicy(1)
	if _, err := NewAgentExecutionJournal(repos.AgentExecution).EnsureRun(context.Background(), AgentJournalRunRequest{RunID: req.RunID, UserID: 7, Session: session, Goal: "价格多少？", Mode: "agent", Policy: policy, Budget: budget}); err != nil {
		t.Fatal(err)
	}
	return repos, req, citations[1]
}

func inspectorJudge(t *testing.T, counterRelation string, calls *int) ai.ChatClient {
	t.Helper()
	return inspectorChatFunc(func(ctx context.Context, messages []ai.ChatMessage) (string, error) {
		*calls++
		if len(messages) != 2 || messages[0].Role != "system" || messages[1].Role != "user" {
			t.Fatalf("inspector context leaked: %+v", messages)
		}
		if strings.HasPrefix(messages[0].Content, "Generate one") {
			return `{"query":"价格 更正 不是十元 二十元"}`, nil
		}
		var input struct {
			Evidence []model.InspectedEvidence `json:"evidence"`
		}
		if err := json.Unmarshal([]byte(messages[1].Content), &input); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(messages[1].Content, "confidence") || strings.Contains(messages[1].Content, "planner-secret") {
			t.Fatal("planner confidence or scratchpad leaked")
		}
		verdicts := []map[string]string{}
		for _, e := range input.Evidence {
			relation := "support"
			if e.SourceRef == "price-correction" {
				relation = counterRelation
			}
			verdicts = append(verdicts, map[string]string{"source_ref": e.SourceRef, "relation": relation, "reason": "explicit price in the observed source"})
		}
		data, _ := json.Marshal(map[string]any{"evidence": verdicts})
		return string(data), nil
	})
}

func TestEvidenceInspectorSupportContradictAndInsufficient(t *testing.T) {
	for _, tc := range []struct {
		name, counter string
		missing       bool
		want          string
	}{
		{"support", "insufficient", false, "support"}, {"conflict takes precedence", "contradict", false, "contradict"}, {"unknown citation", "insufficient", true, "insufficient"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repos, req, counter := inspectorFixture(t)
			if tc.missing {
				req.RawAnswer = "价格是十元。[C1][C9]"
			}
			calls, searches := 0, 0
			inspector := &EvidenceInspector{repos: repos, model: "independent-model", chat: inspectorJudge(t, tc.counter, &calls), search: func(_ context.Context, q string) ([]Citation, error) {
				searches++
				if !strings.Contains(q, "更正") {
					t.Fatal("not a counter query")
				}
				return []Citation{counter}, nil
			}}
			checks, additional, err := inspector.Inspect(context.Background(), req)
			if err != nil || len(checks) != 1 || checks[0].Result != tc.want || calls != 2 || searches != 1 {
				t.Fatalf("checks=%+v err=%v calls=%d searches=%d", checks, err, calls, searches)
			}
			if inspectionsAllowPublication(checks, req.RawAnswer) != (tc.want == "support") {
				t.Fatal("publication gate disagrees")
			}
			req.Inspections, req.Retrieved = checks, additional
			ledger := NewEvidenceLedgerService(repos)
			if err := ledger.RecordAnswer(context.Background(), req); err != nil {
				t.Fatal(err)
			}
			if err := ledger.RecordAnswer(context.Background(), req); err != nil {
				t.Fatal(err)
			}
			view, err := ledger.GetRun(context.Background(), 7, req.RunID)
			if err != nil || view == nil || len(view.Claims) != 1 || len(view.ClaimEvidence) != 2 || view.Claims[0].Inspection.Result != tc.want {
				t.Fatalf("ledger=%+v err=%v", view, err)
			}
			if view.Claims[0].Inspection.Version != evidenceInspectorVersion || view.Claims[0].Inspection.Model != "independent-model" || view.Evidence[0].ContentHash == "" || view.Evidence[0].SourceRevisionStatus != model.EvidenceSourceRevisionUnavailable {
				t.Fatal("missing check provenance")
			}
			if tc.want == "contradict" && view.ClaimEvidence[0].Relation != "contradicts" && view.ClaimEvidence[1].Relation != "contradicts" {
				t.Fatal("counter evidence edge missing")
			}
			if other, err := ledger.GetRun(context.Background(), 8, req.RunID); err != nil || other != nil {
				t.Fatal("cross-owner inspection exposed")
			}
			correction, err := ledger.CorrectClaim(context.Background(), 7, view.Claims[0].ID, EvidenceClaimCorrectionRequest{Text: "价格是二十元。", Reason: "更正"})
			if err != nil || correction.Inspection != nil {
				t.Fatal("correction inherited approval")
			}
		})
	}
}

func TestEvidenceInspectorRejectsForgedQuoteAndScope(t *testing.T) {
	repos, req, _ := inspectorFixture(t)
	calls := 0
	inspector := &EvidenceInspector{repos: repos, chat: inspectorJudge(t, "insufficient", &calls), search: func(context.Context, string) ([]Citation, error) { return nil, nil }}
	req.Evidence[0].Content = "这是伪造的引用，planner confidence=1"
	checks, _, err := inspector.Inspect(context.Background(), req)
	if err != nil || checks[0].Result != "insufficient" || strings.Contains(checks[0].Evidence[0].Content, "伪造") {
		t.Fatalf("forged quote accepted: %+v %v", checks, err)
	}
	req.Evidence[0].TaskID++
	if _, _, err := inspector.Inspect(context.Background(), req); err == nil {
		t.Fatal("cross-task citation accepted")
	}
	req.Evidence[0].TaskID--
	req.UserID = 8
	checks, _, err = inspector.Inspect(context.Background(), req)
	if err != nil || checks[0].Result != "insufficient" || len(checks[0].Evidence) != 0 {
		t.Fatal("cross-owner source accepted")
	}
}

func TestEvidenceInspectorInvalidVerdictsFailClosed(t *testing.T) {
	for _, raw := range []string{
		`{"confidence":1,"result":"support"}`, `{"evidence":[{"source_ref":"forged","relation":"support","reason":"yes"}]}`,
		`{"evidence":[{"source_ref":"a","relation":"approved","reason":"yes"}]}`, `not-json`,
		`{"evidence":[{"source_ref":"a","relation":"support","reason":"yes"},{"source_ref":"a","relation":"support","reason":"yes"}]}`,
	} {
		check := model.ClaimInspection{Result: "insufficient", SearchCompleted: true, Evidence: []model.InspectedEvidence{{SourceRef: "a", Cited: true}}}
		applyInspectionVerdict(&check, raw, false)
		if check.Result != "insufficient" {
			t.Fatalf("accepted %s", raw)
		}
	}
}

func TestEvidenceInspectorFailureAndBudget(t *testing.T) {
	repos, req, _ := inspectorFixture(t)
	calls := 0
	inspector := &EvidenceInspector{repos: repos, chat: inspectorJudge(t, "insufficient", &calls), search: func(context.Context, string) ([]Citation, error) { return nil, errors.New("retrieval unavailable") }}
	checks, _, err := inspector.Inspect(context.Background(), req)
	if err != nil || checks[0].Result != "insufficient" || checks[0].SearchCompleted || calls != 1 {
		t.Fatal("search failure authorized answer")
	}
	req.RawAnswer = strings.Repeat("价格是十元。[C1]\n", 10)
	calls = 0
	checks, _, err = inspector.Inspect(context.Background(), req)
	if err != nil || len(checks) != 10 || calls != 8 || inspectionsAllowPublication(checks, req.RawAnswer) {
		t.Fatalf("budget escaped: calls=%d err=%v", calls, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := inspector.Inspect(ctx, req); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost")
	}
}

func TestInspectedPublicationPersistsBeforePublishAndReusesChecks(t *testing.T) {
	repos, req, _ := inspectorFixture(t)
	calls := 0
	inspector := &EvidenceInspector{repos: repos, model: "judge", chat: inspectorJudge(t, "insufficient", &calls), search: func(context.Context, string) ([]Citation, error) { return nil, nil }}
	checks, _, err := inspector.Inspect(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	req.Inspections = checks
	agent := NewVideoAgentService(NewChatService(repos, &fakeRetriever{}, ChatConfig{}))
	result := &VideoAgentResult{Answer: "价格是十元。", RunID: req.RunID, Model: "answerer"}
	publishFailure := errors.New("publish failed")
	agent.evidenceFunnelResultPublisher = func(_, _, _ int64, _, _, _ string) (bool, error) { return false, publishFailure }
	if err := agent.publishInspectedAnswer(context.Background(), "价格多少？", req, result); !errors.Is(err, publishFailure) {
		t.Fatal(err)
	}
	messages, _ := repos.Chat.ListMessages(7, req.SessionID)
	if len(messages) != 2 || messages[1].Content != evidenceFunnelPendingAnswer {
		t.Fatal("draft leaked into history")
	}
	retryReq := req
	retryReq.Inspections = nil
	if err := agent.inspectAnswer(context.Background(), &retryReq, result, nil, nil, ai.Profile{}); err != nil {
		t.Fatal(err)
	}
	if !retryReq.inspectionRecorded || len(retryReq.Inspections) != 1 {
		t.Fatal("saved check not recovered")
	}
	agent.evidenceFunnelResultPublisher = nil
	if err := agent.publishInspectedAnswer(context.Background(), "价格多少？", retryReq, result); err != nil {
		t.Fatal(err)
	}
	messages, _ = repos.Chat.ListMessages(7, req.SessionID)
	if len(messages) != 2 || messages[1].Content != result.Answer {
		t.Fatal("publication retry failed")
	}
}

func TestInspectedPublicationLedgerFailureNeverPublishes(t *testing.T) {
	repos, req, _ := inspectorFixture(t)
	agent := NewVideoAgentService(NewChatService(repos, &fakeRetriever{}, ChatConfig{}))
	agent.chatSvc.evidenceLedger = NewEvidenceLedgerService(&repository.Repositories{EvidenceLedger: repository.NewEvidenceLedgerRepository(nil)})
	result := &VideoAgentResult{Answer: "不可泄漏的强结论", RunID: req.RunID}
	if err := agent.publishInspectedAnswer(context.Background(), "价格多少？", req, result); err == nil {
		t.Fatal("ledger failure ignored")
	}
	messages, _ := repos.Chat.ListMessages(7, req.SessionID)
	if len(messages) != 2 || messages[1].Content != evidenceFunnelPendingAnswer {
		t.Fatal("unchecked answer persisted")
	}
}

func TestEvidenceInspectorReusesQueryVisualObservation(t *testing.T) {
	repos, req, _ := inspectorFixture(t)
	row := &model.VideoVisualObservation{ID: "query-observed", UserID: 7, TaskID: req.TaskID, CacheKey: "key", VideoRevision: "33333333333333333333333333333333", ArtifactKind: model.VisualArtifactKindFrame, ObjectKey: "frame.jpg", FrameRef: "frame-hash", StartMS: 1000, EndMS: 1001, Source: queryVisualSource, CapturePolicyVersion: queryVisualCapturePolicyVersion, Model: "investigator-model", PromptVersion: queryVisualPromptVersion, Status: model.VisualObservationStatusObserved, Observation: `{"facts":["价格是十元。"],"gaps":[]}`, StructuredFacts: `["价格是十元。"]`, StructuredGaps: `[]`}
	if err := repos.VisualObservation.Append(context.Background(), row); err != nil {
		t.Fatal(err)
	}
	req.Evidence = []Citation{{TaskID: req.TaskID, CitationID: "C1", EvidenceID: "visual-observation:" + row.ID, Content: "价格是十元。"}}
	calls := 0
	inspector := &EvidenceInspector{repos: repos, chat: inspectorJudge(t, "insufficient", &calls), search: func(context.Context, string) ([]Citation, error) { return nil, nil }}
	pixelVision := &investigatorVisionClient{response: `{"relation":"support","observation":"画面中直接可见价格为十元。","reason":"像素文字与 claim 一致。"}`}
	artifactPath := filepath.Join(t.TempDir(), "frame.jpg")
	if err := os.WriteFile(artifactPath, []byte("jpeg-pixels"), 0o600); err != nil {
		t.Fatal(err)
	}
	inspector.SetVisualPixelVerifier(pixelVision, "pixel-model", func(_ context.Context, objectKey string) (string, error) {
		if objectKey != row.ObjectKey {
			t.Fatalf("object key = %q, want %q", objectKey, row.ObjectKey)
		}
		return artifactPath, nil
	})
	checks, _, err := inspector.Inspect(context.Background(), req)
	if err != nil || checks[0].Result != "support" || checks[0].Evidence[0].Modality != "image" || !strings.Contains(checks[0].Evidence[0].SourceRefs, "frame-hash") {
		t.Fatalf("query image not inspected: %+v %v", checks, err)
	}
	evidence := checks[0].Evidence[0]
	if pixelVision.calls != 1 || !evidence.PixelChecked || evidence.PixelRelation != "support" || evidence.PixelModel != "pixel-model" || evidence.PixelPromptVersion != evidenceInspectorPixelVersion || evidence.ObjectKey != row.ObjectKey || evidence.ArtifactKind != model.VisualArtifactKindFrame || evidence.Source != row.Source || evidence.CapturePolicyVersion != row.CapturePolicyVersion || evidence.Model != row.Model || evidence.PromptVersion != row.PromptVersion || evidence.PixelObservationHash == "" {
		t.Fatalf("pixel evidence provenance incomplete: %+v calls=%d", evidence, pixelVision.calls)
	}
	req.Inspections = checks
	ledger := NewEvidenceLedgerService(repos)
	if err := ledger.RecordAnswer(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	view, err := ledger.GetRun(context.Background(), req.UserID, req.RunID)
	if err != nil || view == nil || len(view.Claims) != 1 || view.Claims[0].Inspection == nil {
		t.Fatalf("pixel inspection was not persisted: %+v err=%v", view, err)
	}
	stored := view.Claims[0].Inspection.Evidence[0]
	if stored.ObjectKey != row.ObjectKey || stored.PixelRelation != "support" || !strings.Contains(view.Evidence[0].StableLocator, `"pixel_prompt_version":"`+evidenceInspectorPixelVersion+`"`) {
		t.Fatalf("ledger lost pixel provenance: inspection=%+v evidence=%+v", stored, view.Evidence[0])
	}
	if inspectionsAllowPublication(checks, "另一条未经检查的结论。[C1]") {
		t.Fatal("approval reused for another claim")
	}
}

func TestEvidenceInspectorPixelFailureCannotSupportQueryVisualEvidence(t *testing.T) {
	repos, req, _ := inspectorFixture(t)
	row := &model.VideoVisualObservation{ID: "query-pixel-failure", UserID: 7, TaskID: req.TaskID, CacheKey: "pixel-failure-key", VideoRevision: "33333333333333333333333333333333", ArtifactKind: model.VisualArtifactKindFrame, ObjectKey: "query/frame.jpg", FrameRef: "pixel-failure-frame", StartMS: 1000, EndMS: 1001, Source: queryVisualSource, CapturePolicyVersion: queryVisualCapturePolicyVersion, Model: "investigator-model", PromptVersion: queryVisualPromptVersion, Status: model.VisualObservationStatusObserved, Observation: `{"facts":["价格是十元。"],"gaps":[]}`, StructuredFacts: `["价格是十元。"]`, StructuredGaps: `[]`}
	if err := repos.VisualObservation.Append(context.Background(), row); err != nil {
		t.Fatal(err)
	}
	req.Evidence = []Citation{{TaskID: req.TaskID, CitationID: "C1", EvidenceID: "visual-observation:" + row.ID, Content: "价格是十元。"}}
	calls := 0
	inspector := &EvidenceInspector{repos: repos, chat: inspectorJudge(t, "insufficient", &calls), search: func(context.Context, string) ([]Citation, error) { return nil, nil }}
	inspector.SetVisualPixelVerifier(&investigatorVisionClient{response: `not-json`}, "pixel-model", func(context.Context, string) (string, error) {
		return "", errors.New("object unavailable")
	})
	checks, _, err := inspector.Inspect(context.Background(), req)
	if err != nil || len(checks) != 1 || checks[0].Result != "insufficient" || checks[0].Evidence[0].PixelChecked || checks[0].Evidence[0].PixelRelation != "insufficient" || inspectionsAllowPublication(checks, req.RawAnswer) {
		t.Fatalf("pixel failure authorized publication: checks=%+v err=%v", checks, err)
	}
}

func TestEvidenceInspectorPixelContradictionWinsOverTextSupport(t *testing.T) {
	repos, req, _ := inspectorFixture(t)
	row := &model.VideoVisualObservation{ID: "query-pixel-contradiction", UserID: 7, TaskID: req.TaskID, CacheKey: "pixel-contradiction-key", VideoRevision: "33333333333333333333333333333333", ArtifactKind: model.VisualArtifactKindFrame, ObjectKey: "query/contradiction.jpg", FrameRef: "contradiction-frame", StartMS: 1000, EndMS: 1001, Source: queryVisualSource, CapturePolicyVersion: queryVisualCapturePolicyVersion, Model: "investigator-model", PromptVersion: queryVisualPromptVersion, Status: model.VisualObservationStatusObserved, Observation: `{"facts":["价格是十元。"],"gaps":[]}`, StructuredFacts: `["价格是十元。"]`, StructuredGaps: `[]`}
	if err := repos.VisualObservation.Append(context.Background(), row); err != nil {
		t.Fatal(err)
	}
	req.Evidence = []Citation{{TaskID: req.TaskID, CitationID: "C1", EvidenceID: "visual-observation:" + row.ID, Content: "价格是十元。"}}
	calls := 0
	inspector := &EvidenceInspector{repos: repos, chat: inspectorJudge(t, "insufficient", &calls), search: func(context.Context, string) ([]Citation, error) { return nil, nil }}
	inspector.SetVisualPixelVerifier(&investigatorVisionClient{response: `{"relation":"contradict","observation":"画面价格为二十元。","reason":"像素中的价格与 claim 冲突。"}`}, "pixel-model", func(context.Context, string) (string, error) {
		return filepath.Join(t.TempDir(), "contradiction.jpg"), nil
	})
	checks, _, err := inspector.Inspect(context.Background(), req)
	if err != nil || len(checks) != 1 || checks[0].Result != "contradict" || !strings.Contains(checks[0].Reason, "pixel") {
		t.Fatalf("pixel contradiction did not block publication: checks=%+v err=%v", checks, err)
	}
}

func TestEvidenceInspectorGatesStreamAndHistory(t *testing.T) {
	for _, relation := range []string{"insufficient", "contradict"} {
		t.Run(relation, func(t *testing.T) {
			repos, req, _ := inspectorFixture(t)
			chunks, _ := repos.VideoChunk.ListByTaskID(7, req.TaskID, "embed")
			hits := []RetrievedChunk{}
			for _, chunk := range chunks {
				hits = append(hits, RetrievedChunk{TaskID: req.TaskID, EvidenceID: chunk.VectorID, ChunkID: chunk.ID, ChunkIndex: chunk.ChunkIndex, Content: chunk.Content, Modality: chunk.Modality, StartMS: chunk.StartMS, EndMS: chunk.EndMS, TimeRangeStatus: "exact", SourceMappingStatus: "mapped"})
			}
			calls := 0
			judge := inspectorJudge(t, relation, &calls)
			client := inspectorChatFunc(func(ctx context.Context, messages []ai.ChatMessage) (string, error) {
				if strings.HasPrefix(messages[0].Content, "Generate one") || strings.HasPrefix(messages[0].Content, "You are an independent") {
					return judge.Chat(ctx, messages)
				}
				if strings.Contains(messages[0].Content, "查询改写") {
					return "not-json", nil
				}
				return req.RawAnswer, nil
			})
			agent := NewVideoAgentService(NewChatService(repos, &fakeRetriever{results: hits}, ChatConfig{TopK: 2, CandidateK: 2, MinScore: .01}))
			var streamed strings.Builder
			result, err := agent.Stream(context.Background(), VideoAgentStreamRequest{UserID: 7, SessionID: req.SessionID, Question: "价格是十元吗？", TopK: 2}, &fakeEmbeddingClient{dim: 3}, client, ai.Profile{EmbeddingModel: "embed", LLMModel: "judge"}, func(event AgentStreamEvent) error {
				if event.Type == AgentEventAnswer {
					streamed.WriteString(event.Data.(string))
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			want := "价格是十元。"
			if relation == "contradict" {
				want = inspectorBlockedAnswer
			}
			if result.Answer != want || streamed.String() != want {
				t.Fatalf("result=%q stream=%q want=%q", result.Answer, streamed.String(), want)
			}
			messages, _ := repos.Chat.ListMessages(7, req.SessionID)
			if len(messages) != 2 || messages[1].Content != want {
				t.Fatal("history bypassed inspection")
			}
			view, err := NewEvidenceLedgerService(repos).GetRun(context.Background(), 7, result.RunID)
			if err != nil || view == nil || len(view.Claims) != 1 || view.Claims[0].Inspection == nil {
				t.Fatal("stream published without an inspection")
			}
		})
	}
}

func TestInspectorPublicationUsesCommittedVerdict(t *testing.T) {
	repos, req, _ := inspectorFixture(t)
	calls := 0
	inspector := &EvidenceInspector{repos: repos, model: "judge", chat: inspectorJudge(t, "insufficient", &calls), search: func(context.Context, string) ([]Citation, error) { return nil, nil }}
	checks, _, err := inspector.Inspect(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	req.Inspections = checks
	req.Inspections[0].Result = "contradict"
	if err := NewEvidenceLedgerService(repos).RecordAnswer(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	// Simulate a concurrent worker whose support check lost the immutable insert.
	req.Inspections[0].Result = "support"
	agent := NewVideoAgentService(NewChatService(repos, &fakeRetriever{}, ChatConfig{}))
	result := &VideoAgentResult{Answer: "价格是十元。", RunID: req.RunID}
	if err := agent.publishInspectedAnswer(context.Background(), "价格多少？", req, result); err != nil {
		t.Fatal(err)
	}
	if result.Answer != inspectorBlockedAnswer {
		t.Fatal("in-memory approval overrode committed conflict")
	}
}
