package messaging

import (
	"errors"
	"strings"
	"testing"
)

func TestParseIDRejectsZeroAndGarbage(t *testing.T) {
	if _, err := ParseID(""); !errors.Is(err, errZeroID) {
		t.Fatalf("empty err = %v", err)
	}
	if _, err := ParseID("not-a-uuid"); !errors.Is(err, errZeroID) {
		t.Fatalf("garbage err = %v", err)
	}
	if _, err := ParseID("00000000-0000-0000-0000-000000000000"); !errors.Is(err, errZeroID) {
		t.Fatalf("zero err = %v", err)
	}
}

func TestNormalizeMessageBody(t *testing.T) {
	if _, err := NormalizeMessageBody("   "); !errors.Is(err, errInvalidBody) {
		t.Fatalf("whitespace err = %v", err)
	}
	got, err := NormalizeMessageBody("  hello  ")
	if err != nil || got != "hello" {
		t.Fatalf("got = %q err=%v", got, err)
	}
	oversize := strings.Repeat("a", MaxMessageBytes+1)
	if _, err := NormalizeMessageBody(oversize); !errors.Is(err, errInvalidBody) {
		t.Fatalf("oversize err = %v", err)
	}
	html := "<script>x</script>"
	got, err = NormalizeMessageBody(html)
	if err != nil || got != html {
		t.Fatalf("html stored as text got = %q err=%v", got, err)
	}
}

func TestConversationRejectsSelf(t *testing.T) {
	id := mustID(t)
	user := mustID(t)
	conv := Conversation{
		ID: id, ListingID: mustID(t), BuyerUserID: user, SellerUserID: user,
		CreatedAt: mustIDTime(), UpdatedAt: mustIDTime(),
	}
	if err := conv.Validate(); !errors.Is(err, errSelfConversation) {
		t.Fatalf("err = %v", err)
	}
}

func mustID(t *testing.T) ID {
	t.Helper()
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
