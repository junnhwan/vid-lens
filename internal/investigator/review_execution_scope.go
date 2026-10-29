package investigator

import (
	"context"
	"slices"
)

// V7 adds host-owned execution facts; these are distinct from the author's
// observations and never establish what an image depicts or a speaker said.
const modalityReviewInstructions = `本协议已由服务器按引用分配必审模态：纯画面引用单元只进入visual，混合文字/画面引用分别经过两种复核，无画面引用仍经过source_text。visual收到完整原始文字及本单元全部引用图片，结合二者判断，不能因未另经source_text而遗漏文字事实。
source.execution_context是服务器记录的执行范围，只能核对“抽样取帧、来源时长、可用帧数量/时间、未连续逐帧检查、文字来源”等流程声明，不是画面内容证据，也不证明具体字幕/章卡存在。作者描述和limitation code本身都不是证据，不能自动通过。
与服务器记录一致的诚实调查局限可以supported，无须原视频逐字说出该局限；包含具体可见内容的局限仍须看实际图片，包含讲话事实仍须核对原文，混合断言不得只核对流程部分。not_assessed只表示未完成本步骤真正要求的复核；不要用它表示无需另一种模态、原文未逐字陈述流程声明，或已如实披露的未知。真正未完成时仍返回not_assessed，绝不猜测通过。`

func (s *session) reviewExecutionContext(ctx context.Context) (map[string]any, error) {
	ids := append([]string{}, s.cp.OverviewIDs...)
	ids = append(ids, s.cp.CitationFrames...)
	for _, frames := range s.cp.Inspections {
		ids = append(ids, frames...)
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	frames := []any{}
	for _, id := range ids {
		asset, err := s.engine.Store.InternalFrame(ctx, s.claim, id)
		if err != nil {
			return nil, err
		}
		frames = append(frames, map[string]any{"id": asset.ID, "time_ms": asset.TimeMS, "sha256": asset.SHA256})
	}
	origins := map[string]int{}
	for _, segment := range s.transcript {
		origins[segment.Origin]++
	}
	scope := map[string]any{
		"snapshot_id":                  s.snapshot.ID,
		"source_duration_ms":           s.snapshot.DurationMS,
		"visual_access":                "sampled_frames",
		"continuous_visual_inspection": false,
		"original_audio_provided":      false,
		"author_frame_count":           len(frames),
		"author_frames":                frames,
		"transcript_segment_count":     len(s.transcript),
		"transcript_origins":           origins,
		"scope_rule":                   "Frame identities describe delivered sampling instants, not visible content or continuous visual coverage. The complete original transcript and this task's actual pixels are supplied separately.",
	}
	if s.indexedReview() {
		scope["text_access"] = "indexed original passages, with the complete chronological source navigation ledger"
		scope["scope_rule"] = "Frame identities describe sampled delivery instants, not visible content or continuous coverage. All cited original text and its chronological neighbors accompany the complete navigation ledger; derived notes do not prove claims."
	}
	return scope, nil
}
