package media

import (
	"context"

	"backend/internal/platform/outbox"
)

// ProcessHandler consumes media.image.process V1 outbox events.
type ProcessHandler struct {
	svc *Service
}

func NewProcessHandler(svc *Service) (*ProcessHandler, error) {
	if svc == nil {
		return nil, errStoreRequired
	}
	return &ProcessHandler{svc: svc}, nil
}

func (h *ProcessHandler) Handle(ctx context.Context, event outbox.Event) error {
	if h == nil || h.svc == nil {
		return errStoreRequired
	}
	if event.EventType != ProcessEventType || event.EventVersion != ProcessEventVersion {
		return errInvalidAsset
	}
	id, err := decodeProcessPayload(event.Payload)
	if err != nil {
		return err
	}
	if err := h.svc.ProcessAsset(ctx, id); err != nil {
		return mapStoreErr(err)
	}
	return nil
}

var _ outbox.Handler = (*ProcessHandler)(nil)
