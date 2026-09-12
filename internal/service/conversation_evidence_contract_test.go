package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"vid-lens/internal/model"
)

func TestNeighborEvidenceCanBeCanonicalizedAndBoundToOriginalGoal(t *testing.T) {
	repos := newChatServiceTestRepositories(t)
	seedVideoChunks(t, repos, 7, 1, "embed", []string{"first step", "second step contains answer", "third step"})
	tools := NewVideoAgentTools(repos, nil, nil)
	window, _, err := tools.GetTranscriptWindow(context.Background(), TranscriptWindowInput{UserID: 7, TaskID: 1, EmbeddingModel: "embed", ChunkIndex: 0, Radius: 1})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(window)
	observation, err := (DefaultVideoAgentLoopObserver{}).Observe(VideoAgentLoopState{}, VideoAgentToolResult{Step: VideoAgentStep{Tool: VideoAgentToolGetTranscriptWindow}, Output: raw})
	if err != nil || len(observation.NewEvidence) != 2 {
		t.Fatalf("neighbor evidence: %+v %v", observation, err)
	}
	b := observation.NewEvidence[1]
	args, _ := json.Marshal(buildCitedAnswerToolArguments{Question: "planner substituted goal", Citations: []RetrievedChunk{{TaskID: b.TaskID, ChunkID: b.ChunkID}}})
	runner := &VideoAgentLoopRunner{registry: tools.Registry()}
	decision, err := runner.validatedResearchDecision(VideoAgentLoopState{Goal: "original question", Evidence: observation.NewEvidence}, 1, VideoAgentLoopDecision{Tool: VideoAgentToolBuildCitedAnswer, Reason: "answer", Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	var bound buildCitedAnswerToolArguments
	_ = json.Unmarshal(decision.Arguments, &bound)
	if bound.Question != "original question" || bound.Citations[0].Content != b.Content || bound.Citations[0].Source != "transcript_window" {
		t.Fatalf("bound=%+v", bound)
	}
	if _, err := canonicalizeResearchCitations(observation.NewEvidence, 2, bound.Citations); err == nil {
		t.Fatal("accepted foreign scope")
	}
	empty, _, err := tools.GetTranscriptWindow(context.Background(), TranscriptWindowInput{UserID: 7, TaskID: 2, EmbeddingModel: "embed"})
	if err != nil || len(empty.Evidence) != 0 {
		t.Fatalf("empty is observation: %+v %v", empty, err)
	}
}

func TestConversationHistoryReachesPlannerAndWriterWithoutSnapshots(t *testing.T) {
	recent := []model.ChatMessage{{Role: "user", Content: "展开第二步"}, {Role: "assistant", Content: "第二步是拆解问题"}, {Role: "user", Content: "刚才那一步如何操作？"}, {Role: "tool", Content: "private tool json"}}
	client := &recordingChatClient{}
	state := VideoAgentLoopState{Goal: "举个例子", Conversation: boundedConversationContext(recent)}
	_, _ = NewLLMVideoAgentLoopPlanner(client).NextDecision(context.Background(), state, nil)
	encoded, _ := json.Marshal(client.messages)
	if !strings.Contains(string(encoded), "第二步是拆解问题") || strings.Contains(string(encoded), "private tool json") {
		t.Fatalf("planner messages=%s", encoded)
	}
	tools := NewVideoAgentTools(nil, nil, client)
	_, _, _ = tools.BuildCitedAnswer(context.Background(), BuildCitedAnswerInput{Question: "举个例子", Recent: recent})
	encoded, _ = json.Marshal(client.messages)
	if !strings.Contains(string(encoded), "第二步是拆解问题") {
		t.Fatalf("writer messages=%s", encoded)
	}
}
