package investigator

import (
	"unicode/utf8"
	"vidlens/internal/domain"
)

type conceptPlanInput struct {
	Title       string              `json:"title"`
	Sections    []sectionPlan       `json:"sections" jsonschema:"description=Full-video topic plan. Each chapter has distinct key_points and representative evidence_ids. No body text."`
	MindMap     *domain.MindMap     `json:"mind_map" jsonschema:"description=Concept tree: root -> 3-6 meaningful themes -> relationships, causes, mechanisms or tradeoffs. Usually 2-4 levels, at most 80 nodes. Labels are short supported ideas, not copied chapter headings. Group nodes may have null section_id; leaves link to an existing planned section_id. It can combine concepts across chapters. Do not repeat every key point or invent claims."`
	Limitations []domain.Limitation `json:"limitations"`
}

func validateConceptMap(m *domain.MindMap, sections map[string]bool) error {
	if m == nil {
		return invalid("请提供mind_map概念树：主题、关系与要点分层，叶节点链接计划章节。")
	}
	if err := validateMap(m, sections); err != nil {
		return err
	}
	if len(m.Nodes) > 80 {
		return invalid("概念树最多80个节点，请保留核心关系。")
	}
	byID := map[string]domain.MapNode{}
	parents := map[string]bool{}
	for _, n := range m.Nodes {
		byID[n.ID] = n
		if utf8.RuneCountInString(n.Label) > 100 {
			return invalid("概念节点标签过长，请使用短语。")
		}
		if n.ParentID != nil {
			parents[*n.ParentID] = true
		}
	}
	deep := false
	for _, n := range m.Nodes {
		if !parents[n.ID] && n.ID != m.RootID && n.SectionID == nil {
			return invalid("概念树叶节点需要section_id以定位原文。")
		}
		depth := 0
		for n.ParentID != nil {
			depth++
			n = byID[*n.ParentID]
		}
		if depth >= 2 {
			deep = true
		}
		if depth > 4 {
			return invalid("概念树最多4层分支，请合并过细的节点。")
		}
	}
	if len(sections) > 1 && !deep {
		return invalid("导图需要主题下的关系或要点分支，不能只是根节点下罗列章节。")
	}
	return nil
}

const conceptMapInstructions = `
本轮begin_article同时保存mind_map概念关系树。导图与目录承担不同任务：目录由真实文章章节生成；导图按概念组织，可跨章节归类。以3–6个核心主题展开因果、机制、流程依赖或取舍，通常有2–4层，不复制章节标题或逐条照搬全部key_points。每个叶节点section_id指向最合适的计划章节；纯分组节点可为null。最多80节点，标签用短语。只整理视频已支持的关系，不编造关联。导图随计划一次持久保存，无需额外模型调用；后续批次保留它。
提纲阶段可用完整composition输出预算，但应选择代表性证据和精炼概念，不重复正文或引用元数据。`
