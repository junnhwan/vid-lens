package investigator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
	"github.com/jackc/pgx/v5"
	"vidlens/internal/domain"
	"vidlens/internal/store"
)

const draftLayout = "source-context-v3"

type sectionPlan struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	StartMS     int64    `json:"start_ms"`
	EndMS       int64    `json:"end_ms"`
	KeyPoints   []string `json:"key_points,omitempty" jsonschema:"description=Distinct supported points to retain in this chapter, 1-12 items. Keep necessary steps/conditions at all levels. Do not repeat other chapter points."`
	EvidenceIDs []string `json:"evidence_ids,omitempty" jsonschema:"description=1-24 existing tN/fN citation aliases supporting these points, intersecting this chapter source range."`
}

type legacySectionPlan struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	StartMS int64  `json:"start_ms"`
	EndMS   int64  `json:"end_ms"`
}
type legacyBeginArticleInput struct {
	Title       string              `json:"title"`
	Sections    []legacySectionPlan `json:"sections" jsonschema:"description=Ordered outline covering the whole video. Usually 4-8 chapters; use exact video range boundaries, first start_ms=0, final end_ms=video duration. No body text yet."`
	Limitations []domain.Limitation `json:"limitations"`
}
type beginArticleInput struct {
	Title       string              `json:"title"`
	Sections    []sectionPlan       `json:"sections" jsonschema:"description=Full-video topic plan with distinct key_points and supporting evidence_ids. Use 1-40 chapters according to actual topics, never a fixed time/word quota. No body text yet."`
	Limitations []domain.Limitation `json:"limitations"`
}
type saveSectionsInput struct {
	Version  int              `json:"version" jsonschema:"description=Copy the latest draft version from the last successful write. Put multiple chapters into this ONE call, do not issue concurrent writes with the same version."`
	Sections []domain.Section `json:"sections" jsonschema:"description=Write or replace 1-3 planned chapters in one bounded call. Use tN/fN citation aliases, including fN in figure.frame_ids. Do not repeat source metadata or citations dictionary. Keep each batch concise."`
}
type submitDraftInput struct {
	Version int `json:"version"`
}
type articleDraft struct {
	Title        string                     `json:"title"`
	Plan         []sectionPlan              `json:"plan"`
	Sections     map[string]domain.Section  `json:"sections"`
	Citations    map[string]domain.Citation `json:"citations"`
	Limitations  []domain.Limitation        `json:"limitations"`
	MindMap      *domain.MindMap            `json:"mind_map,omitempty"`
	BatchReceipt *batchReceipt              `json:"batch_receipt,omitempty"`
}

