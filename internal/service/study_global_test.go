package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
)

func TestStudyGlobalPreservesDetailsOpposingViewsCitationsAndLineage(t *testing.T) {
	blocks := []artifact.Block{
		{BlockID: "s1-a", Type: "concept", Title: "事务提交", Content: "提交使修改生效", ClaimOrigin: "source", EvidenceRefs: []artifact.Ref{{EvidenceID: "asr-1", Relation: "supports"}}},
		{BlockID: "s2-b", Type: "concept", Title: "事务提交", Content: "讲者也指出失败时回滚", ClaimOrigin: "source", EvidenceRefs: []artifact.Ref{{EvidenceID: "asr-2", Relation: "supports"}, {EvidenceID: "vision-1", Relation: "context"}}},
		{BlockID: "s2-c", Type: "concept", Title: "事务隔离", Content: "相反观点：隔离不等于提交", ClaimOrigin: "source", EvidenceRefs: []artifact.Ref{{EvidenceID: "asr-3", Relation: "contradicts"}}},
	}
	input := artifact.Body{SchemaVersion: 1, Kind: "study", Title: "数据库", Blocks: blocks, Warnings: []string{}}
	plan := studyGlobalPlan{Groups: []studyGlobalGroup{{OldBlockIDs: []string{"s1-a", "s2-b"}, Title: "事务提交"}, {OldBlockIDs: []string{"s2-c"}, Title: "事务隔离"}}}
	out, err := organizeStudyBlocks(input, plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Blocks) != 2 || strings.Join(out.Blocks[0].SourceBlockIDs, ",") != "s1-a,s2-b" {
		t.Fatalf("lineage lost: %+v", out.Blocks)
	}
	if !strings.Contains(out.Blocks[0].Content, "提交使修改生效") || !strings.Contains(out.Blocks[0].Content, "失败时回滚") || len(out.Blocks[0].EvidenceRefs) != 3 {
		t.Fatalf("content or refs lost: %+v", out.Blocks[0])
	}
	allowed := map[string]bool{"asr-1": true, "asr-2": true, "asr-3": true, "vision-1": true}
	if err := out.Validate(allowed); err != nil {
		t.Fatal(err)
	}
	if _, err := organizeStudyBlocks(input, studyGlobalPlan{Groups: []studyGlobalGroup{{OldBlockIDs: []string{"s1-a", "s2-b", "s2-c"}, Title: "事务提交"}}}); err == nil {
		t.Fatal("opposing viewpoint merged")
	}
}

func TestStudyGlobalRejectsOmittedOrReusedBlocksAndKeepsSimilarTopicsSeparate(t *testing.T) {
	input := artifact.Body{SchemaVersion: 1, Kind: "study", Title: "课程", Blocks: []artifact.Block{
		{BlockID: "a", Type: "concept", Title: "相似主题 A", Content: "独立功能 A", ClaimOrigin: "source", EvidenceRefs: []artifact.Ref{{EvidenceID: "asr-a", Relation: "supports"}}},
		{BlockID: "b", Type: "concept", Title: "相似主题 B", Content: "独立功能 B", ClaimOrigin: "source", EvidenceRefs: []artifact.Ref{{EvidenceID: "asr-b", Relation: "supports"}}},
	}, Warnings: []string{}}
	for _, plan := range []studyGlobalPlan{
		{Groups: []studyGlobalGroup{{OldBlockIDs: []string{"a"}, Title: "相似主题 A"}}},
		{Groups: []studyGlobalGroup{{OldBlockIDs: []string{"a", "a"}, Title: "相似主题 A"}, {OldBlockIDs: []string{"b"}, Title: "相似主题 B"}}},
	} {
		if _, err := organizeStudyBlocks(input, plan); err == nil {
			t.Fatalf("accepted invalid plan %+v", plan)
		}
	}
	separate := studyGlobalPlan{Groups: []studyGlobalGroup{{OldBlockIDs: []string{"a"}, Title: "相似主题 A"}, {OldBlockIDs: []string{"b"}, Title: "相似主题 B"}}}
	out, err := organizeStudyBlocks(input, separate)
	if err != nil || len(out.Blocks) != 2 || out.Blocks[0].Content == out.Blocks[1].Content {
		t.Fatalf("similar themes incorrectly merged: %+v %v", out, err)
	}
}

func TestStudyGlobalRejectsOversizedInputBeforeProvider(t *testing.T) {
	input := artifact.Body{Blocks: make([]artifact.Block, 121)}
	_, err := (&ArtifactService{}).studyGlobalCall(context.Background(), &model.AgentRun{}, "", input, nil)
	var domain *artifact.Error
	if !errors.As(err, &domain) || domain.Code != "source_limit_exceeded" {
		t.Fatalf("oversized input: %v", err)
	}
}
