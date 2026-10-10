package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"vid-lens/internal/ai"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
)

// These regressions verify prompt delivery through existing execution paths;
// fixture answers do not establish real-model semantic quality.
type summaryQualityChat struct {
	delegate ai.ChatClient
	messages [][]ai.ChatMessage
}

func (c *summaryQualityChat) Chat(ctx context.Context, messages []ai.ChatMessage) (string, error) {
	c.messages = append(c.messages, append([]ai.ChatMessage(nil), messages...))
	return c.delegate.Chat(ctx, messages)
}

type summaryQualityFactory struct {
	chat   ai.ChatClient
	visual *summaryVisualFixture
}

func (f summaryQualityFactory) NewChatClient(ai.Profile) (ai.ChatClient, error) { return f.chat, nil }
func (f summaryQualityFactory) NewVisionClient(profile ai.Profile) (ai.VisionClient, error) {
	return f.visual.NewVisionClient(profile)
}

func TestSummaryGenerationQualityInstructionsReachShortRepairAndLongReduction(t *testing.T) {
	for _, mode := range []string{"short", "repair", "long"} {
		t.Run(mode, func(t *testing.T) {
			f := newGenerationFixture(t, mode == "long")
			if mode == "repair" {
				f.chat.invalid = 1
			}
			recorder := &summaryQualityChat{delegate: f.chat}
			f.svc = NewSummaryGenerationService(f.repos, f.profiles, summaryQualityFactory{chat: recorder})
			if err := f.svc.Generate(context.Background(), f.task, f.job, f.job.ProcessingToken); err != nil {
				t.Fatal(err)
			}
			if mode == "short" && len(recorder.messages) != 1 || mode == "repair" && len(recorder.messages) != 2 || mode == "long" && len(recorder.messages) < 4 {
				t.Fatalf("unexpected execution shape: mode=%s calls=%d", mode, len(recorder.messages))
			}
			for _, messages := range recorder.messages {
				if messages[0].Role != "system" {
					t.Fatal("quality constraints were sent as source data")
				}
				for _, boundary := range []string{"假设的读者想法不能写成普遍看法", "推断（非原文明示）", "短来源", "不与正文机械重复", "不能静默纠正转写"} {
					if !strings.Contains(messages[0].Content, boundary) {
						t.Fatalf("%s provider call lost %q", mode, boundary)
					}
				}
			}
			published, err := f.repos.Summary.FindByTaskID(f.task.ID)
			if err != nil || published == nil || published.SourceDigest != f.source.SourceDigest {
				t.Fatal("prompt change disturbed canonical publication/source fence", err)
			}
		})
	}
}

func TestSummaryAnnotationQualityInstructionsReachCurrentHistoryAndAgent(t *testing.T) {
	quote, question := "可能做出成果，但结果仍有疑问。", "只解释原文中这两层意思。"
	annotations := []model.ContextAnnotation{{SummaryContextRef: model.SummaryContextRef{Quote: quote}, Provenance: "summary_selection"}}
	historyJSON := artifact.JSON(annotations)
	history := []model.ChatMessage{{Role: "user", Content: "上一轮讨论这段话", ContextAnnotationsJSON: &historyJSON}}
	video := buildVideoAssistantMessages("授权转写："+quote, nil, question)
	rag := buildRAGMessages(nil, nil, question)
	agent, err := renderPlannerMessages(VideoAgentLoopState{Goal: question, ContextAnnotations: annotations}, nil)
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string][]ai.ChatMessage{
		"video-current": appendAnnotationMessages(video, annotations),
		"rag-current":   appendAnnotationMessages(rag, annotations),
		"video-history": buildVideoAssistantMessages("授权转写："+quote, history, question),
		"rag-history":   buildRAGMessages(nil, history, question),
		"agent":         agent,
	}
	for path, messages := range paths {
		var all strings.Builder
		for _, message := range messages {
			all.WriteString(message.Content)
		}
		for _, boundary := range []string{"不能沿用摘要中被加强的断言", "推断（非原文明示）", "用户只要求解释原文时不追加", "转写为…，画面文字为…", "普通问答不得修改摘要"} {
			if !strings.Contains(all.String(), boundary) {
				t.Errorf("%s lost grounding boundary %q", path, boundary)
			}
		}
		if !strings.Contains(all.String(), quote) || !strings.Contains(messages[len(messages)-1].Content, question) {
			t.Errorf("%s changed frozen quote or user question", path)
		}
	}
}

func TestSummaryVisualQualityInstructionsReachSelectorWithFrozenEvidence(t *testing.T) {
	f := newGenerationFixture(t, false)
	visual := enableGenerationVisualFixture(t, f)
	recorder := &summaryQualityChat{delegate: visual}
	factory := summaryQualityFactory{chat: recorder, visual: visual}
	f.svc = NewSummaryGenerationService(f.repos, f.profiles, factory).WithVisualEnricher(NewSummaryVisualService(f.repos, factory, visual))
	if err := f.svc.Generate(context.Background(), f.task, f.job, f.job.ProcessingToken); err != nil {
		t.Fatal(err)
	}
	selected := 0
	for _, messages := range recorder.messages {
		if !strings.Contains(messages[0].Content, "已实际看图") {
			continue
		}
		selected++
		for _, boundary := range []string{"caption、alt、supports全部遵守证据边界", "不能从文字存在或文字条数推断", "不把普通字幕当成标签框", "转写为…，画面文字为…", "图与相邻结论只有间接关联"} {
			if !strings.Contains(messages[0].Content, boundary) {
				t.Errorf("selector lost visual grounding boundary %q", boundary)
			}
		}
		var input struct {
			Cues []struct {
				ID   string `json:"cue_id"`
				Text string `json:"text"`
			} `json:"frozen_cues"`
			Candidates []struct {
				ObservationID string `json:"observation_id"`
			} `json:"candidates"`
		}
		if err := json.Unmarshal([]byte(messages[1].Content), &input); err != nil {
			t.Fatal(err)
		}
		if len(input.Cues) != 1 || input.Cues[0].ID != f.source.Cues[0].ID || input.Cues[0].Text != f.source.Cues[0].Text || len(input.Candidates) != 1 || input.Candidates[0].ObservationID != visual.selectedID {
			t.Fatalf("selector lost exact frozen source/observed candidate: %+v", input)
		}
	}
	if selected != 1 || visual.inspectCalls != 1 {
		t.Fatalf("quality prompt added selection/inspection calls: selections=%d inspections=%d", selected, visual.inspectCalls)
	}
}
