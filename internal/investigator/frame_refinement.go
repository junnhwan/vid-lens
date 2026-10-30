package investigator

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
	"vidlens/internal/media"
)

type frameRefinement struct {
	Generation    int      `json:"generation"`
	ParentIDs     []string `json:"parent_ids"`
	InspectionIDs []string `json:"inspection_ids"`
	QuestionID    string   `json:"question_id"`
	Question      string   `json:"question"`
	Reason        string   `json:"reason"`
}

type refineFrameInput struct {
	QuestionID string   `json:"question_id"`
	Question   string   `json:"question"`
	FrameIDs   []string `json:"frame_ids" jsonschema:"description=1-2 actual delivered low-information frame IDs. Use only when the user's question still needs missing pixels; omit decorative or irrelevant frames."`
	Reason     string   `json:"reason" jsonschema:"description=Brief visible deficiency for this question, such as blank embedded player or transitional card. White/black background alone is not a deficiency. No private reasoning."`
}

func nearbyFrameWindow(at, duration int64, generation int) media.Window {
	halfSpan := int64(4000)
	if generation > 1 {
		halfSpan = 10000
	}
	return media.Window{StartMS: max(int64(0), at-halfSpan), EndMS: min(duration, at+halfSpan), Count: 2, Mode: "detail"}
}

