package service

import (
	"testing"

	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
)

func TestStudyAttributionGuardKeepsSourceClaimsAndNegatedJudgments(t *testing.T) {
	for _, test := range []struct {
		name, source, content string
		allowed               bool
	}{
		{"unrelated citation", "视频展示模型对比表。", "该模型在当前时间点尚未发布。", false},
		{"source assertion", "讲解者说该模型尚未发布。", "讲解者称该模型尚未发布。", true},
		{"negated inference", "视频展示模型对比表。", "不能据此判断模型尚未发布或视为虚构场景。", true},
		{"ordinary future topic", "讲解未来技术趋势。", "未来的发展方向是降低成本。", true},
		{"invented future date", "对比表日期为2026年9月21日。", "这是未来时间戳。", false},
		{"unrelated future language", "讲解未来技术趋势，对比表日期为2026年9月21日。", "由于涉及未来日期和非主流命名，无法通过外部常识核验其真实性。", false},
		{"invented future models", "对比表列出Claude Haiku 4.5和Gpt-5.6-Luna。", "性能对比与未来模型列表", false},
		{"hypothetical source", "讲解者明确设想虚构场景。", "视频采用虚构场景说明概念。", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			a := newStudyEvidenceAliases([]model.SourceSnapshotItem{{ID: "canonical", Content: test.source}})
			body := artifact.Body{Blocks: []artifact.Block{{Title: "说明", Content: test.content, EvidenceRefs: []artifact.Ref{{EvidenceID: "canonical"}}}}}
			if err := a.validateAttribution(body); (err == nil) != test.allowed {
				t.Fatalf("attribution allowed=%t err=%v", test.allowed, err)
			}
		})
	}
}
