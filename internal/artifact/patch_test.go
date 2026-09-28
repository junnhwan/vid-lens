package artifact_test

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"vid-lens/internal/artifact"
)

func TestEditPatchUpdatesOnlyAuthorizedBlockFields(t *testing.T) {
	base := fixtureBody()
	title := "安装步骤"
	content := "运行 scoped-models 命令。"
	patch := artifact.Patch{
		SchemaVersion: 1,
		ArtifactID:    "artifact-1",
		BaseVersionID: "version-6",
		BaseVersion:   6,
		Basis:         artifact.PatchBasisEvidenceSupported,
		EvidenceIDs:   []string{"ev-install"},
		Operations: []artifact.PatchOperation{{
			Op:           artifact.PatchOpUpdateBlock,
			BlockID:      "install",
			ExpectedHash: artifact.BlockHash(base.Blocks[1]),
			Title:        &title,
			Content:      &content,
		}},
	}
	got, err := artifact.EditPatch(base, patch, fixtureAuthorization("install"))
	if err != nil {
		t.Fatalf("EditPatch() error = %v", err)
	}
	if got.Body.Blocks[1].Title != title || got.Body.Blocks[1].Content != content {
		t.Fatalf("updated block = %#v", got.Body.Blocks[1])
	}
	if got.Body.Blocks[1].ClaimOrigin != "synthesis" {
		t.Fatalf("claim origin = %q, want synthesis", got.Body.Blocks[1].ClaimOrigin)
	}
	if !reflect.DeepEqual(got.Body.Blocks[0], base.Blocks[0]) || !reflect.DeepEqual(got.Body.Blocks[2], base.Blocks[2]) {
		t.Fatal("out-of-scope blocks changed")
	}
	if got.Diff.Counts != (artifact.PatchCounts{Updated: 1}) {
		t.Fatalf("counts = %#v", got.Diff.Counts)
	}
	if len(got.Diff.Changes) != 1 || got.Diff.Changes[0].Before == nil || got.Diff.Changes[0].After == nil {
		t.Fatalf("changes = %#v", got.Diff.Changes)
	}
}

func TestEditPatchRejectsNoopAndKeepsTheWholeGroupAtomic(t *testing.T) {
	base := fixtureBody()
	sameTitle := base.Blocks[1].Title
	noop := fixturePatch(artifact.PatchOperation{
		Op: artifact.PatchOpUpdateBlock, BlockID: "install", ExpectedHash: artifact.BlockHash(base.Blocks[1]), Title: &sameTitle,
	})
	_, err := artifact.EditPatch(base, noop, fixtureAuthorization("install"))
	assertArtifactError(t, err, "invalid_patch")

	changedTitle, changedSibling := "已修改", "不应生效"
	atomic := fixturePatch(artifact.PatchOperation{
		Op: artifact.PatchOpUpdateBlock, BlockID: "install", ExpectedHash: artifact.BlockHash(base.Blocks[1]), Title: &changedTitle,
	})
	atomic.Operations = append(atomic.Operations, artifact.PatchOperation{
		Op: artifact.PatchOpUpdateBlock, BlockID: "child", ExpectedHash: artifact.BlockHash(base.Blocks[2]), Title: &changedSibling,
	})
	before := artifact.JSON(base)
	_, err = artifact.EditPatch(base, atomic, fixtureAuthorization("install"))
	assertArtifactError(t, err, "target_scope_mismatch")
	if artifact.JSON(base) != before {
		t.Fatal("partially valid patch mutated the base body")
	}
}

func TestEditPatchRejectsUnboundedOrUnfoundedEnvelope(t *testing.T) {
	base := fixtureBody()
	title := "修改"
	patch := fixturePatch(artifact.PatchOperation{
		Op: artifact.PatchOpUpdateBlock, BlockID: "install", ExpectedHash: artifact.BlockHash(base.Blocks[1]), Title: &title,
	})
	patch.Basis = artifact.PatchBasisEvidenceSupported
	_, err := artifact.EditPatch(base, patch, fixtureAuthorization("install"))
	assertArtifactError(t, err, "invalid_patch")

	patch.EvidenceIDs = []string{"not-in-manifest"}
	_, err = artifact.EditPatch(base, patch, fixtureAuthorization("install"))
	assertArtifactError(t, err, "invalid_evidence")

	patch.Basis, patch.EvidenceIDs = artifact.PatchBasisUserInstruction, []string{}
	patch.Operations = make([]artifact.PatchOperation, 51)
	_, err = artifact.EditPatch(base, patch, fixtureAuthorization("install"))
	assertArtifactError(t, err, "invalid_patch")

	patch.Operations = []artifact.PatchOperation{{Op: artifact.PatchOpUpdateBlock, BlockID: "install", ExpectedHash: artifact.BlockHash(base.Blocks[1]), Title: &title}}
	auth := fixtureAuthorization()
	for i := range 21 {
		auth.SelectedBlockIDs = append(auth.SelectedBlockIDs, fmt.Sprintf("block-%d", i))
	}
	_, err = artifact.EditPatch(base, patch, auth)
	assertArtifactError(t, err, "target_scope_mismatch")
}

func TestCanonicalPatchIsStableAndPayloadSensitive(t *testing.T) {
	base := fixtureBody()
	title := "稳定内容"
	patch := fixturePatch(artifact.PatchOperation{Op: artifact.PatchOpUpdateBlock, BlockID: "install", ExpectedHash: artifact.BlockHash(base.Blocks[1]), Title: &title})
	canonical, hash, err := artifact.CanonicalPatch(patch)
	if err != nil {
		t.Fatal(err)
	}
	replayedCanonical, replayedHash, err := artifact.CanonicalPatch(patch)
	if err != nil {
		t.Fatal(err)
	}
	if canonical != replayedCanonical || hash != replayedHash || !strings.Contains(canonical, `"evidence_ids":[]`) {
		t.Fatalf("unstable canonical patch: %q %q", canonical, replayedCanonical)
	}
	changed := patch
	changedTitle := "不同内容"
	changed.Operations = slices.Clone(patch.Operations)
	changed.Operations[0].Title = &changedTitle
	_, changedHash, err := artifact.CanonicalPatch(changed)
	if err != nil {
		t.Fatal(err)
	}
	if changedHash == hash {
		t.Fatal("different patch payload reused canonical hash")
	}
}

