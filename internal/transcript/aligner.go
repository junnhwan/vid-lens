package transcript

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
	"unicode"

	"vid-lens/internal/model"
)

type Aligner interface {
	Align(context.Context, string, []model.VideoTranscriptionChunk) ([]model.VideoTranscriptionChunk, error)
}

// CommandAligner invokes a local acoustic model without a shell. The same
// process handles all windows of a task, loading model weights only once.
type CommandAligner struct {
	Command []string
	FFmpeg  string
	once    sync.Once
	slots   chan struct{}
}

func (a *CommandAligner) Align(ctx context.Context, audioPath string, rows []model.VideoTranscriptionChunk) ([]model.VideoTranscriptionChunk, error) {
	if len(a.Command) == 0 || strings.TrimSpace(a.Command[0]) == "" {
		return nil, fmt.Errorf("音文对齐命令未配置")
	}
	// Serialize local model loading across video workers to bound RAM use.
	a.once.Do(func() { a.slots = make(chan struct{}, 1) })
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case a.slots <- struct{}{}:
	}
	defer func() { <-a.slots }()
	ctx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	defer cancel()
	request, err := json.Marshal(struct {
		AudioPath string                          `json:"audio_path"`
		FFmpeg    string                          `json:"ffmpeg"`
		Rows      []model.VideoTranscriptionChunk `json:"rows"`
	}{audioPath, a.FFmpeg, rows})
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, a.Command[0], a.Command[1:]...)
	cmd.Stdin = bytes.NewReader(request)
	// The protocol owns stdout. Do not expose library diagnostics which may
	// contain transcript text, model download URLs or local paths to API users.
	var output bytes.Buffer
	cmd.Stdout = &output
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		var failure struct {
			Code string `json:"error_code"`
		}
		_ = json.Unmarshal(output.Bytes(), &failure)
		if failure.Code == "dependencies_missing" {
			return nil, fmt.Errorf("音文对齐依赖未安装，请检查配置的 Python 环境")
		}
		return nil, fmt.Errorf("音文对齐进程失败: %w", err)
	}
	var response struct {
		Rows []struct {
			Index int                          `json:"chunk_index"`
			Words []model.TranscriptionSegment `json:"words"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(output.Bytes(), &response); err != nil {
		return nil, fmt.Errorf("音文对齐响应无效")
	}
	if len(response.Rows) != len(rows) {
		return nil, fmt.Errorf("音文对齐未覆盖全部窗口")
	}
	out := append([]model.VideoTranscriptionChunk(nil), rows...)
	for i, result := range response.Rows {
		if result.Index != rows[i].ChunkIndex {
			return nil, fmt.Errorf("音文对齐窗口顺序不一致")
		}
		if err := ValidateAlignedWords(rows[i], result.Words); err != nil {
			return nil, fmt.Errorf("第 %d 段音文对齐无效: %w", i+1, err)
		}
		raw, err := json.Marshal(result.Words)
		if err != nil {
			return nil, err
		}
		out[i].TimedSegments = string(raw)
	}
	return out, nil
}

// ValidateAlignedWords requires full lexical coverage, source order and
// observed absolute timing. Punctuation remains part of the immutable text.
func ValidateAlignedWords(row model.VideoTranscriptionChunk, words []model.TranscriptionSegment) error {
	runes := []rune(strings.TrimSpace(row.Content))
	cursor := 0
	lastTime := int64(-1)
	for _, word := range words {
		if word.Method != "forced_alignment" || word.TextStart < cursor || word.TextEnd <= word.TextStart || word.TextEnd > len(runes) ||
			string(runes[word.TextStart:word.TextEnd]) != word.Text || word.StartMS < row.WindowStartMS || word.EndMS > row.WindowEndMS ||
			word.EndMS <= word.StartMS || word.StartMS < lastTime {
			return fmt.Errorf("原文位置或语音时间不合法")
		}
		for _, r := range runes[cursor:word.TextStart] {
			if unicode.IsLetter(r) || unicode.IsNumber(r) {
				return fmt.Errorf("对齐遗漏原文")
			}
		}
		cursor, lastTime = word.TextEnd, word.StartMS
	}
	for _, r := range runes[cursor:] {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return fmt.Errorf("对齐遗漏原文")
		}
	}
	return nil
}
