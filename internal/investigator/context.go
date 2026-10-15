package investigator

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"vidlens/internal/domain"
)

type checkpoint struct {
	SchemaVersion int                 `json:"schema_version"`
	Protocol      string              `json:"protocol"`
	Messages      []*schema.Message   `json:"messages"`
	Calls         int                 `json:"calls"`
	Inspections   map[string][]string `json:"inspections"`
	OverviewIDs   []string            `json:"overview_ids"`
	Repeated      map[string]int      `json:"repeated"`
	Ready         bool                `json:"ready"`
	Repairs       int                 `json:"repairs"`
}
type observation struct {
	InspectionID        string   `json:"inspection_id"`
	QuestionID          string   `json:"question_id"`
	Question            string   `json:"question"`
	FrameIDs            []string `json:"frame_ids"`
	VisibleFacts        []string `json:"visible_facts"`
	LiteralDetails      []string `json:"literal_details"`
	Inferences          []string `json:"inferences"`
	Conflicts           []string `json:"conflicts"`
	Unresolved          []string `json:"unresolved"`
	Status              string   `json:"status" jsonschema:"enum=supported,enum=unresolved,description=Use supported for recorded facts and unresolved for an unconfirmed observation. Never use completed."`
	Critical            bool     `json:"critical"`
	RetainForComparison bool     `json:"retain_for_comparison"`
	NextAction          string   `json:"next_action"`
}
type callIDKey struct{}

func callID(ctx context.Context) string { v, _ := ctx.Value(callIDKey{}).(string); return v }

type middleware struct {
	*adk.BaseChatModelAgentMiddleware
	s *session
}

