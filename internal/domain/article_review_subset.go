package domain

import (
	"fmt"
	"slices"
	"strings"
)

// 仅完整核验过的来源/正文/像素超集可以限制为局部结论；关系核对不能变成事实批准。
func ArticleReviewScopeContains(parent, child ArticleReviewScope) bool {
	if child.Global || parent.Global && len(parent.SourceIDs) == 0 {
		return false
	}
	for _, pair := range [][2][]string{{parent.SectionIDs, child.SectionIDs}, {parent.SourceIDs, child.SourceIDs}, {parent.FrameIDs, child.FrameIDs}} {
		for _, id := range pair[1] {
			if !slices.Contains(pair[0], id) {
				return false
			}
		}
	}
	delivered := append(slices.Clone(parent.SourceIDs), parent.ContextSourceIDs...)
	for _, id := range child.ContextSourceIDs {
		if !slices.Contains(delivered, id) {
			return false
		}
	}
	return true
}

// 无法精确定位或影响全篇的阻断按全部受影响处理，不能靠猜位置复用局部结论。
func articleIssueOwners(a Article, issue ArticleReviewIssue) (owners []string, global bool) {
	combined := issue.Location + " " + issue.Problem + " " + issue.Correction
	for _, word := range []string{"全文", "全篇", "全局", "整篇", "所有章节", "entire article", "all chapters", "throughout"} {
		if strings.Contains(combined, word) {
			return nil, true
		}
	}
	parts := strings.FieldsFunc(issue.Location, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-')
	})
	for _, section := range a.Sections {
		found := slices.Contains(parts, section.ID)
		for _, block := range section.Blocks {
			found = found || slices.Contains(parts, block.ID)
			for _, item := range block.Items {
				found = found || slices.Contains(parts, item.ID)
			}
		}
		if found {
			owners = append(owners, section.ID)
		}
	}
	return owners, len(owners) == 0
}

func ArticleReviewSubset(report ArticleReviewReport, a Article, scope ArticleReviewScope, text []Segment) (ArticleReviewReport, bool) {
	out := ArticleReviewReport{Complete: true, Unassessed: []string{}, Topics: []ArticleTopic{}, Issues: []ArticleReviewIssue{}}
	if !report.Complete || len(report.Unassessed) > 0 {
		return out, false
	}
	for _, issue := range report.Issues {
		owners, global := articleIssueOwners(a, issue)
		touches := global || slices.ContainsFunc(owners, func(id string) bool { return slices.Contains(scope.SectionIDs, id) })
		if issue.Severity == "blocking" && touches {
			return out, false
		}
	}
	delivered := map[string]bool{}
	for i, original := range text {
		if slices.Contains(scope.SourceIDs, original.ID) || slices.Contains(scope.ContextSourceIDs, original.ID) {
			delivered[original.ID] = true
			delivered[fmt.Sprint("t", i+1)] = true
		}
	}
	for _, topic := range report.Topics {
		projected := topic
		projected.SectionIDs = nil
		projected.SourceIDs = nil
		for _, id := range topic.SectionIDs {
			if slices.Contains(scope.SectionIDs, id) {
				projected.SectionIDs = append(projected.SectionIDs, id)
			}
		}
		if len(projected.SectionIDs) == 0 {
			continue
		}
		for _, id := range topic.SourceIDs {
			if delivered[id] {
				projected.SourceIDs = append(projected.SourceIDs, id)
			}
		}
		if len(scope.SourceIDs) > 0 && len(projected.SourceIDs) == 0 {
			continue
		}
		out.Topics = append(out.Topics, projected)
	}
	for _, issue := range report.Issues {
		if issue.Severity != "advisory" {
			continue
		}
		owners, global := articleIssueOwners(a, issue)
		if global || !slices.ContainsFunc(owners, func(id string) bool { return slices.Contains(scope.SectionIDs, id) }) {
			continue
		}
		allowed := true
		for _, id := range issue.EvidenceIDs {
			allowed = allowed && (delivered[id] || slices.Contains(scope.FrameIDs, id))
		}
		if allowed {
			out.Issues = append(out.Issues, issue)
		}
	}
	return out, len(out.Topics) > 0
}

func ArticleReviewSubsetObservations(observations []ArticleVisualObservation, scope ArticleReviewScope) []ArticleVisualObservation {
	out := []ArticleVisualObservation{}
	for _, observation := range observations {
		if slices.Contains(scope.FrameIDs, observation.FrameID) {
			out = append(out, observation)
		}
	}
	return out
}
