package investigator

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"vidlens/internal/domain"
	"vidlens/internal/media"
	"vidlens/internal/store"
)

type workflowChapter struct {
	Title     string   `json:"title"`
	StartMS   int64    `json:"start_ms"`
	KeyPoints []string `json:"key_points"`
}

type workflowInvestigation struct {
	Question string `json:"question"`
	StartMS  int64  `json:"start_ms"`
	EndMS    int64  `json:"end_ms"`
}

type workflowPlan struct {
	Title          string                  `json:"title"`
	Chapters       []workflowChapter       `json:"chapters"`
	FrameNotes     []workflowFrameNote     `json:"frame_notes"`
	Investigations []workflowInvestigation `json:"investigations"`
	Limitations    []domain.Limitation     `json:"limitations"`
}

const workflowPlanInstructions = `通读已交付的全片原文和全部真实概览图片，返回中文标题、按来源顺序的主题章节，以及每张图片的简短可见事实。chapters按实际内容划分，不套固定章节/字数配额；每章start_ms为主题大致起点，第一章为0，严格递增且小于duration_ms。程序负责分配s1、s2等章节编号和连续覆盖区间。key_points保留具体方法、例子、数字、条件与反例，不能只写泛泛主题。
frame_notes必须覆盖全部delivered_pixels的fN，每张记录1–4条短的实际可见事实，模糊内容写uncertain。只有影响主要结论且概览不足时才给investigations，最多2项，明确问题和有界时间范围；无需额外取证则[]。limitations使用允许的code；section_ids暂用[]，citation_ids只能用已交付tN/fN。此轮只做计划和观察，不写正文。`

const workflowWriteInstructions = `一次完成assigned_chapters中全部指定章节，返回sections:[{id,blocks}]，只使用给定章节ID。保持其他章节不变。用详细、可独立理解的中文解释，保留该主题的具体机制、例子、步骤、数字、前提、反例和不确定性，不逐句翻译或重复铺垫。段落每段围绕一个可核对的断言；列表每项、表格每行各有就近citation_ids，不能只给整块一组引用。
正文块使用type=paragraph/quote/list/code/table/figure及origin=video或background_explanation。段落用text和citation_ids；列表用items:[{text,citation_ids}]；table使用columns和rows:[{cells,citation_ids}]；代码用text、language、citation_ids。figure使用frame_ids:["fN"]、caption，必须来自本轮真实交付像素；图注说明可见内容和作用，不用旁白补画面。保留有信息价值的实际配图，不要求每章配图。不要写content/内联对象、块ID、引用字典、版本号或全文重复稿。`