func (s *session) draftTools(add func(tool.EnhancedInvokableTool, error) error) error {
	beginWithMap := func(ctx context.Context, in beginArticleInput, mindMap *domain.MindMap) (*schema.ToolResult, error) {
		var args any = in
		if s.policy().ConceptMaps {
			args = conceptPlanInput{Title: in.Title, Sections: in.Sections, MindMap: mindMap, Limitations: in.Limitations}
		}
		if result, err := s.replayedDraft(ctx, args); result != nil || err != nil {
			return result, err
		}
		old, err := s.engine.Store.ArticleDraft(ctx, s.claim.Run.ID)
		if err != nil {
			return nil, err
		}
		if old.Version != 0 {
			return s.draftFeedback(ctx, "文章结构已经保存，请使用当前version继续保存章节，不要重建。")
		}
		if strings.TrimSpace(in.Title) == "" || len(in.Sections) == 0 || len(in.Sections) > maxPlanSections {
			return s.draftFeedback(ctx, "需要标题与1–40个覆盖全片的章节。")
		}
		if !s.compositionReady {
			return s.draftFeedback(ctx, "请先保存所有新图片的观察并解决关键取证问题，再开始成文。")
		}
		ids := map[string]bool{}
		for _, p := range in.Sections {
			ids[p.ID] = true
		}
		draft := articleDraft{Title: in.Title, Plan: in.Sections, Sections: map[string]domain.Section{}, Citations: map[string]domain.Citation{}, Limitations: in.Limitations, MindMap: mindMap}
		if s.policy().ConceptMaps {
			mapError := validateConceptMap(mindMap, ids)
			if s.authoring() && mapError != nil {
				mapError = authoringConceptMapIssue(mindMap, ids, mapError)
			}
			if err := mapError; err != nil {
				if s.authoring() {
					return s.draftFeedback(ctx, string(domain.JSON(candidateIssue(err))))
				}
				return s.draftFeedback(ctx, err.Error())
			}
		}
		catalog, err := s.citationCatalog(ctx)
		if err != nil {
			return nil, err
		}
		if s.authoring() {
			draft.Plan = deriveAuthoringRanges(in.Sections, catalog, s.snapshot.DurationMS)
		}
		planErr := validateSectionPlan(draft.Plan, s.snapshot.DurationMS, s.policy().Enabled)
		if s.authoring() {
			planErr = validateAuthoringPlan(draft.Plan, s.snapshot.DurationMS)
		}
		if s.spec.DetailPolicy != nil && s.spec.DetailPolicy.ContentPlan {
			planErr = errors.Join(planErr, validatePlanFacts(draft.Plan, catalog))
		}
		if planErr != nil {
			return s.draftFeedback(ctx, string(domain.JSON(candidateIssue(planErr))))
		}
		if s.authoring() {
			if err = authoringLimitationIssues(draft.Limitations, ids, catalog); err != nil {
				return s.draftFeedback(ctx, string(domain.JSON(candidateIssue(err))))
			}
		}
		if err = resolveDraftCitations(&draft, catalog); err != nil {
			return s.draftFeedback(ctx, err.Error())
		}
		if err = validateLimitations(draft.Limitations, ids, draft.Citations); err != nil {
			return s.draftFeedback(ctx, err.Error())
		}
		title := "正在保存文章结构"
		if s.policy().ConceptMaps {
			title += "与概念关系"
		}
		return s.writeDraft(ctx, args, 0, draft, title)
	}
	begin := func(ctx context.Context, in beginArticleInput) (*schema.ToolResult, error) {
		return beginWithMap(ctx, in, nil)
	}
	beginDescription := "Finish investigation and persist a full-video article outline. No giant article JSON. Save body chapters in bounded batches with save_article_sections, then submit_article_draft with its version."
	options := []utils.Option{}
	if s.policy().ContentPlan {
		beginDescription = "Finish investigation and persist a full-video article outline. For each chapter choose only 1-24 representative citation IDs that support its key points; do not list every citation in the time range. No giant article JSON. Save body chapters in bounded batches with save_article_sections, then submit_article_draft with its version."
		options = append(options, utils.WithSchemaModifier(contentPlanSchema))
	}
	if s.policy().ConceptMaps {
		if err := add(utils.InferEnhancedTool("begin_article", "Persist the full-video outline AND a concept relationship tree in one operation. Save body chapters afterward in a few complete batches; the server publishes automatically when complete.", func(ctx context.Context, in conceptPlanInput) (*schema.ToolResult, error) {
			return beginWithMap(ctx, beginArticleInput{Title: in.Title, Sections: in.Sections, Limitations: in.Limitations}, in.MindMap)
		}, options...)); err != nil {
			return err
		}
	} else if s.spec.DetailPolicy != nil && s.spec.DetailPolicy.ContentPlan {
		if err := add(utils.InferEnhancedTool("begin_article", beginDescription, begin, options...)); err != nil {
			return err
		}
	} else {
		if err := add(utils.InferEnhancedTool("begin_article", beginDescription, func(ctx context.Context, in legacyBeginArticleInput) (*schema.ToolResult, error) {
			var next beginArticleInput
			if err := json.Unmarshal(domain.JSON(in), &next); err != nil {
				return nil, err
			}
			return begin(ctx, next)
		})); err != nil {
			return err
		}
	}
	if err := add(utils.InferEnhancedTool("save_article_sections", "Save or replace 1-3 planned chapters. Accepted chapters survive malformed later calls. Citation aliases are resolved by the server. Use one call per response and latest version; final publication always revalidates the entire article.", func(ctx context.Context, in saveSectionsInput) (*schema.ToolResult, error) {
		if result, err := s.replayedDraft(ctx, in); result != nil || err != nil {
			return result, err
		}
		version, draft, err := s.loadDraft(ctx)
		if s.forceDraftSubmit {
			return s.draftFeedback(ctx, "全部计划章节已保存。现在只允许submit_article_draft正式提交；校验指出问题后才开放修订。")
		}
		if err != nil {
			return nil, err
		}
		if version == 0 || version != in.Version {
			return s.draftFeedback(ctx, "version不匹配，请读取反馈中的当前草稿版本。")
		}
		if s.policy().SectionRepairs {
			return s.saveFlexibleBatch(ctx, in, version, draft)
		}
		if len(in.Sections) < 1 || len(in.Sections) > 3 || len(domain.JSON(in.Sections)) > 64<<10 {
			return s.draftFeedback(ctx, "每批保存1–3章，序列化内容最多64KiB；请缩小当前批次，已有章节保持。")
		}
		catalog, err := s.citationCatalog(ctx)
		if err != nil {
			return nil, err
		}
		next, err := assembleDraftBatch(draft, in.Sections, catalog)
		if err != nil {
			return s.draftFeedback(ctx, err.Error())
		}
		for _, section := range in.Sections {
			sec := next.Sections[section.ID]
			if err = s.validateBlocks(ctx, sec.Blocks, next.Citations, true); err != nil {
				return s.draftFeedback(ctx, string(domain.JSON(candidateIssue(err))))
			}
		}
		return s.writeDraft(ctx, in, version, next, "正在保存文章章节")
	})); err != nil {
		return err
	}
	if err := add(utils.InferEnhancedTool("update_article_limitations", "Replace the saved draft limitations using its latest version; keep accepted chapter text unchanged.", func(ctx context.Context, in struct {
		Version     int                 `json:"version"`
		Limitations []domain.Limitation `json:"limitations"`
	}) (*schema.ToolResult, error) {
		if r, e := s.replayedDraft(ctx, in); r != nil || e != nil {
			return r, e
		}
		version, draft, err := s.loadDraft(ctx)
		if err != nil {
			return nil, err
		}
		if version == 0 || version != in.Version {
			return s.draftFeedback(ctx, "限制说明的version不匹配。")
		}
		if s.forceDraftSubmit {
			return s.draftFeedback(ctx, "全部计划章节已保存。现在只允许submit_article_draft正式提交；校验指出问题后才开放修订。")
		}
		draft.Limitations = in.Limitations
		catalog, err := s.citationCatalog(ctx)
		if err != nil {
			return nil, err
		}
		if err = resolveDraftCitations(&draft, catalog); err != nil {
			return s.draftFeedback(ctx, err.Error())
		}
		ids := map[string]bool{}
		for _, p := range draft.Plan {
			ids[p.ID] = true
		}
		if err = validateLimitations(draft.Limitations, ids, draft.Citations); err != nil {
			return s.draftFeedback(ctx, err.Error())
		}
		return s.writeDraft(ctx, in, version, draft, "正在保存文章限制说明")
	})); err != nil {
		return err
	}
	if err := add(utils.InferEnhancedTool("read_article_sections", "Read 1-3 saved chapters for a local correction, without regenerating other chapters.", func(ctx context.Context, in struct {
		SectionIDs []string `json:"section_ids"`
	}) (*schema.ToolResult, error) {
		version, draft, err := s.loadDraft(ctx)
		if err != nil {
			return nil, err
		}
		if len(in.SectionIDs) == 0 || len(in.SectionIDs) > 3 {
			return s.draftFeedback(ctx, "一次读取1–3个已保存章节。")
		}
		sections := []domain.Section{}
		for _, id := range in.SectionIDs {
			if sec, ok := draft.Sections[id]; ok {
				sections = append(sections, sec)
			}
		}
		return textResult(map[string]any{"version": version, "sections": sections}), nil
	})); err != nil {
		return err
	}
	return add(utils.InferEnhancedTool("submit_article_draft", "Submit the latest saved draft version for full validation and immutable publication. Do not repeat article body. All planned chapters must have been saved.", func(ctx context.Context, in submitDraftInput) (*schema.ToolResult, error) {
		return s.submitSavedDraft(ctx, in)
	}))
}

