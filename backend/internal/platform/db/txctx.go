package db

import "context"

type txCtxKey struct{}

// WithTx attaches a PostgreSQL transaction to ctx. Pool Exec/Query/QueryRow
// use that transaction when present so multiple domains can share one Tx
// without querying another domain's tables.
func WithTx(ctx context.Context, tx *Tx) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if tx == nil {
		return ctx
	}
	return context.WithValue(ctx, txCtxKey{}, tx)
}

// TxFrom returns the transaction attached by WithTx, or nil.
func TxFrom(ctx context.Context) *Tx {
	if ctx == nil {
		return nil
	}
	tx, _ := ctx.Value(txCtxKey{}).(*Tx)
	return tx
}