func (s *session) runWorkflowArticle(ctx context.Context) error {
	if err := s.prepareWorkflowFrames(ctx); err != nil {
		return err
	}
	evidence, err := s.workflowEvidence(ctx, 0, s.snapshot.DurationMS, s.cp.OverviewIDs)
	if err != nil {
		return err
	}
	frames, err := s.workflowFrames(ctx, s.cp.OverviewIDs)
	if err != nil {
		return err
	}
	plan, err := workflowStage[workflowPlan](ctx, s, "plan", "plan", "plan_video_article", workflowPlanInstructions, evidence, s.cp.OverviewIDs, func(p *workflowPlan) error {
		if strings.TrimSpace(p.Title) == "" || len(p.Chapters) == 0 || len(p.Chapters) > 40 || len(p.Investigations) > 2 {
			return errors.New("标题/主题章节缺失或计划超限")
		}
		for i, c := range p.Chapters {
			if c.Title == "" || len(c.KeyPoints) == 0 || c.StartMS < 0 || c.StartMS >= s.snapshot.DurationMS || i == 0 && c.StartMS != 0 || i > 0 && c.StartMS <= p.Chapters[i-1].StartMS {
				return fmt.Errorf("第%d章主题、要点或起点无效", i+1)
			}
		}
		for _, investigation := range p.Investigations {
			if strings.TrimSpace(investigation.Question) == "" || investigation.StartMS < 0 || investigation.EndMS <= investigation.StartMS || investigation.EndMS > s.snapshot.DurationMS || investigation.EndMS-investigation.StartMS > 60000 {
				return errors.New("补充取证问题或范围无效")
			}
		}
		return validateWorkflowFrameNotes(p.FrameNotes, frames)
	})
	if err != nil {
		return err
	}
	if err = s.saveWorkflowFrameNotes(ctx, "plan", plan.FrameNotes); err != nil {
		return err
	}
	for i, investigation := range plan.Investigations {
		key := fmt.Sprint("investigate:", i)
		result, err := s.engine.Media.Inspect(ctx, s.claim, "workflow:"+key, investigation.Question, media.Window{StartMS: investigation.StartMS, EndMS: investigation.EndMS, Count: 3, Mode: "detail"}, nil)
		if err != nil {
			return err
		}
		for _, id := range result.FrameIDs {
			if !slices.Contains(s.cp.CitationFrames, id) {
				s.cp.CitationFrames = append(s.cp.CitationFrames, id)
			}
		}
		if err = s.persist(ctx, s.cp.Messages); err != nil {
			return err
		}
		payload, err := s.workflowEvidence(ctx, investigation.StartMS, investigation.EndMS, result.FrameIDs)
		if err != nil {
			return err
		}
		payload["question"] = investigation.Question
		frames, err := s.workflowFrames(ctx, result.FrameIDs)
		if err != nil {
			return err
		}
		notes, err := workflowStage[struct {
			FrameNotes []workflowFrameNote `json:"frame_notes"`
		}](ctx, s, key, "evidence", "record_video_evidence", "仅依据实际像素回答本次取证问题。每张delivered_pixels记录frame_notes；不能确认的内容写uncertain，不把旁白当画面。", payload, result.FrameIDs, func(n *struct {
			FrameNotes []workflowFrameNote `json:"frame_notes"`
		}) error {
			return validateWorkflowFrameNotes(n.FrameNotes, frames)
		})
		if err != nil {
			return err
		}
		if err = s.saveWorkflowFrameNotes(ctx, key, notes.FrameNotes); err != nil {
			return err
		}
	}
	catalog, err := s.citationCatalog(ctx)
	if err != nil {
		return err
	}
	draft := articleDraft{Title: plan.Title, Sections: map[string]domain.Section{}, Citations: map[string]domain.Citation{}, Limitations: plan.Limitations}
	for i, chapter := range plan.Chapters {
		end := s.snapshot.DurationMS
		if i+1 < len(plan.Chapters) {
			end = plan.Chapters[i+1].StartMS
		}
		draft.Plan = append(draft.Plan, sectionPlan{ID: fmt.Sprint("s", i+1), Title: chapter.Title, StartMS: chapter.StartMS, EndMS: end, KeyPoints: chapter.KeyPoints})
	}
	version, saved, err := s.loadDraft(ctx)
	if err != nil {
		return err
	}
	if version > 0 {
		if saved.Title != draft.Title || store.Hash(saved.Plan) != store.Hash(draft.Plan) {
			return domain.Err(422, "WORKFLOW_RESULT_INVALID", "已保存草稿与冻结计划不兼容；原成果保留，未覆盖旧稿继续。")
		}
		draft = saved
	}
	s.cp.DraftResume = false
	if err = s.persist(ctx, s.cp.Messages); err != nil {
		return err
	}
	if err = s.workflowDraft(ctx, "plan", draft); err != nil {
		return err
	}
	// Batches are assigned by the server once. Their output budget covers body
	// writing only; the model never spends a call scheduling the next chapter.
	batchSize := max(1, (len(draft.Plan)+4)/5)
	for offset := 0; offset < len(draft.Plan); offset += batchSize {
		chapters := []sectionPlan{}
		for _, chapter := range draft.Plan[offset:min(offset+batchSize, len(draft.Plan))] {
			if _, saved := draft.Sections[chapter.ID]; !saved {
				chapters = append(chapters, chapter)
			}
		}
		if len(chapters) == 0 {
			continue
		}
		frameIDs, err := s.workflowChapterFrames(ctx, chapters)
		if err != nil {
			return err
		}
		payload, err := s.workflowEvidence(ctx, 0, s.snapshot.DurationMS, frameIDs)
		if err != nil {
			return err
		}
		payload["outline"], payload["assigned_chapters"] = draft.Plan, chapters
		payload["observations"], err = s.observations(ctx)
		if err != nil {
			return err
		}
		allowed := workflowDeliveredCatalog(catalog, frameIDs)
		key := fmt.Sprint("write:", offset)
		body, err := workflowStage[workflowBody](ctx, s, key, "write", "write_video_chapters", workflowWriteInstructions, payload, frameIDs, func(b *workflowBody) error {
			_, e := workflowCanonicalBody(*b, chapters, allowed)
			return e
		})
		if err != nil {
			return err
		}
		sections, err := workflowCanonicalBody(body, chapters, allowed)
		if err != nil {
			return err
		}
		for _, section := range sections {
			draft.Sections[section.ID] = section
		}
		workflowDraftCitations(&draft, catalog)
		if err = s.workflowDraft(ctx, key, draft); err != nil {
			return err
		}
	}
	article, err := s.assembleArticle(draft)
	if err != nil {
		return err
	}
	mechanical := s.validateArticle(ctx, article)
	issues := []map[string]any{}
	alreadyRevised := false
	if old, e := s.engine.Store.Operation(ctx, s.claim.Run.ID, "article-draft:workflow:revision"); e == nil && old.Status == "completed" {
		alreadyRevised = true
	} else if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return e
	}
	if mechanical == nil {
		phase := "review"
		if alreadyRevised {
			phase = "recheck"
		}
		issues, err = s.reviewWorkflowArticle(ctx, article, draft.Plan, phase)
		if err != nil {
			return err
		}
	} else {
		issues = append(issues, map[string]any{"path": "/sections", "reason": mechanical.Error()})
	}
	if len(issues) > 0 {
		if alreadyRevised {
			return domain.Err(422, "WORKFLOW_REVIEW_UNRESOLVED", "一次定点修订后仍存在未解决问题；原稿和审核保留，本轮停止。")
		}
		draft, err = s.reviseWorkflowArticle(ctx, draft, catalog, issues)
		if err != nil {
			return err
		}
		article, err = s.assembleArticle(draft)
		if err != nil {
			return err
		}
		if err = s.validateArticle(ctx, article); err != nil {
			return err
		}
		issues, err = s.reviewWorkflowArticle(ctx, article, draft.Plan, "recheck")
		if err != nil {
			return err
		}
		if len(issues) > 0 {
			return domain.Err(422, "WORKFLOW_REVIEW_UNRESOLVED", "一次定点修订后仍存在未解决的证据问题；草稿与逐项审核已保留，本轮停止。")
		}
	}
	if err = s.workflowSave(ctx, "candidate", article); err != nil {
		return err
	}
	if err = s.workflowProgress(ctx, "publish"); err != nil {
		return err
	}
	return s.publish(ctx, domain.JSON(article))
}

