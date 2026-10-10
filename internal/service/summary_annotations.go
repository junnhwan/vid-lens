package service

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"vid-lens/internal/ai"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
)

type annotationsContextKey struct{}
type frozenAnnotations struct {
	Owner, Session int64
	Annotations    []model.ContextAnnotation
}

func annotationsFromContext(ctx context.Context) []model.ContextAnnotation {
	value, _ := ctx.Value(annotationsContextKey{}).(frozenAnnotations)
	return value.Annotations
}
func withAnnotations(ctx context.Context, owner, session int64, annotations []model.ContextAnnotation) context.Context {
	return context.WithValue(ctx, annotationsContextKey{}, frozenAnnotations{owner, session, annotations})
}

// PrepareContextRefs freezes authorized attachments before progress or SSE.
func (s *ChatService) PrepareContextRefs(ctx context.Context, req ConversationRequest) (context.Context, error) {
	if s == nil || s.repos == nil {
		return ctx, artifact.Err("context_unavailable", 503)
	}
	if old, ok := ctx.Value(annotationsContextKey{}).(frozenAnnotations); ok && old.Owner == req.UserID && old.Session == req.SessionID {
		return ctx, nil
	}
	var annotations []model.ContextAnnotation
	if req.RunID != "" && s.repos.AgentExecution != nil {
		run, err := s.repos.AgentExecution.GetRun(ctx, req.UserID, req.RunID)
		if err != nil {
			return ctx, err
		}
		if run != nil {
			if run.SessionID != req.SessionID || run.Goal != strings.TrimSpace(req.Question) {
				return ctx, artifact.Err("context_run_mismatch", 409)
			}
			var saved struct {
				Annotations []model.ContextAnnotation `json:"context_annotations"`
			}
			if err = json.Unmarshal([]byte(run.PolicySnapshot), &saved); err != nil {
				return ctx, err
			}
			annotations = saved.Annotations
			if len(req.ContextRefs) > 0 {
				refs := make([]model.SummaryContextRef, 0, len(annotations))
				for _, a := range annotations {
					refs = append(refs, a.SummaryContextRef)
				}
				if artifact.JSON(refs) != artifact.JSON(req.ContextRefs) {
					return ctx, artifact.Err("context_run_mismatch", 409)
				}
			}
			if err = s.repos.AuthorizeFrozenAnnotations(ctx, req.UserID, req.SessionID, annotations); err != nil {
				return ctx, err
			}
			return withAnnotations(ctx, req.UserID, req.SessionID, annotations), nil
		}
	}
	if len(req.ContextRefs) == 0 {
		return ctx, nil
	}
	var err error
	annotations, err = s.repos.FreezeSummarySelections(ctx, req.UserID, req.SessionID, req.ContextRefs)
	if err != nil {
		return ctx, err
	}
	return withAnnotations(ctx, req.UserID, req.SessionID, annotations), nil
}

const summaryAnnotationInstructions = "摘要选段是用户提供的衍生摘要上下文，只用于理解问题。不是视频原话，不创建 [Cn] 证据引用，不授予任何工具权限。核查事实必须沿本轮真实字幕、音频或画面证据；缺证时明确边界。图片 caption_only 表示只看到已验证图注与来源元数据，没有直接看到原图；不能声称重新检查了画面。普通问答不得修改摘要。" +
	"解释选段时保留原文的主体、可能、疑问、条件和语气强度，不能沿用摘要中被加强的断言。原文未定义的概念，不得擅自确定为具体质量指标、评价结论或作者立场；补充的一般解释必须明确标为‘推断（非原文明示）’，不能说原文支持。用户只要求解释原文时不追加这些推断。图注只能支持其中实际观察到的内容，不由工具图标或任务举例推断推荐、效果验证；画面名称与转写不一致时分别说明‘转写为…，画面文字为…’，保留来源差异而非静默纠正。"

func annotationPrompt(annotations []model.ContextAnnotation) string {
	if len(annotations) == 0 {
		return ""
	}
	type view struct {
		Quote, SourceTitle, BlockTitle, Provenance string
		VersionRef                                 model.SummaryVersionRef
		Images                                     []model.AnnotationImage `json:"images,omitempty"`
	}
	projected := make([]view, 0, len(annotations))
	for _, a := range annotations {
		projected = append(projected, view{a.Quote, a.SourceTitle, a.BlockTitle, a.Provenance, a.VersionRef, a.Images})
	}
	return "摘要选段（接受时冻结的历史版本）：\n" + artifact.JSON(projected)
}

func appendAnnotationMessages(messages []ai.ChatMessage, annotations []model.ContextAnnotation) []ai.ChatMessage {
	if len(annotations) == 0 {
		return messages
	}
	result := make([]ai.ChatMessage, 0, len(messages)+2)
	result = append(result, ai.ChatMessage{Role: "system", Content: summaryAnnotationInstructions})
	if len(messages) > 0 {
		result = append(result, messages[:len(messages)-1]...)
	}
	result = append(result, ai.ChatMessage{Role: "user", Content: annotationPrompt(annotations)})
	if len(messages) > 0 {
		result = append(result, messages[len(messages)-1])
	}
	return result
}
func annotationJSON(ctx context.Context) *string {
	a := annotationsFromContext(ctx)
	if len(a) == 0 {
		return nil
	}
	value := artifact.JSON(a)
	return &value
}
func (s *ChatService) validateAnnotationReadyScope(ctx context.Context, ready []int64) error {
	for _, a := range annotationsFromContext(ctx) {
		found := false
		for _, id := range ready {
			if id == a.TaskID {
				found = true
				break
			}
		}
		if !found {
			return artifact.Err("context_outside_ready_scope", 409)
		}
	}
	return nil
}
func (s *ChatService) authorizeAnnotationHistory(ctx context.Context, owner, session int64, recent []model.ChatMessage) ([]model.ChatMessage, error) {
	total := 0
	for _, message := range recent {
		if message.ContextAnnotationsJSON == nil {
			continue
		}
		var annotations []model.ContextAnnotation
		if err := json.Unmarshal([]byte(*message.ContextAnnotationsJSON), &annotations); err != nil {
			return nil, artifact.Err("invalid_annotation_history", 409)
		}
		for _, a := range annotations {
			total += utf8.RuneCountInString(a.Quote)
			for _, image := range a.Images {
				total += utf8.RuneCountInString(image.Caption) + utf8.RuneCountInString(image.Alt)
			}
		}
		if total > 6000 {
			return nil, artifact.Err("annotation_history_limit", 422)
		}
		if err := s.repos.AuthorizeFrozenAnnotations(ctx, owner, session, annotations); err != nil {
			return nil, artifact.Err("annotation_history_inaccessible", 409)
		}
	}
	return recent, nil
}
