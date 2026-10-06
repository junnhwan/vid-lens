package mq

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"vid-lens/internal/model"
	"vid-lens/internal/pkg/ffmpeg"
	"vid-lens/internal/repository"
	"vid-lens/internal/transcript"
)

func (c *Consumer) alignExistingTranscript(ctx context.Context, taskID int64, audioPath string) (string, error) {
	if c.transcriptAligner == nil {
		return "", fmt.Errorf("音文对齐服务未配置；保留已有转写")
	}
	rows, err := c.repo.TranscriptionChunk.ListByTaskID(taskID)
	if err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "", fmt.Errorf("转写缺少原始音频窗口")
	}
	for _, row := range rows {
		if row.Status != model.TranscriptionChunkStatusCompleted || row.WindowEndMS <= row.WindowStartMS {
			return "", fmt.Errorf("转写窗口尚未全部完成")
		}
	}
	if err := requireProcessingLease(ctx); err != nil {
		return "", err
	}
	return c.alignTranscriptRows(ctx, taskID, audioPath, rows)
}

func (c *Consumer) alignTranscriptRows(ctx context.Context, taskID int64, audioPath string, rows []model.VideoTranscriptionChunk) (string, error) {
	if c.repo != nil && c.repo.Task != nil {
		if err := c.transitionTaskStage(ctx, taskID, model.TaskStageAligning); err != nil {
			return "", err
		}
	}
	aligned, err := c.transcriptAligner.Align(ctx, audioPath, rows)
	if err != nil {
		return "", err
	}
	if len(aligned) != len(rows) {
		return "", fmt.Errorf("音文对齐窗口数量不一致")
	}
	for i, row := range aligned {
		original := row
		original.TimedSegments = rows[i].TimedSegments
		if !reflect.DeepEqual(original, rows[i]) {
			return "", fmt.Errorf("音文对齐修改了原始转写")
		}
		var words []model.TranscriptionSegment
		if json.Unmarshal([]byte(row.TimedSegments), &words) != nil || transcript.ValidateAlignedWords(row, words) != nil {
			return "", fmt.Errorf("音文对齐未通过原文与时间校验")
		}
	}
	content := transcript.Assemble(aligned).Content
	if strings.TrimSpace(content) == "" {
		return "", fmt.Errorf("音文对齐未保留有效转写")
	}
	if c.repo == nil || c.repo.TranscriptionChunk == nil {
		return content, nil
	}
	// Publish timing, the continuous corpus and retrieval invalidation together,
	// fenced by the task lease. Failed/cancelled alignment leaves old evidence.
	err = c.runLeasedSideEffect(ctx, func(repos *repository.Repositories) error {
		return repos.TransactionContext(ctx, func(tx *repository.Repositories) error {
			for _, row := range aligned {
				var words []model.TranscriptionSegment
				if err := json.Unmarshal([]byte(row.TimedSegments), &words); err != nil {
					return err
				}
				window := ffmpeg.AudioSegment{SegmentKey: row.SegmentKey, Version: row.SegmenterVersion, WindowStartMS: row.WindowStartMS, WindowEndMS: row.WindowEndMS, CoreStartMS: row.CoreStartMS, CoreEndMS: row.CoreEndMS}
				if err := tx.TranscriptionChunk.UpsertCompletedWithTimedSegments(taskID, row.ChunkIndex, row.AudioObject, row.Content, transcriptionChunkTimeline(window), words); err != nil {
					return err
				}
			}
			task, err := tx.Task.FindByID(taskID)
			if err != nil {
				return err
			}
			return tx.SaveTranscriptionAndInvalidateIndex(&model.VideoTranscription{TaskID: taskID, FileMD5: task.FileMD5, Content: content, Words: len([]rune(content))})
		})
	})
	return content, err
}