func (s *session) refinementTool(add func(tool.EnhancedInvokableTool, error) error) error {
	if !s.limitedQuestionDelivery() {
		return nil
	}
	return add(utils.InferEnhancedTool("refine_video_frames", "Dynamically sample real nearby pixels for 1-2 delivered frames that do not answer a necessary visual question. Classify relevance from actual pixels. Batches choose two nearby temporal samples per parent, first narrowly and then more widely. At most two refinement batches per turn; stop when a useful frame answers the question. No model call per frame. Irrelevant images can be omitted from submit_answer instead.", func(ctx context.Context, in refineFrameInput) (*schema.ToolResult, error) {
		if len(in.FrameIDs) < 1 || len(in.FrameIDs) > 2 || len(slices.Compact(slices.Sorted(slices.Values(in.FrameIDs)))) != len(in.FrameIDs) || strings.TrimSpace(in.QuestionID) == "" || strings.TrimSpace(in.Question) == "" || strings.TrimSpace(in.Reason) == "" || utf8.RuneCountInString(in.Reason) > 512 {
			return toolError("补采需要明确问题、1–2个不重复的已交付 frame_ids，以及不超过512字的可见缺口。")
		}
		key := callID(ctx)
		if s.cp.FrameRefinements == nil {
			s.cp.FrameRefinements = map[string]frameRefinement{}
		}
		plan, exists := s.cp.FrameRefinements[key]
		if exists && (!slices.Equal(plan.ParentIDs, in.FrameIDs) || plan.Reason != in.Reason || plan.QuestionID != in.QuestionID || plan.Question != in.Question) {
			return toolError("同一补采动作不能替换来源或缺口。")
		}
		if !exists {
			if len(s.cp.FrameRefinements) >= 2 {
				return toolError("附近补采额度已用完；用已有清晰画面先回答，把仍未确认的视觉细节单列。")
			}
			inspectionIDs := []string{}
			for id := range s.cp.Inspections {
				inspectionIDs = append(inspectionIDs, id)
			}
			slices.Sort(inspectionIDs)
			parents := []string{}
			for _, id := range in.FrameIDs {
				inspection := ""
				for _, key := range inspectionIDs {
					if slices.Contains(s.cp.Inspections[key], id) {
						inspection = key
						break
					}
				}
				if inspection == "" {
					return toolError("只能围绕本轮已交付图片补采；未知 frame_id=" + id)
				}
				parents = append(parents, inspection)
			}
			plan = frameRefinement{Generation: len(s.cp.FrameRefinements) + 1, ParentIDs: slices.Clone(in.FrameIDs), InspectionIDs: parents, QuestionID: in.QuestionID, Question: in.Question, Reason: in.Reason}
		}
		parents := []struct {
			ID string
			At int64
		}{}
		for _, id := range plan.ParentIDs {
			a, _, err := s.engine.Media.ReadImage(ctx, s.claim, id)
			if err != nil {
				return nil, err
			}
			if a.TimeMS == nil || a.SnapshotID == nil || *a.SnapshotID != s.snapshot.ID {
				return toolError("补采来源必须是当前视频中的真实帧。")
			}
			parents = append(parents, struct {
				ID string
				At int64
			}{id, *a.TimeMS})
		}
		room, err := s.inspectionHeadroom(ctx)
		if err != nil {
			return nil, err
		}
		if room < 2*len(parents) {
			return toolError("当前比较图片过多；先记录观察并释放已完成的比较，再补采。")
		}
		// Freeze the global budget before any extraction. Worker replay reuses this
		// exact plan and the media operation keys instead of granting fresh credit.
		if !exists {
			s.cp.FrameRefinements[key] = plan
			if err = s.engine.Store.Checkpoint(ctx, s.claim, s.cp); err != nil {
				return nil, err
			}
		}
		result := media.Inspection{SchemaVersion: 1, ID: key, Question: in.Question, FrameIDs: []string{}, Sampling: "nearby temporal refinement; samples do not establish continuous events"}
		st, err := s.step(ctx, "正在补看低信息画面附近的真实帧", s.phase(), nil, func() (any, []string, error) {
			for i, p := range parents {
				window := nearbyFrameWindow(p.At, s.snapshot.DurationMS, plan.Generation)
				near, err := s.engine.Media.Inspect(ctx, s.claim, fmt.Sprintf("%s:near:%d", key, i), in.Question, window, nil)
				if err != nil {
					return nil, nil, err
				}
				result.FrameIDs = append(result.FrameIDs, near.FrameIDs...)
				result.Frames = append(result.Frames, near.Frames...)
			}
			for i, p := range parents {
				inspection := plan.InspectionIDs[i]
				o := observation{InspectionID: inspection, QuestionID: in.QuestionID, Question: in.Question, FrameIDs: []string{p.ID}, Unresolved: []string{in.Reason}, Status: "unresolved", NextAction: "附近补采；原帧不是所需细节的证据"}
				if err := s.engine.Store.RecordObservation(ctx, s.claim, key+":low-information:"+p.ID, inspection, o); err != nil {
					return nil, nil, err
				}
			}
			return map[string]any{"inspection": result, "parent_frame_ids": plan.ParentIDs, "generation": plan.Generation, "reason": plan.Reason, "remaining_refinements": 2 - len(s.cp.FrameRefinements)}, result.FrameIDs, nil
		})
		if err != nil {
			return nil, err
		}
		s.cp.Inspections[key] = slices.Clone(result.FrameIDs)
		for _, id := range result.FrameIDs {
			if !slices.Contains(s.cp.CitationFrames, id) {
				s.cp.CitationFrames = append(s.cp.CitationFrames, id)
			}
		}
		parts, err := s.imageInput(ctx, result.FrameIDs, key)
		if err != nil {
			return nil, err
		}
		s.cp.Messages = stripPixels(s.cp.Messages)
		if err = s.engine.Store.Checkpoint(ctx, s.claim, s.cp); err != nil {
			return nil, err
		}
		st.Parts = append(st.Parts, schema.ToolOutputPart{Type: schema.ToolPartTypeText, Text: fmt.Sprintf("inspection_id=%s; nearby samples must be observed before citation", key)})
		for _, part := range parts {
			if part.Image != nil {
				st.Parts = append(st.Parts, schema.ToolOutputPart{Type: schema.ToolPartTypeImage, Image: &schema.ToolOutputImage{MessagePartCommon: part.Image.MessagePartCommon}, Extra: part.Extra})
			} else {
				st.Parts = append(st.Parts, schema.ToolOutputPart{Type: schema.ToolPartTypeText, Text: part.Text})
			}
		}
		return st, nil
	}))
}
