package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
	"vid-lens/internal/studyterms"
)

// Short IDs reduce transcription errors in model citations. Only exact aliases
// from the frozen manifest are accepted; durable results keep canonical UUIDs.
type studyEvidenceAliases struct {
	toShort     map[string]string
	toCanonical map[string]string
	content     map[string]string
}

func newStudyEvidenceAliases(items []model.SourceSnapshotItem) *studyEvidenceAliases {
	a := &studyEvidenceAliases{toShort: map[string]string{}, toCanonical: map[string]string{}, content: map[string]string{}}
	for i, item := range items {
		short := fmt.Sprintf("e%d", i+1)
		a.toShort[item.ID], a.toCanonical[short] = short, item.ID
		a.content[item.ID] = item.Content
	}
	return a
}

// Catch the specific unsupported external judgments reproduced with newer
// source material. This lexical guard is deliberately narrow; it is not a
// semantic citation verifier. Valid IDs alone do not authorize these claims.
func (a *studyEvidenceAliases) validateAttribution(body artifact.Body) error {
	rules := []struct{ claims, cues []string }{
		{[]string{"尚未发布", "还未发布", "未正式发布", "尚不存在"}, []string{"尚未发布", "还未发布", "未发布", "即将发布", "预计发布", "计划发布", "尚不存在", "unreleased", "not released"}},
		{[]string{"未来时间戳", "未来日期", "未来的日期"}, []string{"未来时间戳", "未来日期", "未来的日期", "future date", "future timestamp"}},
		{[]string{"未来模型", "未来命名"}, []string{"未来模型", "未来命名", "future model"}},
		{[]string{"虚构场景", "虚构或预测", "来源中的虚构", "内容为虚构", "内容是虚构", "虚构的模型"}, []string{"虚构", "假想", "设想", "fictional", "hypothetical"}},
	}
	check := func(text, source string) error {
		for _, rule := range rules {
			asserted := false
			for _, claim := range rule.claims {
				start := 0
				for start < len(text) {
					i := strings.Index(text[start:], claim)
					if i < 0 {
						break
					}
					i += start
					prefix := text[:i]
					if boundary := strings.LastIndexAny(prefix, "。；\n"); boundary >= 0 {
						_, size := utf8.DecodeRuneInString(prefix[boundary:])
						prefix = prefix[boundary+size:]
					}
					negated := false
					for _, term := range []string{"不能", "不应", "不可", "无法", "并非", "不是", "不代表", "不等于", "未说明", "不判断"} {
						negated = negated || strings.Contains(prefix, term)
					}
					asserted = asserted || !negated
					start = i + len(claim)
				}
			}
			if !asserted {
				continue
			}
			supported := false
			for _, cue := range rule.cues {
				supported = supported || strings.Contains(strings.ToLower(source), cue)
			}
			if !supported {
				return artifact.Err("invalid_model_output", 422)
			}
		}
		return nil
	}
	for _, block := range body.Blocks {
		var source strings.Builder
		for _, ref := range block.EvidenceRefs {
			source.WriteString(a.content[ref.EvidenceID])
			source.WriteByte('\n')
		}
		if err := check(block.Title+"。"+block.Content, source.String()); err != nil {
			return err
		}
	}
	var allSource strings.Builder
	for _, content := range a.content {
		allSource.WriteString(content)
		allSource.WriteByte('\n')
	}
	for _, warning := range body.Warnings {
		if err := check(warning, allSource.String()); err != nil {
			return err
		}
	}
	return nil
}

func (a *studyEvidenceAliases) promptMaterial(segments [][]studyEvidence, terms studyterms.StudyTermEvidence) ([][]studyEvidence, studyterms.StudyTermEvidence) {
	out := make([][]studyEvidence, len(segments))
	for i, segment := range segments {
		out[i] = append([]studyEvidence(nil), segment...)
		for j := range out[i] {
			out[i][j].ID = a.toShort[out[i][j].ID]
		}
	}
	var copied studyterms.StudyTermEvidence
	_ = json.Unmarshal([]byte(artifact.JSON(terms)), &copied)
	mapSources := func(sources []studyterms.StudyTermSource) {
		for i := range sources {
			sources[i].EvidenceID = a.toShort[sources[i].EvidenceID]
		}
	}
	mapSources(copied.VisualObservations)
	for i := range copied.Candidates {
		mapSources(copied.Candidates[i].VisualSupport)
		mapSources(copied.Candidates[i].ASRMentions)
		mapSources(copied.Candidates[i].PossibleConflicts)
	}
	return out, copied
}

func (a *studyEvidenceAliases) restore(body *artifact.Body) error {
	for i := range body.Blocks {
		for j := range body.Blocks[i].EvidenceRefs {
			ref := &body.Blocks[i].EvidenceRefs[j]
			id, ok := a.toCanonical[ref.EvidenceID]
			if !ok {
				return artifact.Err("invalid_evidence", 422)
			}
			ref.EvidenceID = id
		}
	}
	// Generated segment schema v1 cannot invent semantic relations.
	if len(body.Relations) > 0 {
		return artifact.Err("invalid_model_output", 422)
	}
	return nil
}
