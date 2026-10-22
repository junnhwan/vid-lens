package investigator

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
	"vidlens/internal/domain"
)

func TestQuestionWorkingContextKeepsDistantOriginalsAndDeduplicatesReads(t *testing.T) {
	s := &session{turn: &domain.Turn{}, snapshot: domain.Snapshot{ID: "snapshot"}, cp: checkpoint{ExecutionContract: &domain.ExecutionContract{Version: domain.ExecutionContractV9, InputRevision: domain.QuestionReadingV2}}}
	for i := 0; i < 3; i++ {
		s.transcript = append(s.transcript, domain.Segment{ID: fmt.Sprint("original", i), StartMS: int64(i * 100), EndMS: int64((i + 1) * 100), Text: fmt.Sprint("Unique necessary original ", i), Origin: "asr"})
	}
	messages := []*schema.Message{}
	for i, source := range []int{0, 1, 2, 0} {
		segment := s.transcript[source]
		id := fmt.Sprint("call", i)
		passage := sourcePassage{Segment: segment, Alias: fmt.Sprint("t", source+1), TextEnd: len([]rune(segment.Text))}
		messages = append(messages, &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{ID: id, Function: schema.FunctionCall{Name: "read_source", Arguments: "{}"}}}}, &schema.Message{Role: schema.Tool, ToolName: "read_source", ToolCallID: id, Content: string(domain.JSON(map[string]any{"snapshot_id": "snapshot", "segments": []sourcePassage{passage}, "has_more": false}))})
	}
	projected := s.projectSourceReads(messages)
	if err := validateProtocol(projected); err != nil {
		t.Fatal(err)
	}
	raw := string(domain.JSON(projected))
	for _, segment := range s.transcript {
		if strings.Count(raw, segment.Text) != 1 {
			t.Fatal("a necessary earlier original was evicted or repeated", segment.Text)
		}
	}
	if domain.ContentHash(s.projectSourceReads(projected)) != domain.ContentHash(projected) {
		t.Fatal("checkpoint projection changed on resume")
	}
	if !strings.Contains(raw, "originals_in_prior_results") {
		t.Fatal("duplicate read lost its explicit earlier delivery receipt")
	}
	s.cp.ExecutionContract.InputRevision = domain.QuestionReadingV1
	legacy := s.projectSourceReads(messages)
	if strings.Contains(legacy[1].Content, s.transcript[0].Text) {
		t.Fatal("historical fixed-history QA projection changed")
	}
}

func TestQuestionWorkingContextDoesNotExpandAnUnreadSourceSlice(t *testing.T) {
	segment := domain.Segment{ID: "original", Text: "Read prefix. UNREAD SUFFIX", StartMS: 0, EndMS: 100, Origin: "asr"}
	s := &session{snapshot: domain.Snapshot{ID: "snapshot"}, transcript: []domain.Segment{segment}}
	passage := sourcePassage{Segment: segment, Alias: "t1", TextOffset: 0, TextEnd: 12}
	passage.Text = string([]rune(segment.Text)[:12])
	message := &schema.Message{Role: schema.Tool, ToolCallID: "read", ToolName: "read_source", Content: string(domain.JSON(map[string]any{"snapshot_id": "snapshot", "segments": []sourcePassage{passage}, "has_more": true, "next_offset": 1}))}
	projected := s.projectQuestionSourceReads([]*schema.Message{message})
	if strings.Contains(projected[0].Content, "UNREAD SUFFIX") || !strings.Contains(projected[0].Content, `"has_more":true`) || !strings.Contains(projected[0].Content, `"next_offset":1`) {
		t.Fatal("source working context invented unread text or lost pagination")
	}
}
