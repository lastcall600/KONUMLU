package disputes

import (
	"context"
	"strings"
	"testing"

	dispcontracts "backend/internal/disputes/contracts"
	"backend/internal/platform/outbox"
)

type recordingOutbox struct {
	events []outbox.NewEvent
}

func (r *recordingOutbox) Enqueue(_ context.Context, _ outbox.Execer, in outbox.NewEvent) (outbox.Event, error) {
	r.events = append(r.events, in)
	return outbox.Event{}, nil
}

func TestDisputeCreateEnqueuesUpdateWithoutEvidence(t *testing.T) {
	svc, fx := mustDisputeService(t)
	rec := &recordingOutbox{}
	svc.SetOutbox(nil, rec)
	stmt := "private-statement-must-not-leave-domain"
	d, err := svc.CreateForTransaction(context.Background(), fx.requester, fx.txnID, CreateInput{
		ReasonCode: ReasonOther,
		Statement:  &stmt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.events) != 1 || rec.events[0].EventType != dispcontracts.EventTypeUpdated {
		t.Fatalf("create events=%+v", rec.events)
	}
	p, err := dispcontracts.DecodeUpdated(rec.events[0].Payload)
	if err != nil || p.RequesterUserID != fx.requester.String() || p.ProviderUserID != fx.provider.String() {
		t.Fatalf("payload=%+v err=%v", p, err)
	}
	raw := string(rec.events[0].Payload)
	if strings.Contains(raw, "private-statement") || strings.Contains(strings.ToLower(raw), "evidence") {
		t.Fatal("dispute statement/evidence leaked")
	}
	if _, err := svc.AddEvidence(context.Background(), fx.requester, d.ID, EvidenceInput{
		EvidenceType: EvidencePartyStatement,
		Title:        "secret-evidence",
	}); err != nil {
		t.Fatal(err)
	}
	if len(rec.events) != 1 {
		t.Fatalf("evidence must not notify: %+v", rec.events)
	}
	if _, err := svc.CreateForTransaction(context.Background(), fx.provider, fx.txnID, CreateInput{ReasonCode: ReasonPaymentIssue}); err != nil {
		t.Fatal(err)
	}
	if len(rec.events) != 1 {
		t.Fatalf("idempotent open events=%d", len(rec.events))
	}
}

func TestStaffResolveNotifiesBothPartiesViaEmptyActor(t *testing.T) {
	svc, fx := mustDisputeService(t)
	rec := &recordingOutbox{}
	svc.SetOutbox(nil, rec)
	d, err := svc.CreateForTransaction(context.Background(), fx.requester, fx.txnID, CreateInput{ReasonCode: ReasonOther})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.StartReview(context.Background(), d.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Resolve(context.Background(), d.ID, ResolutionBuyerFavored); err != nil {
		t.Fatal(err)
	}
	last := rec.events[len(rec.events)-1]
	p, err := dispcontracts.DecodeUpdated(last.Payload)
	if err != nil || p.ActorUserID != "" {
		t.Fatalf("staff actor must be empty so both parties are notified: %+v err=%v", p, err)
	}
}
