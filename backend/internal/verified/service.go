package verified

import (
	"context"
	"errors"
	"time"

	listingcontracts "backend/internal/listings/contracts"
	"backend/internal/platform/outbox"
)

type eventEnqueuer interface {
	Enqueue(ctx context.Context, exec outbox.Execer, in outbox.NewEvent) (outbox.Event, error)
}

type Service struct {
	store    appointmentStore
	listings listingcontracts.Ownership
	outbox   eventEnqueuer
	policy   Policy
	now      func() time.Time
}

func NewService(store appointmentStore, listings listingcontracts.Ownership, enqueuer eventEnqueuer, policy Policy, now func() time.Time) (*Service, error) {
	if store == nil {
		return nil, errStoreRequired
	}
	if listings == nil {
		return nil, errListingsReq
	}
	if enqueuer == nil {
		return nil, errOutboxRequired
	}
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{store: store, listings: listings, outbox: enqueuer, policy: policy, now: now}, nil
}

func (s *Service) CreateAppointment(ctx context.Context, requesterID, listingID ID, scheduledAt *time.Time) (Appointment, error) {
	if s == nil || s.store == nil {
		return Appointment{}, errStoreRequired
	}
	if requesterID.IsZero() || listingID.IsZero() {
		return Appointment{}, errZeroID
	}
	providerID, err := s.requirePublishedOwner(ctx, listingID)
	if err != nil {
		return Appointment{}, err
	}
	if providerID == requesterID {
		return Appointment{}, errSelfAppointment
	}
	id, err := NewID()
	if err != nil {
		return Appointment{}, errUnavailable
	}
	now := s.now().UTC()
	appt := Appointment{
		ID:              id,
		ListingID:       listingID,
		RequesterUserID: requesterID,
		ProviderUserID:  providerID,
		Status:          StatusRequested,
		RequestedAt:     now,
		ScheduledAt:     cloneTime(scheduledAt),
		UpdatedAt:       now,
	}
	if err := appt.Validate(); err != nil {
		return Appointment{}, err
	}
	if err := s.store.InsertAppointment(ctx, appt); err != nil {
		return Appointment{}, mapStoreErr(err)
	}
	return appt, nil
}

func (s *Service) GetAppointment(ctx context.Context, userID, appointmentID ID) (Appointment, error) {
	if s == nil || s.store == nil {
		return Appointment{}, errStoreRequired
	}
	if userID.IsZero() || appointmentID.IsZero() {
		return Appointment{}, errZeroID
	}
	return s.requireParticipant(ctx, userID, appointmentID)
}

func (s *Service) ListAppointments(ctx context.Context, userID ID) ([]Appointment, error) {
	if s == nil || s.store == nil {
		return nil, errStoreRequired
	}
	if userID.IsZero() {
		return nil, errZeroID
	}
	rows, err := s.store.ListAppointmentsForUser(ctx, userID, MaxAppointments)
	if err != nil {
		return nil, mapStoreErr(err)
	}
	return rows, nil
}

func (s *Service) AcceptAppointment(ctx context.Context, userID, appointmentID ID) (Appointment, error) {
	return s.providerTransition(ctx, userID, appointmentID, StatusRequested, StatusAccepted)
}

func (s *Service) RejectAppointment(ctx context.Context, userID, appointmentID ID) (Appointment, error) {
	return s.providerTransition(ctx, userID, appointmentID, StatusRequested, StatusRejected)
}

func (s *Service) CancelAppointment(ctx context.Context, userID, appointmentID ID) (Appointment, error) {
	if s == nil || s.store == nil {
		return Appointment{}, errStoreRequired
	}
	if userID.IsZero() || appointmentID.IsZero() {
		return Appointment{}, errZeroID
	}
	appt, err := s.requireParticipant(ctx, userID, appointmentID)
	if err != nil {
		return Appointment{}, err
	}
	if !canCancel(appt.Status) {
		return Appointment{}, errInvalidTransition
	}
	updated, err := s.store.UpdateAppointmentStatus(ctx, appt.ID, appt.Status, StatusCancelled, s.now().UTC())
	if err != nil {
		return Appointment{}, mapStoreErr(err)
	}
	return updated, nil
}

