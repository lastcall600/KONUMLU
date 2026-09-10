package transactions

import (
	"encoding/json"
	"fmt"
	"time"

	"backend/internal/platform/outbox"
	txncontracts "backend/internal/transactions/contracts"
)

const transactionAggregateType = "transaction"

func encodeCompletedEvent(txn Transaction) (outbox.NewEvent, error) {
	if txn.CompletedAt == nil {
		return outbox.NewEvent{}, errInvalidTxn
	}
	payload, err := json.Marshal(txncontracts.CompletedPayload{
		TransactionID:   txn.ID.String(),
		OfferID:         txn.OfferID.String(),
		NeedID:          txn.NeedID.String(),
		RequesterUserID: txn.RequesterUserID.String(),
		CompletedAt:     txn.CompletedAt.UTC().Format(time.RFC3339),
	})
	if err != nil {
		return outbox.NewEvent{}, errUnavailable
	}
	return outbox.NewEvent{
		EventType:      txncontracts.EventTypeCompleted,
		EventVersion:   txncontracts.EventVersion,
		AggregateType:  transactionAggregateType,
		AggregateID:    txn.ID.String(),
		Payload:        payload,
		IdempotencyKey: fmt.Sprintf("%s:%s", txncontracts.EventTypeCompleted, txn.ID.String()),
	}, nil
}
