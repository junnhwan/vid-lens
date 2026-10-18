package jobs

import "context"

// Delivery state is deliberately smaller than Store. A broker retry is needed
// only while durable work still belongs to an unexpired execution.
type deliveryState interface {
	PendingProbe(context.Context, string) (bool, error)
	PendingExport(context.Context, string) (bool, error)
}

func pendingDelivery(ctx context.Context, state deliveryState, kind, id string) error {
	var pending bool
	var err error
	switch kind {
	case "model.probe":
		pending, err = state.PendingProbe(ctx, id)
	case "article.export":
		pending, err = state.PendingExport(ctx, id)
	}
	if err != nil {
		return err
	}
	if pending {
		return errBusy
	}
	return nil
}
