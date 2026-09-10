package db

import (
	"context"
	"errors"
	"time"

	"backend/internal/platform/config"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrUnavailable is returned when PostgreSQL or PostGIS cannot be used.
// It never includes connection strings or credentials.
var ErrUnavailable = errors.New("database unavailable")

// ErrNoRows is a mapped empty-result sentinel. It does not wrap driver errors.
var ErrNoRows = errors.New("no rows")

// ErrConflict is a mapped unique-constraint violation. It does not wrap driver errors.
var ErrConflict = errors.New("conflict")

var errInvalidDatabaseURL = errors.New("invalid DATABASE_URL")

const postgisInstalledSQL = `SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'postgis')`

// Pool is a PostgreSQL connection pool. Canonical domain state lives in PostgreSQL.
type Pool struct {
	pool           *pgxpool.Pool
	connectTimeout time.Duration
}

// Open builds a pool from DATABASE_URL. It does not require the database to be
// reachable; call Ping or Ready to verify connectivity.
func Open(ctx context.Context, databaseURL string, connectTimeout time.Duration) (*Pool, error) {
	return OpenPool(ctx, databaseURL, config.Pool{ConnectTimeout: connectTimeout})
}

// OpenPool applies optional pool bounds. Zero values keep driver defaults.
func OpenPool(ctx context.Context, databaseURL string, poolCfg config.Pool) (*Pool, error) {
	if databaseURL == "" {
		return nil, errInvalidDatabaseURL
	}
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, errInvalidDatabaseURL
	}
	if poolCfg.ConnectTimeout > 0 {
		cfg.ConnConfig.ConnectTimeout = poolCfg.ConnectTimeout
	}
	if poolCfg.MaxConns > 0 {
		cfg.MaxConns = poolCfg.MaxConns
	}
	if poolCfg.MinConns > 0 {
		cfg.MinConns = poolCfg.MinConns
	}
	if poolCfg.MaxConnLifetime > 0 {
		cfg.MaxConnLifetime = poolCfg.MaxConnLifetime
	}
	if poolCfg.MaxConnIdleTime > 0 {
		cfg.MaxConnIdleTime = poolCfg.MaxConnIdleTime
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, ErrUnavailable
	}
	timeout := poolCfg.ConnectTimeout
	return &Pool{pool: pool, connectTimeout: timeout}, nil
}

// Ping reports whether PostgreSQL accepts connections.
func (p *Pool) Ping(ctx context.Context) error {
	if p == nil || p.pool == nil {
		return ErrUnavailable
	}
	ctx, cancel := p.withTimeout(ctx)
	defer cancel()
	if err := p.pool.Ping(ctx); err != nil {
		return ErrUnavailable
	}
	return nil
}

// VerifyPostGIS reports whether the PostGIS extension is installed.
// It does not create schemas or extensions.
func (p *Pool) VerifyPostGIS(ctx context.Context) error {
	if p == nil || p.pool == nil {
		return ErrUnavailable
	}
	ctx, cancel := p.withTimeout(ctx)
	defer cancel()
	return scanPostGIS(p.pool.QueryRow(ctx, postgisInstalledSQL))
}

// Ready is Ping plus PostGIS availability. Safe for startup and /readyz.
// Ready must not call external vendors (EİDS, staff IdP, email/SMS, object storage).
func (p *Pool) Ready(ctx context.Context) error {
	if err := p.Ping(ctx); err != nil {
		return ErrUnavailable
	}
	if err := p.VerifyPostGIS(ctx); err != nil {
		return ErrUnavailable
	}
	return nil
}

// Close releases pool resources.
func (p *Pool) Close() {
	if p == nil || p.pool == nil {
		return
	}
	p.pool.Close()
}

// Exec runs a statement and returns rows affected. Driver errors map to ErrUnavailable.
// If ctx carries a Tx from WithTx, the statement runs on that transaction.
func (p *Pool) Exec(ctx context.Context, sql string, args ...any) (int64, error) {
	if tx := TxFrom(ctx); tx != nil {
		return tx.Exec(ctx, sql, args...)
	}
	if p == nil || p.pool == nil {
		return 0, ErrUnavailable
	}
	tag, err := p.pool.Exec(ctx, sql, args...)
	if err != nil {
		return 0, mapWriteErr(err)
	}
	return tag.RowsAffected(), nil
}

func mapWriteErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrConflict
	}
	return ErrUnavailable
}

