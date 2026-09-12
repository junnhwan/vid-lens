package service

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"vid-lens/internal/model"
)

func TestVideoMapSamplesTailAndInvalidatesSource(t *testing.T) {
	text := "开头" + strings.Repeat("中段", 5000) + "第四步实验在结尾"
	sampled := boundedVideoText(text, 800)
	if len([]rune(sampled)) > 800 || !strings.Contains(sampled, "第四步实验在结尾") || !strings.Contains(sampled, "开头") {
		t.Fatalf("incomplete sample %s", sampled)
	}
	var rows []model.VideoTranscriptionChunk
	for i := 0; i < 30; i++ {
		rows = append(rows, model.VideoTranscriptionChunk{ID: int64(i + 1), ChunkIndex: i, Status: model.TranscriptionChunkStatusCompleted, Content: fmt.Sprintf("片段%d", i), CoreStartMS: int64(i) * 1000, CoreEndMS: int64(i+1) * 1000})
	}
	timeline := BuildVideoTimeline(1, rows, nil)
	m := buildVideoMap(1, "完整视频", text, timeline, 2000)
	if m.DurationMS != nil || len(m.Points) != 6 || m.Points[5].Content != "片段29" || m.Points[5].TimeRangeStatus != model.ChunkTimeRangeCoarse {
		t.Fatalf("map=%+v", m)
	}
	timeline.Atoms[20].Content = "源资产修改"
	if buildVideoMap(1, "完整视频", text, timeline, 2000).SourceVersion == m.SourceVersion {
		t.Fatal("source change did not invalidate identity")
	}
	unknown := buildVideoMap(1, "legacy", "", BuildVideoTimeline(1, []model.VideoTranscriptionChunk{{Status: model.TranscriptionChunkStatusCompleted, Content: text}}, nil), 1000)
	if unknown.Points[0].TimeRangeStatus != model.ChunkTimeRangeUnknown || !strings.Contains(unknown.Points[0].Content, "第四步实验在结尾") {
		t.Fatalf("legacy=%+v", unknown)
	}
}

func TestVideoMapRejectsAnotherOwner(t *testing.T) {
	repos, task, _ := newVideoAgentTestSession(t)
	svc := NewChatService(repos, nil, ChatConfig{})
	if _, err := svc.loadVideoMaps(context.Background(), task.UserID+1, []int64{task.ID}); err == nil {
		t.Fatal("cross-owner map read allowed")
	}
}

func TestComparisonEvidenceKeepsPartnerAndCitationOrder(t *testing.T) {
	var evidence []RetrievedChunk
	for i := 0; i < 14; i++ {
		evidence = append(evidence, RetrievedChunk{TaskID: 1, ChunkID: int64(i + 1), Content: fmt.Sprintf("A证据%d", i)})
	}
	evidence = append(evidence, RetrievedChunk{TaskID: 2, ChunkID: 30, Content: "B实际依据"})
	client := &scriptedChatClient{responses: []string{"B答案 [C2]"}}
	tools := NewVideoAgentTools(nil, nil, client)
	result, _, err := tools.BuildCitedAnswer(context.Background(), BuildCitedAnswerInput{Question: "比较A与B", ScopeTaskIDs: []int64{1, 2, 3}, Citations: evidence})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Citations) != 12 || result.Citations[1].TaskID != 2 || result.Citations[1].ChunkID != 30 {
		t.Fatalf("partner/order lost: %+v", result.Citations)
	}
	messages := buildCitedAnswerMessages(BuildCitedAnswerInput{Question: "比较", ScopeTaskIDs: []int64{1, 2, 3}, Citations: evidence}, nil)
	var prompt string
	for _, m := range messages {
		prompt += m.Content
	}
	for _, want := range []string{"缺少相关证据=[3]", "[C2] (task_id=2", "B实际依据"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("missing %s", want)
		}
	}
	if len(evidence) != 15 || evidence[1].TaskID != 1 {
		t.Fatal("canonical evidence mutated")
	}
}

func TestVideoMapUnknownTimeChunksPreserveTail(t *testing.T) {
	var rows []model.VideoTranscriptionChunk
	for i := 0; i < 20; i++ {
		rows = append(rows, model.VideoTranscriptionChunk{ID: int64(i + 1), ChunkIndex: i, Status: model.TranscriptionChunkStatusCompleted, Content: fmt.Sprintf("片段%d", i+1)})
	}
	timeline := BuildVideoTimeline(1, rows, nil)
	for i, atom := range timeline.Atoms {
		if atom.Content != rows[i].Content || atom.TimeRangeStatus != model.ChunkTimeRangeUnknown {
			t.Fatalf("unknown-time chunk order changed at %d: %+v", i, atom)
		}
	}
	m := buildVideoMap(1, "旧视频", "", timeline, 2000)
	if len(m.Points) != 6 || m.Points[0].Content != "片段1" || m.Points[5].Content != "片段20" {
		t.Fatalf("unknown-time map lost source tail: %+v", m.Points)
	}
	if m.DurationMS != nil || m.Points[5].StartMS != 0 || m.Points[5].EndMS != 0 || m.Points[5].TimeRangeStatus != model.ChunkTimeRangeUnknown {
		t.Fatalf("invented unknown time: %+v", m)
	}
}
