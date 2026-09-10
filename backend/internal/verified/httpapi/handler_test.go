package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	listingcontracts "backend/internal/listings/contracts"
	"backend/internal/platform/outbox"
	"backend/internal/verified"
)

const allowedOrigin = "https://app.example.test"

func TestMutationsRequireAuthOriginCSRF(t *testing.T) {
	h := newTestHandler(t)
	listing := mustPublished(t, h)

	rec := do(t, h, http.MethodPost, "/v1/verified/appointments", allowedOrigin, map[string]string{"listingId": listing.String()}, nil, withCSRF("csrf-token"))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing session status = %d", rec.Code)
	}
	rec = do(t, h, http.MethodPost, "/v1/verified/appointments", "", map[string]string{"listingId": listing.String()}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing origin status = %d", rec.Code)
	}
	rec = do(t, h, http.MethodPost, "/v1/verified/appointments", allowedOrigin, map[string]string{"listingId": listing.String()}, authedCookies())
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status = %d", rec.Code)
	}
}

func TestReadsRequireSession(t *testing.T) {
	h := newTestHandler(t)
	rec := do(t, h, http.MethodGet, "/v1/verified/appointments", "", nil, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("list status = %d", rec.Code)
	}
	rec = do(t, h, http.MethodGet, "/v1/verified/verification-flows", "", nil, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("flow list status = %d", rec.Code)
	}
}

func TestCreateAndForeign404(t *testing.T) {
	h := newTestHandler(t)
	listing := mustPublished(t, h)
	rec := do(t, h, http.MethodPost, "/v1/verified/appointments", allowedOrigin, map[string]string{"listingId": listing.String()}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", rec.Code, rec.Body.String())
	}
	var created appointmentDTO
	decode(t, rec, &created)

	h.sessions.userID = mustID(t)
	h.Handler.sessions = h.sessions
	rec = do(t, h, http.MethodGet, "/v1/verified/appointments/"+created.AppointmentID, "", nil, authedCookies())
	if rec.Code != http.StatusNotFound {
		t.Fatalf("foreign get status = %d", rec.Code)
	}
	rec = do(t, h, http.MethodPost, "/v1/verified/appointments/"+created.AppointmentID+"/accept", allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("foreign accept status = %d", rec.Code)
	}
}

