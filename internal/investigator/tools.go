package investigator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
	"vidlens/internal/domain"
	"vidlens/internal/media"
	"vidlens/internal/store"
)

type findInput struct {
	ActivityTitle string `json:"activity_title"`
	Query         string `json:"query"`
	StartMS       *int64 `json:"start_ms,omitempty"`
	EndMS         *int64 `json:"end_ms,omitempty"`
}
type readInput struct {
	ActivityTitle string   `json:"activity_title"`
	SegmentIDs    []string `json:"segment_ids,omitempty"`
	StartMS       *int64   `json:"start_ms,omitempty"`
	EndMS         *int64   `json:"end_ms,omitempty"`
}
type inspectInput struct {
	ActivityTitle string        `json:"activity_title"`
	QuestionID    string        `json:"question_id"`
	Critical      *bool         `json:"critical,omitempty" jsonschema:"description=Whether this question must be supported before article publication. Use false for optional details or deliberately unanswered story questions."`
	Question      string        `json:"question"`
	Window        *media.Window `json:"window,omitempty" jsonschema:"description=For new frames put start_ms, end_ms, count, mode and optional crop inside this window object. Omit window when reloading frame_ids."`
	FrameIDs      []string      `json:"frame_ids,omitempty"`
}
type articleInput struct {
	Title       string                     `json:"title"`
	Sections    []domain.Section           `json:"sections"`
	MindMap     *domain.MindMap            `json:"mind_map"`
	Citations   map[string]domain.Citation `json:"citations"`
	Limitations []domain.Limitation        `json:"limitations"`
}
type answerInput struct {
	Kind        string                     `json:"kind"`
	Summary     string                     `json:"summary"`
	Blocks      []domain.Block             `json:"blocks"`
	Citations   map[string]domain.Citation `json:"citations"`
	Limitations []domain.Limitation        `json:"limitations"`
}

type argumentCheckedTool struct {
	tool.EnhancedInvokableTool
	name    string
	session *session
}