func TestEditPatchEnforcesFinalBodyLimits(t *testing.T) {
	t.Run("block content", func(t *testing.T) {
		base := fixtureBody()
		content := strings.Repeat("字", 8001)
		patch := fixturePatch(artifact.PatchOperation{Op: artifact.PatchOpUpdateBlock, BlockID: "install", ExpectedHash: artifact.BlockHash(base.Blocks[1]), Content: &content})
		_, err := artifact.EditPatch(base, patch, fixtureAuthorization("install"))
		assertArtifactError(t, err, "invalid_patch")
	})

	t.Run("block count", func(t *testing.T) {
		base := artifact.Body{SchemaVersion: 1, Kind: "study", Title: "上限", Warnings: []string{}}
		for i := range 200 {
			base.Blocks = append(base.Blocks, artifact.Block{BlockID: fmt.Sprintf("b-%03d", i), Type: "note", Title: "块", ClaimOrigin: "user", EvidenceRefs: []artifact.Ref{}})
		}
		typeName, title, content := "note", "额外块", ""
		refs := []artifact.Ref{}
		patch := fixturePatch(artifact.PatchOperation{Op: artifact.PatchOpInsertBlock, Key: "overflow", Type: &typeName, Title: &title, Content: &content, EvidenceRefs: &refs})
		_, err := artifact.EditPatch(base, patch, fixtureAuthorization())
		assertArtifactError(t, err, "invalid_patch")
	})

	t.Run("depth", func(t *testing.T) {
		base := artifact.Body{SchemaVersion: 1, Kind: "study", Title: "深度", Warnings: []string{}}
		for i := range 8 {
			block := artifact.Block{BlockID: fmt.Sprintf("d-%d", i), Type: "note", Title: "层", ClaimOrigin: "user", EvidenceRefs: []artifact.Ref{}}
			if i > 0 {
				parent := fmt.Sprintf("d-%d", i-1)
				block.ParentID = &parent
			}
			base.Blocks = append(base.Blocks, block)
		}
		parent, typeName, title, content := "d-7", "note", "第九层", ""
		refs := []artifact.Ref{}
		patch := fixturePatch(artifact.PatchOperation{Op: artifact.PatchOpInsertBlock, Key: "too-deep", ParentID: &parent, Type: &typeName, Title: &title, Content: &content, EvidenceRefs: &refs})
		_, err := artifact.EditPatch(base, patch, fixtureAuthorization("d-7"))
		assertArtifactError(t, err, "invalid_patch")
	})
}

func TestEditPatchUpdatesTitleOnlyWithFullArtifactScope(t *testing.T) {
	base := fixtureBody()
	newTitle := "Pi 教程笔记"
	patch := artifact.Patch{
		SchemaVersion: 1, ArtifactID: "artifact-1", BaseVersionID: "version-6", BaseVersion: 6,
		Basis: artifact.PatchBasisUserInstruction, EvidenceIDs: []string{},
		Operations: []artifact.PatchOperation{{
			Op: artifact.PatchOpUpdateTitle, ExpectedHash: artifact.Hash(base.Title), Title: &newTitle,
		}},
	}
	got, err := artifact.EditPatch(base, patch, fixtureAuthorization())
	if err != nil {
		t.Fatalf("EditPatch() error = %v", err)
	}
	if got.Body.Title != newTitle || !reflect.DeepEqual(got.Body.Blocks, base.Blocks) {
		t.Fatalf("body = %#v", got.Body)
	}
	if got.Diff.Counts != (artifact.PatchCounts{Updated: 1}) || len(got.Diff.Changes) != 1 || got.Diff.Changes[0].BeforeTitle == nil || *got.Diff.Changes[0].BeforeTitle != base.Title {
		t.Fatalf("diff = %#v", got.Diff)
	}

	_, err = artifact.EditPatch(base, patch, fixtureAuthorization("install"))
	assertArtifactError(t, err, "target_scope_mismatch")
	patch.Operations[0].ExpectedHash = artifact.Hash("stale")
	_, err = artifact.EditPatch(base, patch, fixtureAuthorization())
	assertArtifactError(t, err, "invalid_patch")

	secondTitle := "第二次改名"
	patch.Operations = []artifact.PatchOperation{
		{Op: artifact.PatchOpUpdateTitle, ExpectedHash: artifact.Hash(base.Title), Title: &newTitle},
		{Op: artifact.PatchOpUpdateTitle, ExpectedHash: artifact.Hash(newTitle), Title: &secondTitle},
	}
	_, err = artifact.EditPatch(base, patch, fixtureAuthorization())
	assertArtifactError(t, err, "invalid_patch")
}