// Row is a single-row query result.
type Row interface {
	Scan(dest ...any) error
}

// QueryRow runs a query expected to return one row. Scan maps empty results to
// ErrNoRows and other driver errors to ErrUnavailable.
func (p *Pool) QueryRow(ctx context.Context, sql string, args ...any) Row {
	if tx := TxFrom(ctx); tx != nil {
		return tx.QueryRow(ctx, sql, args...)
	}
	if p == nil || p.pool == nil {
		return errRow{err: ErrUnavailable}
	}
	return mappedRow{row: p.pool.QueryRow(ctx, sql, args...)}
}

// Rows is a multi-row query result. Callers must Close.
type Rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
	Close()
}

// Query runs a query expected to return zero or more rows. Driver errors map to
// ErrUnavailable except context cancellation and deadline.
func (p *Pool) Query(ctx context.Context, sql string, args ...any) (Rows, error) {
	if tx := TxFrom(ctx); tx != nil {
		return tx.Query(ctx, sql, args...)
	}
	if p == nil || p.pool == nil {
		return nil, ErrUnavailable
	}
	rows, err := p.pool.Query(ctx, sql, args...)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, ErrUnavailable
	}
	return mappedRows{rows: rows}, nil
}

// Tx is a PostgreSQL transaction. Domain writes and outbox enqueue share one Tx.
type Tx struct {
	tx pgx.Tx
}

// Begin starts a transaction. Callers must Commit or Rollback.
func (p *Pool) Begin(ctx context.Context) (*Tx, error) {
	if p == nil || p.pool == nil {
		return nil, ErrUnavailable
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, ErrUnavailable
	}
	return &Tx{tx: tx}, nil
}

func (t *Tx) Exec(ctx context.Context, sql string, args ...any) (int64, error) {
	if t == nil || t.tx == nil {
		return 0, ErrUnavailable
	}
	tag, err := t.tx.Exec(ctx, sql, args...)
	if err != nil {
		return 0, mapWriteErr(err)
	}
	return tag.RowsAffected(), nil
}

func (t *Tx) QueryRow(ctx context.Context, sql string, args ...any) Row {
	if t == nil || t.tx == nil {
		return errRow{err: ErrUnavailable}
	}
	return mappedRow{row: t.tx.QueryRow(ctx, sql, args...)}
}

func (t *Tx) Query(ctx context.Context, sql string, args ...any) (Rows, error) {
	if t == nil || t.tx == nil {
		return nil, ErrUnavailable
	}
	rows, err := t.tx.Query(ctx, sql, args...)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, ErrUnavailable
	}
	return mappedRows{rows: rows}, nil
}

func (t *Tx) Commit(ctx context.Context) error {
	if t == nil || t.tx == nil {
		return ErrUnavailable
	}
	if err := t.tx.Commit(ctx); err != nil {
		return mapWriteErr(err)
	}
	return nil
}

func (t *Tx) Rollback(ctx context.Context) error {
	if t == nil || t.tx == nil {
		return ErrUnavailable
	}
	if err := t.tx.Rollback(ctx); err != nil {
		if errors.Is(err, pgx.ErrTxClosed) {
			return nil
		}
		return mapWriteErr(err)
	}
	return nil
}

func (p *Pool) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if p.connectTimeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, p.connectTimeout)
}

type scanner interface {
	Scan(dest ...any) error
}

type mappedRow struct {
	row pgx.Row
}

func (r mappedRow) Scan(dest ...any) error {
	err := r.row.Scan(dest...)
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNoRows
	}
	return ErrUnavailable
}

type mappedRows struct {
	rows pgx.Rows
}

func (r mappedRows) Next() bool {
	return r.rows.Next()
}

func (r mappedRows) Scan(dest ...any) error {
	err := r.rows.Scan(dest...)
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNoRows
	}
	return ErrUnavailable
}

func (r mappedRows) Err() error {
	err := r.rows.Err()
	if err == nil {
		return nil
	}
	return ErrUnavailable
}

func (r mappedRows) Close() {
	r.rows.Close()
}

type errRow struct {
	err error
}

func (r errRow) Scan(dest ...any) error {
	return r.err
}

func scanPostGIS(row scanner) error {
	var installed bool
	if err := row.Scan(&installed); err != nil {
		return ErrUnavailable
	}
	if !installed {
		return ErrUnavailable
	}
	return nil
}
