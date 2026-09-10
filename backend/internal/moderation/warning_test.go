package moderation

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	notifycontracts "backend/internal/notifications/contracts"
	"backend/internal/platform/outbox"
)

func TestListingWarningNotifiesListingOwner(t *testing.T) {
	env := newServiceEnv(t)
	listing := env.publishListing(t, env.other)
	action := mustExecuteWarning(t, env, TargetListing, listing)
	if action.Status != ActionStatusExecuted {
		t.Fatalf("status = %s", action.Status)
	}
	if len(env.outbox.events) != 1 {
		t.Fatalf("events = %d", len(env.outbox.events))
	}
	intent := decodeWarningEvent(t, env.outbox.events[0])
	if intent.Recipient.ID != env.other.String() {
		t.Fatalf("recipient = %s want %s", intent.Recipient.ID, env.other)
	}
	if intent.TargetType != notifycontracts.WarningTargetListing || intent.TargetRef != listing.String() {
		t.Fatalf("target = %s %s", intent.TargetType, intent.TargetRef)
	}
	if intent.MessageKey != notifycontracts.MessageKeyWarningListing || intent.Locale != notifycontracts.LocaleTR {
		t.Fatalf("message/locale = %s %s", intent.MessageKey, intent.Locale)
	}
}

func TestPublicProfileWarningNotifiesProfileOwner(t *testing.T) {
	env := newServiceEnv(t)
	profile := env.putProfile(t, env.other)
	mustExecuteWarning(t, env, TargetPublicProfile, profile)
	if len(env.outbox.events) != 1 {
		t.Fatalf("events = %d", len(env.outbox.events))
	}
	intent := decodeWarningEvent(t, env.outbox.events[0])
	if intent.Recipient.ID != env.other.String() {
		t.Fatalf("recipient = %s", intent.Recipient.ID)
	}
	if intent.TargetType != notifycontracts.WarningTargetPublicProfile || intent.TargetRef != profile.String() {
		t.Fatalf("target = %s %s", intent.TargetType, intent.TargetRef)
	}
	if intent.MessageKey != notifycontracts.MessageKeyWarningPublicProfile {
		t.Fatalf("message = %s", intent.MessageKey)
	}
}

