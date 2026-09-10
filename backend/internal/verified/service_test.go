package verified

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	listingcontracts "backend/internal/listings/contracts"
	"backend/internal/platform/outbox"
)

func TestCreateAppointmentForPublishedListing(t *testing.T) {
	env := newTestEnv(t)
	requester, provider, listing := mustID(t), mustID(t), mustID(t)
	env.listings.setPublished(listing, provider)

	appt, err := env.svc.CreateAppointment(context.Background(), requester, listing, nil)
	if err != nil {
		t.Fatal(err)
	}
	if appt.RequesterUserID != requester || appt.ProviderUserID != provider || appt.Status != StatusRequested {
		t.Fatalf("appt = %+v", appt)
	}
	got, err := env.store.GetAppointment(context.Background(), appt.ID)
	if err != nil || got.ID != appt.ID {
		t.Fatalf("stored = %+v err=%v", got, err)
	}
}

func TestCreateAppointmentRejectsSelf(t *testing.T) {
	env := newTestEnv(t)
	owner, listing := mustID(t), mustID(t)
	env.listings.setPublished(listing, owner)
	if _, err := env.svc.CreateAppointment(context.Background(), owner, listing, nil); !errors.Is(err, errSelfAppointment) {
		t.Fatalf("err = %v", err)
	}
}

func TestCreateAppointmentRejectsNonPublic(t *testing.T) {
	env := newTestEnv(t)
	requester, listing := mustID(t), mustID(t)
	if _, err := env.svc.CreateAppointment(context.Background(), requester, listing, nil); !errors.Is(err, errNotFound) {
		t.Fatalf("missing err = %v", err)
	}
	env.listings.refs[listingcontracts.ID(listing)] = listingcontracts.ListingRef{
		ID: listingcontracts.ID(listing), OwnerUserID: listingcontracts.ID(mustID(t)), Status: "draft",
	}
	if _, err := env.svc.CreateAppointment(context.Background(), requester, listing, nil); !errors.Is(err, errNotFound) {
		t.Fatalf("draft err = %v", err)
	}
}

func TestProviderCanAcceptForeignCannotReadOrModify(t *testing.T) {
	env := newTestEnv(t)
	requester, provider, listing, stranger := mustID(t), mustID(t), mustID(t), mustID(t)
	env.listings.setPublished(listing, provider)
	appt, err := env.svc.CreateAppointment(context.Background(), requester, listing, nil)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := env.svc.AcceptAppointment(context.Background(), provider, appt.ID)
	if err != nil || accepted.Status != StatusAccepted {
		t.Fatalf("accept = %+v err=%v", accepted, err)
	}
	if _, err := env.svc.GetAppointment(context.Background(), stranger, appt.ID); !errors.Is(err, errNotFound) {
		t.Fatalf("stranger get err = %v", err)
	}
	if _, err := env.svc.AcceptAppointment(context.Background(), stranger, appt.ID); !errors.Is(err, errNotFound) {
		t.Fatalf("stranger accept err = %v", err)
	}
	if _, err := env.svc.CancelAppointment(context.Background(), stranger, appt.ID); !errors.Is(err, errNotFound) {
		t.Fatalf("stranger cancel err = %v", err)
	}
}

func TestProviderStartsChallengeRequesterCannot(t *testing.T) {
	env := newTestEnv(t)
	requester, provider, listing := mustID(t), mustID(t), mustID(t)
	env.listings.setPublished(listing, provider)
	appt := mustAccepted(t, env.svc, requester, provider, listing)

	if _, err := env.svc.StartVerification(context.Background(), requester, appt.ID, MethodQR); !errors.Is(err, errForbidden) {
		t.Fatalf("requester start err = %v", err)
	}
	issued, err := env.svc.StartVerification(context.Background(), provider, appt.ID, MethodQR)
	if err != nil || issued.RawToken == "" {
		t.Fatalf("issued = %+v err=%v", issued, err)
	}
	stored, err := env.store.GetChallenge(context.Background(), issued.Challenge.ID)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stored.TokenHash, []byte(issued.RawToken)) {
		t.Fatal("raw token persisted in hash field")
	}
}