func (s *session) submitSavedDraft(ctx context.Context, in submitDraftInput) (*schema.ToolResult, error) {
	version, draft, err := s.loadDraft(ctx)
	if err != nil {
		return nil, err
	}
	if version == 0 || version != in.Version {
		return nil, &candidateError{err: domain.Err(422, "MODEL_RESULT_INVALID", "草稿版本不匹配，请使用当前保存版本提交。")}
	}
	catalog, err := s.citationCatalog(ctx)
	if err != nil {
		return nil, err
	}
	if err = resolveDraftCitations(&draft, catalog); err != nil {
		return nil, &candidateError{err: err}
	}
	article, err := s.assembleArticle(draft)
	if err != nil {
		return nil, &candidateError{err: err}
	}
	step := domain.Step{ID: "validate_" + s.claim.Run.ID + "_" + strconv.Itoa(version), Title: "正在核对文章内容与引用", Phase: "validate", Status: "running", StartedAt: domain.Ptr(time.Now().UTC()), FrameIDs: []string{}}
	step, err = s.engine.Store.Step(ctx, s.claim, step)
	if err != nil {
		return nil, err
	}
	err = s.validateArticle(ctx, article)
	step.FinishedAt = domain.Ptr(time.Now().UTC())
	step.Status = "completed"
	step.Outcome = &domain.Outcome{Message: "全部章节、来源引用与正式配图已通过校验。", EvidenceStatus: "not_applicable"}
	if err != nil {
		step.Status = "failed"
		step.Outcome = &domain.Outcome{Message: "文章还需要修正，已保存章节保留。", EvidenceStatus: "not_assessed"}
	}
	if _, saveErr := s.engine.Store.Step(ctx, s.claim, step); saveErr != nil {
		return nil, saveErr
	}
	if err != nil {
		return nil, &candidateError{err: err}
	}
	if _, err = s.engine.Store.BeginOperation(ctx, s.claim, "candidate", "article_candidate", in, false); err != nil {
		return nil, err
	}
	if err = s.engine.Store.FinishOperation(ctx, s.claim, "candidate", article, "", nil); err != nil {
		return nil, err
	}
	return textResult(map[string]any{"accepted_for_publication": true, "version": version}), nil
}

