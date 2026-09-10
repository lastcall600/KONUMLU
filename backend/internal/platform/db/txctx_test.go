package db

import (
	"context"
	"errors"
	"testing"
)

func TestWithTxRoundTrip(t *testing.T) {
	tx := &Tx{}
	ctx := WithTx(context.Background(), tx)
	if TxFrom(ctx) != tx {
		t.Fatal("TxFrom must return the attached Tx")
	}
	if TxFrom(context.Background()) != nil {
		t.Fatal("empty context must not carry a Tx")
	}
	if TxFrom(WithTx(context.Background(), nil)) != nil {
		t.Fatal("nil Tx must not be attached")
	}
}

func TestPoolPrefersContextTx(t *testing.T) {
	p := &Pool{}
	ctx := WithTx(context.Background(), &Tx{})
	if _, err := p.Exec(ctx, "SELECT 1"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Exec via empty Tx = %v", err)
	}
	if err := p.QueryRow(ctx, "SELECT 1").Scan(); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("QueryRow via empty Tx = %v", err)
	}
	if _, err := p.Query(ctx, "SELECT 1"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Query via empty Tx = %v", err)
	}
}
