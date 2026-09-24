package service

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"unicode"

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
}

var videoQuestionCache = struct {
	sync.Mutex
	items map[int64]VideoQuestionResult
}{items: make(map[int64]VideoQuestionResult)}

// Suggestions are extracted from stored evidence. This endpoint never invokes a
// model; a content fingerprint makes repeated opens free and invalidates stale
// suggestions after transcript, summary, or visual evidence changes.
func (s *MediaService) VideoQuestions(userID, taskID int64) (VideoQuestionResult, error) {
	task, err := s.repo.Task.FindByID(taskID)
	if err != nil || task.UserID != userID {
		return VideoQuestionResult{}, fmt.Errorf("视频不存在或无权访问")
	}
	transcript, err := s.repo.Transcription.FindByTaskID(taskID)
	if err != nil {
		return VideoQuestionResult{}, err
	}
	if transcript == nil || strings.TrimSpace(transcript.Content) == "" {
		return VideoQuestionResult{Status: "waiting_transcription", Message: "转写尚未完成，完成后会根据实际视频内容显示推荐问题。", Questions: []VideoQuestion{}}, nil
	}
	summary, err := s.repo.Summary.FindByTaskID(taskID)
	if err != nil {
		return VideoQuestionResult{}, err
	}
	chunks, err := s.repo.TranscriptionChunk.ListByTaskID(taskID)
	if err != nil {
		return VideoQuestionResult{}, err
	}
	frames, err := s.repo.VisualFrame.ListCompletedWithText(taskID)
	if err != nil {
		return VideoQuestionResult{}, err
	}
	h := sha256.New()
	h.Write([]byte("questions-v1\x00" + transcript.Content))
	if summary != nil {
		h.Write([]byte("\x00" + summary.Content))
	}
	for _, chunk := range chunks {
		h.Write([]byte(fmt.Sprintf("\x00%d:%s", chunk.CoreStartMS, chunk.Content)))
	}
	for _, frame := range frames {
		h.Write([]byte(fmt.Sprintf("\x00%d:%s:%s", frame.TimeMs, frame.OCRText, frame.VisionCaption)))
	}
	version := hex.EncodeToString(h.Sum(nil))
	videoQuestionCache.Lock()
	if cached, ok := videoQuestionCache.items[taskID]; ok && cached.ContentVersion == version {
		videoQuestionCache.Unlock()
		return cached, nil
	}
	videoQuestionCache.Unlock()

	result := VideoQuestionResult{Status: "ready", Message: "根据已有转写、摘要及可用画面证据整理；候选问题不是视频原文或已确认结论。", Questions: []VideoQuestion{}, ContentVersion: version}
	seen := map[string]bool{}
	add := func(raw, source string, at *int64) {
		if len(result.Questions) >= 4 {
			return
		}
		phrase := questionPhrase(raw)
		if phrase == "" || seen[phrase] {
			return
		}
		seen[phrase] = true
		result.Questions = append(result.Questions, VideoQuestion{Question: fmt.Sprintf("视频中关于「%s」有哪些具体说明？", phrase), Source: source, Excerpt: phrase, TimeMS: at})
	}
	if summary != nil {
		for _, line := range strings.Split(summary.Content, "\n") {
			add(line, "摘要", nil)
		}
	}
	for _, chunk := range chunks {
		if chunk.Status != model.TranscriptionChunkStatusCompleted {
			continue
		}
		at := chunk.CoreStartMS
		add(chunk.Content, "转写", &at)
	}
	add(transcript.Content, "转写", nil)
	for _, frame := range frames {
		at := frame.TimeMs
		if frame.OCRText != "" {
			add(frame.OCRText, "画面文字", &at)
		} else {
			add(frame.VisionCaption, "画面描述", &at)
		}
	}
	if len(result.Questions) == 0 {
		result.Status, result.Message = "no_evidence", "已有转写内容不足以生成可信的推荐问题，可以直接输入自己的问题。"
	}
	videoQuestionCache.Lock()
	if len(videoQuestionCache.items) >= 256 {
		videoQuestionCache.items = make(map[int64]VideoQuestionResult)
	}
	videoQuestionCache.items[taskID] = result
	videoQuestionCache.Unlock()
	return result, nil
}

func questionPhrase(raw string) string {
	text := strings.TrimSpace(raw)
	text = strings.TrimLeft(text, "#-*•0123456789.、) ）\t ")
	text = strings.Trim(text, " *`：:。！？!?\t")
	if text == "" || strings.HasPrefix(text, "领域标签") || strings.HasPrefix(text, "核心摘要") || strings.HasPrefix(text, "深度洞察") || strings.HasPrefix(text, "原始内容精选") {
		return ""
	}
	for _, sep := range []string{"。", "！", "？", "\n", "；"} {
		if i := strings.Index(text, sep); i >= 0 {
			text = text[:i]
		}
	}
	runes := []rune(strings.TrimSpace(text))
	if len(runes) < 8 {
		return ""
	}
	if len(runes) > 32 {
		runes = runes[:32]
	}
	text = strings.TrimRightFunc(string(runes), func(r rune) bool { return unicode.IsSpace(r) || strings.ContainsRune("，,：:；;、-", r) })
	return text
}