func validateSectionPlan(plan []sectionPlan, duration int64, allowOverlap bool) error {
	ids := map[string]bool{}
	end, lastStart := int64(0), int64(0)
	if len(plan) < 1 || len(plan) > maxPlanSections {
		return domain.Err(422, "MODEL_RESULT_INVALID", "需要1–40个覆盖全片的主题章节。")
	}
	for i, p := range plan {
		validStart := p.StartMS == end
		if allowOverlap {
			validStart = p.StartMS >= lastStart && p.StartMS <= end
		}
		if p.ID == "" || len(p.ID) > 80 || ids[p.ID] || strings.TrimSpace(p.Title) == "" || !validStart || p.StartMS < 0 || p.EndMS <= p.StartMS || p.EndMS > duration {
			return &domain.Problem{Status: 422, Code: "MODEL_RESULT_INVALID", Detail: "章节ID需唯一，来源范围按开始时间有序且联合覆盖全片；同一片段可解释不同主题，但不能有时间缺口或越界。", Meta: map[string]any{"path": fmt.Sprintf("/sections/%d", i), "expected": fmt.Sprintf("start_ms within %d..%d; end_ms <= %d", lastStart, end, duration), "actual": p}}
		}
		ids[p.ID] = true
		lastStart = p.StartMS
		end = max(end, p.EndMS)
	}
	if end != duration {
		return &domain.Problem{Status: 422, Code: "MODEL_RESULT_INVALID", Detail: "章节计划没有联合覆盖到视频结尾。", Meta: map[string]any{"path": "/sections", "expected": duration, "actual": end}}
	}
	return nil
}

