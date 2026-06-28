package service

import "testing"

func TestVideoAgentClassifiesIntentTemplates(t *testing.T) {
	tests := []struct {
		name     string
		question string
		want     VideoAgentTemplate
	}{
		{name: "direct qa", question: "视频里为什么要校验 owner？", want: VideoAgentDirectQA},
		{name: "summarize topic", question: "总结一下视频里 Redis 分布式锁的观点", want: VideoAgentSummarizeTopic},
		{name: "summarize synonym", question: "归纳作者对 RAG 优化的说法", want: VideoAgentSummarizeTopic},
		{name: "compare topic", question: "对比前后两段对 RAG 的区别", want: VideoAgentCompareTopics},
		{name: "compare change", question: "视频前后观点有什么变化？", want: VideoAgentCompareTopics},
		{name: "critique topic", question: "这段方案有什么风险和不足？", want: VideoAgentCritiqueTopic},
		{name: "critique rebuttal", question: "反驳一下这个说法哪里不严谨", want: VideoAgentCritiqueTopic},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyVideoAgentTemplate(tt.question)
			if got != tt.want {
				t.Fatalf("ClassifyVideoAgentTemplate(%q) = %q, want %q", tt.question, got, tt.want)
			}
		})
	}
}

func TestVideoAgentClassifiesUnknownQuestionAsDirectQA(t *testing.T) {
	got := ClassifyVideoAgentTemplate("owner 校验是什么？")
	if got != VideoAgentDirectQA {
		t.Fatalf("template = %q, want %q", got, VideoAgentDirectQA)
	}
}

func TestVideoAgentAskRejectsBlankQuestion(t *testing.T) {
	svc := &VideoAgentService{}
	_, err := svc.Ask(nil, VideoAgentRequest{Question: "   "})
	if err == nil {
		t.Fatal("Ask() succeeded for blank question")
	}
}