func (t *argumentCheckedTool) InvokableRun(ctx context.Context, args *schema.ToolArgument, opts ...tool.Option) (result *schema.ToolResult, runErr error) {
	if s := t.session; s != nil && s.cp.HarnessRevision > 0 && s.engine != nil {
		before, err := s.progressFingerprint(ctx)
		if err != nil {
			return nil, err
		}
		defer func() {
			if isCandidateTool(t.name) && args != nil {
				if issue := rejectedCandidateIssue(result, runErr); issue != nil {
					if s.cp.HarnessRevision >= 3 {
						canonical := store.Hash(canonicalArguments(args.Text))
						if s.cp.Repeated == nil {
							s.cp.Repeated = map[string]int{}
						}
						marker := "candidate-call:" + callID(ctx)
						if s.cp.Repeated[marker] == 0 {
							s.cp.Repeated[marker] = 1
							s.cp.Repeated["candidate-content:"+canonical]++
						}
						issue["canonical_arguments_hash"] = canonical
						issue["unchanged_candidate"] = s.cp.Repeated["candidate-content:"+canonical] > 1
						if draftMutation(t.name) {
							s.cp.DraftIssue = issue
							if result != nil && len(result.Parts) > 0 {
								var value map[string]any
								if json.Unmarshal([]byte(result.Parts[0].Text), &value) == nil {
									value["error"] = issue
									result.Parts[0].Text = string(domain.JSON(value))
								}
							}
						}
					}
					if err := s.engine.Store.RecordCandidate(ctx, s.claim, callID(ctx), t.name, args.Text, "invalid", issue); err != nil {
						result, runErr = nil, err
						return
					}
					s.cp.LastCandidate = map[string]any{"candidate_id": callID(ctx), "expected_hash": store.Hash(args.Text), "tool": t.name, "issue": issue}
				}
			}
			if err := s.toolFeedback(ctx, t.name, rawToolArguments(args), before, result, runErr); err != nil {
				result, runErr = nil, err
				return
			}
			if result != nil && runErr == nil {
				ready, err := s.readiness(ctx)
				if err != nil {
					result, runErr = nil, err
					return
				}
				// Keep existing result fields at the top level for replay/projection.
				if len(result.Parts) > 0 && result.Parts[0].Type == schema.ToolPartTypeText {
					var body map[string]any
					if json.Unmarshal([]byte(result.Parts[0].Text), &body) == nil && body != nil {
						body["readiness"] = ready
						result.Parts[0].Text = string(domain.JSON(body))
					}
				}
			}
		}()
		if t.name == "inspect_video_window" && s.cp.InspectBatchRejected[callID(ctx)] {
			return toolError("本轮多个inspect已整组拒绝，没有创建图片或问题。每轮只提交一个inspect；先观察已交付图片。")
		}
	}
	if s := t.session; s != nil && s.engine != nil {
		reserved, err := s.reserveDraftCorrection(ctx, t.name, rawToolArguments(args))
		if err != nil {
			return nil, err
		}
		if reserved {
			parent, _ := s.cp.LastCandidate["candidate_id"].(string)
			defer func() {
				data := map[string]any{"tool": t.name, "candidate_id": callID(ctx), "parent_candidate_id": parent, "rejected": rejectedCandidateIssue(result, runErr) != nil}
				if err := s.engine.Store.FinishOperation(ctx, s.claim, "draft-correction:"+callID(ctx), data, "", nil); err != nil {
					result, runErr = nil, err
				}
			}()
		}
	}
	var target any
	switch t.name {
	case "begin_article":
		target = &beginArticleInput{}
	case "save_article_sections":
		target = &saveSectionsInput{}
	case "update_article_limitations":
		target = &struct {
			Version     int                 `json:"version"`
			Limitations []domain.Limitation `json:"limitations"`
		}{}
	case "submit_article_draft":
		target = &submitDraftInput{}
	case "submit_article":
		target = &articleInput{}
	case "submit_answer":
		target = &answerInput{}
	case "record_observation":
		target = &observation{}
	case "inspect_video_window":
		target = &inspectInput{}
	case "find_evidence":
		target = &findInput{}
	case "read_source":
		target = &readInput{}
	case "patch_article_draft":
		target = &draftPatchInput{}
	case "repair_candidate":
		target = &candidatePatchInput{}
	case "repair_article_plan":
		target = &candidatePatchInput{}
	}
	var err error
	if args != nil && isCandidateTool(t.name) && t.session != nil && t.session.engine != nil {
		if e := t.session.engine.Store.RecordCandidate(ctx, t.session.claim, callID(ctx), t.name, args.Text, "received", nil); e != nil {
			return nil, e
		}
	}
	if args == nil {
		err = errors.New("工具参数为空")
	} else if target != nil {
		err = json.Unmarshal([]byte(args.Text), target)
		if err == nil && t.session != nil && t.session.cp.HarnessRevision > 0 {
			d := json.NewDecoder(bytes.NewBufferString(args.Text))
			d.DisallowUnknownFields()
			err = d.Decode(target)
		}
	}
	if err != nil {
		if isCandidateTool(t.name) {
			issue := candidateIssue(err)
			if t.session != nil && t.session.engine != nil {
				if t.session.cp.HarnessRevision > 0 {
					t.session.cp.LastCandidate = map[string]any{"candidate_id": callID(ctx), "expected_hash": store.Hash(rawToolArguments(args)), "tool": t.name, "issue": issue}
				}
				if e := t.session.engine.Store.RecordCandidate(ctx, t.session.claim, callID(ctx), t.name, rawToolArguments(args), "invalid", issue); e != nil {
					return nil, e
				}
				if t.name == "begin_article" || t.name == "save_article_sections" {
					return t.session.draftFeedback(ctx, string(domain.JSON(issue)))
				}
			}
			return nil, &candidateError{err: domain.Err(422, "MODEL_RESULT_INVALID", "文章/回答提交的JSON结构无效："+string(domain.JSON(issue)))}
		}
		return toolError("工具参数不符合字段类型，请按 schema 修正：" + err.Error() + "。观察的 conflicts、unresolved、inferences 等字段必须是字符串数组，status 只能是 supported 或 unresolved。")
	}

	if in, ok := target.(*inspectInput); ok {
		if in.Window == nil && len(in.FrameIDs) == 0 {
			return toolError(`缺少取帧选择：新画面必须提供 window 对象，start_ms、end_ms、count、mode、crop 放在 window 内，不能放在顶层。例如 "window":{"start_ms":0,"end_ms":1000,"count":1,"mode":"detail"}；请改用所需的实际视频时间。重新加载已交付图片时只提供 frame_ids。`)
		}
		if in.Window != nil && len(in.FrameIDs) > 0 {
			return toolError("window 与 frame_ids 只能选择一种：新画面使用 window 对象，重新加载已有图片使用 frame_ids。")
		}
	}
	result, err = t.EnhancedInvokableTool.InvokableRun(ctx, args, opts...)
	if err != nil && isCandidateTool(t.name) && t.session != nil && t.session.engine != nil {
		var candidate *candidateError
		if errors.As(err, &candidate) {
			if saveErr := t.session.engine.Store.RecordCandidate(ctx, t.session.claim, callID(ctx), t.name, args.Text, "invalid", candidateIssue(candidate.err)); saveErr != nil {
				return nil, saveErr
			}
			if t.session.cp.HarnessRevision > 0 {
				t.session.cp.LastCandidate = map[string]any{"candidate_id": callID(ctx), "expected_hash": store.Hash(args.Text), "tool": t.name, "issue": candidateIssue(candidate.err)}
			}
		}
	}
	return result, err
}