func (s *session) loadDraft(ctx context.Context) (int, articleDraft, error) {
	saved, err := s.engine.Store.ArticleDraft(ctx, s.claim.Run.ID)
	var draft articleDraft
	if err == nil && saved.Version > 0 {
		err = json.Unmarshal(saved.Data, &draft)
	}
	return saved.Version, draft, err
}
func (s *session) replayedDraft(ctx context.Context, args any) (*schema.ToolResult, error) {
	op, err := s.engine.Store.Operation(ctx, s.claim.Run.ID, "article-draft:"+callID(ctx))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if op.Kind != "article_draft" {
		return nil, errors.New("draft replay conflict")
	}
	if op.ArgsHash != store.Hash(args) {
		// Older writers hashed their incoming slices after assigning canonical
		// block/frame IDs. Accept that exact legacy hash without changing data.
		legacy, ok := args.(saveSectionsInput)
		if !ok || s.policy().SectionRepairs {
			return nil, errors.New("draft replay conflict")
		}
		var normalized saveSectionsInput
		if json.Unmarshal(domain.JSON(legacy), &normalized) != nil {
			return nil, errors.New("draft replay conflict")
		}
		for i := range normalized.Sections {
			for j := range normalized.Sections[i].Blocks {
				b := &normalized.Sections[i].Blocks[j]
				b.ID = normalized.Sections[i].ID + "_b" + strconv.Itoa(j+1)
				for k, id := range b.FrameIDs {
					if strings.HasPrefix(id, "f") {
						n, e := strconv.Atoi(strings.TrimPrefix(id, "f"))
						if e == nil && n > 0 && n <= len(s.cp.CitationFrames) {
							b.FrameIDs[k] = s.cp.CitationFrames[n-1]
						}
					}
				}
			}
		}
		if op.ArgsHash != store.Hash(normalized) {
			return nil, errors.New("draft replay conflict")
		}
	}
	var saved store.Draft
	if err = json.Unmarshal(op.Result, &saved); err != nil {
		return nil, err
	}
	var draft articleDraft
	if err = json.Unmarshal(saved.Data, &draft); err != nil {
		return nil, err
	}
	return textResult(s.draftState(saved.Version, draft)), nil
}
func (s *session) writeDraft(ctx context.Context, args any, version int, draft articleDraft, title string) (*schema.ToolResult, error) {
	return s.step(ctx, title, "compose", nil, func() (any, []string, error) {
		if version > 0 {
			currentVersion, current, err := s.loadDraft(ctx)
			if err != nil {
				return nil, nil, err
			}
			if currentVersion == version && store.Hash(current) == store.Hash(draft) {
				return nil, nil, domain.Err(422, "MODEL_RESULT_INVALID", "本次修订没有改变草稿；请按校验问题修正后提交，不能重复相同修订。")
			}
		}
		saved, err := s.engine.Store.SaveArticleDraft(ctx, s.claim, callID(ctx), args, version, draft)
		if err != nil {
			return nil, nil, err
		}
		// A resumed/replayed write returns the exact accepted revision.
		if err = json.Unmarshal(saved.Data, &draft); err != nil {
			return nil, nil, err
		}
		s.cp.Composing = true
		s.cp.DraftCorrection = false
		s.cp.DraftIssue = nil
		s.cp.LastCandidate = nil
		return s.draftState(saved.Version, draft), nil, nil
	})
}
func draftSummary(version int, draft articleDraft) map[string]any {
	pending := []string{}
	complete := []string{}
	for _, p := range draft.Plan {
		if _, ok := draft.Sections[p.ID]; ok {
			complete = append(complete, p.ID)
		} else {
			pending = append(pending, p.ID)
		}
	}
	next := "Use save_article_sections with latest version for pending chapters; keep accepted chapters unchanged."
	if version == 0 {
		next = "No article plan or chapter body is saved. Correct begin_article and save a full-video plan before writing chapters."
	}
	if len(pending) == 0 && len(draft.Plan) > 0 {
		next = "All planned chapters are saved. Call submit_article_draft NOW with this version. Do not rewrite chapters or limitations before receiving a validation issue."
	}
	out := map[string]any{"version": version, "title": draft.Title, "plan": draft.Plan, "saved_sections": complete, "saved_section_count": len(complete), "pending_sections": pending, "limitations": draft.Limitations, "next_action": next}
	if draft.MindMap != nil {
		out["mind_map"] = draft.MindMap
	}
	if draft.BatchReceipt != nil {
		if len(pending) == 0 {
			out["next_action"] = "All chapters saved. The server will validate the complete article and publish automatically; no further model call is needed."
		}
		out["accepted_sections"] = draft.BatchReceipt.Accepted
		out["rejected_sections"] = draft.BatchReceipt.Rejected
		if len(draft.BatchReceipt.Rejected) > 0 {
			out["error"] = map[string]any{"code": "chapter_validation", "affected_sections": draft.BatchReceipt.Rejected, "repairable": true}
			out["next_action"] = "Keep accepted_sections unchanged. Correct only rejected_sections and write other pending_sections with this latest version."
		}
	}
	return out
}
func (s *session) draftFeedback(ctx context.Context, message string) (*schema.ToolResult, error) {
	if s.policy().ContentPlan {
		return s.structuredDraftFeedback(ctx, message)
	}
	version, draft, err := s.loadDraft(ctx)
	if err != nil {
		return nil, err
	}
	s.cp.Repeated["draft-error:"+store.Hash(message)]++
	if s.cp.Repeated["draft-error:"+store.Hash(message)] >= 3 {
		return nil, domain.Err(422, "MODEL_RESULT_INVALID", "文章结构连续修正后仍未通过；已保存章节保留，可重新尝试。")
	}
	v := draftSummary(version, draft)
	v["error"] = message
	v["correct_arguments_and_try_again"] = true
	return textResult(v), nil
}

