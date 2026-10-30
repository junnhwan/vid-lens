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
	"vidlens/internal/config"
	"vidlens/internal/domain"
)

type checkpoint struct {
	QuestionTiming        *config.QuestionTiming    `json:"question_timing,omitempty"`
	ArticleDeliveryPolicy string                    `json:"article_delivery_policy,omitempty"`
	ArticleLoopParent     json.RawMessage           `json:"article_loop_parent,omitempty"`
	ArticleLoop           *articleLoopState         `json:"article_loop,omitempty"`
	Workflow              *workflowState            `json:"workflow,omitempty"`
	SourceNotes           []sourceNote              `json:"source_notes,omitempty"`
	SourceIndexProtocol   string                    `json:"source_index_protocol,omitempty"`
	SourceChunkDelivered  string                    `json:"source_chunk_delivered,omitempty"`
	PendingOverview       []string                  `json:"pending_overview,omitempty"`
	ExecutionContract     *domain.ExecutionContract `json:"execution_contract,omitempty"`

	HarnessRevision      int                 `json:"harness_revision,omitempty"`
	PlanHash             string              `json:"plan_hash,omitempty"`
	Progress             progressState       `json:"progress,omitempty"`
	InspectBatchRejected map[string]bool     `json:"inspect_batch_rejected,omitempty"`
	LastCandidate        map[string]any      `json:"last_candidate,omitempty"`
	SchemaVersion        int                 `json:"schema_version"`
	Protocol             string              `json:"protocol"`
	LayoutVersion        string              `json:"layout_version,omitempty"`
	Messages             []*schema.Message   `json:"messages"`
	Calls                int                 `json:"calls"`
	Inspections          map[string][]string `json:"inspections"`
	OverviewIDs          []string            `json:"overview_ids"`
	Repeated             map[string]int      `json:"repeated"`
	Ready                bool                `json:"ready"`
	Repairs              int                 `json:"repairs"`
	DraftRepairVersion   int                 `json:"draft_repair_version,omitempty"`
	DraftCorrection      bool                `json:"draft_correction,omitempty"`
	DraftIssue           map[string]any      `json:"draft_issue,omitempty"`
	EvidenceReviewIssue  map[string]any      `json:"evidence_review_issue,omitempty"`
	Composing            bool                `json:"composing,omitempty"`
	CitationFrames       []string            `json:"citation_frames,omitempty"`
	DraftResume          bool                `json:"draft_resume,omitempty"`
	FinalizationFailure  *domain.Problem     `json:"finalization_failure,omitempty"`
	FinalizationFailed   bool                `json:"finalization_failed,omitempty"`
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
	RetainForComparison bool     `json:"retain_for_comparison" jsonschema:"description=Keep these observed pixels for an ongoing comparison. At most 12 retained plus new images fit in context. Record a new observation with false when comparison is finished to free image capacity; durable facts remain available."`
	NextAction          string   `json:"next_action"`
}
type callIDKey struct{}

