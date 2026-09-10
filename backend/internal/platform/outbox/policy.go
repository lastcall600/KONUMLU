package outbox

import (
	"crypto/rand"
	"math"
	"math/big"
	"time"
)

// Policy is injected claim/lease/backoff configuration. Values are not product defaults.
// Max-attempt / dead-letter handling is OPEN (ADR-003 OI-003-03) and is not applied here.
type Policy struct {
	BatchSize         int
	Lease             time.Duration
	BackoffBase       time.Duration
	BackoffMultiplier float64
	BackoffCap        time.Duration
	Jitter            time.Duration
}

func (p Policy) Validate() error {
	if p.BatchSize <= 0 || p.Lease <= 0 {
		return ErrInvalidPolicy
	}
	if p.BackoffBase <= 0 || p.BackoffMultiplier < 1 || p.BackoffCap < p.BackoffBase {
		return ErrInvalidPolicy
	}
	if p.Jitter < 0 {
		return ErrInvalidPolicy
	}
	return nil
}

func (p Policy) NextAvailableAt(now time.Time, attempts int) (time.Time, error) {
	exp := attempts - 1
	if exp < 0 {
		exp = 0
	}
	delay := float64(p.BackoffBase)
	for i := 0; i < exp; i++ {
		delay *= p.BackoffMultiplier
		if delay >= float64(p.BackoffCap) {
			delay = float64(p.BackoffCap)
			break
		}
	}
	if delay > float64(p.BackoffCap) {
		delay = float64(p.BackoffCap)
	}
	d := time.Duration(delay)
	j, err := jitter(p.Jitter)
	if err != nil {
		return time.Time{}, err
	}
	return now.Add(d + j), nil
}

func jitter(max time.Duration) (time.Duration, error) {
	if max <= 0 {
		return 0, nil
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(max)+1))
	if err != nil {
		return 0, err
	}
	if n.Sign() < 0 || n.Cmp(big.NewInt(math.MaxInt64)) > 0 {
		return 0, ErrUnavailable
	}
	return time.Duration(n.Int64()), nil
}