// Canonical citations are constructed from owned source records. The model
// emits stable short IDs and cannot change their snapshot, timestamps or URLs.
func (s *session) citationCatalog(ctx context.Context) (map[string]domain.Citation, error) {
	catalog := map[string]domain.Citation{}
	for i, t := range s.transcript {
		id := "t" + strconv.Itoa(i+1)
		// Platform cue display windows may run slightly beyond the actual media.
		// Cite only their real intersection; retain the complete original segment,
		// text, ID, ordinal and raw times in the immutable source snapshot.
		start, end := max(int64(0), t.StartMS), min(s.snapshot.DurationMS, t.EndMS)
		if start >= end {
			continue
		}
		catalog[id] = domain.Citation{ID: id, Kind: "video_text", Label: "视频原文", SnapshotID: s.snapshot.ID, Range: &domain.Range{StartMS: start, EndMS: end}, SegmentIDs: []string{t.ID}, TextOrigin: t.Origin}
	}
	observations, err := s.observations(ctx)
	if err != nil {
		return nil, err
	}
	observed := map[string]bool{}
	for _, o := range observations {
		for _, id := range o.FrameIDs {
			observed[id] = true
		}
	}
	for i, id := range s.cp.CitationFrames {
		if !observed[id] {
			continue
		}
		asset, err := s.engine.Store.InternalFrame(ctx, s.claim, id)
		if err != nil {
			return nil, err
		}
		if asset.TimeMS == nil {
			return nil, errors.New("frame time unavailable")
		}
		alias := "f" + strconv.Itoa(i+1)
		catalog[alias] = domain.Citation{ID: alias, Kind: "video_frame", Label: "视频画面", SnapshotID: s.snapshot.ID, FrameID: id, TimeMS: asset.TimeMS}
	}
	return catalog, nil
}
func assembleDraftBatch(draft articleDraft, sections []domain.Section, catalog map[string]domain.Citation) (articleDraft, error) {
	// Copy: a rejected batch must not mutate accepted chapters in memory.
	// Also copy incoming nested blocks: canonical IDs must not mutate the exact
	// tool arguments hashed for idempotent replay.
	var copiedSections []domain.Section
	if err := json.Unmarshal(domain.JSON(sections), &copiedSections); err != nil {
		return draft, err
	}
	sections = copiedSections
	var next articleDraft
	if err := json.Unmarshal(domain.JSON(draft), &next); err != nil {
		return next, err
	}
	if next.Sections == nil {
		next.Sections = map[string]domain.Section{}
	}
	if next.Citations == nil {
		next.Citations = map[string]domain.Citation{}
	}
	planned := map[string]sectionPlan{}
	for _, p := range next.Plan {
		planned[p.ID] = p
	}
	seen := map[string]bool{}
	for _, section := range sections {
		plan, ok := planned[section.ID]
		if !ok || seen[section.ID] || len(section.Blocks) == 0 {
			return draft, fmt.Errorf("未知/重复/空章节 %s；请使用计划中的section id", section.ID)
		}
		seen[section.ID] = true
		section.Title = plan.Title
		for i := range section.Blocks {
			block := &section.Blocks[i]
			block.ID = section.ID + "_b" + strconv.Itoa(i+1)
			for j, id := range block.FrameIDs {
				if c, ok := catalog[id]; ok && c.Kind == "video_frame" {
					block.FrameIDs[j] = c.FrameID
				}
			}
			ids := blockCitationIDs(*block)
			for _, id := range ids {
				c, ok := catalog[id]
				if !ok {
					return draft, fmt.Errorf("章节%s引用%s不存在；只能使用citation_catalog提供的tN/fN", section.ID, id)
				}
				next.Citations[id] = c
			}
		}
		next.Sections[section.ID] = section
	}
	// Discard no-longer-used citations after replacing a chapter.
	used := map[string]bool{}
	for _, sec := range next.Sections {
		for _, b := range sec.Blocks {
			for _, id := range blockCitationIDs(b) {
				used[id] = true
			}
		}
	}
	for _, l := range next.Limitations {
		for _, id := range l.CitationIDs {
			used[id] = true
		}
	}
	for id := range next.Citations {
		if !used[id] {
			delete(next.Citations, id)
		}
	}
	return next, nil
}
func blockCitationIDs(b domain.Block) []string {
	ids := append([]string{}, b.CitationIDs...)
	add := func(content []domain.Inline) {
		for _, p := range content {
			if p.Type == "citation" && !slices.Contains(ids, p.CitationID) {
				ids = append(ids, p.CitationID)
			}
		}
	}
	add(b.Content)
	for _, item := range b.Items {
		add(item.Content)
	}
	for _, row := range b.Rows {
		ids = append(ids, row.CitationIDs...)
	}
	return ids
}
func (s *session) assembleArticle(draft articleDraft) (domain.Article, error) {
	a := domain.Article{SchemaVersion: 1, SnapshotID: s.snapshot.ID, Source: s.snapshot.Source, Title: draft.Title, Sections: []domain.Section{}, Citations: draft.Citations, Limitations: draft.Limitations, Quality: "standard"}
	for _, p := range draft.Plan {
		sec, ok := draft.Sections[p.ID]
		if !ok {
			return a, domain.Err(422, "MODEL_RESULT_INVALID", "还有章节未保存："+p.ID)
		}
		a.Sections = append(a.Sections, sec)
	}
	if len(a.Sections) == 0 {
		return a, domain.Err(422, "MODEL_RESULT_INVALID", "文章尚未保存章节。")
	}
	if a.Limitations == nil {
		a.Limitations = []domain.Limitation{}
	}
	if len(a.Limitations) > 0 {
		a.Quality = "with_limitations"
	}
	if draft.MindMap != nil {
		a.MindMap = draft.MindMap
		return a, nil
	}
	nodes := []domain.MapNode{{ID: "root", Label: a.Title}}
	for _, sec := range a.Sections {
		nodes = append(nodes, domain.MapNode{ID: "section_" + sec.ID, ParentID: domain.Ptr("root"), Label: sec.Title, SectionID: &sec.ID})
	}
	a.MindMap = &domain.MindMap{RootID: "root", Nodes: nodes}
	return a, nil
}

