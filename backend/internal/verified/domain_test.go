package verified

import (
	"bytes"
	"errors"
	"testing"
	"time"
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

func TestAppointmentRejectsSelf(t *testing.T) {
	id := mustID(t)
	user := mustID(t)
	now := time.Unix(1, 0).UTC()
	appt := Appointment{
		ID: id, ListingID: mustID(t), RequesterUserID: user, ProviderUserID: user,
		Status: StatusRequested, RequestedAt: now, UpdatedAt: now,
	}
	if err := appt.Validate(); !errors.Is(err, errSelfAppointment) {
		t.Fatalf("err = %v", err)
	}
}

func TestNormalizeMethod(t *testing.T) {
	got, err := NormalizeMethod("QR")
	if err != nil || got != MethodQR {
		t.Fatalf("qr = %q err=%v", got, err)
	}
	if _, err := NormalizeMethod("nfc"); !errors.Is(err, errInvalidMethod) {
		t.Fatalf("nfc err = %v", err)
	}
}

func TestNormalizeInteractionType(t *testing.T) {
	got, err := NormalizeInteractionType("TRANSACTION")
	if err != nil || got != InteractionTransaction {
		t.Fatalf("transaction = %q err=%v", got, err)
	}
	got, err = NormalizeFlowInteractionType("delivery")
	if err != nil || got != InteractionDelivery {
		t.Fatalf("delivery = %q err=%v", got, err)
	}
	if _, err := NormalizeFlowInteractionType(InteractionListingInspection); !errors.Is(err, errInvalidInteraction) {
		t.Fatalf("listing via flow err = %v", err)
	}
	if _, err := NormalizeInteractionType("escrow"); !errors.Is(err, errInvalidInteraction) {
		t.Fatalf("escrow err = %v", err)
	}
}

func TestVerifiedInteractionTypeScope(t *testing.T) {
	now := time.Unix(1, 0).UTC()
	base := VerifiedInteraction{
		ID: mustID(t), ListingID: mustID(t), RequesterUserID: mustID(t), ProviderUserID: mustID(t),
		VerificationMethod: MethodQR, VerifiedAt: now,
	}
	listing := base
	listing.AppointmentID = mustID(t)
	listing.InteractionType = InteractionListingInspection
	if err := listing.Validate(); err != nil {
		t.Fatalf("listing inspection err = %v", err)
	}
	listing.InteractionType = InteractionTransaction
	if err := listing.Validate(); !errors.Is(err, errInvalidInteraction) {
		t.Fatalf("listing with transaction type err = %v", err)
	}
	flow := base
	flow.FlowID = mustID(t)
	flow.InteractionType = InteractionDelivery
	if err := flow.Validate(); err != nil {
		t.Fatalf("delivery err = %v", err)
	}
	flow.AppointmentID = mustID(t)
	if err := flow.Validate(); !errors.Is(err, errInvalidInteraction) {
		t.Fatalf("both scopes err = %v", err)
	}
}

func TestGenerateNumericOTPExactLengthDigitsAndLeadingZeroSafe(t *testing.T) {
	previous := ""
	for i := 0; i < 32; i++ {
		raw, hash, err := GenerateNumericOTP(DefaultOTPDigits)
		if err != nil {
			t.Fatalf("GenerateNumericOTP: %v", err)
		}
		if len(raw) != DefaultOTPDigits {
			t.Fatalf("len = %d want %d raw=%q", len(raw), DefaultOTPDigits, raw)
		}
		for _, r := range raw {
			if r < '0' || r > '9' {
				t.Fatalf("non-digit OTP %q", raw)
			}
		}
		got, err := HashNumericOTP(raw)
		if err != nil || !bytes.Equal(got, hash) {
			t.Fatalf("hash roundtrip err=%v", err)
		}
		if bytes.Equal(hash, []byte(raw)) || bytes.Contains(hash, []byte(raw)) {
			t.Fatal("hash must not equal or contain plaintext OTP")
		}
		if previous != "" && previous == raw {
			t.Fatal("OTPs must not be a fixed value")
		}
		previous = raw
	}
	padded, err := HashNumericOTP("012345")
	if err != nil {
		t.Fatal(err)
	}
	again, err := HashNumericOTP("012345")
	if err != nil || !bytes.Equal(padded, again) {
		t.Fatal("leading-zero OTP hash must be stable")
	}
	unpadded, err := HashNumericOTP("12345")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(padded, unpadded) {
		t.Fatal("leading zeroes must change the stored hash")
	}
	submitted, err := HashSubmittedChallengeSecret("012345", DefaultOTPDigits)
	if err != nil || !bytes.Equal(submitted, padded) {
		t.Fatalf("submit hash mismatch err=%v", err)
	}
}

func TestGenerateChallengeTokenIsNotNumericOTP(t *testing.T) {
	raw, _, err := GenerateChallengeToken()
	if err != nil {
		t.Fatal(err)
	}
	if looksLikeNumericOTP(raw, DefaultOTPDigits) {
		t.Fatalf("QR token must not look like numeric OTP: %q", raw)
	}
}
