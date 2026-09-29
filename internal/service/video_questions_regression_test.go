package service

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"
	"vid-lens/internal/model"
)

func TestVideoQuestionFallbackNeverLeaksContextTruncation(t *testing.T) {
	repos := newMediaTestRepositories(t)
	task := &model.VideoTask{UserID: 7, Filename: "lesson.mp4", Title: "模型技术分析", FileMD5: "long-question-fixture"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	if err := repos.Transcription.Create(&model.VideoTranscription{TaskID: task.ID, FileMD5: task.FileMD5, Content: "模型通过分类头输出下一步决策。"}); err != nil {
		t.Fatal(err)
	}
	if err := repos.Summary.Upsert(&model.AISummary{TaskID: task.ID, FileMD5: task.FileMD5, Content: "# Gev/Lava 模型技术评估与市场定位分析\n近期引发广泛关注的 Gev（含 Lava 系列）的机制与技术边界"}); err != nil {
		t.Fatal(err)
	}
	svc := NewQuestionSuggestionService(repos, nil, nil)
	result, err := svc.VideoQuestions(context.Background(), 7, task.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range result.Questions {
		if strings.Contains(q.Question, "截断") || strings.Contains(q.Question, "上下文") || utf8.RuneCountInString(q.Question) > 32 {
			t.Fatalf("unusable fallback: %q", q.Question)
		}
	}
}

func TestSuggestedQuestionsRejectInternalContextMarkers(t *testing.T) {
	questions, err := parseSuggestedQuestions(`{"questions":["Gev[已截断，仅提供前半部分上下文]的核心观点是什么？","模型有什么局限？","[摘要]能说明什么？","模型的上下文窗口有多大？"]}`, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(questions) != 2 || questions[0].Question != "模型有什么局限？" || questions[1].Question != "模型的上下文窗口有多大？" {
		t.Fatalf("questions=%+v", questions)
	}
}

func TestVideoQuestionFallbackSkipsMarkdownTables(t *testing.T) {
	for _, row := range []string{"| 名称 | 特点 |", "| --- | :---: |", "模型 | 分类输出", "-----", "https://example.test/video"} {
		if got := questionPhrase(row); got != "" {
			t.Fatalf("formatting became question topic: %q -> %q", row, got)
		}
	}
	if got := questionPhrase("## 分类头的作用"); got != "分类头的作用" {
		t.Fatalf("valid video topic lost: %q", got)
	}
}
