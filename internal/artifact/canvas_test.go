package artifact

import (
	"math"
	"testing"
)

func canvasBody() Body {
	return Body{SchemaVersion: 1, Kind: "study", Title: "安装课程", Blocks: []Block{
		{BlockID: "chapter", Type: "section", Title: "安装", ClaimOrigin: "user", EvidenceRefs: []Ref{}},
		{BlockID: "step-a", ParentID: ptrCanvas("chapter"), Type: "concept", Title: "准备", ClaimOrigin: "user", EvidenceRefs: []Ref{}},
		{BlockID: "step-b", ParentID: ptrCanvas("chapter"), Type: "example", Title: "执行", ClaimOrigin: "user", EvidenceRefs: []Ref{}},
	}, Warnings: []string{}}
}
func ptrCanvas(value string) *string { return &value }
func TestCanvasRelationsSchemaAndCycles(t *testing.T) {
	body := canvasBody()
	if err := body.Validate(map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	first := Relation{ID: "r1", SourceBlockID: "step-a", TargetBlockID: "step-b", Type: "related_to", Origin: "user", EvidenceRefs: []Ref{}}
	body.SchemaVersion = 2
	body.Relations = []Relation{first}
	if err := body.Validate(map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	body.Relations = append(body.Relations, Relation{ID: "r2", SourceBlockID: "step-b", TargetBlockID: "step-a", Type: "related_to", Origin: "user", EvidenceRefs: []Ref{}})
	if body.Validate(map[string]bool{}) == nil {
		t.Fatal("reversed undirected duplicate accepted")
	}
	body.Relations = []Relation{{ID: "r1", SourceBlockID: "step-a", TargetBlockID: "step-b", Type: "depends_on", Origin: "user", EvidenceRefs: []Ref{}}, {ID: "r2", SourceBlockID: "step-b", TargetBlockID: "step-a", Type: "depends_on", Origin: "user", EvidenceRefs: []Ref{}}}
	if body.Validate(map[string]bool{}) == nil {
		t.Fatal("dependency cycle accepted")
	}
	body.Relations = []Relation{{ID: "r1", SourceBlockID: "step-a", TargetBlockID: "step-b", Type: "contrasts_with", Origin: "synthesis", EvidenceRefs: []Ref{}}}
	if body.Validate(map[string]bool{}) == nil {
		t.Fatal("unsupported AI claim accepted")
	}
}
func TestCanvasPatchRelationAndSafeUndo(t *testing.T) {
	base := canvasBody()
	auth := PatchAuthorization{OperationID: "operation-1", ArtifactID: "artifact-1", BaseVersionID: "version-1", BaseVersion: 1, AllowedEvidenceIDs: map[string]bool{}}
	patch := Patch{SchemaVersion: 1, ArtifactID: auth.ArtifactID, BaseVersionID: auth.BaseVersionID, BaseVersion: 1, Basis: PatchBasisUserInstruction, EvidenceIDs: []string{}, Operations: []PatchOperation{{Op: PatchOpAddRelation, Relation: &Relation{SourceBlockID: "step-a", TargetBlockID: "step-b", Type: "related_to", Origin: "user", EvidenceRefs: []Ref{}}}}}
	result, err := EditPatch(base, patch, auth)
	if err != nil {
		t.Fatal(err)
	}
	if result.Body.SchemaVersion != 2 || len(result.Body.Relations) != 1 || result.Body.Relations[0].ID == "" || result.Diff.Changes[0].Kind != "relation_added" {
		t.Fatalf("bad relation patch: %+v", result)
	}
	undone, err := SafeUndo(base, result.Body, result.Body)
	if err != nil {
		t.Fatal(err)
	}
	if len(undone.Body.Relations) != 0 || undone.Body.Blocks[1].Title != base.Blocks[1].Title {
		t.Fatalf("undo lost body: %+v", undone.Body)
	}
}
func TestCanvasGroupSiblingsKeepsLeafContent(t *testing.T) {
	base := canvasBody()
	auth := PatchAuthorization{OperationID: "operation-group", ArtifactID: "artifact-1", BaseVersionID: "version-1", BaseVersion: 1, AllowedEvidenceIDs: map[string]bool{}}
	patch := Patch{SchemaVersion: 1, ArtifactID: auth.ArtifactID, BaseVersionID: auth.BaseVersionID, BaseVersion: 1, Basis: PatchBasisUserInstruction, EvidenceIDs: []string{}, Operations: []PatchOperation{{Op: PatchOpGroupSiblings, BlockIDs: []string{"step-a", "step-b"}, ExpectedHashes: []string{BlockHash(base.Blocks[1]), BlockHash(base.Blocks[2])}, Title: ptrCanvas("操作步骤")}}}
	result, err := EditPatch(base, patch, auth)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Body.Blocks) != 4 || result.Body.Blocks[1].Title != "操作步骤" || result.Body.Blocks[2].ParentID == nil || *result.Body.Blocks[2].ParentID != result.Body.Blocks[1].BlockID || result.Body.Blocks[3].ParentID == nil || *result.Body.Blocks[3].ParentID != result.Body.Blocks[1].BlockID {
		t.Fatalf("group failed: %+v", result.Body.Blocks)
	}
}
func TestCanvasLayoutBoundsAndHiddenContent(t *testing.T) {
	body := canvasBody()
	layout := DefaultCanvasLayout()
	layout.Nodes["step-a"] = CanvasNode{Position: CanvasPoint{X: 80, Y: 120}, Width: 254, Height: 132, Hidden: true, Style: "auto"}
	if err := layout.Validate(body); err != nil {
		t.Fatal(err)
	}
	if len(body.Blocks) != 3 {
		t.Fatal("hiding changed content")
	}
	node := layout.Nodes["step-a"]
	node.Position.X = math.NaN()
	layout.Nodes["step-a"] = node
	if layout.Validate(body) == nil {
		t.Fatal("NaN accepted")
	}
}
