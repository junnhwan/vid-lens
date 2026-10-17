package investigator

import (
	"fmt"
	"reflect"
	"slices"
	"unicode/utf8"

	"github.com/eino-contrib/jsonschema"
	"vidlens/internal/domain"
)

const (
	maxPlanSections = 40
	maxPlanPoints   = 12
	maxPlanEvidence = 24
	maxPlanBytes    = 64 << 10
	maxPlanIssues   = 64
)

// These bounds are applied to the schema actually sent to providers as well as
// validation. Descriptions alone do not express a required or bounded array.
func contentPlanSchema(name string, typ reflect.Type, _ reflect.StructTag, sc *jsonschema.Schema) {
	if typ == reflect.TypeFor[sectionPlan]() && sc.Type == "object" {
		for _, field := range []string{"key_points", "evidence_ids"} {
			if !slices.Contains(sc.Required, field) {
				sc.Required = append(sc.Required, field)
			}
		}
	}
	if typ.Kind() != reflect.Slice || sc.Type != "array" {
		return
	}
	maximum := 0
	switch name {
	case "sections":
		if typ.Elem() == reflect.TypeFor[sectionPlan]() {
			maximum = maxPlanSections
		}
	case "key_points":
		maximum = maxPlanPoints
		sc.Description = "Keep 1-12 distinct supported facts or conditions. Preserve necessary steps and do not repeat other chapters."
	case "evidence_ids":
		maximum = maxPlanEvidence
		sc.Description = "Choose 1-24 representative existing tN/fN aliases supporting this chapter. Their actual source times must intersect the chapter range. Do not list every available citation."
	}
	if maximum > 0 {
		sc.MinItems = domain.Ptr(uint64(1))
		sc.MaxItems = domain.Ptr(uint64(maximum))
	}
}

type planIssues struct {
	bySection map[int][]map[string]any
	total     int
}

func (p *planIssues) add(section int, code, path, detail string, expected, actual any) {
	if p.bySection == nil {
		p.bySection = map[int][]map[string]any{}
	}
	p.total++
	p.bySection[section] = append(p.bySection[section], map[string]any{
		"code": code, "path": path, "detail": detail, "expected": expected, "actual": actual, "repairable": true,
	})
}

func (p *planIssues) err() error {
	if p.total == 0 {
		return nil
	}
	indices := []int{}
	for index := range p.bySection {
		indices = append(indices, index)
	}
	slices.Sort(indices)
	affected := []int{}
	for _, index := range indices {
		if index >= 0 {
			affected = append(affected, index)
		}
	}
	// Round robin ensures a long chapter cannot hide every later chapter.
	issues := []map[string]any{}
	for round := 0; len(issues) < min(p.total, maxPlanIssues); round++ {
		for _, index := range indices {
			if round < len(p.bySection[index]) && len(issues) < maxPlanIssues {
				issues = append(issues, p.bySection[index][round])
			}
		}
	}
	return &domain.Problem{Status: 422, Code: "MODEL_RESULT_INVALID",
		Detail: fmt.Sprintf("文章计划有%d处字段或来源问题，请按反馈纠正对应章节，保留已有要点。", p.total),
		Meta: map[string]any{"code": "plan_invalid", "path": "/sections", "issues": issues,
			"issue_count": p.total, "issues_truncated": p.total > len(issues), "affected_sections": affected},
	}
}

func validateContentPlan(plan []sectionPlan, catalog map[string]domain.Citation) error {
	issues := &planIssues{}
	if n := len(domain.JSON(plan)); n > maxPlanBytes {
		issues.add(-1, "plan_too_large", "/sections", "计划超过保存上限，请保留主要事实并精简表达。", maxPlanBytes, n)
		return issues.err()
	}
	seen := map[string]string{}
	for i, p := range plan {
		base := fmt.Sprintf("/sections/%d", i)
		for _, field := range []struct {
			name           string
			count, maximum int
		}{
			{"key_points", len(p.KeyPoints), maxPlanPoints}, {"evidence_ids", len(p.EvidenceIDs), maxPlanEvidence},
		} {
			code := "missing_" + field.name
			if field.name == "evidence_ids" {
				code = "missing_evidence"
			}
			if field.count > field.maximum {
				code = "point_count_exceeded"
				if field.name == "evidence_ids" {
					code = "evidence_count_exceeded"
				}
			}
			if field.count < 1 || field.count > field.maximum {
				issues.add(i, code, base+"/"+field.name, "字段数量不符合要求；引用只选能支持要点的代表性来源。", map[string]int{"min": 1, "max": field.maximum}, field.count)
			}
		}
		for j, point := range p.KeyPoints {
			path := fmt.Sprintf("%s/key_points/%d", base, j)
			key := pointKey(point)
			switch {
			case key == "":
				issues.add(i, "empty_point", path, "要点不能为空。", "nonempty supported fact", "empty")
			case utf8.RuneCountInString(point) > 300:
				issues.add(i, "point_too_long", path, "要点过长，请保留事实并精简表达。", 300, utf8.RuneCountInString(point))
			case seen[key] != "":
				issues.add(i, "duplicate_point", path, "此要点与已登记事实重复，请保留必要事实并合并或改正重复表达。", "distinct supported fact", seen[key])
			}
			if seen[key] == "" {
				seen[key] = path
			}
		}
		for j, id := range p.EvidenceIDs {
			path := fmt.Sprintf("%s/evidence_ids/%d", base, j)
			c, ok := catalog[id]
			if !ok {
				issues.add(i, "unknown_evidence", path, "引用编号不在当前来源目录中。", "existing tN/fN alias", id)
				continue
			}
			if !planEvidenceIntersects(p, c) {
				issues.add(i, "evidence_out_of_range", path, "引用的真实时间与本章来源范围不相交，请重选来源或纠正章节范围。",
					domain.Range{StartMS: p.StartMS, EndMS: p.EndMS}, map[string]any{"citation_id": id, "range": c.Range, "time_ms": c.TimeMS})
			}
		}
	}
	return issues.err()
}

func planEvidenceIntersects(p sectionPlan, c domain.Citation) bool {
	return c.Range != nil && c.Range.StartMS < p.EndMS && c.Range.EndMS > p.StartMS ||
		c.TimeMS != nil && *c.TimeMS >= p.StartMS && *c.TimeMS <= p.EndMS
}
