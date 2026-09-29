package service

import (
	"testing"

	"vid-lens/internal/artifact"
)

func TestSummaryTextPatchIsScopedAndReversible(t *testing.T) {
	base := "安装章节：旧名称。\n引用：保留原话。\n结尾。"
	patch := SummaryTextPatch{BaseHash: artifact.Hash(base), Edits: []SummaryTextEdit{{OldText: "安装章节：旧名称。", NewText: "安装章节：新名称。"}}}
	changed, err := applySummaryTextPatch(base, patch)
	if err != nil || changed != "安装章节：新名称。\n引用：保留原话。\n结尾。" {
		t.Fatalf("scoped patch = %q, %v", changed, err)
	}
	undone, err := reverseSummaryTextPatch(changed, changed, base, patch)
	if err != nil || undone != base {
		t.Fatalf("immediate undo = %q, %v", undone, err)
	}
	later := changed + "\n用户的后续补充。"
	undone, err = reverseSummaryTextPatch(later, changed, base, patch)
	if err != nil || undone != base+"\n用户的后续补充。" {
		t.Fatalf("later edit lost on undo: %q, %v", undone, err)
	}
}

func TestSummaryTextPatchRejectsAmbiguousAndOverlappingAnchors(t *testing.T) {
	base := "旧名称、旧名称；保留原话"
	for _, patch := range []SummaryTextPatch{
		{BaseHash: artifact.Hash(base), Edits: []SummaryTextEdit{{OldText: "旧名称", NewText: "新名称"}}},
		{BaseHash: artifact.Hash(base), Edits: []SummaryTextEdit{{OldText: "旧名称、旧名称", NewText: "新名称"}, {OldText: "旧名称；保留", NewText: "新名称；保留"}}},
		{BaseHash: "wrong", Edits: []SummaryTextEdit{{OldText: "保留原话", NewText: "改写原话"}}},
	} {
		if _, err := applySummaryTextPatch(base, patch); err == nil {
			t.Fatalf("accepted unsafe patch: %+v", patch)
		}
	}
}

func TestSummaryTextPatchUndoConflictsWhenLaterEditChangesReplacement(t *testing.T) {
	base := "原名。"
	patch := SummaryTextPatch{BaseHash: artifact.Hash(base), Edits: []SummaryTextEdit{{OldText: "原名", NewText: "新名"}}}
	changed, err := applySummaryTextPatch(base, patch)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = reverseSummaryTextPatch("再次改名。", changed, base, patch); err == nil {
		t.Fatal("undo overwrote a later edit")
	}
}

func TestSummaryTextPatchPreservesQuotedAndCodeEvidence(t *testing.T) {
	base := "叙述：旧名。\n引文：“旧名”。\n> 原话旧名\n```sh\n旧名 --flag\n```\n"
	for _, anchor := range []string{"“旧名”", "> 原话旧名", "旧名 --flag"} {
		patch := SummaryTextPatch{BaseHash: artifact.Hash(base), Edits: []SummaryTextEdit{{OldText: anchor, NewText: "新名"}}}
		if _, err := applySummaryTextPatch(base, patch); err == nil {
			t.Fatalf("protected quotation/code was changed: %q", anchor)
		}
	}
	patch := SummaryTextPatch{BaseHash: artifact.Hash(base), Edits: []SummaryTextEdit{{OldText: "叙述：旧名。", NewText: "叙述：新名。"}}}
	if _, err := applySummaryTextPatch(base, patch); err != nil {
		t.Fatalf("narration was blocked: %v", err)
	}
}

func TestSummaryTextPatchAllowsUnchangedQuotesInUniqueContext(t *testing.T) {
	base := "Jeff通过“禁创作推理+确定性路由”实现自动化。另一个章节也提到Jeff。"
	patch := SummaryTextPatch{BaseHash: artifact.Hash(base), Edits: []SummaryTextEdit{{OldText: "Jeff通过“禁创作推理+确定性路由”实现自动化。", NewText: "Jev通过“禁创作推理+确定性路由”实现自动化。"}}}
	result, err := applySummaryTextPatch(base, patch)
	if err != nil || result != "Jev通过“禁创作推理+确定性路由”实现自动化。另一个章节也提到Jeff。" {
		t.Fatalf("unchanged quotation in contextual anchor was blocked: %q, %v", result, err)
	}
	for _, next := range []string{
		"Jev通过“禁推理+确定性路由”实现自动化。",
		"Jeff通过“禁创作新增推理+确定性路由”实现自动化。",
	} {
		patch.Edits[0].NewText = next
		if _, err := applySummaryTextPatch(base, patch); err == nil {
			t.Fatalf("quote modification accepted: %q", next)
		}
	}
}

func TestSummaryTextPatchRejectsStaleOrProtectedExplicitPositions(t *testing.T) {
	base := "Jeff。Jeff。“Jeff”"
	for _, position := range []int{-1, 1, len(base), len("Jeff。Jeff。“")} {
		start := position
		patch := SummaryTextPatch{BaseHash: artifact.Hash(base), Edits: []SummaryTextEdit{{OldText: "Jeff", NewText: "Jev", Start: &start}}}
		if _, err := applySummaryTextPatch(base, patch); err == nil {
			t.Fatalf("invalid or protected position %d accepted", position)
		}
	}
	var patch SummaryTextPatch
	if err := decodeSummaryPatch(`{"base_hash":"x","edits":[{"old_text":"Jeff","new_text":"Jev","start":0}]}`, &patch); err == nil {
		t.Fatal("model-supplied position accepted outside the server anchor map")
	}
}

func TestSummaryPatchDecoderAcceptsOnlyStrictObjectOrWholeJSONFence(t *testing.T) {
	var patch SummaryTextPatch
	if err := decodeSummaryPatch("```json\n{\"base_hash\":\"x\",\"edits\":[]}\n```", &patch); err != nil || patch.BaseHash != "x" {
		t.Fatalf("whole JSON fence = %+v, %v", patch, err)
	}
	for _, raw := range []string{"before {\"base_hash\":\"x\",\"edits\":[]}", "```javascript\n{}\n```", "{\"base_hash\":\"x\",\"edits\":[],\"unknown\":true}"} {
		if err := decodeSummaryPatch(raw, &patch); err == nil {
			t.Fatalf("unsafe JSON wrapper accepted: %q", raw)
		}
	}
}
