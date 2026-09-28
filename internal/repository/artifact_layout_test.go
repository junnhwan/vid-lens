package repository

import (
	"context"
	"encoding/json"
	"testing"

	"vid-lens/internal/artifact"
)

func TestCanvasLayoutRevisionAndVersionIsolation(t *testing.T) {
	fx := newArtifactEditFixture(t)
	ctx := context.Background()
	layout := artifact.DefaultCanvasLayout()
	layout.Nodes["install"] = artifact.CanvasNode{Position: artifact.CanvasPoint{X: 325, Y: 90}, Width: 254, Height: 132, Pinned: true, Style: "auto"}
	first, err := fx.repo.SaveCanvasLayout(ctx, fx.owner, fx.artifact.ID, fx.base.ID, 0, "canvas-key-1", layout)
	if err != nil {
		t.Fatal(err)
	}
	if first.Revision != 1 {
		t.Fatalf("revision = %d", first.Revision)
	}
	replay, err := fx.repo.SaveCanvasLayout(ctx, fx.owner, fx.artifact.ID, fx.base.ID, 0, "canvas-key-1", layout)
	if err != nil || replay.Revision != 1 {
		t.Fatalf("replay = %+v, %v", replay, err)
	}
	changed := layout
	changed.Direction = "DOWN"
	if _, err := fx.repo.SaveCanvasLayout(ctx, fx.owner, fx.artifact.ID, fx.base.ID, 0, "canvas-key-1", changed); artifactErrorCodeForTest(err) != "idempotency_conflict" {
		t.Fatalf("key reuse = %v", err)
	}
	if _, err := fx.repo.SaveCanvasLayout(ctx, fx.owner, fx.artifact.ID, fx.base.ID, 0, "canvas-key-2", changed); artifactErrorCodeForTest(err) != "version_conflict" {
		t.Fatalf("stale revision = %v", err)
	}
	read, err := fx.repo.CanvasLayout(ctx, fx.owner, fx.artifact.ID, fx.base.ID, 0)
	if err != nil || read.Revision != 1 || !read.Layout.Nodes["install"].Pinned {
		t.Fatalf("read = %+v, %v", read, err)
	}
	if _, err := fx.repo.CanvasLayout(ctx, fx.owner+1, fx.artifact.ID, fx.base.ID, 0); err == nil {
		t.Fatal("other owner read layout")
	}
	var body artifact.Body
	if err := json.Unmarshal([]byte(fx.base.BodyJSON), &body); err != nil {
		t.Fatal(err)
	}
	body.Blocks[0].Title = "安装步骤"
	body.Blocks = append(body.Blocks, artifact.Block{BlockID: "new-step", Type: "note", Title: "新步骤", ClaimOrigin: "user", EvidenceRefs: []artifact.Ref{}})
	if err := fx.repo.Save(ctx, fx.owner, fx.artifact.ID, fx.artifact.HeadVersion, &body, ""); err != nil {
		t.Fatal(err)
	}
	_, next, err := fx.repo.Get(ctx, fx.owner, fx.artifact.ID)
	if err != nil {
		t.Fatal(err)
	}
	inherited, err := fx.repo.CanvasLayout(ctx, fx.owner, fx.artifact.ID, next.ID, 0)
	if err != nil || inherited.Revision != 0 || !inherited.Layout.Nodes["install"].Pinned || inherited.Layout.Nodes["new-step"].Width != 0 {
		t.Fatalf("inherited = %+v, %v", inherited, err)
	}
	if _, err := fx.repo.SaveCanvasLayout(ctx, fx.owner, fx.artifact.ID, fx.base.ID, 1, "canvas-old", layout); artifactErrorCodeForTest(err) != "version_conflict" {
		t.Fatalf("old version write = %v", err)
	}
	if _, err := fx.repo.CanvasLayout(ctx, fx.owner, fx.artifact.ID, fx.base.ID, 1); err != nil {
		t.Fatalf("old layout unreadable: %v", err)
	}
}
