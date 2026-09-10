package transactions

import (
	"context"

	"backend/internal/platform/db"
)

// Transactor starts a PostgreSQL transaction so Transaction state and the
// completion outbox row commit together. It is not a cross-domain distributed transaction.
type Transactor interface {
	Begin(ctx context.Context) (Tx, error)
}

// Tx is one unit of work. Context attaches the DB transaction when backed by PostgreSQL.
type Tx interface {
	Context(ctx context.Context) context.Context
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

// PoolTransactor begins a PostgreSQL transaction shared by the Transactions store via db.WithTx.
type PoolTransactor struct {
	Pool *db.Pool
}

func (p PoolTransactor) Begin(ctx context.Context) (Tx, error) {
	if p.Pool == nil {
		return nil, errUnavailable
	}
	tx, err := p.Pool.Begin(ctx)
	if err != nil {
		return nil, mapStoreErr(err)
	}
	return poolTx{tx: tx}, nil
}

type poolTx struct {
	tx *db.Tx
}

func (t poolTx) Context(ctx context.Context) context.Context {
	return db.WithTx(ctx, t.tx)
}

func (t poolTx) Commit(ctx context.Context) error {
	return t.tx.Commit(ctx)
}

func (t poolTx) Rollback(ctx context.Context) error {
	return t.tx.Rollback(ctx)
}
