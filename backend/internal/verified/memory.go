package verified

import (
	"bytes"
	"context"
	"sort"
	"sync"
	"time"

	"backend/internal/platform/outbox"
)

var _ appointmentStore = (*MemoryStore)(nil)

type MemoryStore struct {
	mu           sync.Mutex
	appointments map[ID]Appointment
	flows        map[ID]VerificationFlow
	challenges   map[ID]Challenge
	interactions map[ID]VerifiedInteraction
	byAppt       map[ID]ID
	byFlow       map[ID]ID
	fail         error
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		appointments: make(map[ID]Appointment),
		flows:        make(map[ID]VerificationFlow),
		challenges:   make(map[ID]Challenge),
		interactions: make(map[ID]VerifiedInteraction),
		byAppt:       make(map[ID]ID),
		byFlow:       make(map[ID]ID),
	}
}

func (m *MemoryStore) SetFail(err error) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fail = err
}

func (m *MemoryStore) ChallengesSnapshot() []Challenge {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Challenge, 0, len(m.challenges))
	for _, ch := range m.challenges {
		copied := ch
		copied.TokenHash = append([]byte(nil), ch.TokenHash...)
		out = append(out, copied)
	}
	return out
}

func (m *MemoryStore) InsertAppointment(ctx context.Context, appt Appointment) error {
	if m == nil {
		return errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := appt.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	if _, ok := m.appointments[appt.ID]; ok {
		return errConflict
	}
	m.appointments[appt.ID] = cloneAppointment(appt)
	return nil
}

func (m *MemoryStore) GetAppointment(ctx context.Context, id ID) (Appointment, error) {
	if m == nil {
		return Appointment{}, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return Appointment{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return Appointment{}, m.fail
	}
	appt, ok := m.appointments[id]
	if !ok {
		return Appointment{}, errNotFound
	}
	return cloneAppointment(withInteractionID(appt, m.byAppt)), nil
}

func (m *MemoryStore) ListAppointmentsForUser(ctx context.Context, userID ID, limit int) ([]Appointment, error) {
	if m == nil {
		return nil, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return nil, m.fail
	}
	out := make([]Appointment, 0)
	for _, appt := range m.appointments {
		if appt.HasParticipant(userID) {
			out = append(out, cloneAppointment(withInteractionID(appt, m.byAppt)))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].UpdatedAt.After(out[j].UpdatedAt)
		}
		return out[i].ID.String() > out[j].ID.String()
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *MemoryStore) UpdateAppointmentStatus(ctx context.Context, id ID, fromStatus, toStatus string, updatedAt time.Time) (Appointment, error) {
	if m == nil {
		return Appointment{}, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return Appointment{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return Appointment{}, m.fail
	}
	appt, ok := m.appointments[id]
	if !ok {
		return Appointment{}, errNotFound
	}
	if appt.Status != fromStatus {
		return Appointment{}, errInvalidTransition
	}
	appt.Status = toStatus
	appt.UpdatedAt = updatedAt.UTC()
	if err := appt.Validate(); err != nil {
		return Appointment{}, err
	}
	m.appointments[id] = cloneAppointment(appt)
	return cloneAppointment(appt), nil
}

func (m *MemoryStore) ReplaceChallenge(ctx context.Context, ch Challenge, now time.Time) error {
	if m == nil {
		return errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ch.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	if !ch.AppointmentID.IsZero() {
		if _, ok := m.appointments[ch.AppointmentID]; !ok {
			return errNotFound
		}
	}
	if !ch.FlowID.IsZero() {
		if _, ok := m.flows[ch.FlowID]; !ok {
			return errNotFound
		}
	}
	for id, existing := range m.challenges {
		sameParent := (!ch.AppointmentID.IsZero() && existing.AppointmentID == ch.AppointmentID) ||
			(!ch.FlowID.IsZero() && existing.FlowID == ch.FlowID)
		if sameParent && existing.ConsumedAt == nil && now.Before(existing.ExpiresAt) {
			existing.ExpiresAt = now.UTC()
			m.challenges[id] = existing
		}
	}
	copied := ch
	copied.TokenHash = append([]byte(nil), ch.TokenHash...)
	m.challenges[ch.ID] = copied
	return nil
}

func (m *MemoryStore) CreateFlowWithChallenge(ctx context.Context, flow VerificationFlow, ch Challenge, now time.Time) error {
	if m == nil {
		return errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := flow.Validate(); err != nil {
		return err
	}
	if err := ch.Validate(); err != nil {
		return err
	}
	if ch.FlowID != flow.ID || !ch.AppointmentID.IsZero() {
		return errInvalidChallenge
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	if _, ok := m.flows[flow.ID]; ok {
		return errConflict
	}
	if _, ok := m.challenges[ch.ID]; ok {
		return errConflict
	}
	m.flows[flow.ID] = cloneFlow(flow)
	copied := ch
	copied.TokenHash = append([]byte(nil), ch.TokenHash...)
	m.challenges[ch.ID] = copied
	return nil
}

func (m *MemoryStore) GetFlow(ctx context.Context, id ID) (VerificationFlow, error) {
	if m == nil {
		return VerificationFlow{}, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return VerificationFlow{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return VerificationFlow{}, m.fail
	}
	flow, ok := m.flows[id]
	if !ok {
		return VerificationFlow{}, errNotFound
	}
	return cloneFlow(withFlowInteractionID(flow, m.byFlow)), nil
}

func (m *MemoryStore) ListFlowsForUser(ctx context.Context, userID ID, listingID ID, limit int) ([]VerificationFlow, error) {
	if m == nil {
		return nil, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return nil, m.fail
	}
	out := make([]VerificationFlow, 0)
	for _, flow := range m.flows {
		if !flow.HasParticipant(userID) {
			continue
		}
		if !listingID.IsZero() && flow.ListingID != listingID {
			continue
		}
		out = append(out, cloneFlow(withFlowInteractionID(flow, m.byFlow)))
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID.String() > out[j].ID.String()
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *MemoryStore) GetChallenge(ctx context.Context, id ID) (Challenge, error) {
	if m == nil {
		return Challenge{}, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return Challenge{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return Challenge{}, m.fail
	}
	ch, ok := m.challenges[id]
	if !ok {
		return Challenge{}, errNotFound
	}
	copied := ch
	copied.TokenHash = append([]byte(nil), ch.TokenHash...)
	return copied, nil
}

func (m *MemoryStore) ListChallenges(ctx context.Context, appointmentID ID) ([]Challenge, error) {
	if m == nil {
		return nil, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return nil, m.fail
	}
	out := make([]Challenge, 0)
	for _, ch := range m.challenges {
		if ch.AppointmentID == appointmentID {
			copied := ch
			copied.TokenHash = append([]byte(nil), ch.TokenHash...)
			out = append(out, copied)
		}
	}
	return out, nil
}

func (m *MemoryStore) GetInteraction(ctx context.Context, id ID) (VerifiedInteraction, error) {
	if m == nil {
		return VerifiedInteraction{}, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return VerifiedInteraction{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return VerifiedInteraction{}, m.fail
	}
	row, ok := m.interactions[id]
	if !ok {
		return VerifiedInteraction{}, errNotFound
	}
	return row, nil
}

func (m *MemoryStore) GetInteractionByAppointment(ctx context.Context, appointmentID ID) (VerifiedInteraction, error) {
	if m == nil {
		return VerifiedInteraction{}, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return VerifiedInteraction{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return VerifiedInteraction{}, m.fail
	}
	id, ok := m.byAppt[appointmentID]
	if !ok {
		return VerifiedInteraction{}, errNotFound
	}
	return m.interactions[id], nil
}

func (m *MemoryStore) CompleteVerification(ctx context.Context, appointmentID ID, tokenHash []byte, interaction VerifiedInteraction, now time.Time, enqueue func(ctx context.Context, exec outbox.Execer, completed VerifiedInteraction) error) (VerifiedInteraction, error) {
	if m == nil {
		return VerifiedInteraction{}, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return VerifiedInteraction{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return VerifiedInteraction{}, m.fail
	}
	appt, ok := m.appointments[appointmentID]
	if !ok {
		return VerifiedInteraction{}, errNotFound
	}
	if appt.Status != StatusAccepted {
		return VerifiedInteraction{}, errInvalidTransition
	}
	var match *Challenge
	for id, ch := range m.challenges {
		if ch.AppointmentID != appointmentID {
			continue
		}
		if len(ch.TokenHash) != len(tokenHash) || !bytes.Equal(ch.TokenHash, tokenHash) {
			continue
		}
		copied := ch
		copied.ID = id
		match = &copied
		break
	}
	if match == nil {
		return VerifiedInteraction{}, errInvalidToken
	}
	if match.Consumed() {
		return VerifiedInteraction{}, errChallengeConsumed
	}
	if match.Expired(now) {
		return VerifiedInteraction{}, errExpiredChallenge
	}
	if _, exists := m.byAppt[appointmentID]; exists {
		return VerifiedInteraction{}, errConflict
	}
	consumed := now.UTC()
	match.ConsumedAt = &consumed
	stored := *match
	stored.TokenHash = append([]byte(nil), match.TokenHash...)
	m.challenges[match.ID] = stored
	appt.Status = StatusCompleted
	appt.UpdatedAt = now.UTC()
	m.appointments[appointmentID] = cloneAppointment(appt)
	interaction.AppointmentID = appointmentID
	interaction.FlowID = ID{}
	interaction.InteractionType = InteractionListingInspection
	interaction.VerificationMethod = match.Method
	interaction.VerifiedAt = now.UTC()
	if err := interaction.Validate(); err != nil {
		return VerifiedInteraction{}, err
	}
	m.interactions[interaction.ID] = interaction
	m.byAppt[appointmentID] = interaction.ID
	if enqueue != nil {
		if err := enqueue(ctx, nil, interaction); err != nil {
			delete(m.interactions, interaction.ID)
			delete(m.byAppt, appointmentID)
			return VerifiedInteraction{}, err
		}
	}
	return interaction, nil
}

func (m *MemoryStore) CompleteFlowVerification(ctx context.Context, flowID ID, tokenHash []byte, interactionID ID, now time.Time, enqueue func(ctx context.Context, exec outbox.Execer, completed VerifiedInteraction) error) (VerifiedInteraction, error) {
	if m == nil {
		return VerifiedInteraction{}, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return VerifiedInteraction{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return VerifiedInteraction{}, m.fail
	}
	flow, ok := m.flows[flowID]
	if !ok {
		return VerifiedInteraction{}, errNotFound
	}
	if flow.Status != FlowOpen {
		return VerifiedInteraction{}, errInvalidTransition
	}
	var match *Challenge
	for id, ch := range m.challenges {
		if ch.FlowID != flowID {
			continue
		}
		if len(ch.TokenHash) != len(tokenHash) || !bytes.Equal(ch.TokenHash, tokenHash) {
			continue
		}
		copied := ch
		copied.ID = id
		match = &copied
		break
	}
	if match == nil {
		return VerifiedInteraction{}, errInvalidToken
	}
	if match.Consumed() {
		return VerifiedInteraction{}, errChallengeConsumed
	}
	if match.Expired(now) {
		return VerifiedInteraction{}, errExpiredChallenge
	}
	if _, exists := m.byFlow[flowID]; exists {
		return VerifiedInteraction{}, errConflict
	}
	consumed := now.UTC()
	match.ConsumedAt = &consumed
	stored := *match
	stored.TokenHash = append([]byte(nil), match.TokenHash...)
	m.challenges[match.ID] = stored
	flow.Status = FlowCompleted
	flow.UpdatedAt = now.UTC()
	m.flows[flowID] = cloneFlow(flow)
	interaction := VerifiedInteraction{
		ID:                 interactionID,
		FlowID:             flow.ID,
		ListingID:          flow.ListingID,
		RequesterUserID:    flow.RequesterUserID,
		ProviderUserID:     flow.ProviderUserID,
		InteractionType:    flow.InteractionType,
		VerificationMethod: match.Method,
		VerifiedAt:         now.UTC(),
	}
	if err := interaction.Validate(); err != nil {
		return VerifiedInteraction{}, err
	}
	m.interactions[interaction.ID] = interaction
	m.byFlow[flowID] = interaction.ID
	if enqueue != nil {
		if err := enqueue(ctx, nil, interaction); err != nil {
			delete(m.interactions, interaction.ID)
			delete(m.byFlow, flowID)
			return VerifiedInteraction{}, err
		}
	}
	return interaction, nil
}

func withInteractionID(appt Appointment, byAppt map[ID]ID) Appointment {
	if id, ok := byAppt[appt.ID]; ok {
		copied := id
		appt.VerifiedInteractionID = &copied
	}
	return appt
}

func withFlowInteractionID(flow VerificationFlow, byFlow map[ID]ID) VerificationFlow {
	if id, ok := byFlow[flow.ID]; ok {
		copied := id
		flow.CompletedInteractionID = &copied
	}
	return flow
}

func cloneFlow(flow VerificationFlow) VerificationFlow {
	out := flow
	if flow.CompletedInteractionID != nil {
		id := *flow.CompletedInteractionID
		out.CompletedInteractionID = &id
	}
	return out
}

func cloneAppointment(appt Appointment) Appointment {
	out := appt
	if appt.ScheduledAt != nil {
		t := appt.ScheduledAt.UTC()
		out.ScheduledAt = &t
	}
	if appt.VerifiedInteractionID != nil {
		id := *appt.VerifiedInteractionID
		out.VerifiedInteractionID = &id
	}
	return out
}
