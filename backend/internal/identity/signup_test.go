package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"backend/internal/notifications/contracts"
	"backend/internal/platform/outbox"
)

func TestStartSignupVerificationAtomicSuccess(t *testing.T) {
	ctx := context.Background()
	svc, store, intents, txns, _ := newTestSignup(t)

	got, err := svc.StartSignupVerification(ctx, StartSignupVerificationInput{
		Kind:          IdentifierEmail,
		Destination:   "  Owner@Example.com ",
		Locale:        contracts.LocaleTR,
		ClientIP:      "203.0.113.10",
		CorrelationID: "corr-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.ChallengeID.IsZero() {
		t.Fatal("challenge id required")
	}
	if txns.begins != 1 || txns.last == nil || !txns.last.committed || txns.last.rolled {
		t.Fatal("single transaction must commit")
	}
	stored := store.must(t, got.ChallengeID)
	if stored.Kind != IdentifierEmail || stored.DestinationCanonical != "owner@example.com" {
		t.Fatalf("challenge mismatch: %+v", stored)
	}
	if _, ok := store.materials[got.ChallengeID]; !ok {
		t.Fatal("material must be committed with the challenge")
	}
	if len(intents.committed) != 1 {
		t.Fatalf("intents = %d, want 1", len(intents.committed))
	}
	secret, err := svc.challenges.ResolveDeliverySecret(ctx, got.ChallengeID)
	if err != nil {
		t.Fatal(err)
	}
	assertSignupIntent(t, intents.committed[0], got.ChallengeID, contracts.ChannelEmail, contracts.LocaleTR, "corr-1", secret, "owner@example.com")
}

func TestStartSignupVerificationPhoneUsesSMS(t *testing.T) {
	ctx := context.Background()
	svc, _, intents, _, _ := newTestSignup(t)
	got, err := svc.StartSignupVerification(ctx, StartSignupVerificationInput{
		Kind:        IdentifierPhone,
		Destination: "+15551234567",
		Locale:      contracts.LocaleEN,
	})
	if err != nil {
		t.Fatal(err)
	}
	secret, err := svc.challenges.ResolveDeliverySecret(ctx, got.ChallengeID)
	if err != nil {
		t.Fatal(err)
	}
	assertSignupIntent(t, intents.committed[0], got.ChallengeID, contracts.ChannelSMS, contracts.LocaleEN, "", secret, "+15551234567")
}

func TestStartSignupVerificationOutboxFailureRollsBack(t *testing.T) {
	ctx := context.Background()
	svc, store, intents, txns, _ := newTestSignup(t)
	intents.fail = true
	if _, err := svc.StartSignupVerification(ctx, StartSignupVerificationInput{
		Kind:        IdentifierEmail,
		Destination: "rollback@example.com",
		Locale:      contracts.LocaleRU,
	}); !errors.Is(err, errUnavailable) {
		t.Fatalf("err = %v, want %v", err, errUnavailable)
	}
	if len(store.rows) != 0 || len(store.materials) != 0 || len(intents.committed) != 0 {
		t.Fatal("outbox failure must roll back challenge, material, and intent")
	}
	if txns.last == nil || !txns.last.rolled || txns.last.committed {
		t.Fatal("transaction must roll back")
	}
}

func TestStartSignupVerificationOutboxExecFailureRollsBack(t *testing.T) {
	ctx := context.Background()
	svc, store, intents, txns, _ := newTestSignup(t)
	txns.failSQL = "platform.outbox_events"
	if _, err := svc.StartSignupVerification(ctx, StartSignupVerificationInput{
		Kind:        IdentifierEmail,
		Destination: "execfail@example.com",
		Locale:      contracts.LocaleAR,
	}); !errors.Is(err, errUnavailable) {
		t.Fatalf("err = %v, want %v", err, errUnavailable)
	}
	if len(store.rows) != 0 || len(store.materials) != 0 || len(intents.committed) != 0 {
		t.Fatal("outbox write failure must leave no durable rows")
	}
}

func TestStartSignupVerificationChallengeFailureCreatesNoIntent(t *testing.T) {
	ctx := context.Background()
	svc, store, intents, txns, _ := newTestSignup(t)
	store.materialFail = true
	if _, err := svc.StartSignupVerification(ctx, StartSignupVerificationInput{
		Kind:        IdentifierEmail,
		Destination: "nointent@example.com",
		Locale:      contracts.LocaleTR,
	}); !errors.Is(err, errUnavailable) {
		t.Fatalf("err = %v, want %v", err, errUnavailable)
	}
	if len(intents.committed) != 0 || intents.enqueues != 0 {
		t.Fatal("challenge/material failure must not enqueue an intent")
	}
	if len(store.rows) != 0 || len(store.materials) != 0 {
		t.Fatal("failed challenge insert must not persist")
	}
	if txns.begins != 1 {
		t.Fatalf("begins = %d, want 1", txns.begins)
	}
}

func TestStartSignupVerificationLocaleValidation(t *testing.T) {
	ctx := context.Background()
	svc, store, intents, txns, _ := newTestSignup(t)
	if _, err := svc.StartSignupVerification(ctx, StartSignupVerificationInput{
		Kind:        IdentifierEmail,
		Destination: "locale@example.com",
		Locale:      "fr",
	}); !errors.Is(err, errInvalidChallenge) {
		t.Fatalf("err = %v, want %v", err, errInvalidChallenge)
	}
	if txns.begins != 0 || len(store.rows) != 0 || len(intents.committed) != 0 {
		t.Fatal("invalid locale must stop before the transaction")
	}
}

func TestStartSignupVerificationLimiterFailureStopsBeforeTransaction(t *testing.T) {
	ctx := context.Background()
	store := newMemChallengeStore()
	inc := &memIssuanceCounter{fail: true}
	ch, err := NewChallenges(store, testChallengePolicy(), mustLimiter(t, inc), mustProtector(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	intents := &memIntentEnqueuer{}
	txns := &memTransactor{store: store, intents: intents}
	proofs, err := NewSignupProofs(newMemSignupProofStore(), SignupProofPolicy{TTL: 15 * time.Minute}, nil)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := NewSignupVerification(ch, newMemIdentifierStore(), txns, intents, proofs)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.StartSignupVerification(ctx, StartSignupVerificationInput{
		Kind:        IdentifierEmail,
		Destination: "limited@example.com",
		Locale:      contracts.LocaleEN,
		ClientIP:    "192.0.2.9",
	}); !errors.Is(err, errUnavailable) {
		t.Fatalf("err = %v, want %v", err, errUnavailable)
	}
	if txns.begins != 0 || len(store.rows) != 0 || len(intents.committed) != 0 {
		t.Fatal("limiter failure must not open a transaction")
	}
}

func TestStartSignupVerificationIdempotencyKeysAreStablePerChallenge(t *testing.T) {
	ctx := context.Background()
	svc, _, intents, _, _ := newTestSignup(t)
	a, err := svc.StartSignupVerification(ctx, StartSignupVerificationInput{
		Kind: IdentifierEmail, Destination: "a@example.com", Locale: contracts.LocaleTR,
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.StartSignupVerification(ctx, StartSignupVerificationInput{
		Kind: IdentifierEmail, Destination: "b@example.com", Locale: contracts.LocaleTR,
	})
	if err != nil {
		t.Fatal(err)
	}
	if a.ChallengeID == b.ChallengeID {
		t.Fatal("each start mints a new challenge")
	}
	if intents.committed[0].IdempotencyKey != signupIntentIdempotencyPrefix+a.ChallengeID.String() {
		t.Fatalf("idempotency a = %q", intents.committed[0].IdempotencyKey)
	}
	if intents.committed[1].IdempotencyKey != signupIntentIdempotencyPrefix+b.ChallengeID.String() {
		t.Fatalf("idempotency b = %q", intents.committed[1].IdempotencyKey)
	}
	if intents.committed[0].IdempotencyKey == intents.committed[1].IdempotencyKey {
		t.Fatal("idempotency keys must be unique per challenge")
	}
}

func TestStartSignupVerificationDuplicateIdempotencyRollsBack(t *testing.T) {
	ctx := context.Background()
	svc, store, intents, _, _ := newTestSignup(t)
	intents.conflict = true
	if _, err := svc.StartSignupVerification(ctx, StartSignupVerificationInput{
		Kind:        IdentifierEmail,
		Destination: "dup@example.com",
		Locale:      contracts.LocaleEN,
	}); !errors.Is(err, errUnavailable) {
		t.Fatalf("err = %v, want %v", err, errUnavailable)
	}
	if len(store.rows) != 0 || len(intents.committed) != 0 {
		t.Fatal("idempotency conflict must roll back")
	}
}

func TestStartSignupVerificationProductionResultOmitsSecret(t *testing.T) {
	raw, _ := json.Marshal(StartSignupVerificationResult{})
	if strings.Contains(strings.ToLower(string(raw)), "secret") ||
		strings.Contains(strings.ToLower(string(raw)), "otp") ||
		strings.Contains(strings.ToLower(string(raw)), "token") {
		t.Fatalf("result json leaked secret field: %s", raw)
	}
}

func assertSignupIntent(t *testing.T, ev outbox.NewEvent, challengeID ID, channel contracts.Channel, locale, corr, secret, dest string) {
	t.Helper()
	if ev.EventType != contracts.IntentEventType || ev.EventVersion != contracts.IntentEventVersion {
		t.Fatalf("routing = %s v%d", ev.EventType, ev.EventVersion)
	}
	if ev.AggregateType != signupAggregateType || ev.AggregateID != challengeID.String() {
		t.Fatalf("aggregate = %s %s", ev.AggregateType, ev.AggregateID)
	}
	if ev.IdempotencyKey != signupIntentIdempotencyPrefix+challengeID.String() {
		t.Fatalf("idempotency = %q", ev.IdempotencyKey)
	}
	intent, err := contracts.DecodeIntent(ev.Payload)
	if err != nil {
		t.Fatalf("decode intent: %v", err)
	}
	if intent.Purpose != contracts.PurposeSecurity || intent.TemplateCode != contracts.TemplateIdentityVerificationSignup {
		t.Fatalf("intent catalog: %+v", intent)
	}
	if intent.Channel != channel || intent.Locale != locale {
		t.Fatalf("channel/locale = %s %s", intent.Channel, intent.Locale)
	}
	if intent.Recipient.Kind != contracts.RecipientVerificationChallenge || intent.Recipient.ID != challengeID.String() {
		t.Fatalf("recipient = %+v", intent.Recipient)
	}
	if intent.IntentID != challengeID.String() {
		t.Fatalf("intent_id = %s, want challenge id", intent.IntentID)
	}
	if intent.CorrelationID != corr {
		t.Fatalf("correlation = %q", intent.CorrelationID)
	}
	body := string(ev.Payload)
	if strings.Contains(body, dest) || strings.Contains(strings.ToLower(body), "owner@") {
		t.Fatal("raw destination must not appear in outbox payload")
	}
	if secret != "" && strings.Contains(body, secret) {
		t.Fatal("OTP/token must not appear in outbox payload")
	}
	if strings.Contains(strings.ToLower(body), "otp") || strings.Contains(strings.ToLower(body), "token") ||
		strings.Contains(strings.ToLower(body), "secret") {
		t.Fatal("outbox payload must not use secret field names")
	}
}

func newTestSignup(t *testing.T) (*SignupVerification, *memChallengeStore, *memIntentEnqueuer, *memTransactor, func() time.Time) {
	t.Helper()
	store := newMemChallengeStore()
	inc := &memIssuanceCounter{}
	now := func() time.Time { return time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC) }
	ch, err := NewChallenges(store, testChallengePolicy(), mustLimiter(t, inc), mustProtector(t), now)
	if err != nil {
		t.Fatal(err)
	}
	intents := &memIntentEnqueuer{keys: make(map[string]struct{})}
	txns := &memTransactor{store: store, intents: intents}
	idents := newMemIdentifierStore()
	proofs, err := NewSignupProofs(newMemSignupProofStore(), SignupProofPolicy{TTL: 15 * time.Minute}, now)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := NewSignupVerification(ch, idents, txns, intents, proofs)
	if err != nil {
		t.Fatal(err)
	}
	return svc, store, intents, txns, now
}

type memTransactor struct {
	store    *memChallengeStore
	intents  *memIntentEnqueuer
	begins   int
	failBegin bool
	failSQL  string
	last     *memTx
}

func (t *memTransactor) Begin(context.Context) (transaction, error) {
	t.begins++
	if t.failBegin {
		return nil, errUnavailable
	}
	tx := &memTx{store: t.store, intents: t.intents, failSQL: t.failSQL}
	t.last = tx
	return tx, nil
}

type memTx struct {
	store        *memChallengeStore
	intents      *memIntentEnqueuer
	failSQL      string
	stagedCh     []VerificationChallenge
	stagedMat    []VerificationMaterial
	stagedEvents []outbox.NewEvent
	committed    bool
	rolled       bool
}

func (tx *memTx) Exec(_ context.Context, sql string, _ ...any) (int64, error) {
	if tx.committed || tx.rolled {
		return 0, errUnavailable
	}
	if tx.failSQL != "" && strings.Contains(sql, tx.failSQL) {
		return 0, errUnavailable
	}
	return 1, nil
}

func (tx *memTx) stageChallenge(ch VerificationChallenge, mat VerificationMaterial) {
	tx.stagedCh = append(tx.stagedCh, cloneChallenge(ch))
	tx.stagedMat = append(tx.stagedMat, cloneMaterial(mat))
}

func (tx *memTx) stageEvent(in outbox.NewEvent) {
	tx.stagedEvents = append(tx.stagedEvents, in)
}

func (tx *memTx) Commit(context.Context) error {
	if tx.committed || tx.rolled {
		return errUnavailable
	}
	tx.store.mu.Lock()
	for i, ch := range tx.stagedCh {
		tx.store.rows[ch.ID] = cloneChallenge(ch)
		tx.store.materials[ch.ID] = cloneMaterial(tx.stagedMat[i])
	}
	tx.store.mu.Unlock()
	tx.intents.apply(tx.stagedEvents)
	tx.committed = true
	return nil
}

func (tx *memTx) Rollback(context.Context) error {
	if tx.committed {
		return nil
	}
	tx.rolled = true
	tx.stagedCh = nil
	tx.stagedMat = nil
	tx.stagedEvents = nil
	return nil
}

type memIntentEnqueuer struct {
	mu         sync.Mutex
	committed  []outbox.NewEvent
	keys       map[string]struct{}
	enqueues   int
	fail       bool
	conflict   bool
	forceKey   string
}

func (e *memIntentEnqueuer) seedKey(key string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.keys == nil {
		e.keys = make(map[string]struct{})
	}
	e.keys[key] = struct{}{}
}

func (e *memIntentEnqueuer) Enqueue(ctx context.Context, exec outbox.Execer, in outbox.NewEvent) (outbox.Event, error) {
	e.enqueues++
	if e.fail {
		return outbox.Event{}, errUnavailable
	}
	if e.conflict {
		return outbox.Event{}, outbox.ErrConflict
	}
	if e.forceKey != "" {
		in.IdempotencyKey = e.forceKey
	}
	e.mu.Lock()
	_, dup := e.keys[in.IdempotencyKey]
	e.mu.Unlock()
	if in.IdempotencyKey != "" && dup {
		return outbox.Event{}, outbox.ErrConflict
	}
	if exec != nil {
		if _, err := exec.Exec(ctx, "platform.outbox_events", in); err != nil {
			return outbox.Event{}, err
		}
		if tx, ok := exec.(*memTx); ok {
			tx.stageEvent(in)
			return outbox.Event{EventType: in.EventType, EventVersion: in.EventVersion, Payload: in.Payload}, nil
		}
		if tx, ok := exec.(*memResetTx); ok {
			tx.stageEvent(in)
			return outbox.Event{EventType: in.EventType, EventVersion: in.EventVersion, Payload: in.Payload}, nil
		}
	}
	e.apply([]outbox.NewEvent{in})
	return outbox.Event{EventType: in.EventType, EventVersion: in.EventVersion, Payload: in.Payload}, nil
}

func (e *memIntentEnqueuer) apply(events []outbox.NewEvent) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.keys == nil {
		e.keys = make(map[string]struct{})
	}
	for _, ev := range events {
		if ev.IdempotencyKey != "" {
			e.keys[ev.IdempotencyKey] = struct{}{}
		}
		e.committed = append(e.committed, ev)
	}
}

func TestStartSignupVerificationExistingIdentifierDoesNotLeak(t *testing.T) {
	ctx := context.Background()
	svc, store, intents, txns, _ := newTestSignup(t)
	idents := svc.identifiers.(*memIdentifierStore)
	user := idents.putUser(t, User{})
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	ident := UserIdentifier{
		ID:             mustID(t),
		UserID:         user.ID,
		Kind:           IdentifierEmail,
		ValueCanonical: "owner@example.com",
		CreatedAt:      now,
	}
	if err := idents.InsertIdentifier(ctx, ident); err != nil {
		t.Fatal(err)
	}

	got, err := svc.StartSignupVerification(ctx, StartSignupVerificationInput{
		Kind:        IdentifierEmail,
		Destination: "  Owner@Example.com ",
		Locale:      contracts.LocaleTR,
		ClientIP:    "203.0.113.10",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.ChallengeID.IsZero() {
		t.Fatal("generic challenge id required")
	}
	if txns.begins != 0 || len(store.rows) != 0 || len(intents.committed) != 0 {
		t.Fatal("existing identifier must not issue a challenge or intent")
	}
	raw, _ := json.Marshal(got)
	body := strings.ToLower(string(raw))
	if strings.Contains(body, "exist") || strings.Contains(body, "registered") || strings.Contains(body, "conflict") {
		t.Fatalf("existence leak: %s", raw)
	}
}

func TestFinishSignupVerificationIssuesHashOnlyProof(t *testing.T) {
	ctx := context.Background()
	svc, _, _, _, _ := newTestSignup(t)
	started, err := svc.StartSignupVerification(ctx, StartSignupVerificationInput{
		Kind:        IdentifierEmail,
		Destination: "new@example.com",
		Locale:      contracts.LocaleEN,
	})
	if err != nil {
		t.Fatal(err)
	}
	secret, err := svc.challenges.ResolveDeliverySecret(ctx, started.ChallengeID)
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.FinishSignupVerification(ctx, FinishSignupVerificationInput{
		ChallengeID: started.ChallengeID,
		Code:        secret,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Verified || got.SignupProof == "" {
		t.Fatalf("result = %+v", got)
	}
	proofs := svc.proofs.store.(*memSignupProofStore)
	if len(proofs.rows) != 1 {
		t.Fatalf("stored proofs = %d", len(proofs.rows))
	}
	var stored SignupProof
	for _, p := range proofs.rows {
		stored = p
	}
	if bytes.Contains(stored.TokenHash, []byte(got.SignupProof)) {
		t.Fatal("raw signup proof must not appear in stored hash")
	}
	if stored.Purpose != SignupProofSignup {
		t.Fatalf("purpose = %s", stored.Purpose)
	}
	if stored.Kind != IdentifierEmail || stored.DestinationCanonical != "new@example.com" {
		t.Fatalf("binding = %+v", stored)
	}
	if stored.ChallengeID != started.ChallengeID {
		t.Fatal("proof must bind to challenge id")
	}
	if stored.ConsumedAt != nil {
		t.Fatal("fresh proof must be unused")
	}
}

func TestFinishSignupVerificationRejectsWrongExpiredConsumed(t *testing.T) {
	ctx := context.Background()
	svc, _, _, _, clock := newTestSignup(t)
	started, err := svc.StartSignupVerification(ctx, StartSignupVerificationInput{
		Kind:        IdentifierPhone,
		Destination: "+15551234567",
		Locale:      contracts.LocaleAR,
	})
	if err != nil {
		t.Fatal(err)
	}
	secret, err := svc.challenges.ResolveDeliverySecret(ctx, started.ChallengeID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.FinishSignupVerification(ctx, FinishSignupVerificationInput{
		ChallengeID: started.ChallengeID,
		Code:        "000000",
	}); !errors.Is(err, errInvalidChallenge) {
		t.Fatalf("wrong code err = %v", err)
	}

	ok, err := svc.FinishSignupVerification(ctx, FinishSignupVerificationInput{
		ChallengeID: started.ChallengeID,
		Code:        secret,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !ok.Verified {
		t.Fatal("valid code must succeed once")
	}
	if _, err := svc.FinishSignupVerification(ctx, FinishSignupVerificationInput{
		ChallengeID: started.ChallengeID,
		Code:        secret,
	}); !errors.Is(err, errChallengeConsumed) {
		t.Fatalf("replay err = %v, want consumed", err)
	}

	expired, err := svc.StartSignupVerification(ctx, StartSignupVerificationInput{
		Kind:        IdentifierEmail,
		Destination: "later@example.com",
		Locale:      contracts.LocaleRU,
	})
	if err != nil {
		t.Fatal(err)
	}
	expiredSecret, err := svc.challenges.ResolveDeliverySecret(ctx, expired.ChallengeID)
	if err != nil {
		t.Fatal(err)
	}
	ch := svc.challenges
	past := clock().Add(ch.policy.TTL + time.Second)
	ch.now = func() time.Time { return past }
	if _, err := svc.FinishSignupVerification(ctx, FinishSignupVerificationInput{
		ChallengeID: expired.ChallengeID,
		Code:        expiredSecret,
	}); !errors.Is(err, errChallengeExpired) {
		t.Fatalf("expired err = %v", err)
	}
}

func TestFinishSignupVerificationInfrastructureUnavailable(t *testing.T) {
	ctx := context.Background()
	svc, store, _, _, _ := newTestSignup(t)
	started, err := svc.StartSignupVerification(ctx, StartSignupVerificationInput{
		Kind: IdentifierEmail, Destination: "infra@example.com", Locale: contracts.LocaleTR,
	})
	if err != nil {
		t.Fatal(err)
	}
	secret, err := svc.challenges.ResolveDeliverySecret(ctx, started.ChallengeID)
	if err != nil {
		t.Fatal(err)
	}
	store.unavailable = true
	if _, err := svc.FinishSignupVerification(ctx, FinishSignupVerificationInput{
		ChallengeID: started.ChallengeID,
		Code:        secret,
	}); !errors.Is(err, errUnavailable) {
		t.Fatalf("err = %v, want unavailable", err)
	}
}

type memSignupProofStore struct {
	mu          sync.Mutex
	rows        map[ID]SignupProof
	unavailable bool
}

func newMemSignupProofStore() *memSignupProofStore {
	return &memSignupProofStore{rows: make(map[ID]SignupProof)}
}

func (m *memSignupProofStore) InsertSignupProof(_ context.Context, proof SignupProof) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.unavailable {
		return errUnavailable
	}
	for _, existing := range m.rows {
		if existing.ChallengeID == proof.ChallengeID || bytes.Equal(existing.TokenHash, proof.TokenHash) {
			return errInvalidSignupProof
		}
	}
	cloned := proof
	cloned.TokenHash = cloneBytes(proof.TokenHash)
	m.rows[proof.ID] = cloned
	return nil
}

func (m *memSignupProofStore) GetSignupProof(_ context.Context, id ID) (SignupProof, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.unavailable {
		return SignupProof{}, errUnavailable
	}
	p, ok := m.rows[id]
	if !ok {
		return SignupProof{}, errNotFound
	}
	cloned := p
	cloned.TokenHash = cloneBytes(p.TokenHash)
	return cloned, nil
}

func (m *memSignupProofStore) ConsumeSignupProofByTokenHash(_ context.Context, _ transaction, tokenHash []byte, now time.Time) (SignupProof, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.unavailable {
		return SignupProof{}, errUnavailable
	}
	for id, p := range m.rows {
		if !bytes.Equal(p.TokenHash, tokenHash) {
			continue
		}
		if p.Purpose != SignupProofSignup {
			return SignupProof{}, errInvalidSignupProof
		}
		if err := p.rejectUnusable(now); err != nil {
			return SignupProof{}, err
		}
		consumed := now
		p.ConsumedAt = &consumed
		m.rows[id] = p
		cloned := p
		cloned.TokenHash = cloneBytes(p.TokenHash)
		return cloned, nil
	}
	return SignupProof{}, errInvalidSignupProof
}

