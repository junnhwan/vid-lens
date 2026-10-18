package store

import (
	"context"
	"testing"

	"vidlens/internal/domain"
)

func TestOperationalReadsUseDurableWorkAndPreserveStatusProjection(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	owner := session(t, s)
	request, claim := prepared(t, s, owner)
	for _, kind := range []string{"field_repair", "draft_correction:draft:1", "inspect"} {
		if _, err := s.BeginOperation(ctx, claim, domain.ID(), kind, map[string]any{}, false); err != nil {
			t.Fatal(err)
		}
	}
	if count, err := s.OperationCount(ctx, claim.Run.ID, "field_repair", "draft_correction:draft:1"); err != nil || count != 2 {
		t.Fatalf("repair ledger count=%d err=%v", count, err)
	}
	if count, err := s.OperationCount(ctx, "another-run", "field_repair"); err != nil || count != 0 {
		t.Fatalf("cross-run ledger count=%d err=%v", count, err)
	}
	status, err := s.RuntimeStatus(ctx)
	if err != nil || status.SchemaVersion != 1 || status.Runs["running"] != 1 || status.Storage.MediaBytes != 42 {
		t.Fatalf("status projection changed: %+v err=%v", status, err)
	}
	usage, err := s.SearchUsage(ctx)
	if err != nil || usage != (SearchUsage{}) {
		t.Fatalf("empty usage changed: %+v err=%v", usage, err)
	}
	if err := s.Publish(ctx, claim, domain.Article{SnapshotID: *request.SnapshotID, Title: "operational reads"}); err != nil {
		t.Fatal(err)
	}
	export, _, err := s.CreateExport(ctx, owner, request.ID, "markdown", domain.ID())
	if err != nil {
		t.Fatal(err)
	}
	if pending, err := s.PendingExport(ctx, export.ID); err != nil || !pending {
		t.Fatalf("queued export is not pending: %v %v", pending, err)
	}
	if _, err := s.Pool.Exec(ctx, "UPDATE article_exports SET deadline_at=now()-interval '1 second' WHERE id=$1", export.ID); err != nil {
		t.Fatal(err)
	}
	if pending, err := s.PendingExport(ctx, export.ID); err != nil || pending {
		t.Fatalf("expired export remains pending: %v %v", pending, err)
	}
	model := enabledModel(t, s, "operational")
	probe, err := s.CreateProbe(ctx, model.ID, model.RevisionID, "capabilities", domain.ID())
	if err != nil {
		t.Fatal(err)
	}
	if pending, err := s.PendingProbe(ctx, probe.ID); err != nil || !pending {
		t.Fatalf("queued probe is not pending: %v %v", pending, err)
	}
	if _, err := s.Pool.Exec(ctx, "UPDATE model_probes SET deadline_at=now()-interval '1 second' WHERE id=$1", probe.ID); err != nil {
		t.Fatal(err)
	}
	if pending, err := s.PendingProbe(ctx, probe.ID); err != nil || pending {
		t.Fatalf("expired probe remains pending: %v %v", pending, err)
	}
}