func textResult(v any) *schema.ToolResult {
	return &schema.ToolResult{Parts: []schema.ToolOutputPart{{Type: schema.ToolPartTypeText, Text: string(domain.JSON(v))}}}
}
func toolError(msg string) (*schema.ToolResult, error) {
	return textResult(map[string]any{"error": msg, "correct_arguments_and_try_again": true}), nil
}
func publicTitle(input, fallback string) string {
	input = strings.TrimSpace(input)
	if input == "" || utf8.RuneCountInString(input) > 60 || strings.ContainsAny(input, "\n\r{}<>") || strings.Contains(input, "http") || strings.Contains(input, "sk-") {
		return fallback
	}
	return input
}
func (s *session) step(ctx context.Context, title, phase string, r *domain.Range, work func() (any, []string, error)) (*schema.ToolResult, error) {
	id := "step_" + s.claim.Run.ID + "_" + store.Hash(callID(ctx))[:16]
	st := domain.Step{ID: id, Kind: "tool_execution", ModelCall: s.cp.Calls, Title: title, Phase: phase, Status: "running", SourceRange: r, FrameIDs: []string{}, StartedAt: domain.Ptr(time.Now().UTC())}
	st, e := s.engine.Store.Step(ctx, s.claim, st)
	if e != nil {
		return nil, e
	}
	v, ids, e := work()
	if e != nil {
		if errors.Is(e, media.ErrMediaMissing) {
			return nil, e
		}
		var p *domain.Problem
		if errors.As(e, &p) && p.Code == "INVALID_ARGUMENT" {
			st.Status = "failed"
			st.FinishedAt = domain.Ptr(time.Now().UTC())
			st.Outcome = &domain.Outcome{Message: "请求参数需要调整。", EvidenceStatus: "not_assessed"}
			if _, err := s.engine.Store.Step(ctx, s.claim, st); err != nil {
				return nil, err
			}
			return toolError(p.Detail)
		}
		return nil, e
	}
	st.Status = "completed"
	st.FrameIDs = ids
	if st.FrameIDs == nil {
		st.FrameIDs = []string{}
	}
	st.FinishedAt = domain.Ptr(time.Now().UTC())
	st.Outcome = &domain.Outcome{Message: "已取得所需来源，等待内容判断。", EvidenceStatus: "not_assessed"}
	if phase == "compose" {
		st.Outcome = &domain.Outcome{Message: "草稿已保存，全部章节核对通过后才会发布。", EvidenceStatus: "not_applicable"}
	}
	if title == "保存视频画面观察" {
		st.Outcome = &domain.Outcome{Message: "已保存可见事实与未确认问题，后续可从此继续。", EvidenceStatus: "not_assessed"}
	}
	if _, e = s.engine.Store.Step(ctx, s.claim, st); e != nil {
		return nil, e
	}
	return textResult(v), nil
}
func (s *session) makeTools() ([]tool.BaseTool, error) {
	s.tools = map[string]tool.EnhancedInvokableTool{}
	if s.cp.Repeated == nil {
		s.cp.Repeated = map[string]int{}
	}
	out := []tool.BaseTool{}
	add := func(t tool.EnhancedInvokableTool, e error) error {
		if e != nil {
			return e
		}
		info, e := t.Info(context.Background())
		if e != nil {
			return e
		}
		t = &argumentCheckedTool{EnhancedInvokableTool: t, name: info.Name, session: s}
		s.tools[info.Name] = t
		out = append(out, t)
		return nil
	}
	refs, e := s.referenceTools(context.Background())
	if e != nil {
		return nil, e
	}
	for _, t := range refs {
		if e = add(t, nil); e != nil {
			return nil, e
		}
	}
	if e := add(utils.InferEnhancedTool("find_evidence", "Find candidate transcript positions by words and/or time range in this video. Candidates are not confirmed evidence.", func(ctx context.Context, in findInput) (*schema.ToolResult, error) {
		return s.step(ctx, publicTitle(in.ActivityTitle, "正在定位视频原文"), s.phase(), nil, func() (any, []string, error) {
			terms := strings.Fields(strings.ToLower(in.Query))
			found := []domain.Segment{}
			for _, t := range s.transcript {
				if in.StartMS != nil && t.EndMS <= *in.StartMS || in.EndMS != nil && t.StartMS >= *in.EndMS {
					continue
				}
				matched := len(terms) == 0
				for _, term := range terms {
					if strings.Contains(strings.ToLower(t.Text), term) {
						matched = true
						break
					}
				}
				if matched {
					found = append(found, t)
					if len(found) == 12 {
						break
					}
				}
			}
			return map[string]any{"snapshot_id": s.snapshot.ID, "candidates": found, "note": "candidate positions only; use read_source and inspect real pixels when needed"}, nil, nil
		})
	})); e != nil {
		return nil, e
	}
	if e := add(utils.InferEnhancedTool("read_source", "Read exact contiguous transcript segments plus neighboring context; supply segment_ids or a time range.", func(ctx context.Context, in readInput) (*schema.ToolResult, error) {
		return s.step(ctx, publicTitle(in.ActivityTitle, "正在回读视频原文"), s.phase(), nil, func() (any, []string, error) {
			indices := map[int]bool{}
			for i, t := range s.transcript {
				match := slices.Contains(in.SegmentIDs, t.ID)
				if len(in.SegmentIDs) == 0 && in.StartMS != nil && in.EndMS != nil {
					match = t.StartMS < *in.EndMS && t.EndMS > *in.StartMS
				}
				if match {
					for j := max(0, i-1); j <= min(len(s.transcript)-1, i+1); j++ {
						indices[j] = true
					}
				}
			}
			found := []domain.Segment{}
			n := 0
			more := false
			for i, t := range s.transcript {
				if !indices[i] {
					continue
				}
				if n+len(t.Text) > 24000 {
					more = true
					break
				}
				found = append(found, t)
				n += len(t.Text)
			}
			return map[string]any{"snapshot_id": s.snapshot.ID, "segments": found, "has_more": more, "note": "exact source text; coarse ASR ranges are audio windows"}, nil, nil
		})
	})); e != nil {
		return nil, e
	}
	if e := add(utils.InferEnhancedTool("inspect_video_window", "Return actual pixels. Use window with start_ms/end_ms/count/mode (overview, detail, sequence). Omit crop unless cropping a real delivered parent_frame_id. Or supply frame_ids without window to reload. Record new images before requesting more. No paths/URLs.", func(ctx context.Context, in inspectInput) (*schema.ToolResult, error) {
		if len(s.unobserved(ctx)) > 0 {
			return toolError("请先记录刚收到图片的观察；尚未记录：" + strings.Join(s.unobserved(ctx), ","))
		}
		if strings.TrimSpace(in.Question) == "" {
			return toolError("看图必须有明确问题。")
		}
		w := media.Window{}
		if in.Window != nil {
			w = *in.Window
		}
		key := callID(ctx)
		critical := s.turn == nil
		if s.cp.HarnessRevision >= 4 {
			critical = false
		}
		if in.Critical != nil {
			critical = *in.Critical
		}
		if s.cp.HarnessRevision > 0 && !critical && s.turn == nil && s.spec.DetailPolicy != nil {
			p := s.spec.DetailPolicy
			closing := false
			if s.cp.HarnessRevision >= 4 {
				stats, err := s.engine.Store.ModelExecution(ctx, s.claim.Run.ID)
				if err != nil {
					return nil, err
				}
				closing = s.wrapUp(stats)
			}
			if closing || s.cp.Progress.OptionalInspections >= p.OptionalInspections || s.profile.MaxIterations-s.cp.Calls <= p.FinalCallsReserved {
				return toolError("可选调查额度已用完；保留必要证据，将非关键未知写入limitations，开始成文并正式提交。")
			}
		}
		if in.QuestionID == "" {
			return toolError("必须提供稳定的 question_id。")
		}
		room, err := s.inspectionHeadroom(ctx)
		if err != nil {
			return nil, err
		}
		requested := w.Count
		if requested == 0 {
			requested = 4
		}
		if w.Crop != nil {
			requested = 1
		}
		if len(in.FrameIDs) > 0 {
			requested = len(in.FrameIDs)
		}
		if requested > room {
			return toolError(fmt.Sprintf("当前图片上下文最多还能接收 %d 帧，本次请求 %d 帧。请减少 window.count；或先用 record_observation 为不再需要同时比较的旧图片记录 retain_for_comparison=false，释放容量后再取帧。已保存的观察事实和图片引用仍然保留。", room, requested))
		}
		if err := s.engine.Store.Question(ctx, s.claim, in.QuestionID, map[string]any{"id": in.QuestionID, "question": in.Question, "critical": critical, "status": "open", "window": in.Window}); err != nil {
			return nil, err
		}
		argsHash := store.Hash(map[string]any{"window": w, "frames": in.FrameIDs})
		if _, exists := s.cp.Inspections[key]; !exists {
			s.cp.Repeated[argsHash]++
			if s.cp.Repeated[argsHash] > 2 {
				return toolError("等价取证已重复，请改变时间、细节或明确无法确认。")
			}
		}
		var result media.Inspection
		st, e := s.step(ctx, publicTitle(in.ActivityTitle, "正在查看与问题相关的视频画面"), s.phase(), nil, func() (any, []string, error) {
			var err error
			result, err = s.engine.Media.Inspect(ctx, s.claim, key, in.Question, w, in.FrameIDs)
			if err != nil {
				return nil, nil, err
			}
			return result, result.FrameIDs, nil
		})
		if e != nil {
			return nil, e
		}
		if len(result.FrameIDs) == 0 {
			return st, nil
		}
		s.cp.Inspections[key] = result.FrameIDs
		if s.cp.HarnessRevision > 0 && !critical && s.turn == nil {
			s.cp.Progress.OptionalInspections++
		}
		for _, id := range result.FrameIDs {
			if !slices.Contains(s.cp.CitationFrames, id) {
				s.cp.CitationFrames = append(s.cp.CitationFrames, id)
			}
		}
		parts, e := s.imageInput(ctx, result.FrameIDs, key)
		if e != nil {
			return nil, e
		}
		s.cp.Messages = stripPixels(s.cp.Messages)
		if e = s.engine.Store.Checkpoint(ctx, s.claim, s.cp); e != nil {
			return nil, e
		}
		st.Parts = append(st.Parts, schema.ToolOutputPart{Type: schema.ToolPartTypeText, Text: fmt.Sprintf("inspection_id=%s", key)})
		for _, p := range parts {
			if p.Image != nil {
				st.Parts = append(st.Parts, schema.ToolOutputPart{Type: schema.ToolPartTypeImage, Image: &schema.ToolOutputImage{MessagePartCommon: p.Image.MessagePartCommon}, Extra: p.Extra})
			} else {
				st.Parts = append(st.Parts, schema.ToolOutputPart{Type: schema.ToolPartTypeText, Text: p.Text})
			}
		}
		return st, nil
	})); e != nil {
		return nil, e
	}
	if e := add(utils.InferEnhancedTool("record_observation", "Durably record what the main agent saw in images. Preserve literal numbers/units/conditions, distinguish inferences, conflicts and unresolved gaps. One inspection per record.", func(ctx context.Context, in observation) (*schema.ToolResult, error) {
		known, ok := s.cp.Inspections[in.InspectionID]
		if !ok {
			ids := make([]string, 0, len(s.cp.Inspections))
			for id := range s.cp.Inspections {
				ids = append(ids, id)
			}
			slices.Sort(ids)
			return toolError("inspection_id 不存在或不属于这次执行；可用 inspection_id=" + string(domain.JSON(ids)))
		}
		if len(in.FrameIDs) == 0 {
			return toolError("观察必须引用实际图片；此组可用 frame_ids=" + string(domain.JSON(known)))
		}
		invalidIDs := []string{}
		for _, id := range in.FrameIDs {
			if !slices.Contains(known, id) {
				invalidIDs = append(invalidIDs, id)
			}
		}
		if len(invalidIDs) > 0 {
			return toolError("以下 frame_ids 未由此 inspection 交付：" + string(domain.JSON(invalidIDs)) + "；此组真实可用 frame_ids=" + string(domain.JSON(known)) + "。请逐字复制正确 ID，先记录已收到图片的观察；无需重新加载图片。")
		}
		if in.Status != "supported" && in.Status != "unresolved" {
			return toolError("status 必须为 supported 或 unresolved。")
		}
		if len(in.VisibleFacts) == 0 && len(in.Unresolved) == 0 {
			return toolError("必须记录可见事实或明确缺口。")
		}
		return s.step(ctx, "保存视频画面观察", s.phase(), nil, func() (any, []string, error) {
			if e := s.engine.Store.RecordObservation(ctx, s.claim, callID(ctx), in.InspectionID, in); e != nil {
				return nil, nil, e
			}
			if in.QuestionID != "" {
				if e := s.engine.Store.Question(ctx, s.claim, in.QuestionID, map[string]any{"id": in.QuestionID, "question": in.Question, "critical": in.Critical, "status": in.Status, "unresolved": in.Unresolved, "frame_ids": in.FrameIDs}); e != nil {
					return nil, nil, e
				}
			}
			return map[string]any{"saved": true, "inspection_id": in.InspectionID, "status": in.Status}, nil, nil
		})
	})); e != nil {
		return nil, e
	}
	if s.turn == nil && s.cp.LayoutVersion == draftLayout {
		if e := s.draftTools(add); e != nil {
			return nil, e
		}
	} else if s.turn == nil {
		if e := add(utils.InferEnhancedTool("submit_article", "Submit a complete illustrated article candidate. The server validates references and images before immutable publication.", func(ctx context.Context, in articleInput) (*schema.ToolResult, error) {
			a := domain.Article{SchemaVersion: 1, SnapshotID: s.snapshot.ID, Source: s.snapshot.Source, Title: in.Title, Sections: in.Sections, MindMap: in.MindMap, Citations: in.Citations, Limitations: in.Limitations, Quality: "standard"}
			if a.Limitations == nil {
				a.Limitations = []domain.Limitation{}
			}
			if len(a.Limitations) > 0 {
				a.Quality = "with_limitations"
			}
			if e := s.validateArticle(ctx, a); e != nil {
				return nil, &candidateError{err: e}
			}
			if _, e := s.engine.Store.BeginOperation(ctx, s.claim, "candidate", "article_candidate", in, false); e != nil {
				return nil, e
			}
			if e := s.engine.Store.FinishOperation(ctx, s.claim, "candidate", a, "", nil); e != nil {
				return nil, e
			}
			return textResult(map[string]any{"accepted_for_publication": true}), nil
		})); e != nil {
			return nil, e
		}
	} else {
		if e := add(utils.InferEnhancedTool("submit_answer", "Submit a finalized answer for this private turn. It cannot edit the article. All references will be validated.", func(ctx context.Context, in answerInput) (*schema.ToolResult, error) {
			a := domain.Answer{Kind: in.Kind, Summary: in.Summary, Blocks: in.Blocks, Citations: in.Citations, Frames: []domain.Frame{}, Limitations: in.Limitations}
			if a.Limitations == nil {
				a.Limitations = []domain.Limitation{}
			}
			if e := s.validateAnswer(ctx, &a); e != nil {
				return nil, &candidateError{err: e}
			}
			if _, e := s.engine.Store.BeginOperation(ctx, s.claim, "candidate", "answer_candidate", in, false); e != nil {
				return nil, e
			}
			if e := s.engine.Store.FinishOperation(ctx, s.claim, "candidate", a, "", nil); e != nil {
				return nil, e
			}
			return textResult(map[string]any{"accepted_for_publication": true}), nil
		})); e != nil {
			return nil, e
		}
	}
	if e := s.patchTools(add); e != nil {
		return nil, e
	}
	return out, nil
}
func (s *session) phase() string {
	if s.turn != nil {
		return "answer"
	}
	return "investigate"
}
