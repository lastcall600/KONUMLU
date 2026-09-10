// Package cache is a Valkey client for disposable derived data (rate limits,
// session hot-cache, presence). PostgreSQL remains the durable source of truth.
package cache

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// ErrUnavailable is returned when Valkey cannot be used.
// It never includes URLs or credentials.
var ErrUnavailable = errors.New("cache unavailable")

// ErrMiss is returned when a key is absent. It does not wrap driver errors.
var ErrMiss = errors.New("cache miss")

var (
	errInvalidValkeyURL = errors.New("invalid VALKEY_URL")
	errEmptyKey         = errors.New("empty cache key")
	errInvalidTTL       = errors.New("ttl must be greater than zero")
)

const defaultOpTimeout = 5 * time.Second

// incrWithTTLLua atomically INCR and sets PEXPIRE only when the key is created
// (count == 1), preserving a rate-limit window.
const incrWithTTLLua = `
local n = redis.call('INCR', KEYS[1])
if n == 1 then
  redis.call('PEXPIRE', KEYS[1], ARGV[1])
end
return n
`

var incrWithTTL = redis.NewScript(incrWithTTLLua)

// Client is a Valkey connection. Values stored here are rebuildable.
type Client struct {
	rdb       *redis.Client
	opTimeout time.Duration
}

// Open builds a client from VALKEY_URL. It does not require Valkey to be
// reachable; call Ping to verify connectivity.
func Open(ctx context.Context, valkeyURL string) (*Client, error) {
	_ = ctx
	if valkeyURL == "" {
		return nil, errInvalidValkeyURL
	}
	opt, err := redis.ParseURL(valkeyURL)
	if err != nil {
		return nil, errInvalidValkeyURL
	}
	return &Client{rdb: redis.NewClient(opt), opTimeout: defaultOpTimeout}, nil
}

// Ping reports whether Valkey accepts connections.
func (c *Client) Ping(ctx context.Context) error {
	if c == nil || c.rdb == nil {
		return ErrUnavailable
	}
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	if err := mapCmdErr(c.rdb.Ping(ctx).Err()); err != nil {
		return ErrUnavailable
	}
	return nil
}

// Close releases client resources.
func (c *Client) Close() {
	if c == nil || c.rdb == nil {
		return
	}
	_ = c.rdb.Close()
}

// Get returns the string value for key.
func (c *Client) Get(ctx context.Context, key string) (string, error) {
	if err := c.require(key, 0, false); err != nil {
		return "", err
	}
	s, err := c.rdb.Get(ctx, key).Result()
	if err != nil {
		return "", mapCmdErr(err)
	}
	return s, nil
}

// Set writes key with a positive TTL. Data is disposable.
func (c *Client) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	if err := c.require(key, ttl, true); err != nil {
		return err
	}
	return mapCmdErr(c.rdb.Set(ctx, key, value, ttl).Err())
}

// Delete removes key. Missing keys are not an error.
func (c *Client) Delete(ctx context.Context, key string) error {
	if err := c.require(key, 0, false); err != nil {
		return err
	}
	return mapCmdErr(c.rdb.Del(ctx, key).Err())
}

// Increment atomically increments key. TTL is applied when the key is created.
func (c *Client) Increment(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	if err := c.require(key, ttl, true); err != nil {
		return 0, err
	}
	ms := ttl.Milliseconds()
	n, err := incrWithTTL.Run(ctx, c.rdb, []string{key}, strconv.FormatInt(ms, 10)).Int64()
	if err != nil {
		return 0, mapCmdErr(err)
	}
	return n, nil
}

func (c *Client) require(key string, ttl time.Duration, needTTL bool) error {
	if c == nil || c.rdb == nil {
		return ErrUnavailable
	}
	if key == "" {
		return errEmptyKey
	}
	if needTTL && ttl <= 0 {
		return errInvalidTTL
	}
	return nil
}

func (c *Client) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if c.opTimeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, c.opTimeout)
}

func mapCmdErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, redis.Nil) {
		return ErrMiss
	}
	return ErrUnavailable
}
