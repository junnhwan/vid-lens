package investigator

import (
	"encoding/json"
	"fmt"

	"vidlens/internal/domain"
)

func (s *session) deliveryConceptMaps() bool {
	return s.cp.ArticleLoop != nil && s.cp.ArticleLoop.ReviewRevision == domain.ArticleDeliveryV7
}

func decodeDeliveryConceptMapWrite(raw []byte, in *loopSaveInput) error {
	fields := map[string]json.RawMessage{}
	if err := loopUniqueJSON(raw); err != nil {
		return err
	}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	tree := fields["mind_map"]
	delete(fields, "mind_map")
	if err := decodeDeliveryVisualWrite(domain.JSON(fields), in); err != nil {
		return err
	}
	if len(tree) > 0 && string(tree) != "null" {
		var value domain.MindMap
		if err := loopDecode(tree, &value); err != nil {
			in.mindMapParseIssue = "导图格式无效，正文已保留；请用save_sections重新提交mind_map：" + err.Error()
		} else {
			in.MindMap = &value
		}
	}
	return nil
}

func validateDeliveryConceptMap(tree *domain.MindMap, chapters []domain.Section) error {
	sections := map[string]bool{}
	for _, chapter := range chapters {
		sections[chapter.ID] = true
	}
	if err := validateConceptMap(tree, sections); err != nil {
		return err
	}
	linked := map[string]bool{}
	for _, node := range tree.Nodes {
		if node.SectionID != nil {
			linked[*node.SectionID] = true
		}
	}
	for _, chapter := range chapters {
		if !linked[chapter.ID] {
			return invalid(fmt.Sprintf("思维导图缺少章节%s的概念链接；在save_sections中提交覆盖全部章节的mind_map，可用sections:[]保留正文。", chapter.ID))
		}
	}
	return nil
}

// Map errors preserve independently accepted prose. Structural revisions stay
// atomic, including the map, so deleted chapters cannot leave dangling links.
func saveDeliveryConceptMap(article *domain.Article, input *domain.MindMap, receipt *loopReceipt, structural bool) error {
	if input == nil && (!structural || article.MindMap == nil) {
		return nil
	}
	tree := article.MindMap
	if input != nil {
		tree = &domain.MindMap{RootID: input.RootID, Nodes: append([]domain.MapNode{}, input.Nodes...)}
		known := map[string]bool{}
		for _, chapter := range article.Sections {
			known[chapter.ID] = true
		}
		for i, node := range tree.Nodes {
			if node.SectionID == nil || known[*node.SectionID] {
				continue
			}
			if id, ok := receipt.Created[*node.SectionID]; ok {
				tree.Nodes[i].SectionID = domain.Ptr(id)
			}
		}
	}
	if err := validateDeliveryConceptMap(tree, article.Sections); err != nil {
		receipt.Rejected = append(receipt.Rejected, loopRejection{Path: "/mind_map", Reason: err.Error()})
		if structural {
			return err
		}
		return nil
	}
	article.MindMap = tree
	return nil
}

const deliveryConceptMapInstructions = `文章必须同时交付mind_map概念关系树。在最后一批save_sections中一并提交，无需单独规划或额外调用；此前可先保存正文。若完整正文已经保存但缺图，只提交save_sections的expected_version、sections:[]、mind_map和finish:true，不重发正文。
mind_map={root_id,nodes:[{id,parent_id,label,section_id}]}。一个根的parent_id为null；以核心主题展开有来源依据的因果、机制、流程依赖或取舍，通常2–4层，不复制章节目录或逐条照搬正文。最多80节点、4层分支，短标签不超过100字。纯分组section_id为null；每个叶节点链接真实章节，每个保留章节至少有一个概念链接。只整理当前视频支持的关系，不编造关联。
section_id使用current_article中的真实section_id，或同一次save_sections中新建章节的精确key，程序会转换为真实ID；不得编造稳定ID。省略mind_map保留已有导图。后续新增、删除或调整主题时，完成前更新完整概念树；删除被链接章节须在同一次原子保存中提供更新后的导图。finish_article只能提交已保存且导图完整的稿件。`
