package notifications

import "context"

type deliveryStore interface {
	UpsertDelivery(ctx context.Context, delivery Delivery) (Delivery, error)
	SaveDelivery(ctx context.Context, delivery Delivery) (Delivery, error)
	UpsertWarning(ctx context.Context, row WarningRecord) (WarningRecord, error)
}

// DeliveryPersister is the store surface the worker composition root injects.
type DeliveryPersister = deliveryStore