func (m *middleware) WrapEnhancedInvokableToolCall(ctx context.Context, next adk.EnhancedInvokableToolCallEndpoint, tc *adk.ToolContext) (adk.EnhancedInvokableToolCallEndpoint, error) {
	return func(ctx context.Context, a *schema.ToolArgument, o ...tool.Option) (*schema.ToolResult, error) {
		return next(context.WithValue(ctx, callIDKey{}, tc.CallID), a, o...)
	}, nil
}
func (m *middleware) BeforeModelRewriteState(ctx context.Context, state *adk.ChatModelAgentState, mc *adk.ModelContext) (context.Context, *adk.ChatModelAgentState, error) {
	s := m.s
	if s.cp.Calls >= s.profile.MaxIterations {
		return ctx, nil, domain.Err(422, "EVIDENCE_INSUFFICIENT", "取证达到运行上限，尚不能发布。")
	}
	observations, e := s.observations(ctx)
	if e != nil {
		return ctx, nil, e
	}
	seen := map[string]bool{}
	keep := map[string]bool{}
	for _, o := range observations {
		for _, id := range o.FrameIDs {
			seen[id] = true
			keep[id] = o.RetainForComparison
		}
	}
	for _, msg := range state.Messages {
		parts := []schema.MessageInputPart{}
		for _, part := range msg.UserInputMultiContent {
			if part.Type == schema.ChatMessagePartTypeImageURL {
				frame, _ := part.Extra["frame_id"].(string)
				if frame == "" {
					return ctx, nil, errors.New("image reference missing")
				}
				if seen[frame] && !keep[frame] {
					parts = append(parts, schema.MessageInputPart{Type: schema.ChatMessagePartTypeText, Text: "图片已存档，可按 frame_id 重新加载：" + frame})
					continue
				}
			}
			parts = append(parts, part)
		}
		msg.UserInputMultiContent = parts
	}
	// Capsules are rebuilt from original records, never from earlier summaries.
	questions, e := s.engine.Store.Questions(ctx, s.claim.Run.ID)
	if e != nil {
		return ctx, nil, e
	}
	// A call may use the long-writing allowance only after the current visual
	// observations and critical questions are accounted for. Reconstruct this on resume.
	s.compositionReady = s.turn == nil && len(observations) > 0
	for _, ids := range s.cp.Inspections {
		for _, id := range ids {
			if !seen[id] {
				s.compositionReady = false
			}
		}
	}
	for _, raw := range questions {
		var q struct {
			Critical bool
			Status   string
		}
		if json.Unmarshal(raw, &q) != nil || q.Critical && q.Status != "supported" {
			s.compositionReady = false
		}
	}
	capsule := "持久观察记录（不是重新看图）：" + string(domain.JSON(observations)) + "\n当前取证问题：" + string(domain.JSON(questions))
	const capsuleName = "vidlens_evidence_capsule"
	filtered := []*schema.Message{}
	for _, x := range state.Messages {
		if x.Name != capsuleName {
			filtered = append(filtered, x)
		}
	}
	if len(observations) > 0 {
		filtered = append(filtered, &schema.Message{Role: schema.User, Name: capsuleName, Content: capsule})
	}
	state.Messages = filtered
	if e = s.persist(ctx, state.Messages); e != nil {
		return ctx, nil, e
	}
	hydrated, e := s.hydrate(ctx, state.Messages)
	if e != nil {
		return ctx, nil, e
	}
	state.Messages = hydrated
	if e = s.persist(ctx, hydrated); e != nil {
		return ctx, nil, e
	}
	return ctx, state, nil
}
func (m *middleware) AfterModelRewriteState(ctx context.Context, state *adk.ChatModelAgentState, mc *adk.ModelContext) (context.Context, *adk.ChatModelAgentState, error) {
	m.s.cp.Calls++
	for _, x := range state.Messages {
		x.ReasoningContent = ""
		x.Extra = cleanExtra(x.Extra)
	}
	if e := m.s.persist(ctx, state.Messages); e != nil {
		return ctx, nil, e
	}
	return ctx, state, nil
}
func stripPixels(msgs []*schema.Message) []*schema.Message {
	out := make([]*schema.Message, 0, len(msgs))
	for _, m := range msgs {
		copy := *m
		copy.ReasoningContent = ""
		copy.Extra = cleanExtra(m.Extra)
		copy.UserInputMultiContent = append([]schema.MessageInputPart{}, m.UserInputMultiContent...)
		for i := range copy.UserInputMultiContent {
			p := &copy.UserInputMultiContent[i]
			if p.Image != nil {
				im := *p.Image
				im.Base64Data = nil
				im.URL = nil
				p.Image = &im
			}
		}
		copy.ResponseMeta = nil
		out = append(out, &copy)
	}
	return out
}
func (s *session) persist(ctx context.Context, msgs []*schema.Message) error {
	s.cp.Messages = stripPixels(msgs)
	return s.engine.Store.Checkpoint(ctx, s.claim, s.cp)
}
func (s *session) hydrate(ctx context.Context, msgs []*schema.Message) ([]*schema.Message, error) {
	out := stripPixels(msgs)
	var bytes int
	count := 0
	for _, m := range out {
		for i := range m.UserInputMultiContent {
			p := &m.UserInputMultiContent[i]
			if p.Type != schema.ChatMessagePartTypeImageURL {
				continue
			}
			id, _ := p.Extra["frame_id"].(string)
			a, b, e := s.engine.Media.Reload(ctx, s.claim, id, "checkpoint")
			if e != nil {
				return nil, e
			}
			if a.ID != id {
				extra := map[string]any{}
				for k, v := range p.Extra {
					extra[k] = v
				}
				extra["frame_id"] = a.ID
				p.Extra = extra
				for key, ids := range s.cp.Inspections {
					for j, old := range ids {
						if old == id {
							ids[j] = a.ID
						}
					}
					s.cp.Inspections[key] = ids
				}
				for j, old := range s.cp.OverviewIDs {
					if old == id {
						s.cp.OverviewIDs[j] = a.ID
					}
				}
				m.UserInputMultiContent = append(m.UserInputMultiContent, schema.MessageInputPart{Type: schema.ChatMessagePartTypeText, Text: fmt.Sprintf("原帧 %s 的字节不可用，已重新物化为 frame_id=%s actual_time_ms=%d；必须记录新观察。", id, a.ID, *a.TimeMS)})
			}
			count++
			bytes += len(b)
			if count > 12 || bytes > 16<<20 {
				return nil, domain.Err(422, "RESOURCE_LIMIT_EXCEEDED", "当前必须保留的图片超过上下文限额，请拆分比较。")
			}
			p.Image = &schema.MessageInputImage{MessagePartCommon: schema.MessagePartCommon{Base64Data: domain.Ptr(base64.StdEncoding.EncodeToString(b)), MIMEType: a.MIME}}
		}
	}
	if e := validateProtocol(out); e != nil {
		return nil, e
	}
	return out, nil
}
func validateProtocol(msgs []*schema.Message) error {
	pending := map[string]bool{}
	for _, m := range msgs {
		if m.Role == schema.Assistant {
			if len(pending) > 0 {
				return errors.New("incomplete tool exchange")
			}
			for _, c := range m.ToolCalls {
				pending[c.ID] = true
			}
		} else if m.Role == schema.Tool {
			if !pending[m.ToolCallID] {
				return fmt.Errorf("orphan tool result")
			}
			delete(pending, m.ToolCallID)
		} else if len(pending) > 0 {
			return errors.New("content interrupts pending tool exchange")
		}
	}
	if len(pending) > 0 {
		return errors.New("pending tool results")
	}
	return nil
}
func (s *session) observations(ctx context.Context) ([]observation, error) {
	raw, e := s.engine.Store.Observations(ctx, s.claim.Run.ID)
	if e != nil {
		return nil, e
	}
	out := []observation{}
	for _, b := range raw {
		var o observation
		if e = json.Unmarshal(b, &o); e != nil {
			return nil, e
		}
		out = append(out, o)
	}
	return out, nil
}
func (s *session) unobserved(ctx context.Context) []string {
	obs, e := s.observations(ctx)
	if e != nil {
		return []string{"storage unavailable"}
	}
	done := map[string]bool{}
	for _, o := range obs {
		for _, id := range o.FrameIDs {
			done[id] = true
		}
	}
	ids := []string{}
	for _, frames := range s.cp.Inspections {
		for _, id := range frames {
			if !done[id] && !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

func cleanExtra(in map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range in {
		low := strings.ToLower(k)
		if strings.Contains(low, "reasoning") || strings.Contains(low, "thinking") || low == "thoughts" {
			continue
		}
		out[k] = v
	}
	return out
}
