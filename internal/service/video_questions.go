package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"vid-lens/internal/model"
)

type VideoQuestion struct {
	Question string `json:"question"`
	Source   string `json:"source"`
	Excerpt  string `json:"excerpt"`
	TimeMS   *int64 `json:"time_ms,omitempty"`
}

type VideoQuestionResult struct {
	Status         string          `json:"status"`
	Message        string          `json:"message"`
	Questions      []VideoQuestion `json:"questions"`
	ContentVersion string          `json:"content_version,omitempty"`
	MessageID      int64           `json:"message_id,omitempty"`
}

type videoQuestionEvidence struct {
	result                             VideoQuestionResult
	title, summary, transcript, visual string
}

// The read endpoint never invokes a model. Generation is an explicit POST.
func (s *MediaService) VideoQuestions(userID, taskID int64) (VideoQuestionResult, error) {
	evidence, err := s.videoQuestionEvidence(userID, taskID)
	return evidence.result, err
}

func (s *MediaService) videoQuestionEvidence(userID, taskID int64) (videoQuestionEvidence, error) {
	task, err := s.repo.Task.FindByID(taskID)
	if err != nil || task == nil || task.UserID != userID {
		return videoQuestionEvidence{}, fmt.Errorf("视频不存在或无权访问")
	}
	transcript, chunks, err := taskTranscriptSource(s.repo, task)
	if err != nil {
		return videoQuestionEvidence{}, err
	}
	evidence := videoQuestionEvidence{title: task.Title, result: VideoQuestionResult{Questions: []VideoQuestion{}}}
	frames, err := s.repo.VisualFrame.ListCompletedWithText(taskID)
	if err != nil {
		return evidence, err
	}
	if (transcript == nil || strings.TrimSpace(transcript.Content) == "") && len(frames) == 0 {
		evidence.result.Status, evidence.result.Message = "waiting_transcription", "转写完成后，会根据视频内容推荐问题。"
		return evidence, nil
	}
	if transcript != nil {
		evidence.transcript = transcript.Content
	}
	summary, err := s.repo.Summary.FindByTaskID(taskID)
	if err != nil {
		return evidence, err
	}
	if summary == nil && task.FileMD5 != "" {
		summary, err = s.repo.Summary.FindByMD5(task.FileMD5)
		if err != nil {
			return evidence, err
		}
	}
	if s.repo.SummaryRevision != nil {
		effective, readErr := s.repo.SummaryRevision.Effective(context.Background(), userID, taskID)
		if readErr != nil {
			return evidence, readErr
		}
		if effective.Revision != nil {
			summary = &model.AISummary{Content: effective.Content}
		}
	}
	h := sha256.New()
	h.Write([]byte("questions-v3\x00" + task.Title + "\x00" + evidence.transcript))
	if summary != nil {
		evidence.summary = summary.Content
		h.Write([]byte("\x00" + summary.Content))
	}
	for _, chunk := range chunks {
		h.Write([]byte(fmt.Sprintf("\x00%d:%s:%s", chunk.CoreStartMS, chunk.Status, chunk.Content)))
	}
	var visual strings.Builder
	for _, frame := range frames {
		h.Write([]byte(fmt.Sprintf("\x00%d:%s:%s", frame.TimeMs, frame.OCRText, frame.VisionCaption)))
		fmt.Fprintf(&visual, "%dms: %s %s\n", frame.TimeMs, frame.OCRText, frame.VisionCaption)
	}
	evidence.visual = visual.String()
	evidence.result = VideoQuestionResult{Status: "ready", Message: "从视频内容开始，试着问一个问题。", Questions: []VideoQuestion{}, ContentVersion: hex.EncodeToString(h.Sum(nil))}
	seen := map[string]bool{}
	add := func(raw, source string, at *int64) {
		if len(evidence.result.Questions) >= 4 {
			return
		}
		topic := questionPhrase(raw)
		if topic == "" || seen[topic] {
			return
		}
		seen[topic] = true
		question := naturalQuestion(topic, len(evidence.result.Questions))
		evidence.result.Questions = append(evidence.result.Questions, VideoQuestion{Question: question, Source: source, Excerpt: sampleQuestionText(raw, 160), TimeMS: at})
	}
	for _, chunk := range chunks {
		if chunk.Status == model.TranscriptionChunkStatusCompleted {
			at := chunk.CoreStartMS
			add(chunk.Content, "转写", &at)
		}
	}
	if evidence.transcript != "" {
		add(evidence.transcript, "转写", nil)
	}
	for _, frame := range frames {
		at := frame.TimeMs
		if frame.OCRText != "" {
			add(frame.OCRText, "画面文字", &at)
		} else {
			add(frame.VisionCaption, "画面描述", &at)
		}
	}
	if len(evidence.result.Questions) == 0 {
		source := "转写"
		if evidence.transcript == "" {
			source = "画面"
		}
		evidence.result.Questions = []VideoQuestion{{Question: "这段视频最值得记住的是什么？", Source: source, Excerpt: ""}}
	}
	return evidence, nil
}

func questionPhrase(raw string) string {
	text := strings.TrimSpace(strings.NewReplacer("**", "", "__", "", "`", "").Replace(raw))
	// Formatting rows and links are not video topics for fallback questions.
	if strings.Contains(text, "|") || strings.HasPrefix(text, "http://") || strings.HasPrefix(text, "https://") {
		return ""
	}
	text = strings.TrimLeft(text, "#-*•0123456789.、) ）\t ")
	text = strings.Trim(text, " *：:。！？!?\t")
	if text == "" || strings.Contains(text, "[已截断") || strings.Contains(text, "仅提供前半部分") {
		return ""
	}
	for _, heading := range []string{"领域标签", "核心摘要", "深度洞察", "原始内容精选", "视频摘要", "主要内容", "总结", "关键要点"} {
		if strings.HasPrefix(text, heading) {
			return ""
		}
	}
	for _, prefix := range []string{"本课程介绍", "本视频介绍", "这段视频介绍", "视频介绍", "本课程讲解", "今天介绍", "接下来介绍"} {
		text = strings.TrimPrefix(text, prefix)
	}
	for _, sep := range []string{"：", ":", "如何", "怎么", "。", "！", "？", "\n", "；", "，", "（", "("} {
		if i := strings.Index(text, sep); i >= 2 {
			text = text[:i]
		}
	}
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) < 2 || utf8.RuneCountInString(text) > 22 {
		return ""
	}
	return strings.TrimRightFunc(text, func(r rune) bool { return unicode.IsSpace(r) || strings.ContainsRune("，,：:；;、-", r) })
}

func naturalQuestion(topic string, index int) string {
	suffixes := []string{"的核心观点是什么？", "为什么值得关注？", "具体怎么理解？", "能举个例子解释吗？"}
	if strings.Contains(topic, "区别") || strings.Contains(topic, "对比") {
		return topic + "有什么不同？"
	}
	return topic + suffixes[index%len(suffixes)]
}
