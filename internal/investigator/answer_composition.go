package investigator

import (
	"strings"
	"unicode/utf8"
	"vidlens/internal/domain"
)

type composedAnswerInput struct {
	Kind        string                     `json:"kind" jsonschema:"enum=grounded,enum=insufficient_evidence,enum=out_of_scope"`
	Summary     string                     `json:"summary" jsonschema:"description=The direct answer in one short sentence, usually 40-90 Chinese characters. Do not repeat this conclusion in a body introduction or closing summary."`
	Blocks      []domain.Block             `json:"blocks" jsonschema:"description=Focused supporting explanation, not a paraphrase of summary. Prefer short paragraphs or a few concise list items; use tables only for useful comparisons. Ordinary questions need about 150-450 Chinese characters, expand only for requested depth. Cite nearby tN/fN IDs."`
	Parts       []domain.AnswerPart        `json:"parts,omitempty" jsonschema:"description=Optional 1-6 natural subtopics for a complex answer. Each title describes the actual issue, not generic Introduction/Conclusion/Reason 1. block_ids refer to existing blocks, each block in at most one part. Omit for a simple answer; do not invent sections to fill a template."`
	Citations   map[string]domain.Citation `json:"citations,omitempty" jsonschema:"description=Only external_reference/article_passage entries if used. Omit for tN/fN video citations; the server reconstructs them."`
	Limitations []domain.Limitation        `json:"limitations" jsonschema:"description=Only material uncertainties relevant to this question, briefly stated. Do not repeat an identical caveat paragraph in blocks."`
}

func validateAnswerParts(a *domain.Answer) error {
	if len(a.Parts) > 6 {
		return invalid("回答最多6个自然分区，简单问题可省略parts。")
	}
	blocks := map[string]bool{}
	for _, b := range a.Blocks {
		blocks[b.ID] = true
	}
	used := map[string]bool{}
	for _, part := range a.Parts {
		if strings.TrimSpace(part.Title) == "" || utf8.RuneCountInString(part.Title) > 60 || len(part.BlockIDs) == 0 {
			return invalid("回答分区需要简短title与已有block_ids。")
		}
		for _, id := range part.BlockIDs {
			if !blocks[id] || used[id] {
				return invalid("回答分区的block_ids不存在或重复：" + id)
			}
			used[id] = true
		}
	}
	return nil
}

const composedAnswerInstructions = `
直接使用submit_answer完成回答，不先输出一遍普通文本再调用提交工具；需要补证据时可以先调用取证工具。服务器要求正式工具与引用校验，通过后才展示。
表达以当前问题为中心，避免固定“结论一/二/三、总结、最后再总结”的套话。summary是唯一的直接结论，通常一句40–90字；blocks只解释为什么、区别、步骤或例子，不用另一个开场段重述summary，也不在末尾重复概括。普通问题的解释通常150–450字，几个复杂问题可适当展开；用户明确要求详细、证明、代码或推导时保留必要深度，不把建议字数当截断上限。
一个paragraph一个意思，清单每项优先1–3个短句，只在有用时做对比表。复杂回答可用parts为现有block_ids命名自然分区，标题紧扣本次问题，如“隔离解决的是执行边界”“快照解决的是重建成本”，不是所有回答套同一组栏目；简单回答省略parts。不要同一事实在摘要、清单、表格各重复一遍。limitations只保留影响当前结论的不确定性一次。所有内容仍用结构化blocks，text内不写Markdown标题或列表。`
