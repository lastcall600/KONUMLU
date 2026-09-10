package messaging

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	listingcontracts "backend/internal/listings/contracts"
)

func TestCreateConversationForPublishedListing(t *testing.T) {
	svc, store, listings, clock := newTestService(t)
	buyer, seller, listing := mustID(t), mustID(t), mustID(t)
	listings.setPublished(listing, seller)

	conv, err := svc.CreateConversation(context.Background(), buyer, listing)
	if err != nil {
		t.Fatal(err)
	}
	if conv.BuyerUserID != buyer || conv.SellerUserID != seller || conv.ListingID != listing {
		t.Fatalf("conv = %+v", conv)
	}
	got, err := store.GetConversation(context.Background(), conv.ID)
	if err != nil || got.ID != conv.ID {
		t.Fatalf("stored = %+v err=%v", got, err)
	}
	_ = clock
}

func TestCreateConversationIdempotent(t *testing.T) {
	svc, _, listings, _ := newTestService(t)
	buyer, seller, listing := mustID(t), mustID(t), mustID(t)
	listings.setPublished(listing, seller)
	first, err := svc.CreateConversation(context.Background(), buyer, listing)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.CreateConversation(context.Background(), buyer, listing)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("first = %s second = %s", first.ID, second.ID)
	}
}

func TestCreateConversationRejectsSelf(t *testing.T) {
	svc, _, listings, _ := newTestService(t)
	owner, listing := mustID(t), mustID(t)
	listings.setPublished(listing, owner)
	if _, err := svc.CreateConversation(context.Background(), owner, listing); !errors.Is(err, errSelfConversation) {
		t.Fatalf("err = %v", err)
	}
}

func TestCreateConversationRejectsNonPublic(t *testing.T) {
	svc, _, listings, _ := newTestService(t)
	buyer, listing := mustID(t), mustID(t)
	if _, err := svc.CreateConversation(context.Background(), buyer, listing); !errors.Is(err, errNotFound) {
		t.Fatalf("missing err = %v", err)
	}
	listings.refs[listingcontracts.ID(listing)] = listingcontracts.ListingRef{
		ID: listingcontracts.ID(listing), OwnerUserID: listingcontracts.ID(mustID(t)), Status: "draft",
	}
	if _, err := svc.CreateConversation(context.Background(), buyer, listing); !errors.Is(err, errNotFound) {
		t.Fatalf("draft err = %v", err)
	}
}

func TestParticipantCanReadAndSendForeignGetsNotFound(t *testing.T) {
	svc, _, listings, _ := newTestService(t)
	buyer, seller, listing, stranger := mustID(t), mustID(t), mustID(t), mustID(t)
	listings.setPublished(listing, seller)
	conv, err := svc.CreateConversation(context.Background(), buyer, listing)
	if err != nil {
		t.Fatal(err)
	}
	msg, err := svc.SendMessage(context.Background(), buyer, conv.ID, "merhaba")
	if err != nil || msg.Body != "merhaba" {
		t.Fatalf("send = %+v err=%v", msg, err)
	}
	msgs, err := svc.ListMessages(context.Background(), seller, conv.ID)
	if err != nil || len(msgs) != 1 || msgs[0].Body != "merhaba" {
		t.Fatalf("seller msgs = %+v err=%v", msgs, err)
	}
	if _, err := svc.GetConversation(context.Background(), stranger, conv.ID); !errors.Is(err, errNotFound) {
		t.Fatalf("stranger get err = %v", err)
	}
	if _, err := svc.SendMessage(context.Background(), stranger, conv.ID, "nope"); !errors.Is(err, errNotFound) {
		t.Fatalf("stranger send err = %v", err)
	}
	if _, err := svc.ListMessages(context.Background(), stranger, conv.ID); !errors.Is(err, errNotFound) {
		t.Fatalf("stranger list err = %v", err)
	}
}

func TestSendMessageRejectsEmptyAndOversize(t *testing.T) {
	svc, _, listings, _ := newTestService(t)
	buyer, seller, listing := mustID(t), mustID(t), mustID(t)
	listings.setPublished(listing, seller)
	conv, err := svc.CreateConversation(context.Background(), buyer, listing)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SendMessage(context.Background(), buyer, conv.ID, "   "); !errors.Is(err, errInvalidBody) {
		t.Fatalf("empty err = %v", err)
	}
	if _, err := svc.SendMessage(context.Background(), buyer, conv.ID, strings.Repeat("a", MaxMessageBytes+1)); !errors.Is(err, errInvalidBody) {
		t.Fatalf("oversize err = %v", err)
	}
}

