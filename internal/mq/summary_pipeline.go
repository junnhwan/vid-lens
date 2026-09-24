package mq

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
	"vid-lens/internal/transcript"
)

// A byte is a conservative upper bound for a text token in the supported
// OpenAI-compatible tokenizers. Keep room for the fixed prompt, protocol
// overhead, output and provider variance. Operators can lower this for a
// smaller model; a larger value requires an explicit configured window.
const summaryDefaultContextTokens = 8192
const summaryOutputTokens = 2048
const summaryIntermediateOutputTokens = 1536
const summaryReservedTokens = 2048
const summaryPromptVersion = "summary_tree_v1"

type summaryInput struct {
	text           string
	startMS, endMS int64
}

// Keep the original ASR-boundary plan for tasks that already have matching
// checkpoints. Changing their leaves midway would discard completed calls.
func splitSummaryInputLegacy(in summaryInput, limit int) []summaryInput {
	if len(in.text) <= limit {
		return []summaryInput{in}
	}
	runes := []rune(in.text)
	result := make([]summaryInput, 0)
	for start := 0; start < len(runes); {
		end, size := start, 0
		for end < len(runes) && size+utf8.RuneLen(runes[end]) <= limit {
			size += utf8.RuneLen(runes[end])
			end++
		}
		if end == start {
			end++
		}
		part := summaryInput{text: string(runes[start:end]), startMS: in.startMS, endMS: in.endMS}
		if in.endMS > in.startMS {
			span := in.endMS - in.startMS
			part.startMS = in.startMS + span*int64(start)/int64(len(runes))
			part.endMS = in.startMS + span*int64(end)/int64(len(runes))
		}
		result = append(result, part)
		start = end
	}
	return result
}

func summaryInputLimit(profileWindow int) (int, error) {
	window := profileWindow
	if window == 0 {
		window = summaryDefaultContextTokens
	}
	if raw := strings.TrimSpace(os.Getenv("VIDLENS_SUMMARY_CONTEXT_TOKENS")); raw != "" && profileWindow == 0 {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 4096 {
			return 0, fmt.Errorf("VIDLENS_SUMMARY_CONTEXT_TOKENS 必须为不小于 4096 的整数")
		}
		window = parsed
	}
	limit := window - summaryOutputTokens - summaryReservedTokens
	if limit <= 512 {
		return 0, fmt.Errorf("摘要模型上下文不足以保留输出预算")
	}
	return limit, nil
}

func summaryHash(modelName, input string) string {
	h := sha256.Sum256([]byte(summaryPromptVersion + "\x00" + modelName + "\x00" + input))
	return hex.EncodeToString(h[:])
}

func summaryInputs(full string, rows []model.VideoTranscriptionChunk) []summaryInput {
	observations := make([]string, 0, len(rows))
	valid := make([]model.VideoTranscriptionChunk, 0, len(rows))
	for _, row := range rows {
		if row.Status == model.TranscriptionChunkStatusCompleted && strings.TrimSpace(row.Content) != "" {
			observations = append(observations, row.Content)
			valid = append(valid, row)
		}
	}
	inputs := make([]summaryInput, 0)
	if len(valid) > 0 {
		stitched := transcript.Stitch(observations)
		if !summaryRowsOverlap(valid) {
			stitched.Content = strings.Join(observations, "\n\n")
			stitched.Contributions = nil
			for i, text := range observations {
				if i > 0 {
					text = "\n\n" + text
				}
				stitched.Contributions = append(stitched.Contributions, transcript.Contribution{PartIndex: i, Content: text})
			}
		}
		if stitched.Content == full {
			for _, contribution := range stitched.Contributions {
				row := valid[contribution.PartIndex]
				start, end := row.CoreStartMS, row.CoreEndMS
				if end <= start {
					start, end = row.WindowStartMS, row.WindowEndMS
				}
				if end <= start {
					start, end = int64(row.StartSecond)*1000, int64(row.EndSecond)*1000
				}
				inputs = append(inputs, summaryInput{text: contribution.Content, startMS: start, endMS: end})
			}
		}
	}
	if len(inputs) == 0 {
		inputs = []summaryInput{{text: full}}
	} // legacy or changed ASR: cover canonical text, no invented time
	return inputs
}

