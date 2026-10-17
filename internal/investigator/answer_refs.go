package investigator

import (
	"context"
	"encoding/json"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
	"vidlens/internal/domain"
)

const compactAnswerInstructions = `
本轮采用精简问答引用协议。只回答当前问题，不重述整片；按问题中的独立要点组织答案，用户要求深入时再展开。每个要点选1–3个最有代表性的来源，不枚举整段所有字幕。
vidlens_answer_references.citation_catalog 是本轮可信的短引用目录。正文、表格、列表、限制说明直接使用其中的 tN/fN citation_id；figure.frame_ids也可用目录里的fN。submit_answer 不需要重抄 citations 字典，服务端从原记录构造完整引用。禁止重写 video_text/video_frame 的 snapshot_id、segment_ids、时间和URL。citations 字段仅供需要的 external_reference/article_passage，仍须通过原始来源校验；没有这些来源时省略。先完成必要观察，再提交一个完整、简洁的JSON对象，预留闭合括号，不为普通概括问题生成长篇文章或巨大引用清单。`

type compactAnswerInput struct {
	Kind        string                     `json:"kind" jsonschema:"enum=grounded,enum=insufficient_evidence,enum=out_of_scope"`
	Summary     string                     `json:"summary"`
	Blocks      []domain.Block             `json:"blocks" jsonschema:"description=Answer only the current question. Cite representative tN/fN IDs from vidlens_answer_references directly; never repeat full video citation objects."`
	Citations   map[string]domain.Citation `json:"citations,omitempty" jsonschema:"description=Optional selected external_reference or article_passage objects only. OMIT for video-only answers: tN/fN video identities are reconstructed by the server."`
	Limitations []domain.Limitation        `json:"limitations"`
}

func (s *session) compactAnswerProtocol() bool { return s.turn != nil && s.cp.HarnessRevision >= 6 }

func resolveAnswerReferences(in compactAnswerInput, catalog map[string]domain.Citation) (answerInput, error) {
	out := answerInput{Kind: in.Kind, Summary: in.Summary, Citations: map[string]domain.Citation{}, Limitations: in.Limitations}
	if err := json.Unmarshal(domain.JSON(in.Blocks), &out.Blocks); err != nil {
		return out, err
	}
	for id, c := range in.Citations {
		if c.Kind != "external_reference" && c.Kind != "article_passage" {
			return out, invalid("问答只需引用目录中的tN/fN；不要重抄或修改视频引用元数据：" + id)
		}
		out.Citations[id] = c
	}
	used := map[string]bool{}
	for i := range out.Blocks {
		b := &out.Blocks[i]
		for j, id := range b.FrameIDs {
			if c, ok := catalog[id]; ok && c.Kind == "video_frame" {
				b.FrameIDs[j] = c.FrameID
			}
		}
		for _, id := range blockCitationIDs(*b) {
			used[id] = true
		}
	}
	for _, l := range out.Limitations {
		for _, id := range l.CitationIDs {
			used[id] = true
		}
	}
	for id := range used {
		if c, ok := catalog[id]; ok {
			out.Citations[id] = c
		} else if _, ok := out.Citations[id]; !ok {
			return out, invalid("问答引用不存在；请使用本轮citation_catalog提供的tN/fN：" + id)
		}
	}
	return out, nil
}

func (s *session) answerTool(add func(tool.EnhancedInvokableTool, error) error) error {
	persist := func(ctx context.Context, in answerInput, parts []domain.AnswerPart) (*schema.ToolResult, error) {
		a := domain.Answer{Kind: in.Kind, Summary: in.Summary, Blocks: in.Blocks, Parts: parts, Citations: in.Citations, Frames: []domain.Frame{}, Limitations: in.Limitations}
		if a.Limitations == nil {
			a.Limitations = []domain.Limitation{}
		}
		if e := s.validateAnswer(ctx, &a); e != nil {
			return nil, &candidateError{err: e}
		}
		if e := validateAnswerParts(&a); e != nil {
			return nil, &candidateError{err: e}
		}
		var args any = in
		if s.cp.HarnessRevision >= 10 {
			args = map[string]any{"answer": in, "parts": parts}
		}
		if _, e := s.engine.Store.BeginOperation(ctx, s.claim, "candidate", "answer_candidate", args, false); e != nil {
			return nil, e
		}
		if e := s.engine.Store.FinishOperation(ctx, s.claim, "candidate", a, "", nil); e != nil {
			return nil, e
		}
		return textResult(map[string]any{"accepted_for_publication": true}), nil
	}
	submit := func(ctx context.Context, in answerInput) (*schema.ToolResult, error) { return persist(ctx, in, nil) }
	if s.cp.HarnessRevision >= 10 {
		return add(utils.InferEnhancedTool("submit_answer", "Submit the answer directly, with a short direct summary and non-repeating evidence-backed explanation. Optional parts name the actual aspects of this question, not a fixed template. Use existing tN/fN references; no separate prose answer before this tool.", func(ctx context.Context, in composedAnswerInput) (*schema.ToolResult, error) {
			catalog, err := s.citationCatalog(ctx)
			if err != nil {
				return nil, err
			}
			resolved, err := resolveAnswerReferences(compactAnswerInput{Kind: in.Kind, Summary: in.Summary, Blocks: in.Blocks, Citations: in.Citations, Limitations: in.Limitations}, catalog)
			if err != nil {
				return nil, &candidateError{err: err}
			}
			return persist(ctx, resolved, in.Parts)
		}))
	}
	if !s.compactAnswerProtocol() {
		return add(utils.InferEnhancedTool("submit_answer", "Submit a finalized answer for this private turn. It cannot edit the article. All references will be validated.", submit))
	}
	return add(utils.InferEnhancedTool("submit_answer", "Submit a focused answer for this private turn. Use tN/fN citation IDs from vidlens_answer_references without supplying video citation objects. The server reconstructs owned source identities, validates every reference and cannot edit the article. The optional citations map is only for selected external_reference/article_passage objects.", func(ctx context.Context, in compactAnswerInput) (*schema.ToolResult, error) {
		catalog, err := s.citationCatalog(ctx)
		if err != nil {
			return nil, err
		}
		resolved, err := resolveAnswerReferences(in, catalog)
		if err != nil {
			return nil, &candidateError{err: err}
		}
		return submit(ctx, resolved)
	}))
}