func TestEditPatchInsertsStableBlockAtAuthorizedSiblingPosition(t *testing.T) {
	base := fixtureBody()
	typeName, title, content := "note", "命令", "运行 scoped-models。"
	refs := []artifact.Ref{{EvidenceID: "ev-install", Relation: "supports"}}
	parent, after := "root", "install"
	patch := artifact.Patch{
		SchemaVersion: 1, ArtifactID: "artifact-1", BaseVersionID: "version-6", BaseVersion: 6,
		Basis: artifact.PatchBasisEvidenceSupported, EvidenceIDs: []string{"ev-install"},
		Operations: []artifact.PatchOperation{{
			Op: artifact.PatchOpInsertBlock, Key: "command-step", ParentID: &parent, AfterBlockID: &after,
			Type: &typeName, Title: &title, Content: &content, EvidenceRefs: &refs,
		}},
	}
	got, err := artifact.EditPatch(base, patch, fixtureAuthorization("root"))
	if err != nil {
		t.Fatalf("EditPatch() error = %v", err)
	}
	if len(got.Body.Blocks) != 4 || got.Body.Blocks[2].Title != title || got.Body.Blocks[2].BlockID == "" {
		t.Fatalf("blocks = %#v", got.Body.Blocks)
	}
	if got.Body.Blocks[2].ClaimOrigin != "synthesis" || got.Diff.Counts != (artifact.PatchCounts{Added: 1}) {
		t.Fatalf("result = %#v", got)
	}
	replayed, err := artifact.EditPatch(base, patch, fixtureAuthorization("root"))
	if err != nil {
		t.Fatal(err)
	}
	if replayed.Body.Blocks[2].BlockID != got.Body.Blocks[2].BlockID {
		t.Fatalf("replay id = %q, want %q", replayed.Body.Blocks[2].BlockID, got.Body.Blocks[2].BlockID)
	}
	differentAuth := fixtureAuthorization("root")
	differentAuth.OperationID = "operation-2"
	different, err := artifact.EditPatch(base, patch, differentAuth)
	if err != nil {
		t.Fatal(err)
	}
	if different.Body.Blocks[2].BlockID == got.Body.Blocks[2].BlockID {
		t.Fatal("different operation reused derived block ID")
	}
}

func TestEditPatchDeletesAuthorizedSubtreeAtomically(t *testing.T) {
	base := fixtureBody()
	install := "install"
	base.Blocks = slices.Insert(base.Blocks, 2, artifact.Block{
		BlockID: "command", ParentID: &install, Type: "note", Title: "命令", Content: "scoped-models", ClaimOrigin: "user", EvidenceRefs: []artifact.Ref{},
	})
	patch := artifact.Patch{
		SchemaVersion: 1, ArtifactID: "artifact-1", BaseVersionID: "version-6", BaseVersion: 6,
		Basis: artifact.PatchBasisUserInstruction, EvidenceIDs: []string{},
		Operations: []artifact.PatchOperation{{
			Op: artifact.PatchOpDeleteSubtree, BlockID: "install", ExpectedHash: artifact.BlockHash(base.Blocks[1]),
		}},
	}
	got, err := artifact.EditPatch(base, patch, fixtureAuthorization("install"))
	if err != nil {
		t.Fatalf("EditPatch() error = %v", err)
	}
	if ids := blockIDs(got.Body); !reflect.DeepEqual(ids, []string{"root", "child"}) {
		t.Fatalf("block ids = %v", ids)
	}
	if got.Diff.Counts != (artifact.PatchCounts{Deleted: 2}) || len(got.Diff.Changes) != 2 {
		t.Fatalf("diff = %#v", got.Diff)
	}

	deleteEverything := patch
	deleteEverything.Operations = []artifact.PatchOperation{{Op: artifact.PatchOpDeleteSubtree, BlockID: "root", ExpectedHash: artifact.BlockHash(base.Blocks[0])}}
	_, err = artifact.EditPatch(base, deleteEverything, fixtureAuthorization())
	assertArtifactError(t, err, "invalid_patch")
	if ids := blockIDs(base); !reflect.DeepEqual(ids, []string{"root", "install", "command", "child"}) {
		t.Fatalf("failed patch mutated base: %v", ids)
	}
}

func TestEditPatchMovesSubtreeWithoutChangingItsContent(t *testing.T) {
	base := fixtureBody()
	root, child := "root", "child"
	patch := artifact.Patch{
		SchemaVersion: 1, ArtifactID: "artifact-1", BaseVersionID: "version-6", BaseVersion: 6,
		Basis: artifact.PatchBasisUserInstruction, EvidenceIDs: []string{},
		Operations: []artifact.PatchOperation{{
			Op: artifact.PatchOpMoveSubtree, BlockID: "install", ExpectedHash: artifact.BlockHash(base.Blocks[1]), ParentID: &root, AfterBlockID: &child,
		}},
	}
	got, err := artifact.EditPatch(base, patch, fixtureAuthorization("root"))
	if err != nil {
		t.Fatalf("EditPatch() error = %v", err)
	}
	if ids := blockIDs(got.Body); !reflect.DeepEqual(ids, []string{"root", "child", "install"}) {
		t.Fatalf("block ids = %v", ids)
	}
	if !reflect.DeepEqual(got.Body.Blocks[2], base.Blocks[1]) || got.Diff.Counts != (artifact.PatchCounts{Moved: 1}) {
		t.Fatalf("move changed content or counts: %#v", got)
	}

	install := "install"
	cycle := patch
	cycle.Operations = []artifact.PatchOperation{{Op: artifact.PatchOpMoveSubtree, BlockID: "root", ExpectedHash: artifact.BlockHash(base.Blocks[0]), ParentID: &install}}
	_, err = artifact.EditPatch(base, cycle, fixtureAuthorization())
	assertArtifactError(t, err, "invalid_patch")

	_, err = artifact.EditPatch(base, patch, fixtureAuthorization("install"))
	assertArtifactError(t, err, "target_scope_mismatch")
}