// Inspect admission must precede question/frame creation. A model can release
// finished comparisons through another durable observation rather than fail
// the entire video when the next model context is hydrated.
func (s *session) inspectionHeadroom(ctx context.Context) (int, error) {
	observations, err := s.observations(ctx)
	if err != nil {
		return 0, err
	}
	keep := map[string]bool{}
	for _, o := range observations {
		if o.Status != "supported" && o.Status != "unresolved" {
			continue
		}
		for _, id := range o.FrameIDs {
			keep[id] = o.RetainForComparison
		}
	}
	if s.turn != nil && s.turn.Context.FrameID != nil {
		keep[*s.turn.Context.FrameID] = true
	}
	count := 0
	var bytes int64
	for id, retained := range keep {
		if !retained {
			continue
		}
		asset, err := s.engine.Store.Asset(ctx, id)
		if err != nil {
			return 0, err
		}
		count++
		bytes += asset.Bytes
	}
	// Each delivered JPEG is bounded to 2 MiB. Account conservatively for
	// upcoming pixels before they exist, without increasing the 16 MiB cap.
	return max(0, min(6, 12-count, int(((16<<20)-bytes)/(2<<20)))), nil
}

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
	if s.boundedSourceContext() {
		for _, msg := range state.Messages {
			if msg.Name == sourceContextName {
				msg.Content = string(domain.JSON(s.sourceContext()))
			}
		}
		if err := s.deliverOverviewBatch(ctx, &state.Messages); err != nil {
			return ctx, nil, err
		}
	}
	if s.policy().Enabled && s.cp.Progress.Total >= 4*s.profile.MaxIterations {
		return ctx, nil, domain.Err(422, "RESOURCE_LIMIT_EXCEEDED", "工具调用达到本次任务的累计上限；来源和草稿保留。")
	}
	if s.usesBoundedReview() && s.cp.Progress.Recovery != nil && s.cp.Progress.Recovery.Attempts >= 3 && !s.loopCorrectionPending() {
		r := s.cp.Progress.Recovery
		return ctx, nil, domain.Err(422, "AGENT_NO_PROGRESS", fmt.Sprintf("重复执行 %s 在纠偏后仍未推进：%s。来源、已保存草稿和具体错误已保留。", r.Tool, r.Reason))
	}
	if s.policy().Enabled && s.spec.DetailPolicy != nil && s.cp.Progress.Stalled >= s.spec.DetailPolicy.NoProgressLimit && !s.loopCorrectionPending() {
		return ctx, nil, domain.Err(422, "AGENT_NO_PROGRESS", "连续工具调用未增加可用证据或有效草稿；来源和已保存章节保留，可以重新尝试。")
	}
	if s.cp.Calls >= s.profile.MaxIterations {
		return ctx, nil, domain.Err(422, "RESOURCE_LIMIT_EXCEEDED", "模型调用达到运行上限，尚不能发布；来源和草稿保留。")
	}
	observations, e := s.observations(ctx)
	if e != nil {
		return ctx, nil, e
	}
	seen := map[string]bool{}
	keep := map[string]bool{}
	for _, o := range observations {
		if o.Status != "supported" && o.Status != "unresolved" {
			continue
		}
		for _, id := range o.FrameIDs {
			seen[id] = true
			keep[id] = o.RetainForComparison
		}
	}
	if s.turn != nil && s.turn.Context.FrameID != nil {
		keep[*s.turn.Context.FrameID] = true
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
	s.compositionReady = s.turn == nil && len(observations) > 0 && len(s.cp.PendingOverview) == 0 && !s.sourceSurveyPending()
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
	var draftContext *schema.Message
	freshMessages := []*schema.Message{}
	for _, x := range state.Messages {
		if x.Name != "vidlens_draft_state" && x.Name != "vidlens_evidence_capsule" && x.Name != "vidlens_readiness" && x.Name != "vidlens_answer_references" && (x.Name != "vidlens_last_feedback" || s.authoring()) {
			freshMessages = append(freshMessages, x)
		}
	}
	state.Messages = freshMessages
	if s.sourceSurveyPending() {
		s.forceDraftSubmit = false
	}
	if s.cp.LayoutVersion == draftLayout && !s.sourceSurveyPending() {
		saved, draft, err := s.loadDraft(ctx)
		if err != nil {
			return ctx, nil, err
		}
		s.cp.Composing = saved > 0
		if s.cp.DraftCorrection && saved > s.cp.DraftRepairVersion {
			s.cp.DraftCorrection = false
		}
		s.forceDraftSubmit = saved > 0 && len(draft.Sections) == len(draft.Plan) && len(draft.Plan) > 0 && !s.cp.DraftCorrection
		if s.boundedSourceContext() && !s.compositionReady {
			s.forceDraftSubmit = false
		}
		catalog, err := s.citationCatalog(ctx)
		if err != nil {
			return ctx, nil, err
		}
		if s.authoring() {
			state.Messages = projectAuthoringHistory(state.Messages)
		} else {
			state.Messages = projectDraftHistory(state.Messages)
		}
		hints := catalogHints(catalog)
		if s.authoring() {
			hints, err = authoringCatalogHints(catalog, s.transcript)
			if err != nil {
				return ctx, nil, err
			}
		}
		if s.boundedSourceContext() {
			hints = compactSourceCatalog(catalog, state.Messages, draft)
		}
		data := map[string]any{"draft": s.draftState(saved, draft), "citation_catalog": hints}
		if s.policy().EvidenceGrounding {
			data["frame_context"] = frameContexts(catalog, s.transcript)
		}
		if issue := s.pendingEvidenceReview(); issue != nil {
			data["evidence_review_issues"] = issue
		}
		if s.authoring() {
			data["citation_catalog_rule"] = authoringCatalogRule
		}
		if s.boundedSourceContext() {
			data["citation_catalog_rule"] = "Only currently relevant aliases are listed. read_source/find_evidence return original tN aliases and source IDs. Reload archived text for detailed claims; source notes are derived navigation."
		}
		if s.policy().SectionRepairs {
			output := s.profile.MaxOutputTokens
			if s.spec.DetailPolicy != nil {
				output = min(output, s.spec.DetailPolicy.CompositionOutputTokens)
			}
			perChapter := 1200
			if s.spec.DetailLevel == "detailed" {
				perChapter = 2200
			}
			data["batch_guidance"] = map[string]any{"output_tokens": output, "recommended_chapters": min(12, max(1, (output-1000)/perChapter)), "max_chapters": 12, "max_bytes": 128 << 10, "rule": "Prefer fewer complete batches; estimate actual content size. Preserve required details, leave room for closing JSON. Save valid chapters independently. Server validates and publishes automatically when complete."}
			if s.authoring() {
				data["batch_guidance"] = map[string]any{"base_output_tokens": output, "max_chapters": maxPlanSections, "max_bytes": 128 << 10, "rule": "Write ALL pending chapters in ONE complete call when the actual output size fits. No fixed chapter quota. Dense sources may receive more output reservation within provider/context limits. Split only when necessary; preserve required detail, figures and closing JSON. Server validates and publishes automatically."}
			}
			if s.boundedSourceContext() {
				data["batch_guidance"] = map[string]any{"output_tokens": output, "recommended_chapters": min(6, max(1, (output-2048)/perChapter)), "max_chapters": 12, "max_bytes": 128 << 10, "rule": "Save complete bounded batches of pending chapters within this per-call output budget. A longer source does not expand one response to the entire transcript size. Preserve details, figures and closing JSON; the server assembles all saved chapters and reviews the complete article."}
			}
		}
		data["source_duration_ms"] = s.snapshot.DurationMS
		data["citation_time_rule"] = "Citation catalog ranges are intersections of original source windows with actual media. Raw platform cue end times can extend beyond the media. Preserve source text; the full-video plan ends at source_duration_ms, not the last cue end."
		if s.policy().Enabled {
			data["saved_section_previews"] = draftPreviews(draft)
		}
		draftContext = &schema.Message{Role: schema.User, Name: "vidlens_draft_state", Content: string(domain.JSON(data))}
	}
	capsule := "持久观察记录（不是重新看图）：" + string(domain.JSON(observations)) + "\n当前取证问题：" + string(domain.JSON(questions))
	const capsuleName = "vidlens_evidence_capsule"
	filtered := []*schema.Message{}
	for _, x := range state.Messages {
		if x.Name != capsuleName {
			filtered = append(filtered, x)
		}
	}
	if s.cp.LayoutVersion == "source-context-v2" || s.cp.LayoutVersion == draftLayout {
		filtered = projectToolHistory(filtered, s.cp.LayoutVersion == draftLayout)
		filtered = s.projectSourceReads(filtered)
	}
	if s.policy().Enabled {
		filtered = projectRejectedCandidates(filtered)
	}
	if draftContext != nil {
		filtered = append(filtered, draftContext)
	}
	if s.compactAnswerProtocol() {
		catalog, err := s.citationCatalog(ctx)
		if err != nil {
			return ctx, nil, err
		}
		refs := map[string]any{"citation_catalog": catalogHints(catalog), "rule": "Use tN/fN directly in answer blocks; omit video citation objects. Cite representative evidence for this question, not every subtitle."}
		if s.boundedSourceContext() {
			refs["citation_catalog"] = compactSourceCatalog(catalog, filtered, nil)
		}
		if s.questionReading() {
			refs["citation_catalog"] = questionCitationHints(catalog, compactSourceCatalog(catalog, filtered, nil))
			refs["rule"] = "Catalog values are compact identity-kind tags, not source text or proof that pixels were seen. Use existing tN/fN keys in answer blocks; the server reconstructs exact original/frame metadata. Read the original passages needed for the current question and record actual pixel observations. Never repeat video citation objects."
		}
		if s.policy().EvidenceGrounding {
			refs["frame_context"] = frameContexts(catalog, s.transcript)
			refs["question_frame_ids"] = s.cp.Inspections["question-context"]
		}
		filtered = append(filtered, &schema.Message{Role: schema.User, Name: "vidlens_answer_references", Content: string(domain.JSON(refs))})
	}
	if len(observations) > 0 {
		filtered = append(filtered, &schema.Message{Role: schema.User, Name: capsuleName, Content: capsule})
	}
	if s.policy().Enabled {
		ready, err := s.readiness(ctx)
		if err != nil {
			return ctx, nil, err
		}
		filtered = append(filtered, &schema.Message{Role: schema.User, Name: "vidlens_readiness", Content: string(domain.JSON(ready))})
		if s.policy().Paced && s.turn == nil {
			if err = s.saveExecutionPlan(ctx, ready); err != nil {
				return ctx, nil, err
			}
		}
	}
	filtered, e = s.projectReferences(ctx, filtered)
	if e != nil {
		return ctx, nil, e
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
	if m.s.modelMessages != nil && len(state.Messages) > 0 {
		last := state.Messages[len(state.Messages)-1]
		if last.Role == schema.Assistant {
			state.Messages = append(m.s.modelMessages, last)
		}
		m.s.modelMessages = nil
	}
	m.s.cp.Calls++
	if m.s.policy().Enabled {
		m.s.cp.InspectBatchRejected = map[string]bool{}
		// Reject a multi-inspection response as a whole before any frame/question
		// side effect. Eino still emits a paired result for every original call.
		if len(state.Messages) > 0 {
			last := state.Messages[len(state.Messages)-1]
			if last.Role == schema.Assistant && rejectInspectionBatch(last.ToolCalls) {
				for _, c := range last.ToolCalls {
					if c.Function.Name == "inspect_video_window" {
						m.s.cp.InspectBatchRejected[c.ID] = true
					}
				}
			}
		}
	}
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
				if s.stagedWorkflow() || s.articleLoop() {
					return nil, domain.Err(422, "WORKFLOW_EVIDENCE_CHANGED", "冻结调用中的图片身份已经改变；原输入保留，未重新发送或沿用审核。")
				}
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

// A completed observation exchange is represented once by the canonical
// evidence capsule. Mixed, pending and failed groups retain the full protocol.
func projectToolHistory(messages []*schema.Message, dropLast ...bool) []*schema.Message {
	out := make([]*schema.Message, 0, len(messages))
	for i := 0; i < len(messages); i++ {
		m := messages[i]
		if m.Role != schema.Assistant || len(m.ToolCalls) == 0 {
			out = append(out, m)
			continue
		}
		drop := true
		ids := map[string]bool{}
		for _, c := range m.ToolCalls {
			if c.Function.Name != "record_observation" && c.Function.Name != "record_source_notes" {
				drop = false
			}
			ids[c.ID] = true
		}
		j := i + 1
		for j < len(messages) && messages[j].Role == schema.Tool {
			body := messages[j].Content
			for _, part := range messages[j].UserInputMultiContent {
				if part.Type == schema.ChatMessagePartTypeText {
					body += part.Text
				}
			}
			var result struct {
				Saved bool `json:"saved"`
			}
			if !ids[messages[j].ToolCallID] || json.Unmarshal([]byte(body), &result) != nil || !result.Saved {
				drop = false
			}
			delete(ids, messages[j].ToolCallID)
			j++
		}
		if drop && len(ids) == 0 && (j < len(messages) || len(dropLast) > 0 && dropLast[0]) {
			i = j - 1
			continue
		}
		out = append(out, m)
	}
	return out
}
func (s *session) projectSourceReads(messages []*schema.Message) []*schema.Message {
	if s.semanticQuestionReading() {
		return s.projectQuestionSourceReads(messages)
	}
	if s.boundedSourceContext() {
		return projectIndexedSourceReads(messages)
	}
	known := map[string]bool{}
	for _, seg := range s.transcript {
		known[seg.ID] = true
	}
	out := make([]*schema.Message, 0, len(messages))
	for _, m := range messages {
		if m.Role == schema.Tool && m.ToolName == "read_source" {
			body := m.Content
			for _, p := range m.UserInputMultiContent {
				if p.Type == schema.ChatMessagePartTypeText {
					body += p.Text
				}
			}
			var result struct {
				SnapshotID string           `json:"snapshot_id"`
				Segments   []domain.Segment `json:"segments"`
				HasMore    bool             `json:"has_more"`
			}
			if json.Unmarshal([]byte(body), &result) == nil && result.SnapshotID == s.snapshot.ID && len(result.Segments) > 0 {
				refs := []map[string]any{}
				valid := true
				for _, seg := range result.Segments {
					if !known[seg.ID] {
						valid = false
					}
					refs = append(refs, map[string]any{"id": seg.ID, "start_ms": seg.StartMS, "end_ms": seg.EndMS, "origin": seg.Origin})
				}
				if valid {
					copy := *m
					copy.UserInputMultiContent = nil
					copy.Content = string(domain.JSON(map[string]any{"snapshot_id": result.SnapshotID, "segments": refs, "has_more": result.HasMore, "text_location": "exact full text is already present in the frozen source message; IDs and actual windows retained"}))
					out = append(out, &copy)
					continue
				}
			}
		}
		out = append(out, m)
	}
	return out
}