func summaryLeaves(full string, rows []model.VideoTranscriptionChunk, limit int) []summaryInput {
	return packSummaryLeaves(summaryInputs(full, rows), limit)
}

func summaryLeavesLegacy(full string, rows []model.VideoTranscriptionChunk, limit int) []summaryInput {
	var leaves []summaryInput
	for _, input := range summaryInputs(full, rows) {
		leaves = append(leaves, splitSummaryInputLegacy(input, limit)...)
	}
	return leaves
}

// ASR windows are audio boundaries, not an appropriate unit of model work.
// Fill each model input up to its byte budget while preserving the canonical
// transcript and chronological order. A split within an ASR window gets an
// interpolated range; unknown timing stays unknown.
func packSummaryLeaves(inputs []summaryInput, limit int) []summaryInput {
	packed := make([]summaryInput, 0, len(inputs))
	for _, input := range inputs {
		runes := []rune(input.text)
		for start := 0; start < len(runes); {
			if len(packed) == 0 || len(packed[len(packed)-1].text) == limit {
				packed = append(packed, summaryInput{})
			}
			last := &packed[len(packed)-1]
			room := limit - len(last.text)
			end, size := start, 0
			for end < len(runes) && size+utf8.RuneLen(runes[end]) <= room {
				size += utf8.RuneLen(runes[end])
				end++
			}
			if end == start {
				if last.text == "" {
					end = start + 1 // Caller reports an over-budget prompt for an impossible tiny limit.
				} else {
					packed = append(packed, summaryInput{})
					continue
				}
			}
			pieceStart, pieceEnd := int64(0), int64(0)
			if input.endMS > input.startMS {
				span := input.endMS - input.startMS
				pieceStart = input.startMS + span*int64(start)/int64(len(runes))
				pieceEnd = input.startMS + span*int64(end)/int64(len(runes))
			}
			if last.text == "" {
				last.startMS, last.endMS = pieceStart, pieceEnd
			} else if last.endMS <= last.startMS || pieceEnd <= pieceStart {
				last.startMS, last.endMS = 0, 0
			} else {
				last.endMS = pieceEnd
			}
			last.text += string(runes[start:end])
			start = end
		}
	}
	return packed
}

func summaryRowsOverlap(rows []model.VideoTranscriptionChunk) bool {
	for i := 1; i < len(rows); i++ {
		if rows[i].WindowStartMS < rows[i-1].WindowEndMS && rows[i].WindowEndMS > 0 {
			return true
		}
	}
	return false
}

func summaryPartPrompt(input summaryInput, index, total int) string {
	return fmt.Sprintf("这是视频完整转写的第 %d/%d 段，时间范围 %d–%d 毫秒。请仅根据本段整理要点，保留关键事实和时间线；按现有报告的四个栏目输出，供后续全片合并。\n\n%s", index+1, total, input.startMS, input.endMS, input.text)
}

func summaryMergePrompt(parts []summaryInput, level int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "以下是覆盖视频连续时间段的摘要（合并层级 %d）。请逐段吸收所有内容，按现有报告的四个栏目合并为一份客观专业的 Markdown 报告，保留各段独有的重要信息与时间线，不得只挑相关片段。\n", level)
	for i, part := range parts {
		fmt.Fprintf(&b, "\n第 %d 段（%d–%d 毫秒）：\n%s\n", i+1, part.startMS, part.endMS, part.text)
	}
	return b.String()
}