func TestAppointmentReadsExposeVerifiedInteractionID(t *testing.T) {
	h := newTestHandler(t)
	requester := h.sessions.userID
	listing := mustID(t)
	provider := mustID(t)
	h.listings.setPublished(listing, provider)

	rec := do(t, h, http.MethodPost, "/v1/verified/appointments", allowedOrigin, map[string]string{"listingId": listing.String()}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", rec.Code, rec.Body.String())
	}
	var created appointmentDTO
	assertAppointmentDTOPrivacy(t, rec.Body.Bytes())
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.VerifiedInteractionID != nil {
		t.Fatalf("created verifiedInteractionId = %v", created.VerifiedInteractionID)
	}

	rec = do(t, h, http.MethodGet, "/v1/verified/appointments/"+created.AppointmentID, "", nil, authedCookies())
	if rec.Code != http.StatusOK {
		t.Fatalf("non-completed get status = %d body=%s", rec.Code, rec.Body.String())
	}
	var pending appointmentDTO
	assertAppointmentDTOPrivacy(t, rec.Body.Bytes())
	if err := json.Unmarshal(rec.Body.Bytes(), &pending); err != nil {
		t.Fatal(err)
	}

	h.sessions.userID = provider
	h.Handler.sessions = h.sessions
	rec = do(t, h, http.MethodPost, "/v1/verified/appointments/"+created.AppointmentID+"/accept", allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("accept status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, http.MethodPost, "/v1/verified/appointments/"+created.AppointmentID+"/verification/start", allowedOrigin, map[string]string{"method": "qr"}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("start status = %d body=%s", rec.Code, rec.Body.String())
	}
	var issued issuedChallengeDTO
	decode(t, rec, &issued)

	h.sessions.userID = requester
	h.Handler.sessions = h.sessions
	rec = do(t, h, http.MethodPost, "/v1/verified/appointments/"+created.AppointmentID+"/verification/finish", allowedOrigin, map[string]string{"token": issued.Token}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("finish status = %d body=%s", rec.Code, rec.Body.String())
	}
	var interaction interactionDTO
	decode(t, rec, &interaction)

	rec = do(t, h, http.MethodGet, "/v1/verified/appointments/"+created.AppointmentID, "", nil, authedCookies())
	if rec.Code != http.StatusOK {
		t.Fatalf("completed get status = %d body=%s", rec.Code, rec.Body.String())
	}
	rawGet := rec.Body.Bytes()
	assertAppointmentDTOPrivacy(t, rawGet)
	var completed appointmentDTO
	if err := json.Unmarshal(rawGet, &completed); err != nil {
		t.Fatal(err)
	}
	if completed.Status != "completed" || completed.VerifiedInteractionID == nil || *completed.VerifiedInteractionID != interaction.InteractionID {
		t.Fatalf("completed get = %+v want %s", completed, interaction.InteractionID)
	}

	rec = do(t, h, http.MethodGet, "/v1/verified/appointments", "", nil, authedCookies())
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", rec.Code, rec.Body.String())
	}
	var listed appointmentListDTO
	assertAppointmentDTOPrivacy(t, rec.Body.Bytes())
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Appointments) != 1 || listed.Appointments[0].VerifiedInteractionID == nil || *listed.Appointments[0].VerifiedInteractionID != interaction.InteractionID {
		t.Fatalf("list = %+v", listed)
	}

	h.sessions.userID = mustID(t)
	h.Handler.sessions = h.sessions
	rec = do(t, h, http.MethodGet, "/v1/verified/appointments/"+created.AppointmentID, "", nil, authedCookies())
	if rec.Code != http.StatusNotFound {
		t.Fatalf("foreign get status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, http.MethodGet, "/v1/verified/appointments", "", nil, authedCookies())
	if rec.Code != http.StatusOK {
		t.Fatalf("foreign list status = %d body=%s", rec.Code, rec.Body.String())
	}
	var foreignList appointmentListDTO
	decode(t, rec, &foreignList)
	if len(foreignList.Appointments) != 0 {
		t.Fatalf("foreign list leaked = %+v", foreignList)
	}
}

func assertAppointmentDTOPrivacy(t *testing.T, raw []byte) {
	t.Helper()
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("privacy decode: %v body=%s", err, raw)
	}
	rows := []map[string]any{obj}
	if items, ok := obj["appointments"].([]any); ok {
		rows = make([]map[string]any, 0, len(items))
		for _, item := range items {
			m, ok := item.(map[string]any)
			if !ok {
				t.Fatalf("list item = %T", item)
			}
			rows = append(rows, m)
		}
	}
	forbidden := []string{"token", "challengeId", "tokenHash", "rawToken", "expiresAt", "method", "consumedAt"}
	for _, row := range rows {
		for _, key := range forbidden {
			if _, ok := row[key]; ok {
				t.Fatalf("appointment DTO must not expose %s: %v", key, row)
			}
		}
	}
}

