package jobs

import (
	"context"
	"errors"
	"testing"
)

type deliveryFixture struct {
	pending bool
	err     error
	id      string
}

func (f *deliveryFixture) PendingProbe(_ context.Context, id string) (bool, error) {
	f.id = id
	return f.pending, f.err
}
func (f *deliveryFixture) PendingExport(_ context.Context, id string) (bool, error) {
	f.id = id
	return f.pending, f.err
}

func TestDeliveryRetriesOnlyWhileDurableWorkIsPending(t *testing.T) {
	for _, kind := range []string{"model.probe", "article.export"} {
		f := &deliveryFixture{pending: true}
		if err := pendingDelivery(context.Background(), f, kind, "work"); !errors.Is(err, errBusy) || f.id != "work" {
			t.Fatalf("pending delivery lost ownership: %v", err)
		}
		f.pending = false
		if err := pendingDelivery(context.Background(), f, kind, "work"); err != nil {
			t.Fatal("completed delivery retried", err)
		}
		f.err = errors.New("ledger unavailable")
		if err := pendingDelivery(context.Background(), f, kind, "work"); err != f.err {
			t.Fatal("ledger failure was swallowed", err)
		}
	}
}
