package messaging

import (
	"context"

	"backend/internal/platform/db"
)

type Transactor interface {
	Begin(ctx context.Context) (Tx, error)
}

type Tx interface {
	Context(ctx context.Context) context.Context
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

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