func TestEditPatchSplitsLeafWithStableLineageAndExplicitReferences(t *testing.T) {
	base := fixtureBody()
	parts := []artifact.SplitPart{
		{Type: "concept", Title: "安装准备", Content: "先确认环境。", EvidenceRefs: []artifact.Ref{{EvidenceID: "ev-install", Relation: "context"}}},
		{Type: "note", Title: "执行命令", Content: "运行 scoped-models。", EvidenceRefs: []artifact.Ref{{EvidenceID: "ev-install", Relation: "supports"}}},
		{Type: "note", Title: "检查结果", Content: "查看输出。", EvidenceRefs: []artifact.Ref{}},
	}
	patch := artifact.Patch{
		SchemaVersion: 1, ArtifactID: "artifact-1", BaseVersionID: "version-6", BaseVersion: 6,
		Basis: artifact.PatchBasisEvidenceSupported, EvidenceIDs: []string{"ev-install"},
		Operations: []artifact.PatchOperation{{
			Op: artifact.PatchOpSplitBlock, BlockID: "install", ExpectedHash: artifact.BlockHash(base.Blocks[1]), Parts: parts,
		}},
	}
	got, err := artifact.EditPatch(base, patch, fixtureAuthorization("install"))
	if err != nil {
		t.Fatalf("EditPatch() error = %v", err)
	}
	if got.Body.Blocks[1].BlockID != "install" || got.Body.Blocks[2].BlockID == "" || got.Body.Blocks[3].BlockID == "" {
		t.Fatalf("split ids = %v", blockIDs(got.Body))
	}
	if got.Body.Blocks[1].Title != parts[0].Title || got.Body.Blocks[2].Title != parts[1].Title || got.Body.Blocks[3].Title != parts[2].Title {
		t.Fatalf("split blocks = %#v", got.Body.Blocks[1:4])
	}
	if got.Diff.Counts != (artifact.PatchCounts{Added: 2, Updated: 1}) || len(got.Diff.BlockMappings) != 1 || !reflect.DeepEqual(got.Diff.BlockMappings[0].FromBlockIDs, []string{"install"}) {
		t.Fatalf("diff = %#v", got.Diff)
	}
	replayed, err := artifact.EditPatch(base, patch, fixtureAuthorization("install"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(blockIDs(replayed.Body), blockIDs(got.Body)) {
		t.Fatalf("replay ids = %v, want %v", blockIDs(replayed.Body), blockIDs(got.Body))
	}

	nonLeaf := patch
	nonLeaf.Operations = []artifact.PatchOperation{{Op: artifact.PatchOpSplitBlock, BlockID: "root", ExpectedHash: artifact.BlockHash(base.Blocks[0]), Parts: parts}}
	_, err = artifact.EditPatch(base, nonLeaf, fixtureAuthorization())
	assertArtifactError(t, err, "invalid_patch")
}

func TestEditPatchRejectsSplitAndMergeInputsBeyondToolSchemaLimits(t *testing.T) {
	base := fixtureBody()
	parts := make([]artifact.SplitPart, 51)
	for i := range parts {
		parts[i] = artifact.SplitPart{Type: "note", Title: fmt.Sprintf("步骤 %d", i+1), EvidenceRefs: []artifact.Ref{}}
	}
	split := fixturePatch(artifact.PatchOperation{
		Op:           artifact.PatchOpSplitBlock,
		BlockID:      "install",
		ExpectedHash: artifact.BlockHash(base.Blocks[1]),
		Parts:        parts,
	})
	_, err := artifact.EditPatch(base, split, fixtureAuthorization("install"))
	assertArtifactError(t, err, "invalid_patch")

	mergeBase := artifact.Body{SchemaVersion: 1, Kind: "study", Title: "合并上限", Warnings: []string{}}
	blockIDs := make([]string, 51)
	expectedHashes := make([]string, 51)
	for i := range blockIDs {
		block := artifact.Block{BlockID: fmt.Sprintf("b-%02d", i), Type: "note", Title: fmt.Sprintf("块 %d", i+1), ClaimOrigin: "user", EvidenceRefs: []artifact.Ref{}}
		mergeBase.Blocks = append(mergeBase.Blocks, block)
		blockIDs[i] = block.BlockID
		expectedHashes[i] = artifact.BlockHash(block)
	}
	merge := fixturePatch(artifact.PatchOperation{
		Op:             artifact.PatchOpMergeSiblings,
		BlockIDs:       blockIDs,
		ExpectedHashes: expectedHashes,
	})
	_, err = artifact.EditPatch(mergeBase, merge, fixtureAuthorization())
	assertArtifactError(t, err, "invalid_patch")
}

func TestEditPatchMergesAdjacentLeavesWithoutDiscardingUniqueTextOrUserOrigin(t *testing.T) {
	base := fixtureBody()
	root := "root"
	base.Blocks[1].Content = "准备环境。\n\n运行命令。"
	base.Blocks = slices.Insert(base.Blocks, 2, artifact.Block{
		BlockID: "verify", ParentID: &root, Type: "note", Title: "验证", Content: "运行命令。\n\n检查输出。", ClaimOrigin: "user",
		EvidenceRefs: []artifact.Ref{{EvidenceID: "ev-child", Relation: "context"}},
	})
	mergedTitle := "安装与验证"
	patch := artifact.Patch{
		SchemaVersion: 1, ArtifactID: "artifact-1", BaseVersionID: "version-6", BaseVersion: 6,
		Basis: artifact.PatchBasisEvidenceSupported, EvidenceIDs: []string{"ev-install", "ev-child"},
		Operations: []artifact.PatchOperation{{
			Op: artifact.PatchOpMergeSiblings, BlockIDs: []string{"install", "verify"},
			ExpectedHashes: []string{artifact.BlockHash(base.Blocks[1]), artifact.BlockHash(base.Blocks[2])}, Title: &mergedTitle,
		}},
	}
	got, err := artifact.EditPatch(base, patch, fixtureAuthorization("root"))
	if err != nil {
		t.Fatalf("EditPatch() error = %v", err)
	}
	merged := got.Body.Blocks[1]
	if merged.BlockID != "install" || merged.Title != mergedTitle || merged.Content != "准备环境。\n\n运行命令。\n\n检查输出。" {
		t.Fatalf("merged block = %#v", merged)
	}
	if merged.ClaimOrigin != "user" || len(merged.EvidenceRefs) != 2 {
		t.Fatalf("merged provenance = %#v", merged)
	}
	if got.Diff.Counts != (artifact.PatchCounts{Updated: 1, Deleted: 1}) || len(got.Diff.BlockMappings) != 1 || !reflect.DeepEqual(got.Diff.BlockMappings[0].ToBlockIDs, []string{"install"}) {
		t.Fatalf("diff = %#v", got.Diff)
	}

	nonAdjacent := patch
	nonAdjacent.Operations = []artifact.PatchOperation{{
		Op: artifact.PatchOpMergeSiblings, BlockIDs: []string{"install", "child"},
		ExpectedHashes: []string{artifact.BlockHash(base.Blocks[1]), artifact.BlockHash(base.Blocks[3])},
	}}
	_, err = artifact.EditPatch(base, nonAdjacent, fixtureAuthorization("root"))
	assertArtifactError(t, err, "invalid_patch")
}

func TestEditPatchSupportsParentOrderedNonContiguousTrees(t *testing.T) {
	base := nonContiguousTreeBody()

	t.Run("split and merge see descendants outside the adjacent slice", func(t *testing.T) {
		parts := []artifact.SplitPart{
			{Type: "note", Title: "第一段", EvidenceRefs: []artifact.Ref{}},
			{Type: "note", Title: "第二段", EvidenceRefs: []artifact.Ref{}},
		}
		split := fixturePatch(artifact.PatchOperation{Op: artifact.PatchOpSplitBlock, BlockID: "a", ExpectedHash: artifact.BlockHash(base.Blocks[0]), Parts: parts})
		_, err := artifact.EditPatch(base, split, fixtureAuthorization())
		assertArtifactError(t, err, "invalid_patch")

		merge := fixturePatch(artifact.PatchOperation{
			Op: artifact.PatchOpMergeSiblings, BlockIDs: []string{"a", "b"},
			ExpectedHashes: []string{artifact.BlockHash(base.Blocks[0]), artifact.BlockHash(base.Blocks[1])},
		})
		_, err = artifact.EditPatch(base, merge, fixtureAuthorization())
		assertArtifactError(t, err, "invalid_patch")

		parent := "p"
		mergeBase := artifact.Body{SchemaVersion: 1, Kind: "study", Title: "跨布局相邻兄弟", Blocks: []artifact.Block{
			{BlockID: "p", Type: "section", Title: "P", ClaimOrigin: "user", EvidenceRefs: []artifact.Ref{}},
			{BlockID: "left", ParentID: &parent, Type: "note", Title: "左", Content: "左", ClaimOrigin: "user", EvidenceRefs: []artifact.Ref{}},
			{BlockID: "other", Type: "section", Title: "其他", ClaimOrigin: "user", EvidenceRefs: []artifact.Ref{}},
			{BlockID: "right", ParentID: &parent, Type: "note", Title: "右", Content: "右", ClaimOrigin: "user", EvidenceRefs: []artifact.Ref{}},
		}, Warnings: []string{}}
		merge = fixturePatch(artifact.PatchOperation{
			Op: artifact.PatchOpMergeSiblings, BlockIDs: []string{"left", "right"},
			ExpectedHashes: []string{artifact.BlockHash(mergeBase.Blocks[1]), artifact.BlockHash(mergeBase.Blocks[3])},
		})
		merged, err := artifact.EditPatch(mergeBase, merge, fixtureAuthorization())
		if err != nil {
			t.Fatalf("non-contiguous sibling merge error = %v", err)
		}
		if ids := blockIDs(merged.Body); !reflect.DeepEqual(ids, []string{"p", "left", "other"}) {
			t.Fatalf("non-contiguous sibling merge reordered untouched blocks: %v", ids)
		}
	})

	t.Run("delete removes every descendant and preserves untouched order", func(t *testing.T) {
		patch := fixturePatch(artifact.PatchOperation{Op: artifact.PatchOpDeleteSubtree, BlockID: "a", ExpectedHash: artifact.BlockHash(base.Blocks[0])})
		applied, err := artifact.EditPatch(base, patch, fixtureAuthorization("a"))
		if err != nil {
			t.Fatalf("EditPatch() error = %v", err)
		}
		if ids := blockIDs(applied.Body); !reflect.DeepEqual(ids, []string{"b", "d"}) {
			t.Fatalf("delete ids = %v, want [b d]", ids)
		}
		if applied.Diff.Counts.Deleted != 2 {
			t.Fatalf("delete diff = %#v", applied.Diff)
		}

		undone, err := artifact.SafeUndo(base, applied.Body, applied.Body)
		if err != nil {
			t.Fatalf("SafeUndo() error = %v", err)
		}
		if ids := blockIDs(undone.Body); !reflect.DeepEqual(ids, []string{"a", "b", "c", "d"}) {
			t.Fatalf("undo reordered a valid parent-ordered body: %v", ids)
		}
	})

	t.Run("move gathers a non-contiguous subtree without reordering untouched blocks", func(t *testing.T) {
		after := "d"
		patch := fixturePatch(artifact.PatchOperation{Op: artifact.PatchOpMoveSubtree, BlockID: "a", ExpectedHash: artifact.BlockHash(base.Blocks[0]), AfterBlockID: &after})
		applied, err := artifact.EditPatch(base, patch, fixtureAuthorization())
		if err != nil {
			t.Fatalf("EditPatch() error = %v", err)
		}
		if ids := blockIDs(applied.Body); !reflect.DeepEqual(ids, []string{"b", "d", "a", "c"}) {
			t.Fatalf("move ids = %v, want [b d a c]", ids)
		}
		if applied.Body.Blocks[3].ParentID == nil || *applied.Body.Blocks[3].ParentID != "a" {
			t.Fatalf("move detached descendant: %#v", applied.Body.Blocks[3])
		}
		undone, err := artifact.SafeUndo(base, applied.Body, applied.Body)
		if err != nil {
			t.Fatalf("SafeUndo() move error = %v", err)
		}
		if ids := blockIDs(undone.Body); !reflect.DeepEqual(ids, []string{"a", "b", "c", "d"}) {
			t.Fatalf("move undo ids = %v, want [a b c d]", ids)
		}
	})

	t.Run("after block follows direct sibling order", func(t *testing.T) {
		after := "a"
		typeName, title, content := "note", "新增根", "新增内容"
		refs := []artifact.Ref{}
		insert := fixturePatch(artifact.PatchOperation{
			Op: artifact.PatchOpInsertBlock, Key: "after-a", AfterBlockID: &after,
			Type: &typeName, Title: &title, Content: &content, EvidenceRefs: &refs,
		})
		inserted, err := artifact.EditPatch(base, insert, fixtureAuthorization())
		if err != nil {
			t.Fatalf("insert after A error = %v", err)
		}
		insertedIDs := blockIDs(inserted.Body)
		if len(insertedIDs) != 5 || insertedIDs[0] != "a" || insertedIDs[2] != "b" || insertedIDs[3] != "c" || insertedIDs[4] != "d" {
			t.Fatalf("insert after A used descendant extent instead of sibling order: %v", insertedIDs)
		}

		move := fixturePatch(artifact.PatchOperation{Op: artifact.PatchOpMoveSubtree, BlockID: "d", ExpectedHash: artifact.BlockHash(base.Blocks[3]), AfterBlockID: &after})
		moved, err := artifact.EditPatch(base, move, fixtureAuthorization())
		if err != nil {
			t.Fatalf("move after A error = %v", err)
		}
		if ids := blockIDs(moved.Body); !reflect.DeepEqual(ids, []string{"a", "d", "b", "c"}) {
			t.Fatalf("move after A used descendant extent instead of sibling order: %v", ids)
		}
	})

	t.Run("field-only undo preserves a valid non-DFS order", func(t *testing.T) {
		title := "修改后的 A"
		patch := fixturePatch(artifact.PatchOperation{Op: artifact.PatchOpUpdateBlock, BlockID: "a", ExpectedHash: artifact.BlockHash(base.Blocks[0]), Title: &title})
		applied, err := artifact.EditPatch(base, patch, fixtureAuthorization("a"))
		if err != nil {
			t.Fatalf("EditPatch() error = %v", err)
		}
		current := cloneBodyForTest(t, applied.Body)
		current.Blocks[3].Content = "后续人工内容"
		undone, err := artifact.SafeUndo(base, applied.Body, current)
		if err != nil {
			t.Fatalf("SafeUndo() error = %v", err)
		}
		if ids := blockIDs(undone.Body); !reflect.DeepEqual(ids, []string{"a", "b", "c", "d"}) || undone.Body.Blocks[3].Content != "后续人工内容" {
			t.Fatalf("field undo body = %#v", undone.Body)
		}
	})
}

func TestSafeUndoRestoresTouchedFieldsAndPreservesUnrelatedLaterEdit(t *testing.T) {
	base := fixtureBody()
	title := "正确的安装名称"
	patch := artifact.Patch{
		SchemaVersion: 1, ArtifactID: "artifact-1", BaseVersionID: "version-6", BaseVersion: 6,
		Basis: artifact.PatchBasisEvidenceSupported, EvidenceIDs: []string{"ev-install"},
		Operations: []artifact.PatchOperation{{
			Op: artifact.PatchOpUpdateBlock, BlockID: "install", ExpectedHash: artifact.BlockHash(base.Blocks[1]), Title: &title,
		}},
	}
	applied, err := artifact.EditPatch(base, patch, fixtureAuthorization("install"))
	if err != nil {
		t.Fatal(err)
	}
	current := cloneBodyForTest(t, applied.Body)
	current.Blocks[2].Content = "人工稍后补充，必须保留。"
	undone, err := artifact.SafeUndo(base, applied.Body, current)
	if err != nil {
		t.Fatalf("SafeUndo() error = %v", err)
	}
	if undone.Body.Blocks[1].Title != base.Blocks[1].Title || undone.Body.Blocks[1].ClaimOrigin != base.Blocks[1].ClaimOrigin {
		t.Fatalf("touched fields not restored: %#v", undone.Body.Blocks[1])
	}
	if undone.Body.Blocks[2].Content != current.Blocks[2].Content {
		t.Fatalf("later edit lost: %#v", undone.Body.Blocks[2])
	}

	conflicting := cloneBodyForTest(t, applied.Body)
	conflicting.Blocks[1].Title = "又一次人工改名"
	_, err = artifact.SafeUndo(base, applied.Body, conflicting)
	assertArtifactError(t, err, "undo_conflict")

	laterDeleted := cloneBodyForTest(t, applied.Body)
	laterDeleted.Blocks = laterDeleted.Blocks[:2]
	undone, err = artifact.SafeUndo(base, applied.Body, laterDeleted)
	if err != nil {
		t.Fatalf("SafeUndo() after unrelated delete error = %v", err)
	}
	if ids := blockIDs(undone.Body); !reflect.DeepEqual(ids, []string{"root", "install"}) {
		t.Fatalf("unrelated later delete was resurrected: %v", ids)
	}
}

func TestSafeUndoRemovesAddedBlockButRejectsANewDescendant(t *testing.T) {
	base := fixtureBody()
	typeName, title, content := "note", "新增步骤", "检查输出。"
	refs := []artifact.Ref{}
	root, after := "root", "install"
	patch := artifact.Patch{
		SchemaVersion: 1, ArtifactID: "artifact-1", BaseVersionID: "version-6", BaseVersion: 6,
		Basis: artifact.PatchBasisUserInstruction, EvidenceIDs: []string{},
		Operations: []artifact.PatchOperation{{
			Op: artifact.PatchOpInsertBlock, Key: "new-step", ParentID: &root, AfterBlockID: &after,
			Type: &typeName, Title: &title, Content: &content, EvidenceRefs: &refs,
		}},
	}
	applied, err := artifact.EditPatch(base, patch, fixtureAuthorization("root"))
	if err != nil {
		t.Fatal(err)
	}
	addedID := applied.Body.Blocks[2].BlockID
	current := cloneBodyForTest(t, applied.Body)
	current.Blocks[3].Content = "无关的后续人工说明。"
	undone, err := artifact.SafeUndo(base, applied.Body, current)
	if err != nil {
		t.Fatalf("SafeUndo() error = %v", err)
	}
	if ids := blockIDs(undone.Body); !reflect.DeepEqual(ids, []string{"root", "install", "child"}) || undone.Body.Blocks[2].Content != current.Blocks[3].Content {
		t.Fatalf("undo body = %#v", undone.Body)
	}
	laterDeleted := cloneBodyForTest(t, applied.Body)
	laterDeleted.Blocks = laterDeleted.Blocks[:3]
	undone, err = artifact.SafeUndo(base, applied.Body, laterDeleted)
	if err != nil {
		t.Fatalf("SafeUndo() after unrelated delete error = %v", err)
	}
	if ids := blockIDs(undone.Body); !reflect.DeepEqual(ids, []string{"root", "install"}) {
		t.Fatalf("unrelated delete after insert was not preserved: %v", ids)
	}

	withDescendant := cloneBodyForTest(t, applied.Body)
	withDescendant.Blocks = slices.Insert(withDescendant.Blocks, 3, artifact.Block{
		BlockID: "later-child", ParentID: &addedID, Type: "note", Title: "稍后新增", Content: "不能静默删除。", ClaimOrigin: "user", EvidenceRefs: []artifact.Ref{},
	})
	_, err = artifact.SafeUndo(base, applied.Body, withDescendant)
	assertArtifactError(t, err, "undo_conflict")
}

func TestSafeUndoRejectsDeletedStructureWithoutASafeSiblingAnchor(t *testing.T) {
	base := fixtureBody()
	base.Blocks = base.Blocks[:2]
	patch := fixturePatch(artifact.PatchOperation{Op: artifact.PatchOpDeleteSubtree, BlockID: "install", ExpectedHash: artifact.BlockHash(base.Blocks[1])})
	applied, err := artifact.EditPatch(base, patch, fixtureAuthorization("install"))
	if err != nil {
		t.Fatal(err)
	}
	root := "root"
	current := cloneBodyForTest(t, applied.Body)
	current.Blocks = append(current.Blocks, artifact.Block{BlockID: "later", ParentID: &root, Type: "note", Title: "后来新增", Content: "顺序无法安全推断。", ClaimOrigin: "user", EvidenceRefs: []artifact.Ref{}})
	_, err = artifact.SafeUndo(base, applied.Body, current)
	assertArtifactError(t, err, "undo_conflict")
}

func TestSafeUndoRestoresDeletedMovedSplitAndMergedStructure(t *testing.T) {
	t.Run("delete", func(t *testing.T) {
		base := fixtureBody()
		install := "install"
		base.Blocks = slices.Insert(base.Blocks, 2, artifact.Block{BlockID: "command", ParentID: &install, Type: "note", Title: "命令", Content: "运行命令。", ClaimOrigin: "user", EvidenceRefs: []artifact.Ref{}})
		patch := fixturePatch(artifact.PatchOperation{Op: artifact.PatchOpDeleteSubtree, BlockID: "install", ExpectedHash: artifact.BlockHash(base.Blocks[1])})
		applied, err := artifact.EditPatch(base, patch, fixtureAuthorization("install"))
		if err != nil {
			t.Fatal(err)
		}
		current := cloneBodyForTest(t, applied.Body)
		current.Blocks[1].Content = "删除后补充，撤销时保留。"
		assertUndoBody(t, base, applied.Body, current, []string{"root", "install", "command", "child"}, current.Blocks[1].Content)
	})

	t.Run("move", func(t *testing.T) {
		base := fixtureBody()
		root, child := "root", "child"
		patch := fixturePatch(artifact.PatchOperation{Op: artifact.PatchOpMoveSubtree, BlockID: "install", ExpectedHash: artifact.BlockHash(base.Blocks[1]), ParentID: &root, AfterBlockID: &child})
		applied, err := artifact.EditPatch(base, patch, fixtureAuthorization("root"))
		if err != nil {
			t.Fatal(err)
		}
		current := cloneBodyForTest(t, applied.Body)
		current.Blocks[1].Content = "移动后的无关补充。"
		assertUndoBody(t, base, applied.Body, current, []string{"root", "install", "child"}, current.Blocks[1].Content)
	})

	t.Run("split", func(t *testing.T) {
		base := fixtureBody()
		patch := fixturePatch(artifact.PatchOperation{
			Op: artifact.PatchOpSplitBlock, BlockID: "install", ExpectedHash: artifact.BlockHash(base.Blocks[1]),
			Parts: []artifact.SplitPart{
				{Type: "concept", Title: "准备", Content: "准备。", EvidenceRefs: []artifact.Ref{{EvidenceID: "ev-install", Relation: "context"}}},
				{Type: "note", Title: "执行", Content: "执行。", EvidenceRefs: []artifact.Ref{{EvidenceID: "ev-install", Relation: "supports"}}},
			},
		})
		patch.Basis, patch.EvidenceIDs = artifact.PatchBasisEvidenceSupported, []string{"ev-install"}
		applied, err := artifact.EditPatch(base, patch, fixtureAuthorization("install"))
		if err != nil {
			t.Fatal(err)
		}
		current := cloneBodyForTest(t, applied.Body)
		current.Blocks[3].Content = "拆分后的无关补充。"
		assertUndoBody(t, base, applied.Body, current, []string{"root", "install", "child"}, current.Blocks[3].Content)
	})

	t.Run("merge", func(t *testing.T) {
		base := fixtureBody()
		root := "root"
		base.Blocks = slices.Insert(base.Blocks, 2, artifact.Block{BlockID: "verify", ParentID: &root, Type: "note", Title: "验证", Content: "检查。", ClaimOrigin: "user", EvidenceRefs: []artifact.Ref{}})
		patch := fixturePatch(artifact.PatchOperation{
			Op: artifact.PatchOpMergeSiblings, BlockIDs: []string{"install", "verify"},
			ExpectedHashes: []string{artifact.BlockHash(base.Blocks[1]), artifact.BlockHash(base.Blocks[2])},
		})
		applied, err := artifact.EditPatch(base, patch, fixtureAuthorization("root"))
		if err != nil {
			t.Fatal(err)
		}
		current := cloneBodyForTest(t, applied.Body)
		current.Blocks[2].Content = "合并后的无关补充。"
		assertUndoBody(t, base, applied.Body, current, []string{"root", "install", "verify", "child"}, current.Blocks[2].Content)
	})
}

func fixtureAuthorization(selected ...string) artifact.PatchAuthorization {
	return artifact.PatchAuthorization{
		OperationID:        "operation-1",
		ArtifactID:         "artifact-1",
		BaseVersionID:      "version-6",
		BaseVersion:        6,
		SelectedBlockIDs:   selected,
		AllowedEvidenceIDs: map[string]bool{"ev-root": true, "ev-install": true, "ev-child": true},
	}
}

func fixtureBody() artifact.Body {
	root := "root"
	return artifact.Body{
		SchemaVersion: 1,
		Kind:          "study",
		Title:         "教程笔记",
		Blocks: []artifact.Block{
			{BlockID: "root", Type: "section", Title: "课程", Content: "课程概览", ClaimOrigin: "source", EvidenceRefs: []artifact.Ref{{EvidenceID: "ev-root", Relation: "supports"}}},
			{BlockID: "install", ParentID: &root, Type: "concept", Title: "安装", Content: "运行 scoped models 命令。", ClaimOrigin: "source", EvidenceRefs: []artifact.Ref{{EvidenceID: "ev-install", Relation: "supports"}}},
			{BlockID: "child", ParentID: &root, Type: "note", Title: "说明", Content: "保留内容。", ClaimOrigin: "user", EvidenceRefs: []artifact.Ref{{EvidenceID: "ev-child", Relation: "context"}}},
		},
		Warnings: []string{},
	}
}

func nonContiguousTreeBody() artifact.Body {
	a := "a"
	return artifact.Body{
		SchemaVersion: 1,
		Kind:          "study",
		Title:         "兼容旧顺序",
		Blocks: []artifact.Block{
			{BlockID: "a", Type: "section", Title: "A", Content: "A", ClaimOrigin: "user", EvidenceRefs: []artifact.Ref{}},
			{BlockID: "b", Type: "section", Title: "B", Content: "B", ClaimOrigin: "user", EvidenceRefs: []artifact.Ref{}},
			{BlockID: "c", ParentID: &a, Type: "note", Title: "C", Content: "C", ClaimOrigin: "user", EvidenceRefs: []artifact.Ref{}},
			{BlockID: "d", Type: "section", Title: "D", Content: "D", ClaimOrigin: "user", EvidenceRefs: []artifact.Ref{}},
		},
		Warnings: []string{},
	}
}

func assertArtifactError(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want %s", code)
	}
	artifactErr, ok := err.(*artifact.Error)
	if !ok || artifactErr.Code != code {
		t.Fatalf("error = %#v, want %s", err, code)
	}
}

func blockIDs(body artifact.Body) []string {
	ids := make([]string, len(body.Blocks))
	for i := range body.Blocks {
		ids[i] = body.Blocks[i].BlockID
	}
	return ids
}

func cloneBodyForTest(t *testing.T, body artifact.Body) artifact.Body {
	t.Helper()
	var cloned artifact.Body
	if err := artifact.Decode([]byte(artifact.JSON(body)), &cloned); err != nil {
		t.Fatal(err)
	}
	return cloned
}

func fixturePatch(operation artifact.PatchOperation) artifact.Patch {
	return artifact.Patch{
		SchemaVersion: 1, ArtifactID: "artifact-1", BaseVersionID: "version-6", BaseVersion: 6,
		Basis: artifact.PatchBasisUserInstruction, EvidenceIDs: []string{}, Operations: []artifact.PatchOperation{operation},
	}
}

func assertUndoBody(t *testing.T, base, result, current artifact.Body, wantIDs []string, laterChildContent string) {
	t.Helper()
	undone, err := artifact.SafeUndo(base, result, current)
	if err != nil {
		t.Fatalf("SafeUndo() error = %v", err)
	}
	if ids := blockIDs(undone.Body); !reflect.DeepEqual(ids, wantIDs) {
		t.Fatalf("undo ids = %v, want %v", ids, wantIDs)
	}
	for _, block := range undone.Body.Blocks {
		if block.BlockID == "child" && block.Content != laterChildContent {
			t.Fatalf("later child content = %q, want %q", block.Content, laterChildContent)
		}
	}
}
