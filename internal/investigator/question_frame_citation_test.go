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
	"slices"
	"strings"
	"testing"

	"vidlens/internal/domain"
	"vidlens/internal/media"
)

func TestQuestionContextPixelsRemainCitableAfterInitialization(t *testing.T) {
	s := planSession(t)
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
	id := domain.ID()
	if err = os.WriteFile(filepath.Join(s.engine.Config.DataDir, id+".jpg"), pixels.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	asset := domain.Asset{ID: id, SnapshotID: &s.snapshot.ID, SourceID: s.claim.Run.SourceID, RunID: s.claim.Run.ID,
		Kind: "frame", StorageKey: id + ".jpg", SHA256: hex.EncodeToString(hash[:]), MIME: "image/jpeg", Bytes: int64(pixels.Len()),
		TimeMS: domain.Ptr(int64(500)), RequestedTimeMS: domain.Ptr(int64(500))}
	if err = s.engine.Store.AddAsset(ctx, s.claim, asset); err != nil {
		t.Fatal(err)
	}
	s.turn = &domain.Turn{Context: domain.QuestionContext{FrameID: &id}, Question: "这张图能证实什么？"}
	s.article = &domain.Article{Sections: []domain.Section{}}
	p, _ := domain.InterpretationPolicy("standard")
	p.Revision = recoveryPolicy // Published under the preceding article protocol.
	s.spec.Version, s.spec.DetailPolicy = 2, &p
	s.cp = checkpoint{SchemaVersion: 1, Inspections: map[string][]string{}, Repeated: map[string]int{}}
	if err = s.initial(ctx); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(s.cp.Inspections["question-context"], id) {
		t.Fatal("question did not receive the real source pixels")
	}
	if s.cp.HarnessRevision != 14 || s.spec.DetailPolicy.Revision != recoveryPolicy {
		t.Fatal("new question inherited old article protocol or rewrote the immutable article policy")
	}
	if _, err = s.makeTools(); err != nil {
		t.Fatal(err)
	}
	// Delivery alone must not make a frame evidence: the actual observation is
	// still required before it appears in the formal submission catalog.
	catalog, err := s.citationCatalog(ctx)
	if err != nil || catalog["f1"].FrameID != "" {
		t.Fatal("unobserved pixels became citable", catalog, err)
	}
	if _, err = planCall(s, "question-frame-observation", "record_observation", pacedObservation{ObservationFields: observation{
		InspectionID: "question-context", FrameIDs: []string{id}, VisibleFacts: []string{"黑色测试画面。"}, Status: "supported"}}); err != nil {
		t.Fatal(err)
	}
	catalog, err = s.citationCatalog(ctx)
	if err != nil || catalog["f1"].FrameID != id || catalog["f1"].TimeMS == nil || *catalog["f1"].TimeMS != 500 {
		t.Fatalf("observed question pixels lost their formal source reference: f1=%v %v", catalog["f1"], err)
	}
	resolved, err := resolveAnswerReferences(compactAnswerInput{Kind: "grounded", Summary: "这张图是黑色画面。",
		Blocks: []domain.Block{{ID: "visible", Type: "paragraph", Origin: "video", Content: []domain.Inline{
			{Type: "text", Text: "可见黑色画面。"}, {Type: "citation", CitationID: "f1"}}}}}, catalog)
	if err != nil || resolved.Citations["f1"].FrameID != id || resolved.Citations["f1"].SnapshotID != s.snapshot.ID {
		t.Fatal("formal answer cannot trace its visual statement to the delivered pixels", resolved, err)
	}
	var request string
	if err = s.engine.Store.Pool.QueryRow(ctx, "SELECT id FROM generation_requests WHERE active_run_id=$1", s.claim.Run.ID).Scan(&request); err != nil {
		t.Fatal(err)
	}
	s.turn.RequestID = request
	input := composedAnswerInput{Kind: "grounded", Summary: "这张图是黑色画面。", Blocks: []domain.Block{{ID: "visible", Type: "paragraph", Origin: "video", Content: []domain.Inline{
		{Type: "text", Text: "可见黑色画面。"}, {Type: "citation", CitationID: "t1"}}}}}
	feedback, err := planCall(s, "text-cannot-support-pixels", "submit_answer", input)
	if err == nil || !strings.Contains(string(domain.JSON(s.cp.LastCandidate)), "question_frame_uncited") {
		t.Fatal("visual answer accepted a subtitle in place of the supplied pixels", feedback, err)
	}
	input.Blocks[0].Content[1].CitationID = "f1"
	feedback, err = planCall(s, "cite-the-observed-pixels", "submit_answer", input)
	if err != nil || feedback["accepted_for_publication"] != true {
		t.Fatal("correcting the source reference did not repair the same answer", feedback, err)
	}
	op, err := s.engine.Store.Operation(ctx, s.claim.Run.ID, "candidate")
	if err != nil || !strings.Contains(string(op.Result), id) || !strings.Contains(string(op.Result), `"frames"`) {
		t.Fatal("accepted answer did not durably retain the actual image reference", err)
	}
}