func TestUnreadAndMarkRead(t *testing.T) {
	svc, _, listings, _ := newTestService(t)
	buyer, seller, listing := mustID(t), mustID(t), mustID(t)
	listings.setPublished(listing, seller)
	conv, err := svc.CreateConversation(context.Background(), buyer, listing)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SendMessage(context.Background(), buyer, conv.ID, "hi"); err != nil {
		t.Fatal(err)
	}
	rows, err := svc.ListConversations(context.Background(), seller)
	if err != nil || len(rows) != 1 || rows[0].UnreadCount != 1 {
		t.Fatalf("seller list = %+v err=%v", rows, err)
	}
	buyerRows, err := svc.ListConversations(context.Background(), buyer)
	if err != nil || len(buyerRows) != 1 || buyerRows[0].UnreadCount != 0 {
		t.Fatalf("buyer list = %+v err=%v", buyerRows, err)
	}
	if err := svc.MarkRead(context.Background(), seller, conv.ID); err != nil {
		t.Fatal(err)
	}
	rows, err = svc.ListConversations(context.Background(), seller)
	if err != nil || len(rows) != 1 || rows[0].UnreadCount != 0 {
		t.Fatalf("after read = %+v err=%v", rows, err)
	}
}

func TestListConversationsNewestActivityFirst(t *testing.T) {
	svc, _, listings, clock := newTestService(t)
	buyer, seller, listingA, listingB := mustID(t), mustID(t), mustID(t), mustID(t)
	listings.setPublished(listingA, seller)
	listings.setPublished(listingB, seller)
	first, err := svc.CreateConversation(context.Background(), buyer, listingA)
	if err != nil {
		t.Fatal(err)
	}
	clock.advance(time.Second)
	second, err := svc.CreateConversation(context.Background(), buyer, listingB)
	if err != nil {
		t.Fatal(err)
	}
	clock.advance(time.Second)
	if _, err := svc.SendMessage(context.Background(), buyer, first.ID, "later"); err != nil {
		t.Fatal(err)
	}
	rows, err := svc.ListConversations(context.Background(), buyer)
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows = %+v err=%v", rows, err)
	}
	if rows[0].ID != first.ID || rows[1].ID != second.ID {
		t.Fatalf("order = %s then %s", rows[0].ID, rows[1].ID)
	}
}

func TestExistingConversationSurvivesListingGoingPrivate(t *testing.T) {
	svc, _, listings, _ := newTestService(t)
	buyer, seller, listing := mustID(t), mustID(t), mustID(t)
	listings.setPublished(listing, seller)
	conv, err := svc.CreateConversation(context.Background(), buyer, listing)
	if err != nil {
		t.Fatal(err)
	}
	listings.refs[listingcontracts.ID(listing)] = listingcontracts.ListingRef{
		ID: listingcontracts.ID(listing), OwnerUserID: listingcontracts.ID(seller), Status: "archived",
	}
	got, err := svc.GetConversation(context.Background(), buyer, conv.ID)
	if err != nil || got.ID != conv.ID {
		t.Fatalf("existing get = %+v err=%v", got, err)
	}
	if _, err := svc.CreateConversation(context.Background(), mustID(t), listing); !errors.Is(err, errNotFound) {
		t.Fatalf("new create err = %v", err)
	}
}

type testClock struct {
	now time.Time
}

func (c *testClock) advance(d time.Duration) {
	c.now = c.now.Add(d)
}

func newTestService(t *testing.T) (*Service, *MemoryStore, *stubListings, *testClock) {
	t.Helper()
	store := NewMemoryStore()
	listings := &stubListings{refs: map[listingcontracts.ID]listingcontracts.ListingRef{}}
	clock := &testClock{now: time.Unix(100, 0).UTC()}
	svc, err := NewService(store, listings, func() time.Time { return clock.now })
	if err != nil {
		t.Fatal(err)
	}
	return svc, store, listings, clock
}

type stubListings struct {
	refs map[listingcontracts.ID]listingcontracts.ListingRef
	err  error
}

func (s *stubListings) setPublished(listingID, owner ID) {
	s.refs[listingcontracts.ID(listingID)] = listingcontracts.ListingRef{
		ID:          listingcontracts.ID(listingID),
		OwnerUserID: listingcontracts.ID(owner),
		Status:      listingcontracts.StatusPublished,
	}
}

func (s *stubListings) ResolveListingOwner(_ context.Context, listingID listingcontracts.ID) (listingcontracts.ListingRef, error) {
	if s.err != nil {
		return listingcontracts.ListingRef{}, s.err
	}
	ref, ok := s.refs[listingID]
	if !ok {
		return listingcontracts.ListingRef{}, listingcontracts.ErrNotFound
	}
	return ref, nil
}

func (s *stubListings) AssertListingOwnedBy(ctx context.Context, listingID, userID listingcontracts.ID) error {
	ref, err := s.ResolveListingOwner(ctx, listingID)
	if err != nil {
		return err
	}
	if ref.OwnerUserID != userID {
		return listingcontracts.ErrForbidden
	}
	return nil
}

func mustIDTime() time.Time {
	return time.Unix(1, 0).UTC()
}
