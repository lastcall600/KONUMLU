package payments

import "context"

// Gateway is a provider-neutral payment-processor port.
// Domain code must not import a vendor SDK. No production adapter is wired in V1 foundation.
type Gateway interface {
	CreateIntent(ctx context.Context, in IntentCommand) (ProviderAck, error)
	Authorize(ctx context.Context, in PaymentCommand) (ProviderAck, error)
	Capture(ctx context.Context, in PaymentCommand) (ProviderAck, error)
	Cancel(ctx context.Context, in PaymentCommand) (ProviderAck, error)
}

type IntentCommand struct {
	PaymentID ID
	Amount    string
	Currency  string
}

type PaymentCommand struct {
	PaymentID         ID
	ProviderReference string
}

type ProviderAck struct {
	Provider          string
	ProviderReference string
}

// ProviderSignal is an internal/test callback from a future PSP adapter.
// It must not include PAN, CVV, or other payment-instrument secrets.
type ProviderSignal struct {
	Provider          *string
	ProviderReference *string
}