func (s *Service) StartVerification(ctx context.Context, userID, appointmentID ID, method string) (IssuedChallenge, error) {
	if s == nil || s.store == nil {
		return IssuedChallenge{}, errStoreRequired
	}
	if userID.IsZero() || appointmentID.IsZero() {
		return IssuedChallenge{}, errZeroID
	}
	normalized, err := NormalizeMethod(method)
	if err != nil {
		return IssuedChallenge{}, err
	}
	appt, err := s.requireParticipant(ctx, userID, appointmentID)
	if err != nil {
		return IssuedChallenge{}, err
	}
	if appt.ProviderUserID != userID {
		return IssuedChallenge{}, errForbidden
	}
	if appt.Status != StatusAccepted {
		return IssuedChallenge{}, errInvalidTransition
	}
	raw, hash, err := issueChallengeSecret(normalized, s.policy.OTPDigits)
	if err != nil {
		return IssuedChallenge{}, errUnavailable
	}
	id, err := NewID()
	if err != nil {
		return IssuedChallenge{}, errUnavailable
	}
	now := s.now().UTC()
	ch := Challenge{
		ID:            id,
		AppointmentID: appt.ID,
		Method:        normalized,
		TokenHash:     hash,
		CreatedAt:     now,
		ExpiresAt:     now.Add(s.policy.ChallengeTTL),
	}
	if err := ch.Validate(); err != nil {
		return IssuedChallenge{}, err
	}
	if err := s.store.ReplaceChallenge(ctx, ch, now); err != nil {
		return IssuedChallenge{}, mapStoreErr(err)
	}
	return IssuedChallenge{Challenge: ch, RawToken: raw, InteractionType: InteractionListingInspection}, nil
}

