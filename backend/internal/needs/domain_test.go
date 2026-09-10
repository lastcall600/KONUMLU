package needs

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestNewDraftPreservesUnicodeText(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	requester := mustID(t)
	got, err := NewDraft(requester, validContent("  İhtiyaç — حاجة  ", "  Açıklama  "), now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusDraft || got.RequesterUserID != requester {
		t.Fatalf("need = %+v", got)
	}
	if got.Title != "İhtiyaç — حاجة" || got.Description != "Açıklama" {
		t.Fatalf("text = %q / %q", got.Title, got.Description)
	}
}

func TestNewDraftRejectsEmptyAndOverlong(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	requester := mustID(t)
	if _, err := NewDraft(requester, validContent("  ", ""), now); !errors.Is(err, errInvalidContent) {
		t.Fatalf("blank err = %v", err)
	}
	if _, err := NewDraft(requester, validContent(strings.Repeat("a", MaxTitleRunes+1), ""), now); !errors.Is(err, errInvalidContent) {
		t.Fatalf("title len err = %v", err)
	}
	if _, err := NewDraft(requester, validContent("Need", strings.Repeat("я", MaxDescriptionRunes+1)), now); !errors.Is(err, errInvalidContent) {
		t.Fatalf("desc len err = %v", err)
	}
	ok, err := NewDraft(requester, validContent(strings.Repeat("م", MaxTitleRunes), strings.Repeat("б", MaxDescriptionRunes)), now)
	if err != nil {
		t.Fatal(err)
	}
	if utf8.RuneCountInString(ok.Title) != MaxTitleRunes {
		t.Fatalf("title runes = %d", utf8.RuneCountInString(ok.Title))
	}
}

func TestNewDraftRejectsClientControlledRequesterZero(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	if _, err := NewDraft(ID{}, validContent("Need", ""), now); !errors.Is(err, errZeroID) {
		t.Fatalf("err = %v", err)
	}
}

func TestLocationValidation(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	requester := mustID(t)
	c := validContent("Need", "")
	c.Location.Latitude = 91
	if _, err := NewDraft(requester, c, now); !errors.Is(err, errInvalidLatitude) {
		t.Fatalf("lat err = %v", err)
	}
	c = validContent("Need", "")
	c.Location.Longitude = 181
	if _, err := NewDraft(requester, c, now); !errors.Is(err, errInvalidLongitude) {
		t.Fatalf("lon err = %v", err)
	}
	c = validContent("Need", "")
	c.Location.Latitude = math.NaN()
	if _, err := NewDraft(requester, c, now); !errors.Is(err, errInvalidLatitude) {
		t.Fatalf("nan err = %v", err)
	}
}

func TestBudgetValidation(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	requester := mustID(t)
	okBudget := validContent("Need", "")
	okBudget.Budget = &Budget{MinAmount: "10.50", MaxAmount: "20", Currency: "TRY"}
	got, err := NewDraft(requester, okBudget, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Budget == nil || got.Budget.MinAmount != "10.50" || got.Budget.Currency != "TRY" {
		t.Fatalf("budget = %+v", got.Budget)
	}

	bad := validContent("Need", "")
	bad.Budget = &Budget{MinAmount: "30", MaxAmount: "10", Currency: "TRY"}
	if _, err := NewDraft(requester, bad, now); !errors.Is(err, errInvalidBudget) {
		t.Fatalf("min>max err = %v", err)
	}
	partial := validContent("Need", "")
	partial.Budget = &Budget{MinAmount: "10", Currency: "TRY"}
	if _, err := NewDraft(requester, partial, now); !errors.Is(err, errInvalidBudget) {
		t.Fatalf("partial err = %v", err)
	}
	lower := validContent("Need", "")
	lower.Budget = &Budget{MinAmount: "10", MaxAmount: "20", Currency: "try"}
	if _, err := NewDraft(requester, lower, now); !errors.Is(err, errInvalidBudget) {
		t.Fatalf("currency err = %v", err)
	}
}

func TestRadiusBounds(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	requester := mustID(t)
	okR := 5.0
	c := validContent("Need", "")
	c.RadiusKm = &okR
	if _, err := NewDraft(requester, c, now); err != nil {
		t.Fatal(err)
	}
	tooBig := float64(MaxRadiusKm) + 1
	c.RadiusKm = &tooBig
	if _, err := NewDraft(requester, c, now); !errors.Is(err, errInvalidRadius) {
		t.Fatalf("radius err = %v", err)
	}
}

func TestLifecycleTransitions(t *testing.T) {
	n := mustDraft(t)
	at := n.CreatedAt.Add(time.Minute)
	open, err := n.Open(at)
	if err != nil {
		t.Fatal(err)
	}
	if open.Status != StatusOpen {
		t.Fatalf("status = %s", open.Status)
	}
	if _, err := open.Open(at.Add(time.Second)); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("double open err = %v", err)
	}
	fulfilled, err := open.Fulfill(at.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if fulfilled.Status != StatusFulfilled {
		t.Fatalf("status = %s", fulfilled.Status)
	}
	if _, err := fulfilled.UpdateContent(validContent("X", ""), fulfilled.UpdatedAt.Add(time.Second)); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("terminal update err = %v", err)
	}
	if _, err := fulfilled.Cancel(fulfilled.UpdatedAt.Add(time.Second)); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("terminal cancel err = %v", err)
	}
	if _, err := fulfilled.Expire(fulfilled.UpdatedAt.Add(time.Second)); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("terminal expire err = %v", err)
	}

	again, outcome, err := fulfilled.ApplyCompletedTransactionFulfillment(fulfilled.UpdatedAt.Add(time.Second))
	if err != nil || outcome != "already_fulfilled" || again.Status != StatusFulfilled || again.UpdatedAt != fulfilled.UpdatedAt {
		t.Fatalf("already fulfilled = %+v outcome=%s err=%v", again, outcome, err)
	}

	openNeed, err := mustDraft(t).Open(at)
	if err != nil {
		t.Fatal(err)
	}
	fromTxn, outcome, err := openNeed.ApplyCompletedTransactionFulfillment(at.Add(time.Minute))
	if err != nil || outcome != "fulfilled" || fromTxn.Status != StatusFulfilled {
		t.Fatalf("txn fulfill = %+v outcome=%s err=%v", fromTxn, outcome, err)
	}

	cancelledNeed, err := mustDraft(t).Cancel(at)
	if err != nil {
		t.Fatal(err)
	}
	unchanged, outcome, err := cancelledNeed.ApplyCompletedTransactionFulfillment(at.Add(time.Second))
	if err != nil || outcome != "not_fulfillable" || unchanged.Status != StatusCancelled || unchanged.UpdatedAt != cancelledNeed.UpdatedAt {
		t.Fatalf("cancelled = %+v outcome=%s err=%v", unchanged, outcome, err)
	}

	draft := mustDraft(t)
	cancelled, err := draft.Cancel(draft.CreatedAt.Add(time.Second))
	if err != nil || cancelled.Status != StatusCancelled {
		t.Fatalf("cancel = %+v err = %v", cancelled, err)
	}
	if _, err := draft.Fulfill(draft.CreatedAt.Add(time.Second)); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("fulfill draft err = %v", err)
	}
}

