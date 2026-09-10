package trust

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"backend/internal/platform/outbox"
	reviewscontracts "backend/internal/reviews/contracts"
	verifiedcontracts "backend/internal/verified/contracts"
)

func TestFirstVerifiedInteractionCreatesRequesterAndProvider(t *testing.T) {
	p, store := mustProjector(t)
	h := mustHandler(t, p)
	requester, provider := mustID(t), mustID(t)
	at := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	if err := h.Handle(context.Background(), mustEvent(t, requester, provider, mustID(t), at, nil)); err != nil {
		t.Fatal(err)
	}
	req, err := store.GetProfile(context.Background(), requester)
	if err != nil {
		t.Fatal(err)
	}
	if req.VerifiedInteractionCount != 1 || req.RequesterVerifiedInteractionCount != 1 || req.ProviderVerifiedInteractionCount != 0 {
		t.Fatalf("requester = %+v", req)
	}
	if req.TrustLevel != LevelVerified {
		t.Fatalf("requester level = %s", req.TrustLevel)
	}
	prov, err := store.GetProfile(context.Background(), provider)
	if err != nil {
		t.Fatal(err)
	}
	if prov.VerifiedInteractionCount != 1 || prov.ProviderVerifiedInteractionCount != 1 || prov.RequesterVerifiedInteractionCount != 0 {
		t.Fatalf("provider = %+v", prov)
	}
	if prov.TrustLevel != LevelVerified {
		t.Fatalf("provider level = %s", prov.TrustLevel)
	}
	hist, err := store.ListHistory(context.Background(), requester)
	if err != nil || len(hist) != 1 || hist[0].Role != RoleRequester {
		t.Fatalf("history = %+v err=%v", hist, err)
	}
}