func packSummaryMerge(parts []summaryInput, limit, level int) ([][]summaryInput, error) {
	var groups [][]summaryInput
	for i := 0; i < len(parts); {
		group := []summaryInput{parts[i]}
		if len(summaryMergePrompt(group, level)) > limit {
			return nil, fmt.Errorf("合并摘要第 %d 段超过模型输入预算，未生成不完整结果", i+1)
		}
		i++
		for i < len(parts) {
			candidate := append(append([]summaryInput{}, group...), parts[i])
			if len(summaryMergePrompt(candidate, level)) > limit {
				break
			}
			group, i = candidate, i+1
		}
		groups = append(groups, group)
	}
	if len(groups) >= len(parts) {
		return nil, fmt.Errorf("分段摘要无法在模型输入预算内合并，未生成不完整结果")
	}
	return groups, nil
}

func (c *Consumer) summarizeLong(ctx context.Context, task *model.VideoTask, full string, strategy ai.Strategy, limit int) (string, error) {
	if !utf8.ValidString(full) {
		return "", fmt.Errorf("转写文本包含无效 UTF-8，无法保证摘要完整覆盖")
	}
	rows, err := c.repo.TranscriptionChunk.ListByTaskID(task.ID)
	if err != nil {
		return "", err
	}
	leaves := summaryLeaves(full, rows, limit-450)
	modelName := c.llmModelNameForTask(task)
	hashModelName := modelName + "\x00" + ai.SummaryPreference(ctx)
	// A changed transcript, model or plan invalidates old checkpoints together.
	stored, err := c.repo.SummaryPart.List(task.ID)
	if err != nil {
		return "", err
	}
	if !summaryCheckpointsMatch(stored, leaves, hashModelName) {
		legacy := summaryLeavesLegacy(full, rows, limit-450)
		if summaryCheckpointsMatch(stored, legacy, hashModelName) {
			leaves = legacy
		}
	}
	reset := false
	for _, part := range stored {
		if part.Level != 0 {
			continue
		}
		if part.PartIndex >= len(leaves) || part.InputHash != summaryHash(hashModelName, summaryPartPrompt(leaves[part.PartIndex], part.PartIndex, len(leaves))) {
			reset = true
			break
		}
	}
	if reset {
		if err := c.runLeasedSideEffect(ctx, func(r *repository.Repositories) error { return r.SummaryPart.DeleteByTaskID(task.ID) }); err != nil {
			return "", err
		}
	}
	for i, leaf := range leaves {
		prompt := summaryPartPrompt(leaf, i, len(leaves))
		if len(prompt) > limit {
			return "", fmt.Errorf("摘要第 %d/%d 段超过模型输入预算", i+1, len(leaves))
		}
		part := &model.SummaryPart{TaskID: task.ID, Level: 0, PartIndex: i, InputHash: summaryHash(hashModelName, prompt), InputLimit: limit, ModelName: modelName, StartMS: leaf.startMS, EndMS: leaf.endMS, Status: "pending"}
		if err := c.ensureSummaryPart(ctx, part); err != nil {
			return "", err
		}
	}
	current := make([]summaryInput, 0, len(leaves))
	for i, leaf := range leaves {
		prompt := summaryPartPrompt(leaf, i, len(leaves))
		text, err := c.completeSummaryPart(ctx, strategy, &model.SummaryPart{TaskID: task.ID, Level: 0, PartIndex: i, InputHash: summaryHash(hashModelName, prompt), InputLimit: limit, ModelName: modelName, StartMS: leaf.startMS, EndMS: leaf.endMS}, prompt, summaryIntermediateOutputTokens)
		if err != nil {
			return "", fmt.Errorf("摘要第 %d/%d 段失败，已覆盖 %d/%d 段: %w", i+1, len(leaves), i, len(leaves), err)
		}
		current = append(current, summaryInput{text: text, startMS: leaf.startMS, endMS: leaf.endMS})
	}
	for level := 1; len(current) > 1; level++ {
		groups, err := packSummaryMerge(current, limit, level)
		if err != nil {
			return "", err
		}
		next := make([]summaryInput, 0, len(groups))
		for i, group := range groups {
			prompt := summaryMergePrompt(group, level)
			part := &model.SummaryPart{TaskID: task.ID, Level: level, PartIndex: i, InputHash: summaryHash(hashModelName, prompt), InputLimit: limit, ModelName: modelName, StartMS: group[0].startMS, EndMS: group[len(group)-1].endMS, Status: "pending"}
			if err := c.ensureSummaryPart(ctx, part); err != nil {
				return "", err
			}
		}
		for i, group := range groups {
			prompt := summaryMergePrompt(group, level)
			part := &model.SummaryPart{TaskID: task.ID, Level: level, PartIndex: i, InputHash: summaryHash(hashModelName, prompt), InputLimit: limit, ModelName: modelName, StartMS: group[0].startMS, EndMS: group[len(group)-1].endMS}
			outputTokens := int64(summaryIntermediateOutputTokens)
			if len(groups) == 1 {
				outputTokens = summaryOutputTokens
			}
			text, err := c.completeSummaryPart(ctx, strategy, part, prompt, outputTokens)
			if err != nil {
				return "", fmt.Errorf("摘要合并第 %d 层第 %d/%d 组失败，原文已覆盖 %d/%d 段: %w", level, i+1, len(groups), len(leaves), len(leaves), err)
			}
			next = append(next, summaryInput{text: text, startMS: part.StartMS, endMS: part.EndMS})
		}
		current = next
	}
	if len(current) != 1 {
		return "", fmt.Errorf("摘要没有覆盖完整转写")
	}
	return current[0].text, nil
}

