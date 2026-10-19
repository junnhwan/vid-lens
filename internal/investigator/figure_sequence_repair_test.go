package investigator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vidlens/internal/domain"
	"vidlens/internal/media"
)

func TestFigureSequenceFeedbackRepairsOnlyRetainedFrameOrder(t *testing.T) {
	s := planSession(t)
	s.cp.HarnessRevision = 13
	ctx := context.Background()
	ms, err := media.New(s.engine.Store, s.engine.Resources, s.engine.Config)
	if err != nil {
		t.Fatal(err)
	}
	s.engine.Media = ms
	var pixels bytes.Buffer
	if err = jpeg.Encode(&pixels, image.NewRGBA(image.Rect(0, 0, 4, 4)), nil); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(pixels.Bytes())
	ids := []string{}
	for _, at := range []int64{1500, 500} {
		id := domain.ID()
		if err = os.WriteFile(filepath.Join(s.engine.Config.DataDir, id+".jpg"), pixels.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		asset := domain.Asset{ID: id, SnapshotID: &s.snapshot.ID, SourceID: s.claim.Run.SourceID, RunID: s.claim.Run.ID,
			Kind: "frame", StorageKey: id + ".jpg", SHA256: hex.EncodeToString(hash[:]), MIME: "image/jpeg",
			Bytes: int64(pixels.Len()), TimeMS: domain.Ptr(at)}
		if err = s.engine.Store.AddAsset(ctx, s.claim, asset); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	s.cp.OverviewIDs = ids
	s.cp.CitationFrames = append([]string{}, ids...)
	if err = s.engine.Store.RecordObservation(ctx, s.claim, "sequence-observation", "overview",
		observation{InspectionID: "overview", FrameIDs: ids, VisibleFacts: []string{"两个实际黑色测试画面。"}, Status: "supported"}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.makeTools(); err != nil {
		t.Fatal(err)
	}
	plan := thematicPlanFixture()
	if body, err := planCall(s, "sequence-plan", "begin_article", plan); err != nil || body["version"] != float64(1) {
		t.Fatal(body, err)
	}
	catalog, err := s.citationCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	late, early := "", ""
	for alias, citation := range catalog {
		if citation.FrameID == ids[0] {
			late = alias
		}
		if citation.FrameID == ids[1] {
			early = alias
		}
	}
	if late == "" || early == "" {
		t.Fatal("observed frame aliases missing")
	}
	raw := string(domain.JSON(map[string]any{"activity_title": "按来源顺序配图", "version": 1, "sections": []any{
		map[string]any{"id": "start", "blocks": []any{map[string]any{"type": "paragraph", "origin": "video", "text": "开始事实已保存。", "citation_ids": []string{"t1"}}}},
		map[string]any{"id": "finish", "blocks": []any{map[string]any{"type": "figure", "origin": "video", "frame_ids": []string{late, early}, "caption": "两个来源时刻的对照"}}},
	}}))
	body, err := planCall(s, "sequence-body", "save_article_sections", raw)
	path := "/sections/1/blocks/0/frame_ids"
	feedback := string(domain.JSON(body))
	if err != nil || body["saved_section_count"] != float64(1) || !strings.Contains(feedback, path) || !strings.Contains(feedback, "figure_sequence") {
		t.Fatalf("sequence feedback must identify the actual editable frame list: %s %v", feedback, err)
	}
	candidate := s.cp.LastCandidate
	patch := authoringRepairInput{CandidateID: candidate["candidate_id"].(string), ExpectedHash: candidate["expected_hash"].(string), Version: 2,
		Patches: []authoringFieldPatch{{Op: "replace", Path: path, Value: domain.JSON([]string{early, late})}}}
	fixed, err := planCall(s, "sequence-repair", "repair_article_sections", patch)
	if err != nil || fixed["version"] != float64(3) || fixed["saved_section_count"] != float64(2) {
		t.Fatal("reordering the real source frames did not repair the retained block", fixed, err)
	}
	_, draft, err := s.loadDraft(ctx)
	figure := draft.Sections["finish"].Blocks[0]
	if err != nil || figure.Caption != "两个来源时刻的对照" || figure.FrameIDs[0] != ids[1] || figure.FrameIDs[1] != ids[0] || domain.BlockText(draft.Sections["start"].Blocks[0]) != "开始事实已保存。" {
		t.Fatal("repair changed facts, caption, accepted prose, or real frame identity", err)
	}
}
