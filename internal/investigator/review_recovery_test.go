package investigator

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"vidlens/internal/domain"
)

func TestReviewRecoveryFieldCatalogUsesOnlyExistingEditablePointers(t *testing.T) {
	candidate := map[string]any{"title": "标题", "snapshot_id": "private-server-identity", "sections": []any{map[string]any{"id": "actual-chapter", "blocks": []any{
		map[string]any{"id": "server-block", "type": "list", "origin": "background_explanation", "items": []any{map[string]any{"content": []any{map[string]any{"type": "text", "text": "嵌套列表事实"}, map[string]any{"type": "citation", "citation_id": "t1"}}}}},
		map[string]any{"type": "figure", "frame_ids": []string{"server-frame"}, "caption": "真实图注"},
	}}}, "limitations": []any{map[string]any{"code": "visual_unclear", "message": "具体的来源局限"}}}
	before := domain.JSON(candidate)
	fields := reviewFields(candidate, evidenceReviewBatch{Mode: "source_text"})
	if len(fields) != 4 || fields["/sections/0/blocks/0/items/0/content/0/text"] != "嵌套列表事实" || fields["/sections/0/blocks/1/caption"] != "真实图注" {
		t.Fatal("nested editable fields were omitted or reindexed", fields)
	}
	for path := range fields {
		if _, err := jsonField(before, path); err != nil || strings.Contains(path, "frame_ids") || strings.HasSuffix(path, "/id") {
			t.Fatal("review target points outside existing prose", path, err)
		}
	}
	visual := reviewFields(candidate, evidenceReviewBatch{Mode: "visual", Fields: map[string]any{"/sections/0/blocks/1": true}})
	if len(visual) != 1 || visual["/sections/0/blocks/1/caption"] == nil || !bytes.Equal(before, domain.JSON(candidate)) {
		t.Fatal("visual target catalog escaped its batch or mutated the candidate", visual)
	}
	structural := domain.JSON(reviewCandidateContext(candidate))
	if strings.Contains(string(structural), "嵌套列表事实") || !strings.Contains(string(structural), "background_explanation") || !strings.Contains(string(structural), `"citation_id":"t1"`) || !bytes.Equal(before, domain.JSON(candidate)) {
		t.Fatal("flattened review lost origins or citation relationships, duplicated prose, or mutated the candidate", string(structural))
	}
	s := &session{cp: checkpoint{HarnessRevision: 17}}
	if _, err := s.makeTools(); err != nil {
		t.Fatal(err)
	}
	info, err := s.tools["report_evidence_review"].Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	next, err := reviewFieldTool(info, fields)
	if err != nil {
		t.Fatal(err)
	}
	sc, err := next.ParamsOneOf.ToJSONSchema()
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err = json.Unmarshal(domain.JSON(sc), &root); err != nil {
		t.Fatal(err)
	}
	props := root["properties"].(map[string]any)
	if props["candidate_hash"] != nil || props["verdict"] != nil {
		t.Fatal("reviewer was asked to generate redundant server identity or verdict fields", props)
	}
	pathSchema := props["issues"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)["field"].(map[string]any)
	if len(pathSchema["enum"].([]any)) != len(fields) {
		t.Fatal("review tool did not constrain its field selection", pathSchema)
	}
}

func TestReviewRecoveryNewTargetedReadCountsOnce(t *testing.T) {
	s := reviewRecoverySession(t)
	draft := saveReviewRecoveryDraft(t, s, recoveryBody)
	s.cp.EvidenceReviewIssue = map[string]any{"code": "semantic_evidence", "affected_section_ids": []string{"finish"}}
	s.cp.Progress.Stalled = 4
	result, err := planCall(s, "first-targeted-read", "read_article_sections", map[string]any{"section_ids": []string{"finish"}})
	if err != nil || result["error"] != nil || s.cp.Progress.Stalled != 0 {
		t.Fatal("first relevant chapter read was counted as a stall", result, s.cp.Progress, err)
	}
	if _, err = planCall(s, "same-targeted-read", "read_article_sections", map[string]any{"section_ids": []string{"finish"}}); err != nil || s.cp.Progress.Stalled != 1 {
		t.Fatal("duplicate chapter read reset progress", s.cp.Progress, err)
	}
	_, after, err := s.loadDraft(context.Background())
	if err != nil || draftContentHash(after) != draftContentHash(draft) {
		t.Fatal("read changed authoritative chapter content", err)
	}
}

func TestReviewRecoveryEveryFieldGetsTheEvidenceItNeeds(t *testing.T) {
	candidate := map[string]any{"title": "来源标题", "sections": []any{map[string]any{"id": "code", "title": "代码与口述", "blocks": []any{
		map[string]any{"type": "paragraph", "origin": "video", "content": []any{map[string]any{"type": "text", "text": "原文的条件。"}, map[string]any{"type": "citation", "citation_id": "t1"}}},
		map[string]any{"type": "paragraph", "origin": "video", "content": []any{map[string]any{"type": "text", "text": "代码与原文共同说明的事实。"}, map[string]any{"type": "citation", "citation_id": "f1"}, map[string]any{"type": "citation", "citation_id": "t1"}}},
		map[string]any{"type": "figure", "frame_ids": []string{"frame-actual"}, "caption": "画面中的可见代码。"},
	}}}, "limitations": []any{map[string]any{"code": "not_observed", "message": "画面没有解释原因。", "citation_ids": []string{"f1"}}}}
	catalog := map[string]domain.Citation{"t1": {Kind: "video_text"}, "f1": {Kind: "video_frame", FrameID: "frame-actual"}}
	batches := recoveryReviewBatches(candidate, catalog, []domain.Asset{{ID: "frame-actual"}})
	if len(batches) != 2 || !batches[0].containsPath("/sections/0/blocks/0/content/0/text") || batches[0].containsPath("/sections/0/blocks/1/content/0/text") || batches[0].containsPath("/sections/0/blocks/2/caption") || batches[0].containsPath("/limitations/0/message") {
		t.Fatal("a text-only review received claims whose primary source is a real frame", batches)
	}
	all := reviewFields(candidate, evidenceReviewBatch{Mode: "source_text"})
	covered := map[string]int{}
	for _, batch := range batches {
		for path := range reviewFields(candidate, batch) {
			covered[path]++
		}
	}
	for path := range all {
		if covered[path] != 1 {
			t.Fatal("a prose field escaped independent review or was reviewed in incompatible contexts", path, covered)
		}
	}
}

func TestReviewRecoveryWholePointArrayRequiresThatExactCountIssue(t *testing.T) {
	s := &session{cp: checkpoint{HarnessRevision: 17, DraftIssue: map[string]any{"issues": []map[string]any{{"code": "point_count_exceeded", "path": "/sections/2/key_points"}}}}}
	for _, path := range [][]string{{"sections", "1", "key_points"}, {"sections", "2", "evidence_ids"}, {"sections"}, {"sections", "2", "id"}} {
		if s.planCountRepairAllowed(path) {
			t.Fatal("count feedback opened an unrelated plan field", path)
		}
	}
	if !s.planCountRepairAllowed([]string{"sections", "2", "key_points"}) {
		t.Fatal("the exact validator-selected array remained unrepairable")
	}
	s.cp.HarnessRevision = 16
	if s.planCountRepairAllowed([]string{"sections", "2", "key_points"}) {
		t.Fatal("a historical plan acquired new mutation rules")
	}
}
