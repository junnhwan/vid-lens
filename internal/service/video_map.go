package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"vid-lens/internal/model"
)

// VideoMap locates material; only tool observations may become cited evidence.
// Duration is unknown unless a durable measured duration exists.
type VideoMap struct {
	TaskID        int64          `json:"task_id"`
	Title         string         `json:"title"`
	DurationMS    *int64         `json:"duration_ms"`
	Modalities    []string       `json:"modalities"`
	Summary       string         `json:"summary,omitempty"`
	Points        []TimelineAtom `json:"locating_points,omitempty"`
	SourceVersion string         `json:"source_version"`
	Coverage      string         `json:"coverage"`
}

// boundedVideoText samples the entire source, including its tail. Excerpts
// carry no fabricated timestamps or claim that the omitted middle was read.
func boundedVideoText(text string, limit int) string {
	text = strings.TrimSpace(text)
	runes := []rune(text)
	if limit <= 0 {
		return ""
	}
	if len(runes) <= limit {
		return text
	}
	const separator = "\n[…节选，间隔内容省略…]\n"
	count := 4
	if limit < 160 {
		count = 2
	}
	width := (limit - (count-1)*len([]rune(separator))) / count
	if width < 1 {
		return trimRunes(text, limit)
	}
	parts := make([]string, count)
	for i := range parts {
		start := i * (len(runes) - width) / (count - 1)
		parts[i] = string(runes[start : start+width])
	}
	return strings.Join(parts, separator)
}

func (s *ChatService) loadVideoMaps(ctx context.Context, userID int64, taskIDs []int64) ([]VideoMap, error) {
	if s.repos == nil || s.repos.Task == nil {
		return nil, nil
	}
	maps := make([]VideoMap, 0, min(len(taskIDs), 8))
	for _, id := range taskIDs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(maps) == 8 {
			break
		}
		task, err := s.repos.Task.FindByID(id)
		if err != nil {
			return nil, err
		}
		if task == nil || task.UserID != userID {
			return nil, fmt.Errorf("无权访问视频地图")
		}
		var summary, transcript string
		if s.repos.Summary != nil {
			row, err := s.repos.Summary.FindByTaskID(id)
			if err != nil {
				return nil, err
			}
			if row != nil {
				summary = row.Content
			}
		}
		if s.repos.Transcription != nil {
			row, err := s.repos.Transcription.FindByTaskID(id)
			if err != nil {
				return nil, err
			}
			if row != nil {
				transcript = row.Content
			}
		}
		var rows []model.VideoTranscriptionChunk
		if s.repos.TranscriptionChunk != nil {
			rows, err = s.repos.TranscriptionChunk.ListByTaskID(id)
			if err != nil {
				return nil, err
			}
		}
		if len(rows) == 0 && transcript != "" {
			rows = []model.VideoTranscriptionChunk{{TaskID: id, Status: model.TranscriptionChunkStatusCompleted, Content: transcript}}
		}
		var frames []model.VideoVisualFrame
		if s.repos.VisualFrame != nil {
			frames, err = s.repos.VisualFrame.ListCompletedWithText(id)
			if err != nil {
				return nil, err
			}
		}
		title := task.Title
		if title == "" {
			title = task.Filename
		}
		maps = append(maps, buildVideoMap(id, title, summary, BuildVideoTimeline(id, rows, frames), max(280, 3200/min(len(taskIDs), 8))))
	}
	return maps, nil
}

func buildVideoMap(id int64, title, summary string, timeline VideoTimeline, textBudget int) VideoMap {
	raw, _ := json.Marshal(struct {
		Summary  string
		Timeline VideoTimeline
	}{summary, timeline})
	digest := sha256.Sum256(raw)
	m := VideoMap{TaskID: id, Title: trimRunes(title, 100), Summary: boundedVideoText(summary, textBudget/2), SourceVersion: "video-map.v1:" + hex.EncodeToString(digest[:]), Coverage: "从当前持久资产跨全片均匀抽样，仅用于定位；省略内容与未知时间不可推断。"}
	seen := map[string]bool{}
	for _, a := range timeline.Atoms {
		if !seen[a.Modality] {
			m.Modalities = append(m.Modalities, a.Modality)
			seen[a.Modality] = true
		}
	}
	count := min(6, len(timeline.Atoms))
	for i := 0; i < count; i++ {
		index := 0
		if count > 1 {
			index = i * (len(timeline.Atoms) - 1) / (count - 1)
		}
		a := timeline.Atoms[index]
		a.Content = boundedVideoText(a.Content, max(80, textBudget/2/max(count, 1)))
		// The map is not a citation pool. Keep identity/time but no private asset keys.
		a.SourceRefs = nil
		m.Points = append(m.Points, a)
	}
	return m
}

func evidenceCoveragePrompt(scope []int64, evidence []RetrievedChunk) string {
	if len(scope) < 2 {
		return ""
	}
	present := map[int64]bool{}
	for _, c := range evidence {
		present[c.TaskID] = true
	}
	var covered, missing []int64
	for _, id := range scope {
		if present[id] {
			covered = append(covered, id)
		} else {
			missing = append(missing, id)
		}
	}
	return fmt.Sprintf("当前可用知识库范围 task_ids=%v；本次可引用证据覆盖=%v；缺少相关证据=%v。仅比较用户所问的视频，不要求覆盖无关成员。分别核对被比较各方，不能用一方推断另一方；被比较的一方缺证必须明确说明，不能用无关片段凑覆盖。", scope, covered, missing)
}

// Select only observed evidence, round-robin by task to prevent one video's
// earlier hits from evicting all relevant evidence for the comparison partner.
func balancedEvidence(evidence []RetrievedChunk, limit int) []RetrievedChunk {
	groups := map[int64][]RetrievedChunk{}
	var order []int64
	for _, c := range evidence {
		if _, ok := groups[c.TaskID]; !ok {
			order = append(order, c.TaskID)
		}
		groups[c.TaskID] = append(groups[c.TaskID], c)
	}
	var result []RetrievedChunk
	for row := 0; len(result) < limit; row++ {
		added := false
		for _, id := range order {
			if row < len(groups[id]) {
				result = append(result, groups[id][row])
				added = true
				if len(result) == limit {
					break
				}
			}
		}
		if !added {
			break
		}
	}
	return result
}
