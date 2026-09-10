package moderation

import (
	"context"
	"errors"
	"testing"
	"time"

	identitycontracts "backend/internal/identity/contracts"
	listingcontracts "backend/internal/listings/contracts"
)

func TestListingRestrictRemoveExecuteThroughListingsContract(t *testing.T) {
	env := newServiceEnv(t)
	enforcer := &stubListingEnforcer{}
	env.svc.SetListingEnforcement(enforcer)
	subject := mustID(t)
	detail, err := env.svc.CreateCase(context.Background(), CreateCaseInput{
		SubjectType: TargetListing, SubjectID: subject, Title: "enforce",
	})
	if err != nil {
		t.Fatal(err)
	}
	caseID := detail.Case.ID
	restrict, err := env.svc.CreateAction(context.Background(), caseID, CreateActionInput{
		TargetType: TargetListing, TargetID: subject, ActionType: ActionTypeRestrict,
		ReasonCode: ActionReasonPolicyViolation,
	})
	if err != nil {
		t.Fatal(err)
	}
	env.now = env.now.Add(time.Second)
	if _, err := env.svc.TransitionAction(context.Background(), caseID, restrict.ID, ActionTransitionInput{To: ActionStatusApproved}); err != nil {
		t.Fatal(err)
	}
	env.now = env.now.Add(time.Second)
	executed, err := env.svc.TransitionAction(context.Background(), caseID, restrict.ID, ActionTransitionInput{To: ActionStatusExecuted})
	if err != nil || executed.Status != ActionStatusExecuted {
		t.Fatalf("restrict execute = %+v err=%v", executed, err)
	}
	if len(enforcer.calls) != 1 || enforcer.calls[0].State != listingcontracts.ModerationStateRestricted {
		t.Fatalf("restrict calls = %+v", enforcer.calls)
	}
	env.now = env.now.Add(time.Second)
	remove, err := env.svc.CreateAction(context.Background(), caseID, CreateActionInput{
		TargetType: TargetListing, TargetID: subject, ActionType: ActionTypeRemove,
		ReasonCode: ActionReasonProhibitedContent,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.TransitionAction(context.Background(), caseID, remove.ID, ActionTransitionInput{To: ActionStatusApproved}); err != nil {
		t.Fatal(err)
	}
	env.now = env.now.Add(time.Second)
	if _, err := env.svc.TransitionAction(context.Background(), caseID, remove.ID, ActionTransitionInput{To: ActionStatusExecuted}); err != nil {
		t.Fatal(err)
	}
	if len(enforcer.calls) != 2 || enforcer.calls[1].State != listingcontracts.ModerationStateRemoved {
		t.Fatalf("remove calls = %+v", enforcer.calls)
	}
	env.now = env.now.Add(time.Second)
	if _, err := env.svc.TransitionAction(context.Background(), caseID, remove.ID, ActionTransitionInput{To: ActionStatusExecuted}); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("repeat executed transition err = %v", err)
	}
	if err := enforcer.ApplyModerationState(context.Background(), listingcontracts.ApplyModerationInput{
		ListingID: listingcontracts.ID(subject), State: listingcontracts.ModerationStateRemoved,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestListingNoActionAndWarningDoNotCallListings(t *testing.T) {
	env := newServiceEnv(t)
	enforcer := &stubListingEnforcer{}
	env.svc.SetListingEnforcement(enforcer)
	subject := env.publishListing(t, env.other)
	detail, err := env.svc.CreateCase(context.Background(), CreateCaseInput{
		SubjectType: TargetListing, SubjectID: subject, Title: "record only",
	})
	if err != nil {
		t.Fatal(err)
	}
	caseID := detail.Case.ID
	for _, actionType := range []ActionType{ActionTypeNoAction, ActionTypeWarning} {
		env.now = env.now.Add(time.Second)
		row, err := env.svc.CreateAction(context.Background(), caseID, CreateActionInput{
			TargetType: TargetListing, TargetID: subject, ActionType: actionType,
			ReasonCode: ActionReasonNoViolation,
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := env.svc.TransitionAction(context.Background(), caseID, row.ID, ActionTransitionInput{To: ActionStatusApproved}); err != nil {
			t.Fatal(err)
		}
		env.now = env.now.Add(time.Second)
		executed, err := env.svc.TransitionAction(context.Background(), caseID, row.ID, ActionTransitionInput{To: ActionStatusExecuted})
		if err != nil || executed.Status != ActionStatusExecuted {
			t.Fatalf("%s execute = %+v err=%v", actionType, executed, err)
		}
	}
	if len(enforcer.calls) != 0 {
		t.Fatalf("unexpected listings calls = %+v", enforcer.calls)
	}
}

func TestListingSuspendIsUnsupported(t *testing.T) {
	env := newServiceEnv(t)
	enforcer := &stubListingEnforcer{}
	env.svc.SetListingEnforcement(enforcer)
	subject := mustID(t)
	detail, err := env.svc.CreateCase(context.Background(), CreateCaseInput{
		SubjectType: TargetListing, SubjectID: subject, Title: "suspend",
	})
	if err != nil {
		t.Fatal(err)
	}
	row, err := env.svc.CreateAction(context.Background(), detail.Case.ID, CreateActionInput{
		TargetType: TargetListing, TargetID: subject, ActionType: ActionTypeSuspend,
		ReasonCode: ActionReasonSafetyRisk,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.TransitionAction(context.Background(), detail.Case.ID, row.ID, ActionTransitionInput{To: ActionStatusApproved}); err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.TransitionAction(context.Background(), detail.Case.ID, row.ID, ActionTransitionInput{To: ActionStatusExecuted}); !errors.Is(err, errUnsupportedAction) {
		t.Fatalf("suspend execute err = %v", err)
	}
	got, err := env.svc.GetAction(context.Background(), detail.Case.ID, row.ID)
	if err != nil || got.Status != ActionStatusApproved {
		t.Fatalf("must stay approved = %+v err=%v", got, err)
	}
	if len(enforcer.calls) != 0 {
		t.Fatalf("suspend listings calls = %+v", enforcer.calls)
	}
}

func TestFailedListingEnforcementDoesNotExecute(t *testing.T) {
	env := newServiceEnv(t)
	enforcer := &stubListingEnforcer{err: listingcontracts.ErrUnavailable}
	env.svc.SetListingEnforcement(enforcer)
	subject := mustID(t)
	detail, err := env.svc.CreateCase(context.Background(), CreateCaseInput{
		SubjectType: TargetListing, SubjectID: subject, Title: "fail",
	})
	if err != nil {
		t.Fatal(err)
	}
	row, err := env.svc.CreateAction(context.Background(), detail.Case.ID, CreateActionInput{
		TargetType: TargetListing, TargetID: subject, ActionType: ActionTypeRestrict,
		ReasonCode: ActionReasonPolicyViolation,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.TransitionAction(context.Background(), detail.Case.ID, row.ID, ActionTransitionInput{To: ActionStatusApproved}); err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.TransitionAction(context.Background(), detail.Case.ID, row.ID, ActionTransitionInput{To: ActionStatusExecuted}); !errors.Is(err, errUnavailable) {
		t.Fatalf("failed execute err = %v", err)
	}
	got, err := env.svc.GetAction(context.Background(), detail.Case.ID, row.ID)
	if err != nil || got.Status != ActionStatusApproved {
		t.Fatalf("stayed approved = %+v err=%v", got, err)
	}
	enforcer.err = nil
	env.now = env.now.Add(time.Second)
	executed, err := env.svc.TransitionAction(context.Background(), detail.Case.ID, row.ID, ActionTransitionInput{To: ActionStatusExecuted})
	if err != nil || executed.Status != ActionStatusExecuted {
		t.Fatalf("retry execute = %+v err=%v", executed, err)
	}
}

func TestPublicProfileExecuteDoesNotCallListings(t *testing.T) {
	env := newServiceEnv(t)
	enforcer := &stubListingEnforcer{}
	env.svc.SetListingEnforcement(enforcer)
	subject := mustID(t)
	detail, err := env.svc.CreateCase(context.Background(), CreateCaseInput{
		SubjectType: TargetPublicProfile, SubjectID: subject, Title: "profile",
	})
	if err != nil {
		t.Fatal(err)
	}
	row, err := env.svc.CreateAction(context.Background(), detail.Case.ID, CreateActionInput{
		TargetType: TargetPublicProfile, TargetID: subject, ActionType: ActionTypeRemove,
		ReasonCode: ActionReasonSafetyRisk,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.TransitionAction(context.Background(), detail.Case.ID, row.ID, ActionTransitionInput{To: ActionStatusApproved}); err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.TransitionAction(context.Background(), detail.Case.ID, row.ID, ActionTransitionInput{To: ActionStatusExecuted}); err != nil {
		t.Fatal(err)
	}
	if len(enforcer.calls) != 0 {
		t.Fatalf("profile listings calls = %+v", enforcer.calls)
	}
	if len(env.profilesEnforce.calls) != 1 || env.profilesEnforce.calls[0].State != identitycontracts.ModerationStateRemoved {
		t.Fatalf("profile identity calls = %+v", env.profilesEnforce.calls)
	}
}

func TestListingExecuteDoesNotCallIdentity(t *testing.T) {
	env := newServiceEnv(t)
	enforcer := &stubListingEnforcer{}
	env.svc.SetListingEnforcement(enforcer)
	subject := mustID(t)
	detail, err := env.svc.CreateCase(context.Background(), CreateCaseInput{
		SubjectType: TargetListing, SubjectID: subject, Title: "listing",
	})
	if err != nil {
		t.Fatal(err)
	}
	row, err := env.svc.CreateAction(context.Background(), detail.Case.ID, CreateActionInput{
		TargetType: TargetListing, TargetID: subject, ActionType: ActionTypeRemove,
		ReasonCode: ActionReasonSafetyRisk,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.TransitionAction(context.Background(), detail.Case.ID, row.ID, ActionTransitionInput{To: ActionStatusApproved}); err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.TransitionAction(context.Background(), detail.Case.ID, row.ID, ActionTransitionInput{To: ActionStatusExecuted}); err != nil {
		t.Fatal(err)
	}
	if len(enforcer.calls) != 1 {
		t.Fatalf("listing calls = %+v", enforcer.calls)
	}
	if len(env.profilesEnforce.calls) != 0 {
		t.Fatalf("listing identity calls = %+v", env.profilesEnforce.calls)
	}
}

func TestPublicProfileRestrictRemoveExecuteThroughIdentityContract(t *testing.T) {
	env := newServiceEnv(t)
	subject := mustID(t)
	detail, err := env.svc.CreateCase(context.Background(), CreateCaseInput{
		SubjectType: TargetPublicProfile, SubjectID: subject, Title: "enforce",
	})
	if err != nil {
		t.Fatal(err)
	}
	caseID := detail.Case.ID
	restrict, err := env.svc.CreateAction(context.Background(), caseID, CreateActionInput{
		TargetType: TargetPublicProfile, TargetID: subject, ActionType: ActionTypeRestrict,
		ReasonCode: ActionReasonPolicyViolation,
	})
	if err != nil {
		t.Fatal(err)
	}
	env.now = env.now.Add(time.Second)
	if _, err := env.svc.TransitionAction(context.Background(), caseID, restrict.ID, ActionTransitionInput{To: ActionStatusApproved}); err != nil {
		t.Fatal(err)
	}
	env.now = env.now.Add(time.Second)
	executed, err := env.svc.TransitionAction(context.Background(), caseID, restrict.ID, ActionTransitionInput{To: ActionStatusExecuted})
	if err != nil || executed.Status != ActionStatusExecuted {
		t.Fatalf("restrict execute = %+v err=%v", executed, err)
	}
	if len(env.profilesEnforce.calls) != 1 || env.profilesEnforce.calls[0].State != identitycontracts.ModerationStateRestricted {
		t.Fatalf("restrict calls = %+v", env.profilesEnforce.calls)
	}
	env.now = env.now.Add(time.Second)
	remove, err := env.svc.CreateAction(context.Background(), caseID, CreateActionInput{
		TargetType: TargetPublicProfile, TargetID: subject, ActionType: ActionTypeRemove,
		ReasonCode: ActionReasonProhibitedContent,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.TransitionAction(context.Background(), caseID, remove.ID, ActionTransitionInput{To: ActionStatusApproved}); err != nil {
		t.Fatal(err)
	}
	env.now = env.now.Add(time.Second)
	if _, err := env.svc.TransitionAction(context.Background(), caseID, remove.ID, ActionTransitionInput{To: ActionStatusExecuted}); err != nil {
		t.Fatal(err)
	}
	if len(env.profilesEnforce.calls) != 2 || env.profilesEnforce.calls[1].State != identitycontracts.ModerationStateRemoved {
		t.Fatalf("remove calls = %+v", env.profilesEnforce.calls)
	}
	if err := env.profilesEnforce.ApplyModerationState(context.Background(), identitycontracts.ApplyPublicProfileModerationInput{
		PublicProfileID: identitycontracts.ID(subject), State: identitycontracts.ModerationStateRemoved,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPublicProfileNoActionAndWarningDoNotCallIdentity(t *testing.T) {
	env := newServiceEnv(t)
	subject := env.putProfile(t, env.other)
	detail, err := env.svc.CreateCase(context.Background(), CreateCaseInput{
		SubjectType: TargetPublicProfile, SubjectID: subject, Title: "record only",
	})
	if err != nil {
		t.Fatal(err)
	}
	caseID := detail.Case.ID
	for _, actionType := range []ActionType{ActionTypeNoAction, ActionTypeWarning} {
		env.now = env.now.Add(time.Second)
		row, err := env.svc.CreateAction(context.Background(), caseID, CreateActionInput{
			TargetType: TargetPublicProfile, TargetID: subject, ActionType: actionType,
			ReasonCode: ActionReasonNoViolation,
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := env.svc.TransitionAction(context.Background(), caseID, row.ID, ActionTransitionInput{To: ActionStatusApproved}); err != nil {
			t.Fatal(err)
		}
		env.now = env.now.Add(time.Second)
		executed, err := env.svc.TransitionAction(context.Background(), caseID, row.ID, ActionTransitionInput{To: ActionStatusExecuted})
		if err != nil || executed.Status != ActionStatusExecuted {
			t.Fatalf("%s execute = %+v err=%v", actionType, executed, err)
		}
	}
	if len(env.profilesEnforce.calls) != 0 {
		t.Fatalf("unexpected identity calls = %+v", env.profilesEnforce.calls)
	}
}

func TestPublicProfileSuspendIsUnsupported(t *testing.T) {
	env := newServiceEnv(t)
	subject := mustID(t)
	detail, err := env.svc.CreateCase(context.Background(), CreateCaseInput{
		SubjectType: TargetPublicProfile, SubjectID: subject, Title: "suspend",
	})
	if err != nil {
		t.Fatal(err)
	}
	row, err := env.svc.CreateAction(context.Background(), detail.Case.ID, CreateActionInput{
		TargetType: TargetPublicProfile, TargetID: subject, ActionType: ActionTypeSuspend,
		ReasonCode: ActionReasonSafetyRisk,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.TransitionAction(context.Background(), detail.Case.ID, row.ID, ActionTransitionInput{To: ActionStatusApproved}); err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.TransitionAction(context.Background(), detail.Case.ID, row.ID, ActionTransitionInput{To: ActionStatusExecuted}); !errors.Is(err, errUnsupportedAction) {
		t.Fatalf("suspend execute err = %v", err)
	}
	got, err := env.svc.GetAction(context.Background(), detail.Case.ID, row.ID)
	if err != nil || got.Status != ActionStatusApproved {
		t.Fatalf("must stay approved = %+v err=%v", got, err)
	}
	if len(env.profilesEnforce.calls) != 0 {
		t.Fatalf("suspend identity calls = %+v", env.profilesEnforce.calls)
	}
}

func TestFailedIdentityEnforcementDoesNotExecute(t *testing.T) {
	env := newServiceEnv(t)
	env.profilesEnforce.err = identitycontracts.ErrUnavailable
	subject := mustID(t)
	detail, err := env.svc.CreateCase(context.Background(), CreateCaseInput{
		SubjectType: TargetPublicProfile, SubjectID: subject, Title: "fail",
	})
	if err != nil {
		t.Fatal(err)
	}
	row, err := env.svc.CreateAction(context.Background(), detail.Case.ID, CreateActionInput{
		TargetType: TargetPublicProfile, TargetID: subject, ActionType: ActionTypeRestrict,
		ReasonCode: ActionReasonPolicyViolation,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.TransitionAction(context.Background(), detail.Case.ID, row.ID, ActionTransitionInput{To: ActionStatusApproved}); err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.TransitionAction(context.Background(), detail.Case.ID, row.ID, ActionTransitionInput{To: ActionStatusExecuted}); !errors.Is(err, errUnavailable) {
		t.Fatalf("failed execute err = %v", err)
	}
	got, err := env.svc.GetAction(context.Background(), detail.Case.ID, row.ID)
	if err != nil || got.Status != ActionStatusApproved {
		t.Fatalf("stayed approved = %+v err=%v", got, err)
	}
	env.profilesEnforce.err = nil
	env.now = env.now.Add(time.Second)
	executed, err := env.svc.TransitionAction(context.Background(), detail.Case.ID, row.ID, ActionTransitionInput{To: ActionStatusExecuted})
	if err != nil || executed.Status != ActionStatusExecuted {
		t.Fatalf("retry execute = %+v err=%v", executed, err)
	}
}

type stubListingEnforcer struct {
	calls    []listingcontracts.ApplyModerationInput
	clears   []listingcontracts.ID
	err      error
	clearErr error
}

func (s *stubListingEnforcer) ApplyModerationState(_ context.Context, in listingcontracts.ApplyModerationInput) error {
	if err := in.Validate(); err != nil && s.err == nil {
		return err
	}
	s.calls = append(s.calls, in)
	return s.err
}

func (s *stubListingEnforcer) ClearModerationState(_ context.Context, listingID listingcontracts.ID) error {
	if listingID.IsZero() && s.clearErr == nil && s.err == nil {
		return listingcontracts.ErrZeroID
	}
	s.clears = append(s.clears, listingID)
	if s.clearErr != nil {
		return s.clearErr
	}
	return s.err
}

type stubProfileEnforcer struct {
	calls    []identitycontracts.ApplyPublicProfileModerationInput
	clears   []identitycontracts.ID
	err      error
	clearErr error
}

func (s *stubProfileEnforcer) ApplyModerationState(_ context.Context, in identitycontracts.ApplyPublicProfileModerationInput) error {
	if err := in.Validate(); err != nil && s.err == nil {
		return err
	}
	s.calls = append(s.calls, in)
	return s.err
}

func (s *stubProfileEnforcer) ClearModerationState(_ context.Context, publicProfileID identitycontracts.ID) error {
	if publicProfileID.IsZero() && s.clearErr == nil && s.err == nil {
		return identitycontracts.ErrZeroID
	}
	s.clears = append(s.clears, publicProfileID)
	if s.clearErr != nil {
		return s.clearErr
	}
	return s.err
}