func workflowCanonicalBody(body workflowBody, chapters []sectionPlan, catalog map[string]domain.Citation) ([]domain.Section, error) {
	if len(body.Sections) != len(chapters) {
		return nil, errors.New("必须返回本批全部且仅指定的章节")
	}
	sections := []domain.Section{}
	for i, chapter := range chapters {
		section, err := workflowCanonicalSection(body.Sections[i], chapter, catalog)
		if err != nil {
			return nil, err
		}
		sections = append(sections, section)
	}
	return sections, nil
}

func workflowDeliveredCatalog(catalog map[string]domain.Citation, frames []string) map[string]domain.Citation {
	out := map[string]domain.Citation{}
	for alias, citation := range catalog {
		if citation.Kind != "video_frame" || slices.Contains(frames, citation.FrameID) {
			out[alias] = citation
		}
	}
	return out
}

func workflowDraftCitations(draft *articleDraft, catalog map[string]domain.Citation) {
	draft.Citations = map[string]domain.Citation{}
	for _, section := range draft.Sections {
		for _, block := range section.Blocks {
			for _, alias := range sourceAliases(block) {
				if c, ok := catalog[alias]; ok {
					draft.Citations[alias] = c
				}
			}
		}
	}
	for _, limitation := range draft.Limitations {
		for _, alias := range limitation.CitationIDs {
			if c, ok := catalog[alias]; ok {
				draft.Citations[alias] = c
			}
		}
	}
}