func summaryCheckpointsMatch(stored []model.SummaryPart, leaves []summaryInput, hashModelName string) bool {
	found := false
	for _, part := range stored {
		if part.Level != 0 {
			continue
		}
		found = true
		if part.PartIndex >= len(leaves) || part.InputHash != summaryHash(hashModelName, summaryPartPrompt(leaves[part.PartIndex], part.PartIndex, len(leaves))) {
			return false
		}
	}
	return found
}

func (c *Consumer) ensureSummaryPart(ctx context.Context, part *model.SummaryPart) error {
	existing, err := c.repo.SummaryPart.Find(part.TaskID, part.Level, part.PartIndex)
	if err != nil {
		return err
	}
	if existing != nil && existing.InputHash == part.InputHash {
		return nil
	}
	return c.runLeasedSideEffect(ctx, func(r *repository.Repositories) error { return r.SummaryPart.Upsert(part) })
}

func (c *Consumer) completeSummaryPart(ctx context.Context, strategy ai.Strategy, part *model.SummaryPart, prompt string, outputTokens int64) (string, error) {
	existing, err := c.repo.SummaryPart.Find(part.TaskID, part.Level, part.PartIndex)
	if err != nil {
		return "", err
	}
	if existing != nil && existing.InputHash == part.InputHash && existing.Status == "completed" && strings.TrimSpace(existing.Content) != "" {
		return existing.Content, nil
	}
	part.Status = "running"
	if err := c.runLeasedSideEffect(ctx, func(r *repository.Repositories) error { return r.SummaryPart.Upsert(part) }); err != nil {
		return "", err
	}
	answer, callErr := strategy.Summarize(ai.WithChatBudget(ctx, outputTokens, nil), prompt)
	if err := requireProcessingLease(ctx); err != nil {
		return "", err
	}
	if callErr != nil || strings.TrimSpace(answer) == "" {
		if callErr == nil {
			callErr = fmt.Errorf("模型返回空摘要")
		}
		part.Status, part.ErrorMsg = "failed", truncateError(callErr)
		_ = c.runLeasedSideEffect(ctx, func(r *repository.Repositories) error { return r.SummaryPart.Upsert(part) })
		return "", callErr
	}
	part.Status, part.Content, part.ErrorMsg = "completed", strings.TrimSpace(answer), ""
	if err := c.runLeasedSideEffect(ctx, func(r *repository.Repositories) error { return r.SummaryPart.Upsert(part) }); err != nil {
		return "", err
	}
	return part.Content, nil
}