func TestCompletedAppointmentReadExposesVerifiedInteractionID(t *testing.T) {
	env := newTestEnv(t)
	requester, provider, listing, stranger := mustID(t), mustID(t), mustID(t), mustID(t)
	env.listings.setPublished(listing, provider)
	appt := mustAccepted(t, env.svc, requester, provider, listing)
	got, err := env.svc.GetAppointment(context.Background(), requester, appt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusAccepted || got.VerifiedInteractionID != nil {
		t.Fatalf("non-completed get = %+v", got)
	}
	issued, err := env.svc.StartVerification(context.Background(), provider, appt.ID, MethodQR)
	if err != nil {
		t.Fatal(err)
	}
	row, err := env.svc.FinishVerification(context.Background(), requester, appt.ID, issued.RawToken)
	if err != nil {
		t.Fatal(err)
	}
	completed, err := env.svc.GetAppointment(context.Background(), requester, appt.ID)
	if err != nil || completed.Status != StatusCompleted || completed.VerifiedInteractionID == nil || *completed.VerifiedInteractionID != row.ID {
		t.Fatalf("completed get = %+v err=%v want %s", completed, err, row.ID)
	}
	listed, err := env.svc.ListAppointments(context.Background(), requester)
	if err != nil || len(listed) != 1 || listed[0].VerifiedInteractionID == nil || *listed[0].VerifiedInteractionID != row.ID {
		t.Fatalf("list = %+v err=%v", listed, err)
	}
	providerList, err := env.svc.ListAppointments(context.Background(), provider)
	if err != nil || len(providerList) != 1 || providerList[0].VerifiedInteractionID == nil || *providerList[0].VerifiedInteractionID != row.ID {
		t.Fatalf("provider list = %+v err=%v", providerList, err)
	}
	if _, err := env.svc.GetAppointment(context.Background(), stranger, appt.ID); !errors.Is(err, errNotFound) {
		t.Fatalf("stranger get err = %v", err)
	}
	strangerList, err := env.svc.ListAppointments(context.Background(), stranger)
	if err != nil || len(strangerList) != 0 {
		t.Fatalf("stranger list = %+v err=%v", strangerList, err)
	}
}

func TestNumericOTPStartAndSuccessfulVerification(t *testing.T) {
	env := newTestEnv(t)
	requester, provider, listing := mustID(t), mustID(t), mustID(t)
	env.listings.setPublished(listing, provider)
	appt := mustAccepted(t, env.svc, requester, provider, listing)
	issued, err := env.svc.StartVerification(context.Background(), provider, appt.ID, MethodOTP)
	if err != nil {
		t.Fatal(err)
	}
	if len(issued.RawToken) != DefaultOTPDigits {
		t.Fatalf("otp width = %d raw=%q", len(issued.RawToken), issued.RawToken)
	}
	for _, r := range issued.RawToken {
		if r < '0' || r > '9' {
			t.Fatalf("non-digit OTP %q", issued.RawToken)
		}
	}
	stored, err := env.store.GetChallenge(context.Background(), issued.Challenge.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Method != MethodOTP {
		t.Fatalf("method = %s", stored.Method)
	}
	if bytes.Contains(stored.TokenHash, []byte(issued.RawToken)) || string(stored.TokenHash) == issued.RawToken {
		t.Fatal("plaintext OTP persisted")
	}
	want, err := HashNumericOTP(issued.RawToken)
	if err != nil || !bytes.Equal(stored.TokenHash, want) {
		t.Fatal("stored hash mismatch")
	}
	row, err := env.svc.FinishVerification(context.Background(), requester, appt.ID, issued.RawToken)
	if err != nil {
		t.Fatal(err)
	}
	if row.VerificationMethod != MethodOTP {
		t.Fatalf("row = %+v", row)
	}
}

func TestWrongNumericOTPFails(t *testing.T) {
	env := newTestEnv(t)
	requester, provider, listing := mustID(t), mustID(t), mustID(t)
	env.listings.setPublished(listing, provider)
	appt := mustAccepted(t, env.svc, requester, provider, listing)
	issued, err := env.svc.StartVerification(context.Background(), provider, appt.ID, MethodOTP)
	if err != nil {
		t.Fatal(err)
	}
	wrong := "000000"
	if issued.RawToken == wrong {
		wrong = "000001"
	}
	if _, err := env.svc.FinishVerification(context.Background(), requester, appt.ID, wrong); !errors.Is(err, errInvalidToken) {
		t.Fatalf("err = %v", err)
	}
	ch, err := env.store.GetChallenge(context.Background(), issued.Challenge.ID)
	if err != nil || ch.Consumed() {
		t.Fatalf("challenge consumed on wrong OTP: %+v err=%v", ch, err)
	}
	if len(env.enq.events) != 0 {
		t.Fatalf("events = %+v", env.enq.events)
	}
}

func TestExpiredAndConsumedNumericOTPRemainCorrect(t *testing.T) {
	env := newTestEnv(t)
	requester, provider, listing := mustID(t), mustID(t), mustID(t)
	env.listings.setPublished(listing, provider)
	appt := mustAccepted(t, env.svc, requester, provider, listing)
	issued, err := env.svc.StartVerification(context.Background(), provider, appt.ID, MethodOTP)
	if err != nil {
		t.Fatal(err)
	}
	env.clock.advance(DefaultChallengeTTL + time.Second)
	if _, err := env.svc.FinishVerification(context.Background(), requester, appt.ID, issued.RawToken); !errors.Is(err, errExpiredChallenge) {
		t.Fatalf("expired err = %v", err)
	}

	env2 := newTestEnv(t)
	requester2, provider2, listing2 := mustID(t), mustID(t), mustID(t)
	env2.listings.setPublished(listing2, provider2)
	appt2 := mustAccepted(t, env2.svc, requester2, provider2, listing2)
	issued2, err := env2.svc.StartVerification(context.Background(), provider2, appt2.ID, MethodOTP)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env2.svc.FinishVerification(context.Background(), requester2, appt2.ID, issued2.RawToken); err != nil {
		t.Fatal(err)
	}
	_, err = env2.svc.FinishVerification(context.Background(), requester2, appt2.ID, issued2.RawToken)
	if !errors.Is(err, errInvalidTransition) && !errors.Is(err, errChallengeConsumed) {
		t.Fatalf("replay err = %v", err)
	}
	if len(env2.enq.events) != 1 {
		t.Fatalf("events = %d", len(env2.enq.events))
	}
}

func TestQRChallengeUnchangedAlongsideOTP(t *testing.T) {
	env := newTestEnv(t)
	requester, provider, listing := mustID(t), mustID(t), mustID(t)
	env.listings.setPublished(listing, provider)
	appt := mustAccepted(t, env.svc, requester, provider, listing)
	issued, err := env.svc.StartVerification(context.Background(), provider, appt.ID, MethodQR)
	if err != nil {
		t.Fatal(err)
	}
	if looksLikeNumericOTP(issued.RawToken, DefaultOTPDigits) {
		t.Fatalf("QR token looks numeric: %q", issued.RawToken)
	}
	want, err := HashChallengeToken(issued.RawToken)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := env.store.GetChallenge(context.Background(), issued.Challenge.ID)
	if err != nil || !bytes.Equal(stored.TokenHash, want) || stored.Method != MethodQR {
		t.Fatalf("stored = %+v err=%v", stored, err)
	}
	row, err := env.svc.FinishVerification(context.Background(), requester, appt.ID, issued.RawToken)
	if err != nil || row.VerificationMethod != MethodQR {
		t.Fatalf("row = %+v err=%v", row, err)
	}
}

func TestCorrectTokenVerifiesAndCompletes(t *testing.T) {
	env := newTestEnv(t)
	requester, provider, listing := mustID(t), mustID(t), mustID(t)
	env.listings.setPublished(listing, provider)
	appt := mustAccepted(t, env.svc, requester, provider, listing)
	issued, err := env.svc.StartVerification(context.Background(), provider, appt.ID, MethodOTP)
	if err != nil {
		t.Fatal(err)
	}
	row, err := env.svc.FinishVerification(context.Background(), requester, appt.ID, issued.RawToken)
	if err != nil {
		t.Fatal(err)
	}
	if row.InteractionType != InteractionListingInspection || row.VerificationMethod != MethodOTP {
		t.Fatalf("row = %+v", row)
	}
	completed, err := env.store.GetAppointment(context.Background(), appt.ID)
	if err != nil || completed.Status != StatusCompleted {
		t.Fatalf("appt = %+v err=%v", completed, err)
	}
	ch, err := env.store.GetChallenge(context.Background(), issued.Challenge.ID)
	if err != nil || !ch.Consumed() {
		t.Fatalf("challenge = %+v err=%v", ch, err)
	}
	if len(env.enq.events) != 1 || env.enq.events[0].EventType != EventTypeInteractionCompleted {
		t.Fatalf("events = %+v", env.enq.events)
	}
	if strings.Contains(string(env.enq.events[0].Payload), issued.RawToken) {
		t.Fatal("raw token leaked into outbox payload")
	}
	var payload InteractionCompletedPayload
	if err := json.Unmarshal(env.enq.events[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.InteractionID != row.ID.String() || payload.AppointmentID != appt.ID.String() ||
		payload.ListingID != listing.String() || payload.VerificationMethod != MethodOTP ||
		payload.InteractionType != InteractionListingInspection || payload.FlowID != "" {
		t.Fatalf("payload = %+v", payload)
	}
	if strings.Contains(strings.ToLower(string(env.enq.events[0].Payload)), "token") {
		t.Fatal("payload must not contain token keys")
	}
}

func TestWrongTokenFails(t *testing.T) {
	env := newTestEnv(t)
	requester, provider, listing := mustID(t), mustID(t), mustID(t)
	env.listings.setPublished(listing, provider)
	appt := mustAccepted(t, env.svc, requester, provider, listing)
	if _, err := env.svc.StartVerification(context.Background(), provider, appt.ID, MethodQR); err != nil {
		t.Fatal(err)
	}
	other, _, err := GenerateChallengeToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.FinishVerification(context.Background(), requester, appt.ID, other); !errors.Is(err, errInvalidToken) {
		t.Fatalf("err = %v", err)
	}
	if len(env.enq.events) != 0 {
		t.Fatalf("events = %+v", env.enq.events)
	}
}

func TestExpiredTokenFails(t *testing.T) {
	env := newTestEnv(t)
	requester, provider, listing := mustID(t), mustID(t), mustID(t)
	env.listings.setPublished(listing, provider)
	appt := mustAccepted(t, env.svc, requester, provider, listing)
	issued, err := env.svc.StartVerification(context.Background(), provider, appt.ID, MethodQR)
	if err != nil {
		t.Fatal(err)
	}
	env.clock.advance(DefaultChallengeTTL + time.Second)
	if _, err := env.svc.FinishVerification(context.Background(), requester, appt.ID, issued.RawToken); !errors.Is(err, errExpiredChallenge) {
		t.Fatalf("err = %v", err)
	}
}

func TestReplayDoesNotDuplicateInteraction(t *testing.T) {
	env := newTestEnv(t)
	requester, provider, listing := mustID(t), mustID(t), mustID(t)
	env.listings.setPublished(listing, provider)
	appt := mustAccepted(t, env.svc, requester, provider, listing)
	issued, err := env.svc.StartVerification(context.Background(), provider, appt.ID, MethodQR)
	if err != nil {
		t.Fatal(err)
	}
	first, err := env.svc.FinishVerification(context.Background(), requester, appt.ID, issued.RawToken)
	if err != nil {
		t.Fatal(err)
	}
	_, err = env.svc.FinishVerification(context.Background(), requester, appt.ID, issued.RawToken)
	if !errors.Is(err, errInvalidTransition) && !errors.Is(err, errChallengeConsumed) {
		t.Fatalf("replay err = %v", err)
	}
	got, err := env.store.GetInteractionByAppointment(context.Background(), appt.ID)
	if err != nil || got.ID != first.ID {
		t.Fatalf("interaction = %+v err=%v", got, err)
	}
	if len(env.enq.events) != 1 {
		t.Fatalf("events = %d", len(env.enq.events))
	}
}

func TestRawTokenNotPersisted(t *testing.T) {
	env := newTestEnv(t)
	requester, provider, listing := mustID(t), mustID(t), mustID(t)
	env.listings.setPublished(listing, provider)
	appt := mustAccepted(t, env.svc, requester, provider, listing)
	issued, err := env.svc.StartVerification(context.Background(), provider, appt.ID, MethodQR)
	if err != nil {
		t.Fatal(err)
	}
	want, err := HashChallengeToken(issued.RawToken)
	if err != nil {
		t.Fatal(err)
	}
	for _, ch := range env.store.ChallengesSnapshot() {
		if string(ch.TokenHash) == issued.RawToken || bytes.Contains(ch.TokenHash, []byte(issued.RawToken)) {
			t.Fatal("raw token persisted")
		}
		if ch.ID == issued.Challenge.ID && !bytes.Equal(ch.TokenHash, want) {
			t.Fatal("stored hash mismatch")
		}
	}
}

func TestGenerateChallengeTokenHighEntropy(t *testing.T) {
	raw, hash, err := GenerateChallengeToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(hash) != TokenHashSize {
		t.Fatalf("hash len = %d", len(hash))
	}
	got, err := HashChallengeToken(raw)
	if err != nil || !bytes.Equal(got, hash) {
		t.Fatalf("roundtrip hash mismatch err=%v", err)
	}
}

func TestTransactionAndDeliveryVerificationStartComplete(t *testing.T) {
	for _, typ := range []string{InteractionTransaction, InteractionDelivery} {
		env := newTestEnv(t)
		requester, provider, listing := mustID(t), mustID(t), mustID(t)
		env.listings.setPublished(listing, provider)
		var issued IssuedChallenge
		var err error
		if typ == InteractionTransaction {
			issued, err = env.svc.StartTransactionVerification(context.Background(), provider, listing, requester, MethodQR)
		} else {
			issued, err = env.svc.StartDeliveryVerification(context.Background(), provider, listing, requester, MethodOTP)
		}
		if err != nil || issued.RawToken == "" || issued.InteractionType != typ || issued.Challenge.FlowID.IsZero() {
			t.Fatalf("%s issued = %+v err=%v", typ, issued, err)
		}
		stored, err := env.store.GetChallenge(context.Background(), issued.Challenge.ID)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(stored.TokenHash, []byte(issued.RawToken)) {
			t.Fatalf("%s raw secret persisted", typ)
		}
		row, err := env.svc.FinishFlowVerification(context.Background(), requester, issued.Challenge.FlowID, issued.RawToken)
		if err != nil {
			t.Fatal(err)
		}
		if row.InteractionType != typ || row.AppointmentID != (ID{}) || row.FlowID != issued.Challenge.FlowID {
			t.Fatalf("%s row = %+v", typ, row)
		}
		if len(env.enq.events) != 1 {
			t.Fatalf("%s events = %d", typ, len(env.enq.events))
		}
		var payload InteractionCompletedPayload
		if err := json.Unmarshal(env.enq.events[0].Payload, &payload); err != nil {
			t.Fatal(err)
		}
		if payload.InteractionType != typ || payload.AppointmentID != "" || payload.FlowID != issued.Challenge.FlowID.String() {
			t.Fatalf("%s payload = %+v", typ, payload)
		}
		if strings.Contains(string(env.enq.events[0].Payload), issued.RawToken) {
			t.Fatalf("%s raw token leaked into outbox", typ)
		}
		got, err := env.store.GetInteraction(context.Background(), row.ID)
		if err != nil || got.InteractionType != typ {
			t.Fatalf("%s stored = %+v err=%v", typ, got, err)
		}
	}
}

func TestInvalidInteractionTypeRejected(t *testing.T) {
	env := newTestEnv(t)
	requester, provider, listing := mustID(t), mustID(t), mustID(t)
	env.listings.setPublished(listing, provider)
	if _, err := env.svc.StartTypedVerification(context.Background(), provider, listing, requester, MethodQR, "escrow"); !errors.Is(err, errInvalidInteraction) {
		t.Fatalf("escrow err = %v", err)
	}
	if _, err := env.svc.StartTypedVerification(context.Background(), provider, listing, requester, MethodQR, InteractionListingInspection); !errors.Is(err, errInvalidInteraction) {
		t.Fatalf("listing via typed start err = %v", err)
	}
}

func TestParticipantFlowListAndRead(t *testing.T) {
	env := newTestEnv(t)
	requester, provider, listing, stranger := mustID(t), mustID(t), mustID(t), mustID(t)
	otherListing := mustID(t)
	env.listings.setPublished(listing, provider)
	env.listings.setPublished(otherListing, provider)

	first, err := env.svc.StartTransactionVerification(context.Background(), provider, listing, requester, MethodQR)
	if err != nil {
		t.Fatal(err)
	}
	if first.RawToken == "" {
		t.Fatal("start must still issue a one-time secret")
	}
	env.clock.advance(time.Second)
	second, err := env.svc.StartDeliveryVerification(context.Background(), provider, listing, requester, MethodOTP)
	if err != nil {
		t.Fatal(err)
	}
	env.clock.advance(time.Second)
	other, err := env.svc.StartTransactionVerification(context.Background(), provider, otherListing, requester, MethodQR)
	if err != nil {
		t.Fatal(err)
	}

	open, err := env.svc.GetFlow(context.Background(), requester, first.Challenge.FlowID)
	if err != nil {
		t.Fatal(err)
	}
	if open.Status != FlowOpen || open.CompletedInteractionID != nil {
		t.Fatalf("open get = %+v", open)
	}
	providerOpen, err := env.svc.GetFlow(context.Background(), provider, first.Challenge.FlowID)
	if err != nil || providerOpen.ID != open.ID {
		t.Fatalf("provider get = %+v err=%v", providerOpen, err)
	}
	if _, err := env.svc.GetFlow(context.Background(), stranger, first.Challenge.FlowID); !errors.Is(err, errNotFound) {
		t.Fatalf("stranger get err = %v", err)
	}

	row, err := env.svc.FinishFlowVerification(context.Background(), requester, first.Challenge.FlowID, first.RawToken)
	if err != nil {
		t.Fatal(err)
	}
	completed, err := env.svc.GetFlow(context.Background(), requester, first.Challenge.FlowID)
	if err != nil || completed.Status != FlowCompleted || completed.CompletedInteractionID == nil || *completed.CompletedInteractionID != row.ID {
		t.Fatalf("completed get = %+v err=%v want %s", completed, err, row.ID)
	}

	listed, err := env.svc.ListFlows(context.Background(), requester, listing)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 2 {
		t.Fatalf("listing-scoped list = %+v", listed)
	}
	if listed[0].ID != second.Challenge.FlowID || listed[1].ID != first.Challenge.FlowID {
		t.Fatalf("order = %s %s want %s %s", listed[0].ID, listed[1].ID, second.Challenge.FlowID, first.Challenge.FlowID)
	}
	if listed[0].Status != FlowOpen || listed[0].CompletedInteractionID != nil {
		t.Fatalf("active list row = %+v", listed[0])
	}
	if listed[1].CompletedInteractionID == nil || *listed[1].CompletedInteractionID != row.ID {
		t.Fatalf("completed list row = %+v", listed[1])
	}

	providerList, err := env.svc.ListFlows(context.Background(), provider, ID{})
	if err != nil || len(providerList) != 3 {
		t.Fatalf("provider list = %+v err=%v", providerList, err)
	}
	if providerList[0].ID != other.Challenge.FlowID || providerList[1].ID != second.Challenge.FlowID || providerList[2].ID != first.Challenge.FlowID {
		t.Fatalf("unfiltered order = %+v", providerList)
	}

	strangerList, err := env.svc.ListFlows(context.Background(), stranger, ID{})
	if err != nil || len(strangerList) != 0 {
		t.Fatalf("stranger list = %+v err=%v", strangerList, err)
	}
}

func TestListFlowsBounded(t *testing.T) {
	env := newTestEnv(t)
	user := mustID(t)
	now := env.clock.now
	for i := 0; i < MaxVerificationFlows+3; i++ {
		id := mustID(t)
		flow := VerificationFlow{
			ID:              id,
			ListingID:       mustID(t),
			RequesterUserID: user,
			ProviderUserID:  mustID(t),
			InteractionType: InteractionTransaction,
			Status:          FlowOpen,
			CreatedAt:       now.Add(time.Duration(i) * time.Second),
			UpdatedAt:       now.Add(time.Duration(i) * time.Second),
		}
		ch := Challenge{
			ID:        mustID(t),
			FlowID:    id,
			Method:    MethodQR,
			TokenHash: make([]byte, TokenHashSize),
			CreatedAt: flow.CreatedAt,
			ExpiresAt: flow.CreatedAt.Add(DefaultChallengeTTL),
		}
		if err := env.store.CreateFlowWithChallenge(context.Background(), flow, ch, now); err != nil {
			t.Fatal(err)
		}
	}
	listed, err := env.svc.ListFlows(context.Background(), user, ID{})
	if err != nil || len(listed) != MaxVerificationFlows {
		t.Fatalf("len = %d err=%v", len(listed), err)
	}
	for i := 1; i < len(listed); i++ {
		if listed[i-1].CreatedAt.Equal(listed[i].CreatedAt) {
			if listed[i-1].ID.String() <= listed[i].ID.String() {
				t.Fatalf("id tie-break = %s then %s", listed[i-1].ID, listed[i].ID)
			}
			continue
		}
		if !listed[i-1].CreatedAt.After(listed[i].CreatedAt) {
			t.Fatalf("created_at order = %s then %s", listed[i-1].CreatedAt, listed[i].CreatedAt)
		}
	}
}

func TestFlowChallengeSingleUseAndExpired(t *testing.T) {
	env := newTestEnv(t)
	requester, provider, listing := mustID(t), mustID(t), mustID(t)
	env.listings.setPublished(listing, provider)
	issued, err := env.svc.StartTransactionVerification(context.Background(), provider, listing, requester, MethodQR)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.FinishFlowVerification(context.Background(), requester, issued.Challenge.FlowID, issued.RawToken); err != nil {
		t.Fatal(err)
	}
	_, err = env.svc.FinishFlowVerification(context.Background(), requester, issued.Challenge.FlowID, issued.RawToken)
	if !errors.Is(err, errInvalidTransition) && !errors.Is(err, errChallengeConsumed) {
		t.Fatalf("replay err = %v", err)
	}
	if len(env.enq.events) != 1 {
		t.Fatalf("events = %d", len(env.enq.events))
	}

	env2 := newTestEnv(t)
	requester2, provider2, listing2 := mustID(t), mustID(t), mustID(t)
	env2.listings.setPublished(listing2, provider2)
	issued2, err := env2.svc.StartDeliveryVerification(context.Background(), provider2, listing2, requester2, MethodOTP)
	if err != nil {
		t.Fatal(err)
	}
	env2.clock.advance(DefaultChallengeTTL + time.Second)
	if _, err := env2.svc.FinishFlowVerification(context.Background(), requester2, issued2.Challenge.FlowID, issued2.RawToken); !errors.Is(err, errExpiredChallenge) {
		t.Fatalf("expired err = %v", err)
	}
	if len(env2.enq.events) != 0 {
		t.Fatalf("expired events = %+v", env2.enq.events)
	}
}

func TestListingInspectionUnchangedAlongsideFlows(t *testing.T) {
	env := newTestEnv(t)
	requester, provider, listing := mustID(t), mustID(t), mustID(t)
	env.listings.setPublished(listing, provider)
	appt := mustAccepted(t, env.svc, requester, provider, listing)
	issued, err := env.svc.StartVerification(context.Background(), provider, appt.ID, MethodQR)
	if err != nil {
		t.Fatal(err)
	}
	row, err := env.svc.FinishVerification(context.Background(), requester, appt.ID, issued.RawToken)
	if err != nil || row.InteractionType != InteractionListingInspection || row.AppointmentID != appt.ID {
		t.Fatalf("row = %+v err=%v", row, err)
	}
	var payload InteractionCompletedPayload
	if err := json.Unmarshal(env.enq.events[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.InteractionType != InteractionListingInspection || payload.AppointmentID != appt.ID.String() || payload.FlowID != "" {
		t.Fatalf("payload = %+v", payload)
	}
}

func mustAccepted(t *testing.T, svc *Service, requester, provider, listing ID) Appointment {
	t.Helper()
	appt, err := svc.CreateAppointment(context.Background(), requester, listing, nil)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := svc.AcceptAppointment(context.Background(), provider, appt.ID)
	if err != nil {
		t.Fatal(err)
	}
	return accepted
}

type testEnv struct {
	svc      *Service
	store    *MemoryStore
	listings *stubListings
	enq      *memoryEnqueuer
	clock    *testClock
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	store := NewMemoryStore()
	listings := &stubListings{refs: map[listingcontracts.ID]listingcontracts.ListingRef{}}
	enq := &memoryEnqueuer{}
	clock := &testClock{now: time.Unix(100, 0).UTC()}
	svc, err := NewService(store, listings, enq, DefaultPolicy(), func() time.Time { return clock.now })
	if err != nil {
		t.Fatal(err)
	}
	return &testEnv{svc: svc, store: store, listings: listings, enq: enq, clock: clock}
}

type stubListings struct {
	refs map[listingcontracts.ID]listingcontracts.ListingRef
}

func (s *stubListings) setPublished(listingID, owner ID) {
	s.refs[listingcontracts.ID(listingID)] = listingcontracts.ListingRef{
		ID:          listingcontracts.ID(listingID),
		OwnerUserID: listingcontracts.ID(owner),
		Status:      listingcontracts.StatusPublished,
	}
}

func (s *stubListings) ResolveListingOwner(_ context.Context, listingID listingcontracts.ID) (listingcontracts.ListingRef, error) {
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

type memoryEnqueuer struct {
	events []outbox.NewEvent
}

func (e *memoryEnqueuer) Enqueue(_ context.Context, _ outbox.Execer, in outbox.NewEvent) (outbox.Event, error) {
	e.events = append(e.events, in)
	id, err := outbox.NewID()
	if err != nil {
		return outbox.Event{}, err
	}
	return outbox.Event{ID: id, EventType: in.EventType, EventVersion: in.EventVersion, Payload: in.Payload}, nil
}

type testClock struct {
	now time.Time
}

func (c *testClock) advance(d time.Duration) {
	c.now = c.now.Add(d)
}

func mustID(t *testing.T) ID {
	t.Helper()
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
