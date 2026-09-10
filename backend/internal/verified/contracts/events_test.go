package contracts

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestDecodeInteractionCompletedListingInspectionRequiresAppointment(t *testing.T) {
	raw := json.RawMessage(`{
		"interaction_id":"a","appointment_id":"b","listing_id":"c",
		"requester_user_id":"d","provider_user_id":"e",
		"interaction_type":"listing_inspection","verification_method":"qr"
	}`)
	got, err := DecodeInteractionCompleted(raw)
	if err != nil || got.AppointmentID != "b" || got.FlowID != "" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}

func TestDecodeInteractionCompletedTransactionRequiresFlow(t *testing.T) {
	raw := json.RawMessage(`{
		"interaction_id":"a","flow_id":"f","listing_id":"c",
		"requester_user_id":"d","provider_user_id":"e",
		"interaction_type":"transaction","verification_method":"otp"
	}`)
	got, err := DecodeInteractionCompleted(raw)
	if err != nil || got.FlowID != "f" || got.AppointmentID != "" || got.InteractionType != InteractionTypeTransaction {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}

func TestDecodeInteractionCompletedRejectsInvalidTypeAndMismatchedScope(t *testing.T) {
	if _, err := DecodeInteractionCompleted(json.RawMessage(`{
		"interaction_id":"a","appointment_id":"b","listing_id":"c",
		"requester_user_id":"d","provider_user_id":"e",
		"interaction_type":"escrow","verification_method":"qr"
	}`)); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("escrow err=%v", err)
	}
	if _, err := DecodeInteractionCompleted(json.RawMessage(`{
		"interaction_id":"a","listing_id":"c",
		"requester_user_id":"d","provider_user_id":"e",
		"interaction_type":"delivery","verification_method":"qr"
	}`)); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("delivery missing flow err=%v", err)
	}
	if _, err := DecodeInteractionCompleted(json.RawMessage(`{
		"interaction_id":"a","appointment_id":"b","flow_id":"f","listing_id":"c",
		"requester_user_id":"d","provider_user_id":"e",
		"interaction_type":"listing_inspection","verification_method":"qr"
	}`)); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("listing with flow err=%v", err)
	}
}
