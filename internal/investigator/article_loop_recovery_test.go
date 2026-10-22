package investigator

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cloudwego/eino/schema"
	"vidlens/internal/domain"
)

func TestArticleLoopRejectedWriteKeepsRepairContext(t *testing.T) {
	for _, tc := range []struct {
		name, raw, path string
	}{
		{"nested_duplicate", `{"expected_version":0,"sections":[{"key":"chapter","title":"来源","content":[{"list":{"ordered":false,"items":[{"text":"来源事实","sources":["t1"],"sources":["t2"]}]}}]}]}`, "$.sections[0].content[0].list.items[0].sources"},
		{"unknown_envelope_field", `{"expected_version":0,"sections":[],"unexpected":true}`, "unknown field"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &session{cp: checkpoint{ArticleLoop: &articleLoopState{}}}
			msg := &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{Function: schema.FunctionCall{Name: loopSaveTool, Arguments: tc.raw}}}}
			progress, err := s.applyLoopResponse(context.Background(), articleLoopCall{Mode: "author"}, msg)
			if err != nil || progress {
				t.Fatalf("invalid envelope must stay rejected: progress=%v err=%v", progress, err)
			}
			var feedback struct {
				Detail   string `json:"detail"`
				Previous string `json:"previous_arguments"`
			}
			if err := json.Unmarshal(s.cp.ArticleLoop.Feedback, &feedback); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(feedback.Detail, tc.path) || feedback.Previous != tc.raw {
				t.Fatalf("author lost the failed write and its repair location: %s", s.cp.ArticleLoop.Feedback)
			}
		})
	}
}

func TestArticleLoopRejectedWriteRepairPublishesWithoutLosingContext(t *testing.T) {
	const bad = `{"expected_version":0,"sections":[{"key":"chapter","title":"来源","content":[{"list":{"ordered":false,"items":[{"text":"来源事实","sources":["t1"],"sources":["t2"]}]}}]}]}`
	var calls atomic.Int32
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
		p := loopPayload(t, r)
		n := calls.Add(1)
		if p["scope"] != nil {
			writeReviewWireArgs(w, loopReviewTool, loopGoodReport(t, p))
			return
		}
		if n == 1 {
			writeReviewWireArgs(w, loopSaveTool, json.RawMessage(bad))
			return
		}
		var feedback struct {
			Previous string `json:"previous_arguments"`
			Detail   string `json:"detail"`
		}
		if err := json.Unmarshal(p["feedback"], &feedback); err != nil || feedback.Previous != bad || !strings.Contains(feedback.Detail, "$.sections[0].content[0].list.items[0].sources") {
			t.Error("next real provider request lost rejected write repair context", string(p["feedback"]), err)
		}
		writeReviewWireArgs(w, loopSaveTool, loopGoodWrite())
	})
	if err := s.engine.Run(context.Background(), s.claim, s.profile); err != nil {
		t.Fatalf("engine failed after %d provider calls: %v", calls.Load(), err)
	}
	run, err := s.engine.Store.Run(context.Background(), s.claim.Run.ID)
	if err != nil || run.ResultID == nil || calls.Load() != 3 {
		t.Fatalf("repair did not reach publication within three calls: calls=%d %v", calls.Load(), err)
	}
	draft, err := s.engine.Store.ArticleDraft(context.Background(), s.claim.Run.ID)
	if err != nil || draft.Version != 1 {
		t.Fatalf("invalid envelope changed authoritative draft: version=%d %v", draft.Version, err)
	}
}

func TestArticleLoopRejectedWriteFeedbackIsBounded(t *testing.T) {
	raw := `{"x":` + string(domain.JSON(strings.Repeat("x", loopWriteBytes))) + `,"x":0}`
	s := &session{cp: checkpoint{ArticleLoop: &articleLoopState{}}}
	msg := &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{Function: schema.FunctionCall{Name: loopSaveTool, Arguments: raw}}}}
	progress, err := s.applyLoopResponse(context.Background(), articleLoopCall{Mode: "author"}, msg)
	var feedback struct {
		Previous string `json:"previous_arguments"`
		Omitted  int    `json:"previous_arguments_omitted_bytes"`
	}
	if err != nil || progress || json.Unmarshal(s.cp.ArticleLoop.Feedback, &feedback) != nil || feedback.Previous != "" || feedback.Omitted != len(raw) || len(s.cp.ArticleLoop.Feedback) > 1024 {
		t.Fatalf("oversized rejected write expanded repair context: progress=%v err=%v bytes=%d", progress, err, len(s.cp.ArticleLoop.Feedback))
	}
}