func TestReplaySameEventDoesNotDoubleCount(t *testing.T) {
	p, store := mustProjector(t)
	h := mustHandler(t, p)
	requester, provider := mustID(t), mustID(t)
	at := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	event := mustEvent(t, requester, provider, mustID(t), at, nil)
	if err := h.Handle(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if err := h.Handle(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	req, err := store.GetProfile(context.Background(), requester)
	if err != nil {
		t.Fatal(err)
	}
	if req.VerifiedInteractionCount != 1 || req.RequesterVerifiedInteractionCount != 1 {
		t.Fatalf("requester = %+v", req)
	}
	prov, err := store.GetProfile(context.Background(), provider)
	if err != nil {
		t.Fatal(err)
	}
	if prov.VerifiedInteractionCount != 1 || prov.ProviderVerifiedInteractionCount != 1 {
		t.Fatalf("provider = %+v", prov)
	}
}

func TestSecondDifferentEventIncrements(t *testing.T) {
	p, store := mustProjector(t)
	h := mustHandler(t, p)
	requester, provider := mustID(t), mustID(t)
	at := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	if err := h.Handle(context.Background(), mustEvent(t, requester, provider, mustID(t), at, nil)); err != nil {
		t.Fatal(err)
	}
	later := at.Add(time.Hour)
	if err := h.Handle(context.Background(), mustEvent(t, requester, provider, mustID(t), later, nil)); err != nil {
		t.Fatal(err)
	}
	req, err := store.GetProfile(context.Background(), requester)
	if err != nil {
		t.Fatal(err)
	}
	if req.VerifiedInteractionCount != 2 || req.RequesterVerifiedInteractionCount != 2 || req.TrustLevel != LevelVerified {
		t.Fatalf("requester = %+v", req)
	}
	if req.LastVerifiedInteractionAt == nil || !req.LastVerifiedInteractionAt.Equal(later) {
		t.Fatalf("last = %v", req.LastVerifiedInteractionAt)
	}
}

func TestTrustLevelChangesDeterministically(t *testing.T) {
	p, store := mustProjector(t)
	h := mustHandler(t, p)
	user := mustID(t)
	at := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		if err := h.Handle(context.Background(), mustEvent(t, user, mustID(t), mustID(t), at.Add(time.Duration(i)*time.Minute), nil)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := store.GetProfile(context.Background(), user)
	if err != nil {
		t.Fatal(err)
	}
	if got.VerifiedInteractionCount != 5 || got.TrustLevel != LevelEstablished {
		t.Fatalf("got = %+v", got)
	}
}

func TestTransactionDoesNotChangeTrustLevel(t *testing.T) {
	p, store := mustProjector(t)
	h := mustHandler(t, p)
	svc := mustService(t, p)
	requester, provider := mustID(t), mustID(t)
	at := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	if err := h.Handle(context.Background(), mustTypedEvent(t, requester, provider, mustID(t), at, verifiedcontracts.InteractionTypeTransaction, nil)); err != nil {
		t.Fatal(err)
	}
	req, err := svc.GetMe(context.Background(), requester)
	if err != nil {
		t.Fatal(err)
	}
	if req.VerifiedInteractionCount != 0 || req.RequesterVerifiedInteractionCount != 0 || req.TrustLevel != LevelNew {
		t.Fatalf("requester scored from transaction = %+v", req)
	}
	prov, err := svc.GetMe(context.Background(), provider)
	if err != nil {
		t.Fatal(err)
	}
	if prov.VerifiedInteractionCount != 0 || prov.ProviderVerifiedInteractionCount != 0 || prov.TrustLevel != LevelNew {
		t.Fatalf("provider scored from transaction = %+v", prov)
	}
	hist, err := store.ListHistory(context.Background(), requester)
	if err != nil || len(hist) != 1 || hist[0].InteractionType != verifiedcontracts.InteractionTypeTransaction {
		t.Fatalf("history = %+v err=%v", hist, err)
	}
}

func TestDeliveryDoesNotChangeTrustLevel(t *testing.T) {
	p, _ := mustProjector(t)
	h := mustHandler(t, p)
	svc := mustService(t, p)
	requester, provider := mustID(t), mustID(t)
	at := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	if err := h.Handle(context.Background(), mustTypedEvent(t, requester, provider, mustID(t), at, verifiedcontracts.InteractionTypeDelivery, nil)); err != nil {
		t.Fatal(err)
	}
	req, err := svc.GetMe(context.Background(), requester)
	if err != nil {
		t.Fatal(err)
	}
	if req.VerifiedInteractionCount != 0 || req.TrustLevel != LevelNew {
		t.Fatalf("requester scored from delivery = %+v", req)
	}
	prov, err := svc.GetMe(context.Background(), provider)
	if err != nil {
		t.Fatal(err)
	}
	if prov.VerifiedInteractionCount != 0 || prov.TrustLevel != LevelNew {
		t.Fatalf("provider scored from delivery = %+v", prov)
	}
}

func TestListingInspectionStillCountsAfterTransactionAndDelivery(t *testing.T) {
	p, store := mustProjector(t)
	h := mustHandler(t, p)
	requester, provider := mustID(t), mustID(t)
	at := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	if err := h.Handle(context.Background(), mustTypedEvent(t, requester, provider, mustID(t), at, verifiedcontracts.InteractionTypeTransaction, nil)); err != nil {
		t.Fatal(err)
	}
	if err := h.Handle(context.Background(), mustTypedEvent(t, requester, provider, mustID(t), at.Add(time.Minute), verifiedcontracts.InteractionTypeDelivery, nil)); err != nil {
		t.Fatal(err)
	}
	if err := h.Handle(context.Background(), mustEvent(t, requester, provider, mustID(t), at.Add(2*time.Minute), nil)); err != nil {
		t.Fatal(err)
	}
	req, err := store.GetProfile(context.Background(), requester)
	if err != nil {
		t.Fatal(err)
	}
	if req.VerifiedInteractionCount != 1 || req.RequesterVerifiedInteractionCount != 1 || req.TrustLevel != LevelVerified {
		t.Fatalf("listing_inspection count mixed = %+v", req)
	}
	if req.LastVerifiedInteractionAt == nil || !req.LastVerifiedInteractionAt.Equal(at.Add(2*time.Minute)) {
		t.Fatalf("last counted at = %v", req.LastVerifiedInteractionAt)
	}
	if err := h.Handle(context.Background(), mustTypedEvent(t, requester, provider, mustID(t), at.Add(3*time.Minute), verifiedcontracts.InteractionTypeTransaction, nil)); err != nil {
		t.Fatal(err)
	}
	req, err = store.GetProfile(context.Background(), requester)
	if err != nil {
		t.Fatal(err)
	}
	if req.VerifiedInteractionCount != 1 || req.TrustLevel != LevelVerified {
		t.Fatalf("later transaction changed count = %+v", req)
	}
	if req.LastVerifiedInteractionAt == nil || !req.LastVerifiedInteractionAt.Equal(at.Add(2*time.Minute)) {
		t.Fatalf("later transaction moved last counted at = %v", req.LastVerifiedInteractionAt)
	}
}

func TestReplayTransactionIsIdempotent(t *testing.T) {
	p, store := mustProjector(t)
	h := mustHandler(t, p)
	svc := mustService(t, p)
	requester, provider := mustID(t), mustID(t)
	at := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	event := mustTypedEvent(t, requester, provider, mustID(t), at, verifiedcontracts.InteractionTypeTransaction, nil)
	if err := h.Handle(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if err := h.Handle(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	req, err := svc.GetMe(context.Background(), requester)
	if err != nil {
		t.Fatal(err)
	}
	if req.VerifiedInteractionCount != 0 || req.TrustLevel != LevelNew {
		t.Fatalf("replay scored = %+v", req)
	}
	hist, err := store.ListHistory(context.Background(), requester)
	if err != nil || len(hist) != 1 {
		t.Fatalf("history = %+v err=%v", hist, err)
	}
}

func TestUnknownInteractionTypeDoesNotAlterScoring(t *testing.T) {
	p, store := mustProjector(t)
	h := mustHandler(t, p)
	requester, provider := mustID(t), mustID(t)
	at := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	payload, err := json.Marshal(map[string]string{
		"interaction_id":      mustID(t).String(),
		"appointment_id":      mustID(t).String(),
		"listing_id":          mustID(t).String(),
		"requester_user_id":   requester.String(),
		"provider_user_id":    provider.String(),
		"interaction_type":    "escrow",
		"verification_method": "otp",
		"verified_at":         at.UTC().Format(time.RFC3339),
	})
	if err != nil {
		t.Fatal(err)
	}
	eid, err := outbox.NewID()
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Handle(context.Background(), outbox.Event{
		ID:           eid,
		EventType:    verifiedcontracts.EventTypeInteractionCompleted,
		EventVersion: verifiedcontracts.EventVersion,
		Payload:      payload,
		CreatedAt:    at,
	}); !errors.Is(err, errInvalidEvent) {
		t.Fatalf("unknown type err = %v", err)
	}
	if _, err := store.GetProfile(context.Background(), requester); !errors.Is(err, errNotFound) {
		t.Fatalf("unknown type created requester profile err=%v", err)
	}
	if _, err := store.GetProfile(context.Background(), provider); !errors.Is(err, errNotFound) {
		t.Fatalf("unknown type created provider profile err=%v", err)
	}
	unknown := CompletedInteraction{
		EventID:            mustID(t),
		InteractionID:      mustID(t),
		ListingID:          mustID(t),
		RequesterUserID:    requester,
		ProviderUserID:     provider,
		InteractionType:    "escrow",
		VerificationMethod: "otp",
		VerifiedAt:         at,
	}
	if err := p.Apply(context.Background(), unknown); !errors.Is(err, errInvalidEvent) {
		t.Fatalf("apply unknown err = %v", err)
	}
}

func TestMalformedEventFailsSafely(t *testing.T) {
	p, store := mustProjector(t)
	h := mustHandler(t, p)
	eid, err := outbox.NewID()
	if err != nil {
		t.Fatal(err)
	}
	event := outbox.Event{
		ID:           eid,
		EventType:    verifiedcontracts.EventTypeInteractionCompleted,
		EventVersion: verifiedcontracts.EventVersion,
		Payload:      json.RawMessage(`{"listing_id":"not-a-uuid"}`),
		CreatedAt:    time.Now().UTC(),
	}
	if err := h.Handle(context.Background(), event); !errors.Is(err, errInvalidEvent) {
		t.Fatalf("err = %v", err)
	}
	if _, err := store.GetProfile(context.Background(), mustID(t)); !errors.Is(err, errNotFound) {
		t.Fatalf("unexpected profile err = %v", err)
	}
	if err := h.Handle(context.Background(), outbox.Event{
		ID:           eid,
		EventType:    verifiedcontracts.EventTypeInteractionCompleted,
		EventVersion: verifiedcontracts.EventVersion,
		Payload:      json.RawMessage(`not-json`),
		CreatedAt:    time.Now().UTC(),
	}); !errors.Is(err, errInvalidEvent) {
		t.Fatalf("bad json err = %v", err)
	}
}

func TestZeroProfileWhenMissing(t *testing.T) {
	p, _ := mustProjector(t)
	svc, err := NewService(p)
	if err != nil {
		t.Fatal(err)
	}
	user := mustID(t)
	got, err := svc.GetMe(context.Background(), user)
	if err != nil {
		t.Fatal(err)
	}
	if got.UserID != user || got.TrustLevel != LevelNew || got.VerifiedInteractionCount != 0 {
		t.Fatalf("got = %+v", got)
	}
	if got.VerifiedReviewCount != 0 || got.ProviderServiceReviewCount != 0 || got.ProviderServiceAverage() != nil ||
		got.LastVerifiedReviewAt != nil {
		t.Fatalf("review zero = %+v", got)
	}
}

func TestReviewEventIncrementsReviewerCount(t *testing.T) {
	p, store := mustProjector(t)
	h := mustHandler(t, p)
	reviewer, provider := mustID(t), mustID(t)
	at := time.Date(2026, 9, 7, 11, 0, 0, 0, time.UTC)
	if err := h.Handle(context.Background(), mustReviewEvent(t, reviewer, provider, 5, 2, at, nil)); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetProfile(context.Background(), reviewer)
	if err != nil {
		t.Fatal(err)
	}
	if got.VerifiedReviewCount != 1 || got.ProviderServiceReviewCount != 0 || got.ProviderServiceRatingSum != 0 {
		t.Fatalf("reviewer = %+v", got)
	}
	if got.LastVerifiedReviewAt == nil || !got.LastVerifiedReviewAt.Equal(at) {
		t.Fatalf("last authored = %v", got.LastVerifiedReviewAt)
	}
	if got.TrustLevel != LevelNew || got.VerifiedInteractionCount != 0 {
		t.Fatalf("level changed = %+v", got)
	}
}

func TestProviderReceivesServiceCountSumAverage(t *testing.T) {
	p, store := mustProjector(t)
	h := mustHandler(t, p)
	reviewer, provider := mustID(t), mustID(t)
	at := time.Date(2026, 9, 7, 11, 0, 0, 0, time.UTC)
	if err := h.Handle(context.Background(), mustReviewEvent(t, reviewer, provider, 5, 2, at, nil)); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetProfile(context.Background(), provider)
	if err != nil {
		t.Fatal(err)
	}
	if got.VerifiedReviewCount != 0 || got.ProviderServiceReviewCount != 1 || got.ProviderServiceRatingSum != 2 {
		t.Fatalf("provider = %+v", got)
	}
	if avg := got.ProviderServiceAverage(); avg == nil || avg.String() != "2" {
		t.Fatalf("average = %v", avg)
	}
	if got.LastVerifiedReviewAt != nil {
		t.Fatalf("authored timestamp leaked onto provider = %v", got.LastVerifiedReviewAt)
	}
	if got.LastProviderServiceReviewAt == nil || !got.LastProviderServiceReviewAt.Equal(at) {
		t.Fatalf("received at = %v", got.LastProviderServiceReviewAt)
	}
}

func TestListingAccuracyDoesNotChangeProviderTrustProfile(t *testing.T) {
	p, store := mustProjector(t)
	h := mustHandler(t, p)
	reviewer, provider := mustID(t), mustID(t)
	at := time.Date(2026, 9, 7, 11, 0, 0, 0, time.UTC)
	if err := h.Handle(context.Background(), mustReviewEvent(t, reviewer, provider, 5, 1, at, nil)); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetProfile(context.Background(), provider)
	if err != nil {
		t.Fatal(err)
	}
	if got.ProviderServiceReviewCount != 1 || got.ProviderServiceRatingSum != 1 {
		t.Fatalf("listing_accuracy mixed into provider = %+v", got)
	}
	if avg := got.ProviderServiceAverage(); avg == nil || avg.String() != "1" {
		t.Fatalf("average = %v", avg)
	}
}

func TestReplaySameReviewEventNoOp(t *testing.T) {
	p, store := mustProjector(t)
	h := mustHandler(t, p)
	reviewer, provider := mustID(t), mustID(t)
	at := time.Date(2026, 9, 7, 11, 0, 0, 0, time.UTC)
	event := mustReviewEvent(t, reviewer, provider, 4, 3, at, nil)
	if err := h.Handle(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if err := h.Handle(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	req, err := store.GetProfile(context.Background(), reviewer)
	if err != nil {
		t.Fatal(err)
	}
	if req.VerifiedReviewCount != 1 {
		t.Fatalf("reviewer = %+v", req)
	}
	prov, err := store.GetProfile(context.Background(), provider)
	if err != nil {
		t.Fatal(err)
	}
	if prov.ProviderServiceReviewCount != 1 || prov.ProviderServiceRatingSum != 3 {
		t.Fatalf("provider = %+v", prov)
	}
}

func TestSecondDistinctReviewUpdatesDeterministicAverage(t *testing.T) {
	p, store := mustProjector(t)
	h := mustHandler(t, p)
	reviewer, provider := mustID(t), mustID(t)
	at := time.Date(2026, 9, 7, 11, 0, 0, 0, time.UTC)
	if err := h.Handle(context.Background(), mustReviewEvent(t, reviewer, provider, 5, 4, at, nil)); err != nil {
		t.Fatal(err)
	}
	later := at.Add(time.Hour)
	if err := h.Handle(context.Background(), mustReviewEvent(t, reviewer, provider, 1, 5, later, nil)); err != nil {
		t.Fatal(err)
	}
	req, err := store.GetProfile(context.Background(), reviewer)
	if err != nil {
		t.Fatal(err)
	}
	if req.VerifiedReviewCount != 2 || req.LastVerifiedReviewAt == nil || !req.LastVerifiedReviewAt.Equal(later) {
		t.Fatalf("reviewer = %+v", req)
	}
	prov, err := store.GetProfile(context.Background(), provider)
	if err != nil {
		t.Fatal(err)
	}
	if prov.ProviderServiceReviewCount != 2 || prov.ProviderServiceRatingSum != 9 {
		t.Fatalf("provider = %+v", prov)
	}
	if avg := prov.ProviderServiceAverage(); avg == nil || avg.String() != "4.5" {
		t.Fatalf("average = %v", avg)
	}
}

func TestReviewSignalsUnchangedByTransactionAndDelivery(t *testing.T) {
	p, store := mustProjector(t)
	h := mustHandler(t, p)
	reviewer, provider := mustID(t), mustID(t)
	at := time.Date(2026, 9, 7, 11, 0, 0, 0, time.UTC)
	if err := h.Handle(context.Background(), mustReviewEvent(t, reviewer, provider, 5, 4, at, nil)); err != nil {
		t.Fatal(err)
	}
	if err := h.Handle(context.Background(), mustTypedEvent(t, reviewer, provider, mustID(t), at.Add(time.Minute), verifiedcontracts.InteractionTypeTransaction, nil)); err != nil {
		t.Fatal(err)
	}
	if err := h.Handle(context.Background(), mustTypedEvent(t, reviewer, provider, mustID(t), at.Add(2*time.Minute), verifiedcontracts.InteractionTypeDelivery, nil)); err != nil {
		t.Fatal(err)
	}
	rev, err := store.GetProfile(context.Background(), reviewer)
	if err != nil {
		t.Fatal(err)
	}
	if rev.VerifiedReviewCount != 1 || rev.VerifiedInteractionCount != 0 || rev.TrustLevel != LevelNew {
		t.Fatalf("reviewer mixed = %+v", rev)
	}
	prov, err := store.GetProfile(context.Background(), provider)
	if err != nil {
		t.Fatal(err)
	}
	if prov.ProviderServiceReviewCount != 1 || prov.ProviderServiceRatingSum != 4 ||
		prov.VerifiedInteractionCount != 0 || prov.TrustLevel != LevelNew {
		t.Fatalf("provider mixed = %+v", prov)
	}
}

func TestReviewOnlyEventDoesNotChangeTrustLevel(t *testing.T) {
	p, store := mustProjector(t)
	h := mustHandler(t, p)
	user := mustID(t)
	at := time.Date(2026, 9, 7, 11, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		if err := h.Handle(context.Background(), mustReviewEvent(t, user, mustID(t), 5, 5, at.Add(time.Duration(i)*time.Minute), nil)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := store.GetProfile(context.Background(), user)
	if err != nil {
		t.Fatal(err)
	}
	if got.VerifiedReviewCount != 5 || got.VerifiedInteractionCount != 0 || got.TrustLevel != LevelNew {
		t.Fatalf("got = %+v", got)
	}
}

func TestMalformedReviewEventFailsSafely(t *testing.T) {
	p, store := mustProjector(t)
	h := mustHandler(t, p)
	eid, err := outbox.NewID()
	if err != nil {
		t.Fatal(err)
	}
	event := outbox.Event{
		ID:           eid,
		EventType:    reviewscontracts.EventTypeVerifiedCreated,
		EventVersion: reviewscontracts.EventVersion,
		Payload:      json.RawMessage(`{"review_id":"not-a-uuid"}`),
		CreatedAt:    time.Now().UTC(),
	}
	if err := h.Handle(context.Background(), event); !errors.Is(err, errInvalidEvent) {
		t.Fatalf("err = %v", err)
	}
	if _, err := store.GetProfile(context.Background(), mustID(t)); !errors.Is(err, errNotFound) {
		t.Fatalf("unexpected profile err = %v", err)
	}
	if err := h.Handle(context.Background(), outbox.Event{
		ID:           eid,
		EventType:    reviewscontracts.EventTypeVerifiedCreated,
		EventVersion: reviewscontracts.EventVersion,
		Payload:      json.RawMessage(`not-json`),
		CreatedAt:    time.Now().UTC(),
	}); !errors.Is(err, errInvalidEvent) {
		t.Fatalf("bad json err = %v", err)
	}
}

func mustProjector(t *testing.T) (*Projector, *MemoryStore) {
	t.Helper()
	store := NewMemoryStore()
	p, err := NewProjector(store, DefaultLevelPolicy())
	if err != nil {
		t.Fatal(err)
	}
	return p, store
}

func mustHandler(t *testing.T, p *Projector) *ProjectionHandler {
	t.Helper()
	h, err := NewProjectionHandler(p)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func mustService(t *testing.T, p *Projector) *Service {
	t.Helper()
	svc, err := NewService(p)
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func mustEvent(t *testing.T, requester, provider, interaction ID, at time.Time, eventID *outbox.ID) outbox.Event {
	t.Helper()
	return mustTypedEvent(t, requester, provider, interaction, at, verifiedcontracts.InteractionTypeListingInspection, eventID)
}

func mustTypedEvent(t *testing.T, requester, provider, interaction ID, at time.Time, interactionType string, eventID *outbox.ID) outbox.Event {
	t.Helper()
	payloadBody := verifiedcontracts.InteractionCompletedPayload{
		InteractionID:      interaction.String(),
		ListingID:          mustID(t).String(),
		RequesterUserID:    requester.String(),
		ProviderUserID:     provider.String(),
		InteractionType:    interactionType,
		VerificationMethod: "otp",
		VerifiedAt:         at.UTC().Format(time.RFC3339),
	}
	switch interactionType {
	case verifiedcontracts.InteractionTypeListingInspection:
		payloadBody.AppointmentID = mustID(t).String()
	case verifiedcontracts.InteractionTypeTransaction, verifiedcontracts.InteractionTypeDelivery:
		payloadBody.FlowID = mustID(t).String()
	}
	payload, err := json.Marshal(payloadBody)
	if err != nil {
		t.Fatal(err)
	}
	var eid outbox.ID
	if eventID != nil {
		eid = *eventID
	} else {
		eid, err = outbox.NewID()
		if err != nil {
			t.Fatal(err)
		}
	}
	return outbox.Event{
		ID:           eid,
		EventType:    verifiedcontracts.EventTypeInteractionCompleted,
		EventVersion: verifiedcontracts.EventVersion,
		Payload:      payload,
		CreatedAt:    at,
	}
}

func mustReviewEvent(t *testing.T, reviewer, provider ID, listingAccuracy, providerService int, at time.Time, eventID *outbox.ID) outbox.Event {
	t.Helper()
	payload, err := json.Marshal(reviewscontracts.VerifiedCreatedPayload{
		ReviewID:              mustID(t).String(),
		VerifiedInteractionID: mustID(t).String(),
		ListingID:             mustID(t).String(),
		ReviewerUserID:        reviewer.String(),
		ProviderUserID:        provider.String(),
		ListingAccuracy:       listingAccuracy,
		ProviderService:       providerService,
		CreatedAt:             at.UTC().Format(time.RFC3339),
	})
	if err != nil {
		t.Fatal(err)
	}
	var eid outbox.ID
	if eventID != nil {
		eid = *eventID
	} else {
		eid, err = outbox.NewID()
		if err != nil {
			t.Fatal(err)
		}
	}
	return outbox.Event{
		ID:           eid,
		EventType:    reviewscontracts.EventTypeVerifiedCreated,
		EventVersion: reviewscontracts.EventVersion,
		Payload:      payload,
		CreatedAt:    at,
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
