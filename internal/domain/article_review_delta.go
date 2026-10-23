package domain

import (
	"slices"
	"strings"
)

// 差异由程序从两个不可变候选计算，不接受模型自报未变或已修复。
type ArticleReviewDelta struct {
	ReviewKey         string   `json:"review_key"`
	BaseReviewHash    string   `json:"base_review_hash"`
	ChangedSectionIDs []string `json:"changed_section_ids"`
	AffectedBlockIDs  []string `json:"affected_block_ids"`
	RemovedBlockIDs   []string `json:"removed_block_ids"`
	HeaderChanged     bool     `json:"header_changed"`
}

func ArticleReviewCoversWhole(scope ArticleReviewScope, a Article, text []Segment, frames []string) bool {
	if !scope.Global || len(scope.SectionIDs) != len(a.Sections) || len(scope.SourceIDs) != len(text) || len(scope.FrameIDs) != len(frames) {
		return false
	}
	for _, section := range a.Sections {
		if !slices.Contains(scope.SectionIDs, section.ID) {
			return false
		}
	}
	for _, segment := range text {
		if !slices.Contains(scope.SourceIDs, segment.ID) {
			return false
		}
	}
	for _, id := range frames {
		if !slices.Contains(scope.FrameIDs, id) {
			return false
		}
	}
	return true
}

func ArticleReviewDeltaFor(key string, parent ArticleReviewRecord, before, after Article) (*ArticleReviewDelta, bool) {
	if key == "" || parent.Delta != nil || parent.ReusedFrom != "" || !parent.Scope.Global || len(parent.Scope.SourceIDs) == 0 || !parent.Report.Complete || len(parent.Report.Topics) == 0 || len(parent.Report.Unassessed) > 0 || parent.CandidateHash != ArticleHash(before) || before.SnapshotID != after.SnapshotID {
		return nil, false
	}
	for _, issue := range parent.Report.Issues {
		if _, global := articleIssueOwners(before, issue); issue.Severity == "blocking" && global {
			return nil, false
		}
	}
	d := &ArticleReviewDelta{ReviewKey: key, BaseReviewHash: ContentHash(parent), ChangedSectionIDs: []string{}, AffectedBlockIDs: []string{}, RemovedBlockIDs: []string{}}
	oldHeader, newHeader := before, after
	oldHeader.Sections, newHeader.Sections = nil, nil
	oldHeader.Citations, newHeader.Citations = nil, nil
	d.HeaderChanged = ArticleHash(oldHeader) != ArticleHash(newHeader)
	prior := map[string]Section{}
	oldBlocks := map[string]Block{}
	for _, section := range before.Sections {
		prior[section.ID] = section
		for _, block := range section.Blocks {
			oldBlocks[block.ID] = block
		}
	}
	appendID := func(ids *[]string, id string) {
		if !slices.Contains(*ids, id) {
			*ids = append(*ids, id)
		}
	}
	remaining := map[string]bool{}
	for _, section := range after.Sections {
		old, exists := prior[section.ID]
		changed := !exists || ContentHash(old) != ContentHash(section)
		// 局限或标题改变可能改变整章含义，不能只核对字面变更块。
		all := d.HeaderChanged || !exists || old.Title != section.Title
		for _, block := range section.Blocks {
			remaining[block.ID] = true
			old, exists := oldBlocks[block.ID]
			if all || !exists || articleDeltaBlockIdentity(before, old) != articleDeltaBlockIdentity(after, block) {
				appendID(&d.AffectedBlockIDs, block.ID)
				changed = true
			}
		}
		if changed {
			appendID(&d.ChangedSectionIDs, section.ID)
		}
	}
	for _, section := range before.Sections {
		for _, block := range section.Blocks {
			if !remaining[block.ID] {
				appendID(&d.RemovedBlockIDs, block.ID)
				appendID(&d.ChangedSectionIDs, section.ID)
			}
		}
	}
	// 原阻断即使没有修改也必须重新核对，不能靠改其它段落掩盖。
	for _, issue := range parent.Report.Issues {
		if issue.Severity != "blocking" {
			continue
		}
		owners, _ := articleIssueOwners(before, issue)
		for _, section := range before.Sections {
			if !slices.Contains(owners, section.ID) {
				continue
			}
			appendID(&d.ChangedSectionIDs, section.ID)
			for _, block := range section.Blocks {
				if articleDeltaLocation(issue.Location, block.ID) || articleDeltaLocation(issue.Location, section.ID) {
					appendID(&d.AffectedBlockIDs, block.ID)
				}
			}
		}
	}
	return d, true
}

func articleDeltaBlockIdentity(a Article, b Block) string {
	reading := ReadingArticle(Article{Sections: []Section{{ID: "scope", Blocks: []Block{b}}}})
	refs := map[string]Citation{}
	for _, block := range reading.Sections[0].Blocks {
		ids := slices.Clone(block.CitationIDs)
		for _, item := range block.Items {
			ids = append(ids, item.CitationIDs...)
		}
		for _, row := range block.Rows {
			ids = append(ids, row.CitationIDs...)
		}
		for _, id := range ids {
			refs[id] = a.Citations[id]
		}
	}
	return ContentHash(struct {
		Block     Block
		Citations map[string]Citation
	}{b, refs})
}

func articleDeltaLocation(location, id string) bool {
	parts := strings.FieldsFunc(location, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-')
	})
	return slices.Contains(parts, id)
}

func (d ArticleReviewDelta) ContainsIssue(issue ArticleReviewIssue) bool {
	if issue.Severity != "blocking" || issue.AC == "AC06" {
		return true
	}
	ids := append(slices.Clone(d.AffectedBlockIDs), d.RemovedBlockIDs...)
	if issue.AC == "AC02" {
		ids = append(ids, d.ChangedSectionIDs...)
	}
	return slices.ContainsFunc(ids, func(id string) bool { return articleDeltaLocation(issue.Location, id) })
}
