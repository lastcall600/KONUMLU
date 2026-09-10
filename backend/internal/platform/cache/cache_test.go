package cache

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestOpenRejectsEmptyAndInvalidURLWithoutLeaking(t *testing.T) {
	ctx := context.Background()
	cases := []string{"", "not-a-url", "://broken", "postgres://u:p@127.0.0.1:6379/0"}
	for _, url := range cases {
		_, err := Open(ctx, url)
		if err == nil {
			t.Fatalf("Open(%q) expected error", url)
		}
		if !errors.Is(err, errInvalidValkeyURL) {
			t.Fatalf("Open(%q) err = %v, want invalid VALKEY_URL", url, err)
		}
		msg := err.Error()
		if url != "" && strings.Contains(msg, url) {
			t.Fatalf("error leaked valkey url: %v", err)
		}
		if strings.Contains(strings.ToLower(msg), "password") || strings.Contains(msg, "secret") {
			t.Fatalf("error leaked credential material: %v", err)
		}
	}
}

func TestOpenDoesNotRequireLiveValkey(t *testing.T) {
	c, err := Open(context.Background(), "redis://127.0.0.1:1/0")
	if err != nil {
		t.Fatalf("Open valid URL: %v", err)
	}
	t.Cleanup(c.Close)
	if err := c.Ping(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Ping unreachable: %v", err)
	}
}

func TestMapCmdErr(t *testing.T) {
	if err := mapCmdErr(nil); err != nil {
		t.Fatalf("nil: %v", err)
	}
	if err := mapCmdErr(redis.Nil); !errors.Is(err, ErrMiss) {
		t.Fatalf("nil reply: %v", err)
	}
	mapped := mapCmdErr(errors.New("boom redis://u:secret@host"))
	if !errors.Is(mapped, ErrUnavailable) {
		t.Fatalf("other: %v", mapped)
	}
	if strings.Contains(mapped.Error(), "secret") {
		t.Fatalf("mapped error leaked: %v", mapped)
	}
	if err := mapCmdErr(context.Canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled: %v", err)
	}
}

func TestNilClientOperations(t *testing.T) {
	c := &Client{}
	ctx := context.Background()
	if err := c.Ping(ctx); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Ping: %v", err)
	}
	if _, err := c.Get(ctx, "k"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Get: %v", err)
	}
	if err := c.Set(ctx, "k", "v", time.Second); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Set: %v", err)
	}
	if err := c.Delete(ctx, "k"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := c.Increment(ctx, "k", time.Second); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Increment: %v", err)
	}
}

func TestRejectsEmptyKeyAndTTL(t *testing.T) {
	c, err := Open(context.Background(), "redis://127.0.0.1:1/0")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(c.Close)
	ctx := context.Background()
	if _, err := c.Get(ctx, ""); !errors.Is(err, errEmptyKey) {
		t.Fatalf("Get empty key: %v", err)
	}
	if err := c.Set(ctx, "k", "v", 0); !errors.Is(err, errInvalidTTL) {
		t.Fatalf("Set ttl: %v", err)
	}
	if _, err := c.Increment(ctx, "k", 0); !errors.Is(err, errInvalidTTL) {
		t.Fatalf("Increment ttl: %v", err)
	}
}

func TestIncrScriptSetsTTLOnCreate(t *testing.T) {
	if !strings.Contains(incrWithTTLLua, "INCR") || !strings.Contains(incrWithTTLLua, "PEXPIRE") || !strings.Contains(incrWithTTLLua, "n == 1") {
		t.Fatalf("script must atomically INCR and PEXPIRE on create: %s", incrWithTTLLua)
	}
}