func TestNoActionCreatesNoWarningNotification(t *testing.T) {
	env := newServiceEnv(t)
	listing := env.publishListing(t, env.other)
	detail, err := env.svc.CreateCase(context.Background(), CreateCaseInput{
		SubjectType: TargetListing, SubjectID: listing, Title: "no action",
	})
	if err != nil {
		t.Fatal(err)
	}
	row, err := env.svc.CreateAction(context.Background(), detail.Case.ID, CreateActionInput{
		TargetType: TargetListing, TargetID: listing, ActionType: ActionTypeNoAction,
		ReasonCode: ActionReasonNoViolation,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.TransitionAction(context.Background(), detail.Case.ID, row.ID, ActionTransitionInput{To: ActionStatusApproved}); err != nil {
		t.Fatal(err)
	}
	executed, err := env.svc.TransitionAction(context.Background(), detail.Case.ID, row.ID, ActionTransitionInput{To: ActionStatusExecuted})
	if err != nil || executed.Status != ActionStatusExecuted {
		t.Fatalf("execute = %+v err=%v", executed, err)
	}
	if len(env.outbox.events) != 0 {
		t.Fatalf("no_action events = %+v", env.outbox.events)
	}
}

func TestFailedWarningIntentDoesNotExecute(t *testing.T) {
	env := newServiceEnv(t)
	listing := env.publishListing(t, env.other)
	env.outbox.err = errUnavailable
	detail, err := env.svc.CreateCase(context.Background(), CreateCaseInput{
		SubjectType: TargetListing, SubjectID: listing, Title: "fail notify",
	})
	if err != nil {
		t.Fatal(err)
	}
	row, err := env.svc.CreateAction(context.Background(), detail.Case.ID, CreateActionInput{
		TargetType: TargetListing, TargetID: listing, ActionType: ActionTypeWarning,
		ReasonCode: ActionReasonPolicyViolation, Rationale: "internal staff only",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.TransitionAction(context.Background(), detail.Case.ID, row.ID, ActionTransitionInput{To: ActionStatusApproved}); err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.TransitionAction(context.Background(), detail.Case.ID, row.ID, ActionTransitionInput{To: ActionStatusExecuted}); !errors.Is(err, errUnavailable) {
		t.Fatalf("execute err = %v", err)
	}
	got, err := env.svc.GetAction(context.Background(), detail.Case.ID, row.ID)
	if err != nil || got.Status != ActionStatusApproved {
		t.Fatalf("stayed approved = %+v err=%v", got, err)
	}
	if len(env.outbox.events) != 0 {
		t.Fatalf("failed enqueue leaked events = %+v", env.outbox.events)
	}
	env.outbox.err = nil
	env.now = env.now.Add(time.Second)
	executed, err := env.svc.TransitionAction(context.Background(), detail.Case.ID, row.ID, ActionTransitionInput{To: ActionStatusExecuted})
	if err != nil || executed.Status != ActionStatusExecuted {
		t.Fatalf("retry execute = %+v err=%v", executed, err)
	}
	if len(env.outbox.events) != 1 {
		t.Fatalf("retry events = %d", len(env.outbox.events))
	}
}

func TestWarningRetryIsIdempotent(t *testing.T) {
	env := newServiceEnv(t)
	listing := env.publishListing(t, env.other)
	env.svc.SetOutbox(&enqueueThenFailStore{env: env})
	detail, err := env.svc.CreateCase(context.Background(), CreateCaseInput{
		SubjectType: TargetListing, SubjectID: listing, Title: "idempotent",
	})
	if err != nil {
		t.Fatal(err)
	}
	row, err := env.svc.CreateAction(context.Background(), detail.Case.ID, CreateActionInput{
		TargetType: TargetListing, TargetID: listing, ActionType: ActionTypeWarning,
		ReasonCode: ActionReasonSafetyRisk,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.TransitionAction(context.Background(), detail.Case.ID, row.ID, ActionTransitionInput{To: ActionStatusApproved}); err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.TransitionAction(context.Background(), detail.Case.ID, row.ID, ActionTransitionInput{To: ActionStatusExecuted}); !errors.Is(err, errUnavailable) {
		t.Fatalf("first execute err = %v", err)
	}
	env.store.SetFail(nil)
	got, err := env.svc.GetAction(context.Background(), detail.Case.ID, row.ID)
	if err != nil || got.Status != ActionStatusApproved {
		t.Fatalf("must stay approved after enqueue+store fail = %+v err=%v", got, err)
	}
	if len(env.outbox.events) != 1 {
		t.Fatalf("first enqueue events = %d", len(env.outbox.events))
	}
	env.now = env.now.Add(time.Second)
	executed, err := env.svc.TransitionAction(context.Background(), detail.Case.ID, row.ID, ActionTransitionInput{To: ActionStatusExecuted})
	if err != nil || executed.Status != ActionStatusExecuted {
		t.Fatalf("retry = %+v err=%v", executed, err)
	}
	if len(env.outbox.events) != 1 {
		t.Fatalf("duplicate warning events = %d", len(env.outbox.events))
	}
}

func TestWarningPayloadOmitsInternalModerationData(t *testing.T) {
	env := newServiceEnv(t)
	listing := env.publishListing(t, env.other)
	mustExecuteWarning(t, env, TargetListing, listing)
	intent := decodeWarningEvent(t, env.outbox.events[0])
	user, err := json.Marshal(intent.UserPayload())
	if err != nil {
		t.Fatal(err)
	}
	s := strings.ToLower(string(user))
	raw := strings.ToLower(string(env.outbox.events[0].Payload))
	if strings.Contains(raw, "rationale") || strings.Contains(raw, "staff") ||
		strings.Contains(raw, "reporter") || strings.Contains(raw, "evidence") {
		t.Fatalf("outbox leaked internals: %s", env.outbox.events[0].Payload)
	}
	if strings.Contains(s, env.other.String()) {
		t.Fatalf("user payload contained owner uuid: %s", user)
	}
	if strings.Contains(s, "recipient") || strings.Contains(s, "intent_id") {
		t.Fatalf("user payload leaked routing: %s", user)
	}
}

func TestWarningDoesNotNotifyWrongUser(t *testing.T) {
	env := newServiceEnv(t)
	listingA := env.publishListing(t, env.other)
	listingB := env.publishListing(t, env.reporter)
	mustExecuteWarning(t, env, TargetListing, listingA)
	intent := decodeWarningEvent(t, env.outbox.events[0])
	if intent.Recipient.ID != env.other.String() {
		t.Fatalf("wrong recipient = %s", intent.Recipient.ID)
	}
	if intent.TargetRef != listingA.String() || intent.TargetRef == listingB.String() {
		t.Fatalf("wrong target = %s", intent.TargetRef)
	}
}

func TestWarningWithoutOutboxDoesNotExecute(t *testing.T) {
	env := newServiceEnv(t)
	env.svc.SetOutbox(nil)
	listing := env.publishListing(t, env.other)
	detail, err := env.svc.CreateCase(context.Background(), CreateCaseInput{
		SubjectType: TargetListing, SubjectID: listing, Title: "no outbox",
	})
	if err != nil {
		t.Fatal(err)
	}
	row, err := env.svc.CreateAction(context.Background(), detail.Case.ID, CreateActionInput{
		TargetType: TargetListing, TargetID: listing, ActionType: ActionTypeWarning,
		ReasonCode: ActionReasonPolicyViolation,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.TransitionAction(context.Background(), detail.Case.ID, row.ID, ActionTransitionInput{To: ActionStatusApproved}); err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.TransitionAction(context.Background(), detail.Case.ID, row.ID, ActionTransitionInput{To: ActionStatusExecuted}); !errors.Is(err, errUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func TestWarningDoesNotCallListingOrProfileEnforcement(t *testing.T) {
	env := newServiceEnv(t)
	enforcer := &stubListingEnforcer{}
	env.svc.SetListingEnforcement(enforcer)
	listing := env.publishListing(t, env.other)
	mustExecuteWarning(t, env, TargetListing, listing)
	if len(enforcer.calls) != 0 || len(enforcer.clears) != 0 {
		t.Fatalf("listing enforce = %+v clears=%+v", enforcer.calls, enforcer.clears)
	}
	if len(env.profilesEnforce.calls) != 0 {
		t.Fatalf("profile enforce = %+v", env.profilesEnforce.calls)
	}
}

func mustExecuteWarning(t *testing.T, env *serviceEnv, targetType TargetType, subject ID) CaseAction {
	t.Helper()
	detail, err := env.svc.CreateCase(context.Background(), CreateCaseInput{
		SubjectType: targetType, SubjectID: subject, Title: "warning",
	})
	if err != nil {
		t.Fatal(err)
	}
	row, err := env.svc.CreateAction(context.Background(), detail.Case.ID, CreateActionInput{
		TargetType: targetType, TargetID: subject, ActionType: ActionTypeWarning,
		ReasonCode: ActionReasonPolicyViolation,
	})
	if err != nil {
		t.Fatal(err)
	}
	env.now = env.now.Add(time.Second)
	if _, err := env.svc.TransitionAction(context.Background(), detail.Case.ID, row.ID, ActionTransitionInput{To: ActionStatusApproved}); err != nil {
		t.Fatal(err)
	}
	env.now = env.now.Add(time.Second)
	executed, err := env.svc.TransitionAction(context.Background(), detail.Case.ID, row.ID, ActionTransitionInput{To: ActionStatusExecuted})
	if err != nil {
		t.Fatal(err)
	}
	return executed
}

func decodeWarningEvent(t *testing.T, ev outbox.Event) notifycontracts.WarningIntent {
	t.Helper()
	if ev.EventType != notifycontracts.WarningEventType || ev.EventVersion != notifycontracts.WarningEventVersion {
		t.Fatalf("event = %s v%d", ev.EventType, ev.EventVersion)
	}
	intent, err := notifycontracts.DecodeWarningIntent(ev.Payload)
	if err != nil {
		t.Fatal(err)
	}
	return intent
}

type enqueueThenFailStore struct {
	env    *serviceEnv
	failed bool
}

func (f *enqueueThenFailStore) Enqueue(ctx context.Context, exec outbox.Execer, in outbox.NewEvent) (outbox.Event, error) {
	ev, err := f.env.outbox.Enqueue(ctx, exec, in)
	if err != nil {
		return ev, err
	}
	if !f.failed {
		f.failed = true
		f.env.store.SetFail(errUnavailable)
	} else {
		f.env.store.SetFail(nil)
	}
	return ev, nil
}