func (s *Service) FinishVerification(ctx context.Context, userID, appointmentID ID, rawToken string) (VerifiedInteraction, error) {
	if s == nil || s.store == nil {
		return VerifiedInteraction{}, errStoreRequired
	}
	if s.outbox == nil {
		return VerifiedInteraction{}, errOutboxRequired
	}
	if userID.IsZero() || appointmentID.IsZero() {
		return VerifiedInteraction{}, errZeroID
	}
	hash, err := HashSubmittedChallengeSecret(rawToken, s.policy.OTPDigits)
	if err != nil {
		return VerifiedInteraction{}, err
	}
	appt, err := s.requireParticipant(ctx, userID, appointmentID)
	if err != nil {
		return VerifiedInteraction{}, err
	}
	if appt.RequesterUserID != userID {
		return VerifiedInteraction{}, errForbidden
	}
	if appt.Status != StatusAccepted {
		return VerifiedInteraction{}, errInvalidTransition
	}
	interactionID, err := NewID()
	if err != nil {
		return VerifiedInteraction{}, errUnavailable
	}
	now := s.now().UTC()
	interaction := VerifiedInteraction{
		ID:              interactionID,
		AppointmentID:   appt.ID,
		ListingID:       appt.ListingID,
		RequesterUserID: appt.RequesterUserID,
		ProviderUserID:  appt.ProviderUserID,
		InteractionType: InteractionListingInspection,
		VerifiedAt:      now,
	}
	completed, err := s.store.CompleteVerification(ctx, appt.ID, hash, interaction, now, func(ctx context.Context, exec outbox.Execer, completed VerifiedInteraction) error {
		ev, err := encodeInteractionCompleted(completed)
		if err != nil {
			return err
		}
		if _, err := s.outbox.Enqueue(ctx, exec, ev); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return VerifiedInteraction{}, mapStoreErr(err)
	}
	return completed, nil
}

func (s *Service) StartTransactionVerification(ctx context.Context, userID, listingID, requesterUserID ID, method string) (IssuedChallenge, error) {
	return s.startFlowVerification(ctx, userID, listingID, requesterUserID, method, InteractionTransaction)
}

func (s *Service) StartDeliveryVerification(ctx context.Context, userID, listingID, requesterUserID ID, method string) (IssuedChallenge, error) {
	return s.startFlowVerification(ctx, userID, listingID, requesterUserID, method, InteractionDelivery)
}

func (s *Service) StartTypedVerification(ctx context.Context, userID, listingID, requesterUserID ID, method, interactionType string) (IssuedChallenge, error) {
	return s.startFlowVerification(ctx, userID, listingID, requesterUserID, method, interactionType)
}

func (s *Service) startFlowVerification(ctx context.Context, userID, listingID, requesterUserID ID, method, interactionType string) (IssuedChallenge, error) {
	if s == nil || s.store == nil {
		return IssuedChallenge{}, errStoreRequired
	}
	if userID.IsZero() || listingID.IsZero() || requesterUserID.IsZero() {
		return IssuedChallenge{}, errZeroID
	}
	typ, err := NormalizeFlowInteractionType(interactionType)
	if err != nil {
		return IssuedChallenge{}, err
	}
	normalized, err := NormalizeMethod(method)
	if err != nil {
		return IssuedChallenge{}, err
	}
	if requesterUserID == userID {
		return IssuedChallenge{}, errSelfAppointment
	}
	ownerID, err := s.requirePublishedOwner(ctx, listingID)
	if err != nil {
		return IssuedChallenge{}, err
	}
	if ownerID != userID {
		return IssuedChallenge{}, errForbidden
	}
	raw, hash, err := issueChallengeSecret(normalized, s.policy.OTPDigits)
	if err != nil {
		return IssuedChallenge{}, errUnavailable
	}
	flowID, err := NewID()
	if err != nil {
		return IssuedChallenge{}, errUnavailable
	}
	challengeID, err := NewID()
	if err != nil {
		return IssuedChallenge{}, errUnavailable
	}
	now := s.now().UTC()
	flow := VerificationFlow{
		ID:              flowID,
		ListingID:       listingID,
		RequesterUserID: requesterUserID,
		ProviderUserID:  userID,
		InteractionType: typ,
		Status:          FlowOpen,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := flow.Validate(); err != nil {
		return IssuedChallenge{}, err
	}
	ch := Challenge{
		ID:        challengeID,
		FlowID:    flow.ID,
		Method:    normalized,
		TokenHash: hash,
		CreatedAt: now,
		ExpiresAt: now.Add(s.policy.ChallengeTTL),
	}
	if err := ch.Validate(); err != nil {
		return IssuedChallenge{}, err
	}
	if err := s.store.CreateFlowWithChallenge(ctx, flow, ch, now); err != nil {
		return IssuedChallenge{}, mapStoreErr(err)
	}
	return IssuedChallenge{Challenge: ch, RawToken: raw, InteractionType: typ}, nil
}

func (s *Service) FinishFlowVerification(ctx context.Context, userID, flowID ID, rawToken string) (VerifiedInteraction, error) {
	if s == nil || s.store == nil {
		return VerifiedInteraction{}, errStoreRequired
	}
	if s.outbox == nil {
		return VerifiedInteraction{}, errOutboxRequired
	}
	if userID.IsZero() || flowID.IsZero() {
		return VerifiedInteraction{}, errZeroID
	}
	hash, err := HashSubmittedChallengeSecret(rawToken, s.policy.OTPDigits)
	if err != nil {
		return VerifiedInteraction{}, err
	}
	flow, err := s.store.GetFlow(ctx, flowID)
	if err != nil {
		if errors.Is(err, errNotFound) {
			return VerifiedInteraction{}, errNotFound
		}
		return VerifiedInteraction{}, mapStoreErr(err)
	}
	if !flow.HasParticipant(userID) {
		return VerifiedInteraction{}, errNotFound
	}
	if flow.RequesterUserID != userID {
		return VerifiedInteraction{}, errForbidden
	}
	if flow.Status != FlowOpen {
		return VerifiedInteraction{}, errInvalidTransition
	}
	interactionID, err := NewID()
	if err != nil {
		return VerifiedInteraction{}, errUnavailable
	}
	now := s.now().UTC()
	completed, err := s.store.CompleteFlowVerification(ctx, flow.ID, hash, interactionID, now, func(ctx context.Context, exec outbox.Execer, completed VerifiedInteraction) error {
		ev, err := encodeInteractionCompleted(completed)
		if err != nil {
			return err
		}
		if _, err := s.outbox.Enqueue(ctx, exec, ev); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return VerifiedInteraction{}, mapStoreErr(err)
	}
	return completed, nil
}

func (s *Service) GetFlow(ctx context.Context, userID, flowID ID) (VerificationFlow, error) {
	if s == nil || s.store == nil {
		return VerificationFlow{}, errStoreRequired
	}
	if userID.IsZero() || flowID.IsZero() {
		return VerificationFlow{}, errZeroID
	}
	flow, err := s.store.GetFlow(ctx, flowID)
	if err != nil {
		if errors.Is(err, errNotFound) {
			return VerificationFlow{}, errNotFound
		}
		return VerificationFlow{}, mapStoreErr(err)
	}
	if !flow.HasParticipant(userID) {
		return VerificationFlow{}, errNotFound
	}
	return flow, nil
}

func (s *Service) ListFlows(ctx context.Context, userID, listingID ID) ([]VerificationFlow, error) {
	if s == nil || s.store == nil {
		return nil, errStoreRequired
	}
	if userID.IsZero() {
		return nil, errZeroID
	}
	rows, err := s.store.ListFlowsForUser(ctx, userID, listingID, MaxVerificationFlows)
	if err != nil {
		return nil, mapStoreErr(err)
	}
	return rows, nil
}

func (s *Service) GetInteraction(ctx context.Context, userID, interactionID ID) (VerifiedInteraction, error) {
	if s == nil || s.store == nil {
		return VerifiedInteraction{}, errStoreRequired
	}
	if userID.IsZero() || interactionID.IsZero() {
		return VerifiedInteraction{}, errZeroID
	}
	row, err := s.store.GetInteraction(ctx, interactionID)
	if err != nil {
		if errors.Is(err, errNotFound) {
			return VerifiedInteraction{}, errNotFound
		}
		return VerifiedInteraction{}, mapStoreErr(err)
	}
	if !row.HasParticipant(userID) {
		return VerifiedInteraction{}, errNotFound
	}
	return row, nil
}

func (s *Service) providerTransition(ctx context.Context, userID, appointmentID ID, from, to string) (Appointment, error) {
	if s == nil || s.store == nil {
		return Appointment{}, errStoreRequired
	}
	if userID.IsZero() || appointmentID.IsZero() {
		return Appointment{}, errZeroID
	}
	appt, err := s.requireParticipant(ctx, userID, appointmentID)
	if err != nil {
		return Appointment{}, err
	}
	if appt.ProviderUserID != userID {
		return Appointment{}, errForbidden
	}
	if appt.Status != from {
		return Appointment{}, errInvalidTransition
	}
	updated, err := s.store.UpdateAppointmentStatus(ctx, appt.ID, from, to, s.now().UTC())
	if err != nil {
		return Appointment{}, mapStoreErr(err)
	}
	return updated, nil
}

func (s *Service) requirePublishedOwner(ctx context.Context, listingID ID) (ID, error) {
	if s.listings == nil {
		return ID{}, errListingsReq
	}
	ref, err := s.listings.ResolveListingOwner(ctx, listingcontracts.ID(listingID))
	if err != nil {
		if errors.Is(err, listingcontracts.ErrNotFound) || errors.Is(err, listingcontracts.ErrForbidden) {
			return ID{}, errNotFound
		}
		if errors.Is(err, listingcontracts.ErrZeroID) {
			return ID{}, errZeroID
		}
		return ID{}, errUnavailable
	}
	if !ref.PubliclyVisible() {
		return ID{}, errNotFound
	}
	if ref.OwnerUserID.IsZero() {
		return ID{}, errUnavailable
	}
	return ID(ref.OwnerUserID), nil
}

func (s *Service) requireParticipant(ctx context.Context, userID, appointmentID ID) (Appointment, error) {
	appt, err := s.store.GetAppointment(ctx, appointmentID)
	if err != nil {
		if errors.Is(err, errNotFound) {
			return Appointment{}, errNotFound
		}
		return Appointment{}, mapStoreErr(err)
	}
	if !appt.HasParticipant(userID) {
		return Appointment{}, errNotFound
	}
	return appt, nil
}

func issueChallengeSecret(method string, otpDigits int) (raw string, hash []byte, err error) {
	switch method {
	case MethodOTP:
		return GenerateNumericOTP(otpDigits)
	case MethodQR:
		return GenerateChallengeToken()
	default:
		return "", nil, errInvalidMethod
	}
}

func cloneTime(in *time.Time) *time.Time {
	if in == nil {
		return nil
	}
	t := in.UTC()
	return &t
}

func mapStoreErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, errNotFound) || errors.Is(err, errZeroID) || errors.Is(err, errStoreRequired) ||
		errors.Is(err, errSelfAppointment) || errors.Is(err, errForbidden) ||
		errors.Is(err, errInvalidStatus) || errors.Is(err, errInvalidTransition) ||
		errors.Is(err, errInvalidMethod) || errors.Is(err, errInvalidToken) ||
		errors.Is(err, errExpiredChallenge) || errors.Is(err, errChallengeConsumed) ||
		errors.Is(err, errConflict) || errors.Is(err, errInvalidAppointment) ||
		errors.Is(err, errInvalidChallenge) || errors.Is(err, errInvalidInteraction) ||
		errors.Is(err, errInvalidPolicy) || errors.Is(err, errListingsReq) ||
		errors.Is(err, errOutboxRequired) {
		return err
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return errUnavailable
}