const draftInstructions = `
本次文章使用分批草稿协议，禁止输出整篇巨大JSON。调查充分后先 begin_article 保存标题、覆盖全片的连续章节计划和限制说明（章节 start_ms/end_ms 从0到实际duration_ms）。每章的evidence_ids只选1–24个最能支持要点的代表性来源，不要罗列该时间范围内的全部引用；收到包含实际章节号和数量的校验反馈时，逐一按反馈精简。然后每轮一个 save_article_sections 调用，保存1–3章，复制最新version，section.id来自计划；最后 submit_article_draft 只提交最新version。不要在同轮并发修改同一个version。
正文、配图使用citation_catalog中的tN/fN短引用。text/citation inline与其他block格式不变；figure.frame_ids也可填fN。服务器负责展开snapshot/range/segment/frame元数据、分配块ID和组装导图，不要重复写citations字典。引用多个连续字幕时可在正文添加多个tN引用。没有目录中的引用不能编造。
已有章节不因下一次格式错误丢失。错误时只重写相关批次，不要重建文章、不重复全部正文。需要更新限制说明用update_article_limitations。已保存章节、待写章节、当前版本与引用目录在每轮状态中提供。主要内容足够支持后及时成文，不为无关细节继续看图。`

func isCandidateTool(name string) bool {
	return name == "submit_article" || name == "submit_answer" || name == "submit_article_draft" || name == "begin_article" || name == "save_article_sections" || name == "update_article_limitations"
}
func candidateIssue(err error) map[string]any {
	issue := map[string]any{"code": "candidate_schema", "detail": err.Error(), "repairable": true}
	var p *domain.Problem
	if errors.As(err, &p) {
		if original, ok := p.Meta["issue"].(map[string]any); ok && original != nil {
			return original
		}
		if p.Code == "EVIDENCE_INSUFFICIENT" {
			issue["code"] = "evidence_missing"
		}
		for _, key := range []string{"code", "path", "block_id", "citation_id", "expected", "actual", "issues", "issue_count", "issues_truncated", "affected_sections"} {
			if v, ok := p.Meta[key]; ok {
				issue[key] = v
			}
		}
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		issues := []map[string]any{}
		total := 0
		for _, child := range joined.Unwrap() {
			next := candidateIssue(child)
			if children, ok := next["issues"].([]map[string]any); ok {
				count, _ := next["issue_count"].(int)
				total += max(count, len(children))
				issues = append(issues, children...)
			} else {
				total++
				issues = append(issues, next)
			}
		}
		issue["issues"] = issues[:min(len(issues), maxPlanIssues)]
		issue["issue_count"] = total
		issue["issues_truncated"] = total > min(len(issues), maxPlanIssues)
	}
	var syntax *json.SyntaxError
	if errors.As(err, &syntax) {
		issue["code"] = "json_syntax"
		issue["byte_offset"] = syntax.Offset
		issue["action"] = "Rewrite only this small batch as exactly one complete JSON object. Do not truncate trailing chapters or repeat accepted batches."
	}
	var typed *json.UnmarshalTypeError
	if errors.As(err, &typed) {
		issue["path"] = typed.Field
		issue["expected"] = typed.Type.String()
		issue["actual"] = typed.Value
		issue["byte_offset"] = typed.Offset
	}
	return issue
}
func (s *session) finalizationError() error {
	if s.cp.FinalizationFailure != nil {
		if s.cp.FinalizationFailure.Meta["draft_version"] != nil {
			return s.cp.FinalizationFailure
		}
		return domain.Err(422, s.cp.FinalizationFailure.Code, "本次提交仍未通过校验，已取得的来源和草稿保留。"+s.cp.FinalizationFailure.Detail)
	}
	return domain.Err(422, "MODEL_RESULT_NOT_SUBMITTED", "模型结束时未正式提交结果；已取得的来源和草稿保留，可以重新尝试。")
}
func (s *session) resumeDraft(ctx context.Context) error {
	msgs := []*schema.Message{schema.SystemMessage(instructions + draftInstructions), schema.UserMessage(string(domain.JSON(map[string]any{"snapshot_id": s.snapshot.ID, "source": s.snapshot.Source, "duration_ms": s.snapshot.DurationMS, "transcript": s.transcript}))), schema.UserMessage("这是用户明确重新尝试的草稿恢复。使用已保存的真实观察和章节继续，不必重新调查已经支持的事实。未验证内容仍须正式校验提交。")}
	// Include purpose again; it is frozen to the original request/model.
	spec, err := s.engine.Store.GenerationForRun(ctx, s.claim.Run.ID)
	if err != nil {
		return err
	}
	msgs[2].Content += "\n关注点：" + spec.Purpose
	if s.policy().Enabled && spec.DetailPolicy != nil {
		if spec.DetailPolicy.ContentPlan {
			s.cp.HarnessRevision = max(3, s.cp.HarnessRevision)
		}
		msgs[0].Content += harnessInstructions + "\n" + spec.DetailPolicy.Instruction
		if spec.DetailPolicy.ContentPlan {
			msgs[0].Content += contentPlanInstructions
			msgs[0].Content += planRepairInstructions
			if s.policy().Paced {
				msgs[0].Content += pacedHarnessInstructions
			}
			if s.policy().ObservationBudget {
				msgs[0].Content += observationBudgetInstructions
			}
			if s.policy().SectionRepairs {
				msgs[0].Content += efficientDraftInstructions
			}
		}
		msgs[2].Content += "\n解读详细程度：" + spec.DetailLevel
	}
	if s.authoring() && spec.DetailPolicy != nil {
		msgs[0].Content = authoringInstructions(spec.DetailPolicy.Instruction)
	}
	if ids := s.unobserved(ctx); len(ids) > 0 {
		parts, err := s.imageInput(ctx, ids, "resumed-unobserved")
		if err != nil {
			return err
		}
		s.cp.Inspections["resumed-unobserved"] = ids
		msgs = append(msgs, &schema.Message{Role: schema.User, UserInputMultiContent: parts})
	}
	s.cp.DraftResume = false
	return s.persist(ctx, msgs)
}
func projectDraftHistory(messages []*schema.Message) []*schema.Message {
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
			if c.Function.Name != "begin_article" && c.Function.Name != "save_article_sections" && c.Function.Name != "update_article_limitations" {
				drop = false
			}
			ids[c.ID] = true
		}
		j := i + 1
		for j < len(messages) && messages[j].Role == schema.Tool {
			body := messages[j].Content
			for _, p := range messages[j].UserInputMultiContent {
				body += p.Text
			}
			var result struct {
				Version int    `json:"version"`
				Error   string `json:"error"`
			}
			if !ids[messages[j].ToolCallID] || json.Unmarshal([]byte(body), &result) != nil || result.Version == 0 || result.Error != "" {
				drop = false
			}
			delete(ids, messages[j].ToolCallID)
			j++
		}
		if drop && len(ids) == 0 {
			i = j - 1
			continue
		}
		out = append(out, m)
	}
	return out
}
func catalogHints(catalog map[string]domain.Citation) map[string]any {
	out := map[string]any{}
	for id, c := range catalog {
		if c.Kind == "video_text" {
			out[id] = map[string]any{"segment_ids": c.SegmentIDs, "range": c.Range}
		} else {
			out[id] = map[string]any{"frame_id": c.FrameID, "time_ms": c.TimeMS}
		}
	}
	return out
}

func rawToolArguments(a *schema.ToolArgument) string {
	if a == nil {
		return ""
	}
	return a.Text
}

// Limitation references are evidence too: they may cite a frame that does not
// appear in body blocks. Resolve every used alias before validation/publication.
func resolveDraftCitations(draft *articleDraft, catalog map[string]domain.Citation) error {
	used := map[string]bool{}
	for _, sec := range draft.Sections {
		for _, b := range sec.Blocks {
			for _, id := range blockCitationIDs(b) {
				used[id] = true
			}
		}
	}
	for _, l := range draft.Limitations {
		for _, id := range l.CitationIDs {
			used[id] = true
		}
	}
	citations := map[string]domain.Citation{}
	for id := range used {
		c, ok := catalog[id]
		if !ok {
			return fmt.Errorf("引用%s不存在：正文和limitations.citation_ids只能使用citation_catalog中的tN/fN短编号，不能填写frame_id或猜测来源", id)
		}
		citations[id] = c
	}
	draft.Citations = citations
	return nil
}