func TestExpireRequiresDuePolicy(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	expires := now.Add(time.Hour)
	c := validContent("Need", "")
	c.ExpiresAt = &expires
	n, err := NewDraft(mustID(t), c, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := n.Expire(now.Add(30 * time.Minute)); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("early expire err = %v", err)
	}
	expired, err := n.Expire(now.Add(2 * time.Hour))
	if err != nil || expired.Status != StatusExpired {
		t.Fatalf("expire = %+v err = %v", expired, err)
	}
	noDeadline := mustDraft(t)
	if _, err := noDeadline.Expire(now.Add(time.Hour)); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("no deadline expire err = %v", err)
	}
	past := now.Add(-time.Hour)
	c = validContent("Need", "")
	c.ExpiresAt = &past
	if _, err := NewDraft(mustID(t), c, now); !errors.Is(err, errInvalidExpiry) {
		t.Fatalf("past expiry create err = %v", err)
	}
}

func TestParseID(t *testing.T) {
	id := mustID(t)
	got, err := ParseID(id.String())
	if err != nil || got != id {
		t.Fatalf("parse = %v err = %v", got, err)
	}
	if _, err := ParseID("not-a-uuid"); !errors.Is(err, errInvalidNeed) {
		t.Fatalf("bad parse err = %v", err)
	}
}

func validContent(title, desc string) Content {
	return Content{
		Title:       title,
		Description: desc,
		Location:    Coordinates{Latitude: 36.621, Longitude: 29.116},
	}
}

func mustDraft(t *testing.T) Need {
	t.Helper()
	n, err := NewDraft(mustID(t), validContent("Need", "Açıklama"), time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func mustID(t *testing.T) ID {
	t.Helper()
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
