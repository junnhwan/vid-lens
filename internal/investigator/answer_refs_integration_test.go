package investigator

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cloudwego/eino/schema"
	"vidlens/internal/domain"
	"vidlens/internal/testsupport"
)

func TestCompactAnswerResolvesOwnedReferencesBeforePublication(t *testing.T) {
	for _, mode := range []string{"canonical_alias", "unknown_alias", "forged_video_metadata"} {
		t.Run(mode, func(t *testing.T) {
			db, res, cfg := testsupport.Services(t)
			ctx := context.Background()
			turn, claim := testsupport.Question(t, db)
			snap, _ := db.Snapshot(ctx, turn.SnapshotID)
			transcript, _ := db.Transcript(ctx, turn.SnapshotID)
			article, _ := db.PublishedArticle(ctx, turn.ArticleID)
			spec, _ := db.GenerationForRun(ctx, claim.Run.ID)
			s := &session{engine: &Engine{Store: db, Resources: res, Config: cfg}, claim: claim, profile: cfg.Profile, spec: spec, snapshot: snap, transcript: transcript, turn: &turn, article: &article, cp: checkpoint{SchemaVersion: 1, HarnessRevision: 6, Inspections: map[string][]string{}, Repeated: map[string]int{}}}
			if _, err := s.makeTools(); err != nil {
				t.Fatal(err)
			}
			ref := "t1"
			if mode == "unknown_alias" {
				ref = "t99999"
			}
			args := map[string]any{"kind": "grounded", "summary": "当前来源给出的事实。", "blocks": []domain.Block{{ID: "p", Type: "paragraph", Origin: "video", Content: []domain.Inline{{Type: "text", Text: "来源实际提供了fixture text。"}, {Type: "citation", CitationID: ref}}}}, "limitations": []domain.Limitation{}}
			if mode == "forged_video_metadata" {
				args["citations"] = map[string]domain.Citation{"t1": {ID: "t1", Kind: "video_text", SnapshotID: "foreign", Range: &domain.Range{StartMS: 0, EndMS: 1000}}}
			}
			_, err := s.tools["submit_answer"].InvokableRun(context.WithValue(ctx, callIDKey{}, "compact-answer"), &schema.ToolArgument{Text: string(domain.JSON(args))})
			if mode != "canonical_alias" {
				if err == nil {
					t.Fatal("unowned or model-supplied video metadata accepted")
				}
				if _, err = db.Operation(ctx, claim.Run.ID, "candidate"); err == nil {
					t.Fatal("invalid references entered publish slot")
				}
				return
			}
			if err != nil {
				t.Fatalf("short canonical references rejected: %v", err)
			}
			op, err := db.Operation(ctx, claim.Run.ID, "candidate")
			var answer domain.Answer
			if err != nil || json.Unmarshal(op.Result, &answer) != nil {
				t.Fatal("answer candidate missing", err)
			}
			c := answer.Citations["t1"]
			if c.SnapshotID != snap.ID || len(c.SegmentIDs) != 1 || c.SegmentIDs[0] != transcript[0].ID || c.Range == nil {
				t.Fatal("reference identity was not reconstructed from source", c)
			}
			if err = s.publish(ctx, op.Result); err != nil {
				t.Fatal(err)
			}
			unchanged, err := db.PublishedArticle(ctx, article.ID)
			if err != nil || string(domain.JSON(unchanged)) != string(domain.JSON(article)) {
				t.Fatal("answer edited published article", err)
			}
		})
	}
}