func TestStartVerificationQRPayloadOmitsImageAndOTPUnchanged(t *testing.T) {
	h := newTestHandler(t)
	listingQR := mustID(t)
	listingOTP := mustID(t)
	provider := mustID(t)
	h.listings.setPublished(listingQR, provider)
	h.listings.setPublished(listingOTP, provider)

	rec := do(t, h, http.MethodPost, "/v1/verified/appointments", allowedOrigin, map[string]string{"listingId": listingQR.String()}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("create qr status = %d body=%s", rec.Code, rec.Body.String())
	}
	var qrAppt appointmentDTO
	decode(t, rec, &qrAppt)

	rec = do(t, h, http.MethodPost, "/v1/verified/appointments", allowedOrigin, map[string]string{"listingId": listingOTP.String()}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("create otp status = %d body=%s", rec.Code, rec.Body.String())
	}
	var otpAppt appointmentDTO
	decode(t, rec, &otpAppt)

	h.sessions.userID = provider
	h.Handler.sessions = h.sessions
	rec = do(t, h, http.MethodPost, "/v1/verified/appointments/"+qrAppt.AppointmentID+"/accept", allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("accept qr status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, http.MethodPost, "/v1/verified/appointments/"+otpAppt.AppointmentID+"/accept", allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("accept otp status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = do(t, h, http.MethodPost, "/v1/verified/appointments/"+qrAppt.AppointmentID+"/verification/start", allowedOrigin, map[string]string{"method": "qr"}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("start qr status = %d body=%s", rec.Code, rec.Body.String())
	}
	rawQR := rec.Body.Bytes()
	assertIssuedChallengeDTOPrivacy(t, rawQR)
	var qrIssued issuedChallengeDTO
	if err := json.Unmarshal(rawQR, &qrIssued); err != nil {
		t.Fatal(err)
	}
	if qrIssued.Method != "qr" || qrIssued.Token == "" || qrIssued.QrPayload != qrIssued.Token {
		t.Fatalf("qr issued = %+v", qrIssued)
	}

	rec = do(t, h, http.MethodPost, "/v1/verified/appointments/"+otpAppt.AppointmentID+"/verification/start", allowedOrigin, map[string]string{"method": "otp"}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("start otp status = %d body=%s", rec.Code, rec.Body.String())
	}
	rawOTP := rec.Body.Bytes()
	assertIssuedChallengeDTOPrivacy(t, rawOTP)
	var otpIssued issuedChallengeDTO
	if err := json.Unmarshal(rawOTP, &otpIssued); err != nil {
		t.Fatal(err)
	}
	if otpIssued.Method != "otp" || otpIssued.QrPayload != "" {
		t.Fatalf("otp issued = %+v", otpIssued)
	}
	if len(otpIssued.Token) != verified.DefaultOTPDigits {
		t.Fatalf("otp width = %d token=%q", len(otpIssued.Token), otpIssued.Token)
	}
	for _, r := range otpIssued.Token {
		if r < '0' || r > '9' {
			t.Fatalf("otp must be digits only: %q", otpIssued.Token)
		}
	}

	var otpMap map[string]any
	if err := json.Unmarshal(rawOTP, &otpMap); err != nil {
		t.Fatal(err)
	}
	if _, ok := otpMap["qrPayload"]; ok {
		t.Fatalf("otp start must omit qrPayload: %v", otpMap)
	}
}

func TestOTPFinishSucceedsWrongCodeFailsQRUnchanged(t *testing.T) {
	h := newTestHandler(t)
	requester := h.sessions.userID
	listingOTP := mustID(t)
	listingQR := mustID(t)
	listingWrong := mustID(t)
	provider := mustID(t)
	h.listings.setPublished(listingOTP, provider)
	h.listings.setPublished(listingQR, provider)
	h.listings.setPublished(listingWrong, provider)

	rec := do(t, h, http.MethodPost, "/v1/verified/appointments", allowedOrigin, map[string]string{"listingId": listingOTP.String()}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("create otp status = %d body=%s", rec.Code, rec.Body.String())
	}
	var otpAppt appointmentDTO
	decode(t, rec, &otpAppt)
	rec = do(t, h, http.MethodPost, "/v1/verified/appointments", allowedOrigin, map[string]string{"listingId": listingQR.String()}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	var qrAppt appointmentDTO
	decode(t, rec, &qrAppt)
	rec = do(t, h, http.MethodPost, "/v1/verified/appointments", allowedOrigin, map[string]string{"listingId": listingWrong.String()}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	var wrongAppt appointmentDTO
	decode(t, rec, &wrongAppt)

	h.sessions.userID = provider
	h.Handler.sessions = h.sessions
	for _, id := range []string{otpAppt.AppointmentID, qrAppt.AppointmentID, wrongAppt.AppointmentID} {
		rec = do(t, h, http.MethodPost, "/v1/verified/appointments/"+id+"/accept", allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
		if rec.Code != http.StatusOK {
			t.Fatalf("accept %s status = %d", id, rec.Code)
		}
	}

	rec = do(t, h, http.MethodPost, "/v1/verified/appointments/"+otpAppt.AppointmentID+"/verification/start", allowedOrigin, map[string]string{"method": "otp"}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("start otp status = %d body=%s", rec.Code, rec.Body.String())
	}
	var otpIssued issuedChallengeDTO
	decode(t, rec, &otpIssued)

	rec = do(t, h, http.MethodPost, "/v1/verified/appointments/"+wrongAppt.AppointmentID+"/verification/start", allowedOrigin, map[string]string{"method": "otp"}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	var wrongIssued issuedChallengeDTO
	decode(t, rec, &wrongIssued)

	rec = do(t, h, http.MethodPost, "/v1/verified/appointments/"+qrAppt.AppointmentID+"/verification/start", allowedOrigin, map[string]string{"method": "qr"}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	var qrIssued issuedChallengeDTO
	decode(t, rec, &qrIssued)
	if qrIssued.QrPayload != qrIssued.Token || qrIssued.Method != "qr" {
		t.Fatalf("qr issued = %+v", qrIssued)
	}

	h.sessions.userID = requester
	h.Handler.sessions = h.sessions
	wrong := "000000"
	if wrongIssued.Token == wrong {
		wrong = "000001"
	}
	rec = do(t, h, http.MethodPost, "/v1/verified/appointments/"+wrongAppt.AppointmentID+"/verification/finish", allowedOrigin, map[string]string{"token": wrong}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("wrong otp status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = do(t, h, http.MethodPost, "/v1/verified/appointments/"+otpAppt.AppointmentID+"/verification/finish", allowedOrigin, map[string]string{"token": otpIssued.Token}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("otp finish status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = do(t, h, http.MethodPost, "/v1/verified/appointments/"+qrAppt.AppointmentID+"/verification/finish", allowedOrigin, map[string]string{"token": qrIssued.Token}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("qr finish status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = do(t, h, http.MethodPost, "/v1/verified/appointments/"+otpAppt.AppointmentID+"/verification/finish", allowedOrigin, map[string]string{"token": otpIssued.Token}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusConflict {
		t.Fatalf("consumed otp status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func assertIssuedChallengeDTOPrivacy(t *testing.T, raw []byte) {
	t.Helper()
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("issued decode: %v body=%s", err, raw)
	}
	forbidden := []string{"tokenHash", "rawToken", "consumedAt", "image", "qrImage", "png", "svg"}
	for _, key := range forbidden {
		if _, ok := obj[key]; ok {
			t.Fatalf("issued DTO must not expose %s: %v", key, obj)
		}
	}
}

func TestCreateRejectsClientSuppliedUserIDs(t *testing.T) {
	h := newTestHandler(t)
	listing := mustPublished(t, h)
	rec := do(t, h, http.MethodPost, "/v1/verified/appointments", allowedOrigin, map[string]string{
		"listingId": listing.String(),
		"userId":    mustID(t).String(),
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestTransactionAndDeliveryHTTPStartFinish(t *testing.T) {
	h := newTestHandler(t)
	requester := mustID(t)
	listingTx := mustID(t)
	listingDel := mustID(t)
	provider := h.sessions.userID
	h.listings.setPublished(listingTx, provider)
	h.listings.setPublished(listingDel, provider)

	rec := do(t, h, http.MethodPost, "/v1/verified/transaction/verification/start", allowedOrigin, map[string]string{
		"listingId": listingTx.String(), "requesterUserId": requester.String(), "method": "qr",
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("tx start status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertIssuedChallengeDTOPrivacy(t, rec.Body.Bytes())
	var txIssued issuedChallengeDTO
	decode(t, rec, &txIssued)
	if txIssued.FlowID == "" || txIssued.AppointmentID != "" || txIssued.InteractionType != verified.InteractionTransaction || txIssued.QrPayload != txIssued.Token {
		t.Fatalf("tx issued = %+v", txIssued)
	}

	rec = do(t, h, http.MethodPost, "/v1/verified/delivery/verification/start", allowedOrigin, map[string]string{
		"listingId": listingDel.String(), "requesterUserId": requester.String(), "method": "otp",
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("delivery start status = %d body=%s", rec.Code, rec.Body.String())
	}
	var delIssued issuedChallengeDTO
	decode(t, rec, &delIssued)
	if delIssued.InteractionType != verified.InteractionDelivery || delIssued.QrPayload != "" {
		t.Fatalf("delivery issued = %+v", delIssued)
	}

	h.sessions.userID = requester
	h.Handler.sessions = h.sessions
	rec = do(t, h, http.MethodPost, "/v1/verified/verification-flows/"+txIssued.FlowID+"/verification/finish", allowedOrigin, map[string]string{"token": txIssued.Token}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("tx finish status = %d body=%s", rec.Code, rec.Body.String())
	}
	var txRow interactionDTO
	decode(t, rec, &txRow)
	if txRow.InteractionType != verified.InteractionTransaction || txRow.FlowID != txIssued.FlowID || txRow.AppointmentID != "" {
		t.Fatalf("tx row = %+v", txRow)
	}

	rec = do(t, h, http.MethodPost, "/v1/verified/verification-flows/"+delIssued.FlowID+"/verification/finish", allowedOrigin, map[string]string{"token": delIssued.Token}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("delivery finish status = %d body=%s", rec.Code, rec.Body.String())
	}
	var delRow interactionDTO
	decode(t, rec, &delRow)
	if delRow.InteractionType != verified.InteractionDelivery {
		t.Fatalf("delivery row = %+v", delRow)
	}

	rec = do(t, h, http.MethodPost, "/v1/verified/verification-flows/"+txIssued.FlowID+"/verification/finish", allowedOrigin, map[string]string{"token": txIssued.Token}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusConflict {
		t.Fatalf("tx replay status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestVerificationFlowListAndReadHTTP(t *testing.T) {
	h := newTestHandler(t)
	requester := mustID(t)
	listingA := mustID(t)
	listingB := mustID(t)
	provider := h.sessions.userID
	h.listings.setPublished(listingA, provider)
	h.listings.setPublished(listingB, provider)

	rec := do(t, h, http.MethodGet, "/v1/verified/verification-flows", "", nil, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthed list status = %d", rec.Code)
	}

	rec = do(t, h, http.MethodPost, "/v1/verified/transaction/verification/start", allowedOrigin, map[string]string{
		"listingId": listingA.String(), "requesterUserId": requester.String(), "method": "qr",
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("tx start status = %d body=%s", rec.Code, rec.Body.String())
	}
	var first issuedChallengeDTO
	decode(t, rec, &first)
	h.clock.now = h.clock.now.Add(time.Second)
	rec = do(t, h, http.MethodPost, "/v1/verified/delivery/verification/start", allowedOrigin, map[string]string{
		"listingId": listingA.String(), "requesterUserId": requester.String(), "method": "otp",
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("delivery start status = %d body=%s", rec.Code, rec.Body.String())
	}
	var second issuedChallengeDTO
	decode(t, rec, &second)
	h.clock.now = h.clock.now.Add(time.Second)
	rec = do(t, h, http.MethodPost, "/v1/verified/transaction/verification/start", allowedOrigin, map[string]string{
		"listingId": listingB.String(), "requesterUserId": requester.String(), "method": "qr",
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}

	rec = do(t, h, http.MethodGet, "/v1/verified/verification-flows/"+first.FlowID, "", nil, authedCookies())
	if rec.Code != http.StatusOK {
		t.Fatalf("provider get status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertVerificationFlowDTOPrivacy(t, rec.Body.Bytes())
	var openFlow verificationFlowDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &openFlow); err != nil {
		t.Fatal(err)
	}
	if openFlow.FlowID != first.FlowID || openFlow.Status != verified.FlowOpen || openFlow.CompletedInteractionID != nil {
		t.Fatalf("open flow = %+v", openFlow)
	}

	h.sessions.userID = requester
	h.Handler.sessions = h.sessions
	rec = do(t, h, http.MethodGet, "/v1/verified/verification-flows/"+first.FlowID, "", nil, authedCookies())
	if rec.Code != http.StatusOK {
		t.Fatalf("requester get status = %d", rec.Code)
	}
	rec = do(t, h, http.MethodPost, "/v1/verified/verification-flows/"+first.FlowID+"/verification/finish", allowedOrigin, map[string]string{"token": first.Token}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("finish status = %d body=%s", rec.Code, rec.Body.String())
	}
	var finished interactionDTO
	decode(t, rec, &finished)

	rec = do(t, h, http.MethodGet, "/v1/verified/verification-flows/"+first.FlowID, "", nil, authedCookies())
	if rec.Code != http.StatusOK {
		t.Fatalf("completed get status = %d", rec.Code)
	}
	assertVerificationFlowDTOPrivacy(t, rec.Body.Bytes())
	var completed verificationFlowDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &completed); err != nil {
		t.Fatal(err)
	}
	if completed.Status != verified.FlowCompleted || completed.CompletedInteractionID == nil || *completed.CompletedInteractionID != finished.InteractionID {
		t.Fatalf("completed flow = %+v want %s", completed, finished.InteractionID)
	}

	rec = do(t, h, http.MethodGet, "/v1/verified/verification-flows?listingId="+listingA.String(), "", nil, authedCookies())
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertVerificationFlowDTOPrivacy(t, rec.Body.Bytes())
	var listed verificationFlowListDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Flows) != 2 || listed.Flows[0].FlowID != second.FlowID || listed.Flows[1].FlowID != first.FlowID {
		t.Fatalf("list = %+v", listed)
	}
	if listed.Flows[0].CompletedInteractionID != nil || listed.Flows[1].CompletedInteractionID == nil {
		t.Fatalf("list interaction ids = %+v", listed)
	}

	h.sessions.userID = mustID(t)
	h.Handler.sessions = h.sessions
	rec = do(t, h, http.MethodGet, "/v1/verified/verification-flows/"+first.FlowID, "", nil, authedCookies())
	if rec.Code != http.StatusNotFound {
		t.Fatalf("stranger get status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, http.MethodGet, "/v1/verified/verification-flows", "", nil, authedCookies())
	if rec.Code != http.StatusOK {
		t.Fatalf("stranger list status = %d", rec.Code)
	}
	var strangerList verificationFlowListDTO
	decode(t, rec, &strangerList)
	if len(strangerList.Flows) != 0 {
		t.Fatalf("stranger list leaked = %+v", strangerList)
	}
}

func assertVerificationFlowDTOPrivacy(t *testing.T, raw []byte) {
	t.Helper()
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("privacy decode: %v body=%s", err, raw)
	}
	rows := []map[string]any{obj}
	if items, ok := obj["flows"].([]any); ok {
		rows = make([]map[string]any, 0, len(items))
		for _, item := range items {
			m, ok := item.(map[string]any)
			if !ok {
				t.Fatalf("list item = %T", item)
			}
			rows = append(rows, m)
		}
	}
	forbidden := []string{
		"token", "qrPayload", "otp", "challengeId", "tokenHash", "rawToken", "expiresAt",
		"requesterUserId", "providerUserId", "userId",
	}
	for _, row := range rows {
		for _, key := range forbidden {
			if _, ok := row[key]; ok {
				t.Fatalf("flow DTO must not expose %s: %v", key, row)
			}
		}
	}
}

func TestFlowStartRejectsClientInteractionType(t *testing.T) {
	h := newTestHandler(t)
	listing := mustID(t)
	h.listings.setPublished(listing, h.sessions.userID)
	rec := do(t, h, http.MethodPost, "/v1/verified/transaction/verification/start", allowedOrigin, map[string]string{
		"listingId": listing.String(), "requesterUserId": mustID(t).String(), "method": "qr", "interactionType": "listing_inspection",
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

type stubSessions struct {
	userID verified.ID
	err    error
}

func (s stubSessions) Resolve(_ context.Context, rawToken string) (verified.ID, error) {
	if s.err != nil {
		return verified.ID{}, s.err
	}
	if rawToken == "" {
		return verified.ID{}, ErrUnauthenticated
	}
	return s.userID, nil
}

type stubListings struct {
	refs map[listingcontracts.ID]listingcontracts.ListingRef
}

func (s *stubListings) setPublished(listingID, owner verified.ID) {
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

type memoryEnqueuer struct{}

func (memoryEnqueuer) Enqueue(_ context.Context, _ outbox.Execer, in outbox.NewEvent) (outbox.Event, error) {
	id, err := outbox.NewID()
	if err != nil {
		return outbox.Event{}, err
	}
	return outbox.Event{ID: id, EventType: in.EventType, EventVersion: in.EventVersion, Payload: in.Payload}, nil
}

type testClock struct {
	now time.Time
}

type testHandler struct {
	*Handler
	sessions stubSessions
	listings *stubListings
	clock    *testClock
}

func newTestHandler(t *testing.T) *testHandler {
	t.Helper()
	user, err := verified.NewID()
	if err != nil {
		t.Fatal(err)
	}
	store := verified.NewMemoryStore()
	listings := &stubListings{refs: map[listingcontracts.ID]listingcontracts.ListingRef{}}
	clock := &testClock{now: time.Unix(50, 0).UTC()}
	svc, err := verified.NewService(store, listings, memoryEnqueuer{}, verified.DefaultPolicy(), func() time.Time { return clock.now })
	if err != nil {
		t.Fatal(err)
	}
	sessions := stubSessions{userID: user}
	h, err := New(sessions, svc, []string{allowedOrigin})
	if err != nil {
		t.Fatal(err)
	}
	return &testHandler{Handler: h, sessions: sessions, listings: listings, clock: clock}
}

func mustPublished(t *testing.T, h *testHandler) verified.ID {
	t.Helper()
	id := mustID(t)
	owner := mustID(t)
	h.listings.setPublished(id, owner)
	return id
}

func mustID(t *testing.T) verified.ID {
	t.Helper()
	id, err := verified.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func authedCookies() map[string]string {
	return map[string]string{
		sessionCookieName: "session-raw-token",
		csrfCookieName:    "csrf-token",
	}
}

func withCSRF(token string) func(*http.Request) {
	return func(r *http.Request) {
		r.Header.Set(csrfHeaderName, token)
	}
}

func do(t *testing.T, h *testHandler, method, path, origin string, body any, cookies map[string]string, opts ...func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r = httptest.NewRequest(method, path, bytes.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	for name, value := range cookies {
		r.AddCookie(&http.Cookie{Name: name, Value: value})
	}
	for _, opt := range opts {
		opt(r)
	}
	rec := httptest.NewRecorder()
	mux := http.NewServeMux()
	h.Register(mux)
	mux.ServeHTTP(rec, r)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder, dest any) {
	t.Helper()
	if err := json.NewDecoder(rec.Body).Decode(dest); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
}
