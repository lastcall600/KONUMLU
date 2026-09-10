package offers

import (
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestSubmitAndTerminalTransitions(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	got, err := Submit(mustID(t), validContent(), now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusSubmitted || got.ProviderUserID.IsZero() {
		t.Fatalf("offer = %+v", got)
	}
	withdrawn, err := got.Withdraw(now.Add(time.Minute))
	if err != nil || withdrawn.Status != StatusWithdrawn {
		t.Fatalf("withdraw = %+v err = %v", withdrawn, err)
	}
	if _, err := withdrawn.Accept(now.Add(2 * time.Minute)); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("terminal accept err = %v", err)
	}
	accepted, err := got.Accept(now.Add(time.Minute))
	if err != nil || accepted.Status != StatusAccepted {
		t.Fatalf("accept = %+v err = %v", accepted, err)
	}
	if _, err := accepted.Reject(now.Add(2 * time.Minute)); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("terminal reject err = %v", err)
	}
}

func TestPriceAndMessageValidation(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	c := validContent()
	c.Price = &Price{Amount: "100.50", Currency: "TRY"}
	c.Message = "  Merhaba  "
	got, err := Submit(mustID(t), c, now)
	if err != nil || got.Message != "Merhaba" || got.Price.Amount != "100.50" {
		t.Fatalf("got = %+v err = %v", got, err)
	}
	c.Price = &Price{Amount: "10", Currency: ""}
	if _, err := Submit(mustID(t), c, now); !errors.Is(err, errInvalidPrice) {
		t.Fatalf("price err = %v", err)
	}
	c = validContent()
	c.Message = strings.Repeat("a", MaxMessageRunes+1)
	if _, err := Submit(mustID(t), c, now); !errors.Is(err, errInvalidMessage) {
		t.Fatalf("message err = %v", err)
	}
	c.Message = strings.Repeat("م", MaxMessageRunes)
	ok, err := Submit(mustID(t), c, now)
	if err != nil || utf8.RuneCountInString(ok.Message) != MaxMessageRunes {
		t.Fatalf("unicode message err = %v", err)
	}
}

func TestSubmitRejectsZeroIDs(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	if _, err := Submit(ID{}, validContent(), now); !errors.Is(err, errZeroID) {
		t.Fatalf("err = %v", err)
	}
}

func validContent() Content {
	return Content{
		NeedID:             mustStaticID(1),
		ProviderBusinessID: mustStaticID(2),
		ServiceID:          mustStaticID(3),
	}
}

func mustStaticID(n byte) ID {
	var id ID
	id[15] = n
	id[6] = 0x40
	id[8] = 0x80
	return id
}

func mustID(t *testing.T) ID {
	t.Helper()
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