func TestArticleLoopPartialWriteFeedbackRetainsOnlyOriginalRejectedSections(t *testing.T) {
	const bad = `{"key":"bad","title":"待修正章节","content":[{"text":"保留被拒绝的语义内容","unexpected":true}]}`
	raw := []byte(`{"expected_version":0,"sections":[{"key":"good","title":"已保存"},` + bad + `]}`)
	result := map[string]any{"version": 1, "receipt": &loopReceipt{Accepted: []string{"stable-good"}, Created: map[string]string{"good": "stable-good"}, Rejected: []loopRejection{{Path: "/sections/1", Reason: "unknown field unexpected"}}}}
	var feedback struct {
		Version int         `json:"version"`
		Receipt loopReceipt `json:"receipt"`
		Parts   []struct {
			Path      string `json:"path"`
			Arguments string `json:"arguments"`
		} `json:"rejected_sections"`
	}
	if err := json.Unmarshal(loopWriteFeedback(result, raw), &feedback); err != nil {
		t.Fatal(err)
	}
	if feedback.Version != 1 || len(feedback.Receipt.Accepted) != 1 || feedback.Receipt.Accepted[0] != "stable-good" || len(feedback.Parts) != 1 || feedback.Parts[0].Path != "/sections/1" || feedback.Parts[0].Arguments != bad {
		t.Fatalf("partial repair lost the original rejected content or accepted receipt: %+v", feedback)
	}
}

func TestArticleLoopPartialWriteRepairPreservesAcceptedChapter(t *testing.T) {
	const bad = `{"key":"repair","title":"补充背景","content":[{"background":"用于演示局部修复的合成背景解释，修复这一章节时不得重写已经保存的来源正文。","unexpected":true}]}`
	var calls atomic.Int32
	var stable string
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
		p := loopPayload(t, r)
		n := calls.Add(1)
		if p["scope"] != nil {
			writeReviewWireArgs(w, loopReviewTool, loopGoodReport(t, p))
			return
		}
		if n == 1 {
			good := loopGoodWrite()
			good.Finish = false
			var first map[string]any
			_ = json.Unmarshal(domain.JSON(good), &first)
			first["sections"] = []json.RawMessage{domain.JSON(good.Sections[0]), json.RawMessage(bad)}
			writeReviewWireArgs(w, loopSaveTool, first)
			return
		}
		var feedback struct {
			Version int         `json:"version"`
			Receipt loopReceipt `json:"receipt"`
			Parts   []struct {
				Arguments string `json:"arguments"`
			} `json:"rejected_sections"`
		}
		if err := json.Unmarshal(p["feedback"], &feedback); err != nil || feedback.Version != 1 || len(feedback.Receipt.Accepted) != 1 || len(feedback.Parts) != 1 || feedback.Parts[0].Arguments != bad {
			t.Error("next request cannot repair only the rejected chapter", string(p["feedback"]), err)
		} else {
			stable = feedback.Receipt.Accepted[0]
		}
		background := "用于演示局部修复的合成背景解释，修复这一章节时不得重写已经保存的来源正文。"
		writeReviewWireArgs(w, loopSaveTool, loopSaveInput{ExpectedVersion: 1, Finish: true, Sections: []loopSection{{Key: "repair", Title: "补充背景", Content: []loopContent{{Background: &background}}}}})
	})
	if err := s.engine.Run(context.Background(), s.claim, s.profile); err != nil {
		t.Fatal(err)
	}
	draft, err := s.engine.Store.ArticleDraft(context.Background(), s.claim.Run.ID)
	var saved articleLoopDraft
	if err != nil || json.Unmarshal(draft.Data, &saved) != nil || draft.Version != 2 || len(saved.Article.Sections) != 2 || saved.Article.Sections[0].ID != stable || calls.Load() != 3 {
		t.Fatalf("local repair changed accepted chapter or added work: version=%d calls=%d err=%v", draft.Version, calls.Load(), err)
	}
}
