package contracts

import "context"

const (
	FulfillmentFulfilled        = "fulfilled"
	FulfillmentAlreadyFulfilled = "already_fulfilled"
	FulfillmentNotFulfillable   = "not_fulfillable"
)

// TransactionFulfillmentCommand is a Transactions-owned completion signal.
// RequesterUserID must come from the Transaction record, never from a client body.
type TransactionFulfillmentCommand struct {
	NeedID          ID
	RequesterUserID ID
	TransactionID   ID
}

// FulfillmentResult is the explicit Need outcome for a completed Transaction.
// It is not a distributed commit: Transaction completion is independent.
type FulfillmentResult struct {
	Outcome string
	Status  string
}

// TransactionFulfillment is the Needs-owned write surface for transaction-driven fulfillment.
// Callers must not import needs implementation or write needs tables.
type TransactionFulfillment interface {
	FulfillFromCompletedTransaction(ctx context.Context, cmd TransactionFulfillmentCommand) (FulfillmentResult, error)
}