func (s *session) workflowDraft(ctx context.Context, key string, draft articleDraft) error {
	op, err := s.engine.Store.Operation(ctx, s.claim.Run.ID, "article-draft:workflow:"+key)
	if err == nil && op.Status == "completed" {
		return nil
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	version, _, err := s.loadDraft(ctx)
	if err != nil {
		return err
	}
	_, err = s.engine.Store.SaveArticleDraft(ctx, s.claim, "workflow:"+key, map[string]any{"key": key, "hash": store.Hash(draft)}, version, draft)
	if err != nil {
		return err
	}
	step := domain.Step{ID: "workflow_draft_" + s.claim.Run.ID + "_" + strings.ReplaceAll(key, ":", "_"), Title: fmt.Sprintf("已保存 %d / %d 章", len(draft.Sections), len(draft.Plan)), Phase: "compose", Status: "completed", FrameIDs: []string{}}
	_, err = s.engine.Store.Step(ctx, s.claim, step)
	if err == nil {
		err = s.workflowProgress(ctx, "write")
	}
	return err
}

func (s *session) workflowChapterFrames(ctx context.Context, chapters []sectionPlan) ([]string, error) {
	frames, err := s.workflowFrames(ctx, nil)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for _, frame := range frames {
		for _, chapter := range chapters {
			if frame.TimeMS >= chapter.StartMS && frame.TimeMS < chapter.EndMS {
				ids = append(ids, frame.ID)
				break
			}
		}
	}
	if len(ids) > 12 {
		return nil, domain.Err(422, "WORKFLOW_CAPACITY_EXCEEDED", "本批必要图片超过一次可交付容量；未丢弃画面后继续。")
	}
	return ids, nil
}

func (s *session) reviseWorkflowArticle(ctx context.Context, draft articleDraft, catalog map[string]domain.Citation, issues []map[string]any) (articleDraft, error) {
	affected := []sectionPlan{}
	all := false
	for _, issue := range issues {
		path, _ := issue["path"].(string)
		if path == "/sections" || path == "/global" {
			all = true
		}
	}
	for i, chapter := range draft.Plan {
		for _, issue := range issues {
			path, _ := issue["path"].(string)
			if all || strings.HasPrefix(path+"/", fmt.Sprintf("/sections/%d/", i)) {
				affected = append(affected, chapter)
				break
			}
		}
	}
	if len(affected) == 0 {
		return draft, domain.Err(422, "WORKFLOW_REVIEW_UNRESOLVED", "审核问题不能定位到可修订章节，已保留原稿和问题。")
	}
	frames, err := s.workflowChapterFrames(ctx, affected)
	if err != nil {
		return draft, err
	}
	payload, err := s.workflowEvidence(ctx, 0, s.snapshot.DurationMS, frames)
	if err != nil {
		return draft, err
	}
	payload["outline"], payload["assigned_chapters"], payload["issues"], payload["saved_sections"] = draft.Plan, affected, issues, draft.Sections
	allowed := workflowDeliveredCatalog(catalog, frames)
	body, err := workflowStage[workflowBody](ctx, s, "revision", "revision", "revise_video_chapters", workflowWriteInstructions+"\n这是本轮唯一一次语义修订。先回读issues对应原文和实际像素，修正全部受影响字段，保留仍有依据的细节。不确定内容明确限定；不能删去整个主题来回避问题。", payload, frames, func(b *workflowBody) error {
		_, e := workflowCanonicalBody(*b, affected, allowed)
		return e
	})
	if err != nil {
		return draft, err
	}
	sections, err := workflowCanonicalBody(body, affected, allowed)
	if err != nil {
		return draft, err
	}
	for _, section := range sections {
		draft.Sections[section.ID] = section
	}
	workflowDraftCitations(&draft, catalog)
	err = s.workflowDraft(ctx, "revision", draft)
	return draft, err
}
