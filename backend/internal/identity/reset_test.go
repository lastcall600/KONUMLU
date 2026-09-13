package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"backend/internal/notifications/contracts"
	"backend/internal/platform/outbox"
)

func TestStartPasswordResetKnownIdentifier(t *testing.T) {
	ctx := context.Background()
	svc, store, intents, _, user := newTestPasswordReset(t)
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	putVerifiedIdent(t, svc, user.ID, IdentifierEmail, "owner@example.com", now)

	got, err := svc.StartPasswordReset(ctx, StartPasswordResetInput{
		Kind:        IdentifierEmail,
		Destination: "  Owner@Example.com ",
		Locale:      contracts.LocaleTR,
		ClientIP:    "203.0.113.10",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.ChallengeID.IsZero() {
		t.Fatal("challenge id required")
	}
	ch := store.must(t, got.ChallengeID)
	if ch.Purpose != ChallengePasswordReset || ch.DestinationCanonical != "owner@example.com" {
		t.Fatalf("challenge = %+v", ch)
	}
	if len(intents.committed) != 1 {
		t.Fatalf("intents = %d", len(intents.committed))
	}
	assertResetIntent(t, intents.committed[0], got.ChallengeID, contracts.ChannelEmail, contracts.LocaleTR, "")
}

func TestStartPasswordResetUnknownIdentifierSameAcceptedShape(t *testing.T) {
	ctx := context.Background()
	svc, store, intents, txns, user := newTestPasswordReset(t)
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	putVerifiedIdent(t, svc, user.ID, IdentifierEmail, "owner@example.com", now)

	known, err := svc.StartPasswordReset(ctx, StartPasswordResetInput{
		Kind: IdentifierEmail, Destination: "owner@example.com", Locale: contracts.LocaleEN, ClientIP: "203.0.113.10",
	})
	if err != nil {
		t.Fatal(err)
	}
	unknown, err := svc.StartPasswordReset(ctx, StartPasswordResetInput{
		Kind: IdentifierEmail, Destination: "nobody@example.com", Locale: contracts.LocaleEN, ClientIP: "203.0.113.10",
	})
	if err != nil {
		t.Fatal(err)
	}
	if unknown.ChallengeID.IsZero() || known.ChallengeID == unknown.ChallengeID {
		t.Fatalf("known=%s unknown=%s", known.ChallengeID, unknown.ChallengeID)
	}
	if _, ok := store.rows[unknown.ChallengeID]; ok {
		t.Fatal("unknown identifier must not create a claimable challenge")
	}
	if len(store.rows) != 1 {
		t.Fatalf("stored challenges = %d, want 1", len(store.rows))
	}
	if txns.begins != 1 {
		t.Fatalf("unknown start must not open a second transaction: begins=%d", txns.begins)
	}
	if len(intents.committed) != 1 {
		t.Fatal("unknown identifier must not enqueue an intent")
	}
	rawKnown, _ := json.Marshal(known)
	rawUnknown, _ := json.Marshal(unknown)
	if !bytes.Contains(rawKnown, []byte("ChallengeID")) || !bytes.Contains(rawUnknown, []byte("ChallengeID")) {
		t.Fatalf("response shape mismatch known=%s unknown=%s", rawKnown, rawUnknown)
	}
	body := strings.ToLower(string(rawUnknown))
	if strings.Contains(body, "exist") || strings.Contains(body, "unknown") || strings.Contains(body, "not found") {
		t.Fatalf("enumeration leak: %s", rawUnknown)
	}
}

func TestStartPasswordResetPhoneTemplateAndPurpose(t *testing.T) {
	ctx := context.Background()
	svc, _, intents, _, user := newTestPasswordReset(t)
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	putVerifiedIdent(t, svc, user.ID, IdentifierPhone, "+15551234567", now)

	got, err := svc.StartPasswordReset(ctx, StartPasswordResetInput{
		Kind: IdentifierPhone, Destination: "+15551234567", Locale: contracts.LocaleAR, CorrelationID: "corr-reset",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertResetIntent(t, intents.committed[0], got.ChallengeID, contracts.ChannelSMS, contracts.LocaleAR, "corr-reset")
}

func TestVerifyPasswordResetIssuesSingleUseProof(t *testing.T) {
	ctx := context.Background()
	svc, _, _, _, user := newTestPasswordReset(t)
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	putVerifiedIdent(t, svc, user.ID, IdentifierEmail, "reset@example.com", now)

	started, err := svc.StartPasswordReset(ctx, StartPasswordResetInput{
		Kind: IdentifierEmail, Destination: "reset@example.com", Locale: contracts.LocaleEN,
	})
	if err != nil {
		t.Fatal(err)
	}
	secret, err := svc.challenges.ResolveDeliverySecret(ctx, started.ChallengeID)
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.VerifyPasswordReset(ctx, VerifyPasswordResetInput{ChallengeID: started.ChallengeID, Code: secret})
	if err != nil {
		t.Fatal(err)
	}
	if got.ResetProof == "" {
		t.Fatal("reset proof required")
	}
	if strings.Contains(got.ResetProof, secret) {
		t.Fatal("reset proof must not be the OTP")
	}
	proofs := svc.proofs.store.(*memStore)
	if len(proofs.resetProofs) != 1 {
		t.Fatalf("stored proofs = %d", len(proofs.resetProofs))
	}
	for _, p := range proofs.resetProofs {
		if p.UserID != user.ID || p.ChallengeID != started.ChallengeID || p.Purpose != PasswordResetProofPasswordReset {
			t.Fatalf("proof = %+v", p)
		}
		if bytes.Contains(p.TokenHash, []byte(got.ResetProof)) {
			t.Fatal("raw proof must not be persisted")
		}
	}
}

func TestVerifyPasswordResetWrongCodeIsGeneric(t *testing.T) {
	ctx := context.Background()
	svc, store, _, _, user := newTestPasswordReset(t)
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	putVerifiedIdent(t, svc, user.ID, IdentifierEmail, "reset@example.com", now)
	started, err := svc.StartPasswordReset(ctx, StartPasswordResetInput{
		Kind: IdentifierEmail, Destination: "reset@example.com", Locale: contracts.LocaleEN,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.VerifyPasswordReset(ctx, VerifyPasswordResetInput{ChallengeID: started.ChallengeID, Code: "000000"}); !errors.Is(err, errInvalidChallenge) {
		t.Fatalf("err = %v", err)
	}
	ch := store.must(t, started.ChallengeID)
	if ch.ConsumedAt != nil {
		t.Fatal("wrong code must not consume the challenge")
	}
}

func TestVerifyPasswordResetSignupChallengeRejected(t *testing.T) {
	ctx := context.Background()
	svc, _, _, _, _ := newTestPasswordReset(t)
	issued, err := svc.challenges.Issue(ctx, IdentifierEmail, ChallengeSignup, "signup@example.com", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.VerifyPasswordReset(ctx, VerifyPasswordResetInput{ChallengeID: issued.Challenge.ID, Code: issued.RawSecret}); !errors.Is(err, errInvalidChallenge) {
		t.Fatalf("err = %v", err)
	}
	got := svc.challenges.store.(*memChallengeStore).must(t, issued.Challenge.ID)
	if got.ConsumedAt != nil {
		t.Fatal("signup challenge must not be consumed by reset verify")
	}
}

func TestCompletePasswordResetChangesPasswordRevokesSessions(t *testing.T) {
	ctx := context.Background()
	svc, sessions, world, hot, user := newTestPasswordResetComplete(t)
	oldPass := []byte("old-secret")
	if err := svc.passwords.Set(ctx, user.ID, oldPass); err != nil {
		t.Fatal(err)
	}
	s1, err := sessions.Create(ctx, user.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	s2, err := sessions.Create(ctx, user.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	rawProof := issueResetProofFor(t, svc, user, "owner@example.com")

	newPass := []byte("new-secret")
	got, err := svc.CompletePasswordReset(ctx, CompletePasswordResetInput{ResetProof: rawProof, NewPassword: newPass})
	if err != nil {
		t.Fatal(err)
	}
	if got.UserID != user.ID || got.SessionEpoch != 1 {
		t.Fatalf("result = %+v", got)
	}
	if _, err := svc.passwords.Verify(ctx, user.ID, oldPass); !errors.Is(err, errUnauthenticated) {
		t.Fatalf("old password err = %v", err)
	}
	if _, err := svc.passwords.Verify(ctx, user.ID, newPass); err != nil {
		t.Fatalf("new password: %v", err)
	}
	if world.mustSession(t, s1.Session.ID).RevokedAt == nil || world.mustSession(t, s2.Session.ID).RevokedAt == nil {
		t.Fatal("all sessions must be revoked")
	}
	if world.users[user.ID].SessionEpoch != 1 {
		t.Fatalf("session_epoch = %d", world.users[user.ID].SessionEpoch)
	}
	if _, err := sessions.Resolve(ctx, s1.RawToken); err != errSessionRevoked {
		t.Fatalf("resolve err = %v", err)
	}
	if hot.kv[sessionEpochKey(user.ID)] != "1" {
		t.Fatalf("cached epoch = %q", hot.kv[sessionEpochKey(user.ID)])
	}
	assertResetProofConsumedOnce(t, world, rawProof)
}

func TestCompletePasswordResetProofCannotBeUsedTwice(t *testing.T) {
	ctx := context.Background()
	svc, _, _, _, user := newTestPasswordResetComplete(t)
	rawProof := issueResetProofFor(t, svc, user, "once@example.com")
	if _, err := svc.CompletePasswordReset(ctx, CompletePasswordResetInput{ResetProof: rawProof, NewPassword: []byte("first")}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CompletePasswordReset(ctx, CompletePasswordResetInput{ResetProof: rawProof, NewPassword: []byte("second")}); !errors.Is(err, errResetProofConsumed) {
		t.Fatalf("replay err = %v", err)
	}
}

func TestCompletePasswordResetValkeyFailureDoesNotUndo(t *testing.T) {
	ctx := context.Background()
	svc, sessions, world, hot, user := newTestPasswordResetComplete(t)
	issued, err := sessions.Create(ctx, user.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	rawProof := issueResetProofFor(t, svc, user, "cache@example.com")
	hot.unavailable = true
	got, err := svc.CompletePasswordReset(ctx, CompletePasswordResetInput{ResetProof: rawProof, NewPassword: []byte("after-cache-fail")})
	if err != nil {
		t.Fatalf("complete must ignore Valkey: %v", err)
	}
	if got.SessionEpoch != 1 || world.users[user.ID].SessionEpoch != 1 {
		t.Fatalf("epoch = %d durable=%d", got.SessionEpoch, world.users[user.ID].SessionEpoch)
	}
	if world.mustSession(t, issued.Session.ID).RevokedAt == nil {
		t.Fatal("session must stay revoked in PostgreSQL")
	}
	hot.unavailable = false
	if _, err := sessions.Resolve(ctx, issued.RawToken); err != errSessionRevoked {
		t.Fatalf("resolve err = %v", err)
	}
}

func TestCompletePasswordResetSecurityEventFailureRollsBack(t *testing.T) {
	ctx := context.Background()
	svc, sessions, world, _, user := newTestPasswordResetComplete(t)
	oldPass := []byte("keep-me-security")
	if err := svc.passwords.Set(ctx, user.ID, oldPass); err != nil {
		t.Fatal(err)
	}
	issued, err := sessions.Create(ctx, user.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	rawProof := issueResetProofFor(t, svc, user, "security-event-fail@example.com")
	intents := svc.intents.(*memIntentEnqueuer)
	intents.failType = AuthSecurityEventType
	if _, err := svc.CompletePasswordReset(ctx, CompletePasswordResetInput{ResetProof: rawProof, NewPassword: []byte("should-not-stick")}); !errors.Is(err, errUnavailable) {
		t.Fatalf("err = %v", err)
	}
	if _, err := svc.passwords.Verify(ctx, user.ID, oldPass); err != nil {
		t.Fatalf("old password must remain valid: %v", err)
	}
	if world.mustSession(t, issued.Session.ID).RevokedAt != nil {
		t.Fatal("session revoke must roll back when required security event fails")
	}
	if world.users[user.ID].SessionEpoch != 0 {
		t.Fatalf("session_epoch = %d, want 0", world.users[user.ID].SessionEpoch)
	}
	var consumed bool
	for _, p := range world.resetProofs {
		if p.ConsumedAt != nil {
			consumed = true
		}
	}
	if consumed {
		t.Fatal("proof consume must roll back")
	}
	for _, ev := range intents.committed {
		if ev.EventType == AuthSecurityEventType {
			t.Fatal("failed security event must not persist")
		}
	}
}

func TestCompletePasswordResetDBFailureRollsBack(t *testing.T) {
	ctx := context.Background()
	svc, sessions, world, _, user := newTestPasswordResetComplete(t)
	oldPass := []byte("keep-me")
	if err := svc.passwords.Set(ctx, user.ID, oldPass); err != nil {
		t.Fatal(err)
	}
	issued, err := sessions.Create(ctx, user.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	rawProof := issueResetProofFor(t, svc, user, "rollback@example.com")
	world.failUpsert = true
	if _, err := svc.CompletePasswordReset(ctx, CompletePasswordResetInput{ResetProof: rawProof, NewPassword: []byte("should-not-stick")}); !errors.Is(err, errUnavailable) {
		t.Fatalf("err = %v", err)
	}
	if _, err := svc.passwords.Verify(ctx, user.ID, oldPass); err != nil {
		t.Fatalf("old password must remain valid: %v", err)
	}
	if world.mustSession(t, issued.Session.ID).RevokedAt != nil {
		t.Fatal("session revoke must roll back")
	}
	if world.users[user.ID].SessionEpoch != 0 {
		t.Fatalf("session_epoch = %d, want 0", world.users[user.ID].SessionEpoch)
	}
	var consumed bool
	for _, p := range world.resetProofs {
		if p.ConsumedAt != nil {
			consumed = true
		}
	}
	if consumed {
		t.Fatal("proof consume must roll back")
	}
}

func TestCompletePasswordResetDoesNotIssueSession(t *testing.T) {
	ctx := context.Background()
	svc, sessions, world, _, user := newTestPasswordResetComplete(t)
	before := len(world.sessions)
	rawProof := issueResetProofFor(t, svc, user, "nosession@example.com")
	if _, err := svc.CompletePasswordReset(ctx, CompletePasswordResetInput{ResetProof: rawProof, NewPassword: []byte("fresh-pass")}); err != nil {
		t.Fatal(err)
	}
	if len(world.sessions) != before {
		t.Fatal("reset complete must not create a login session")
	}
	if sessions == nil {
		t.Fatal("sessions required")
	}
}

func TestResetProofRejectedAsSignupProof(t *testing.T) {
	ctx := context.Background()
	svc, _, _, _, user := newTestPasswordResetComplete(t)
	raw := issueResetProofFor(t, svc, user, "cross@example.com")
	signupProofs, err := NewSignupProofs(newMemSignupProofStore(), SignupProofPolicy{TTL: 15 * time.Minute}, svc.proofs.now)
	if err != nil {
		t.Fatal(err)
	}
	txns := &memAccountTransactor{store: &memAccountStore{proofs: map[ID]SignupProof{}, users: map[ID]User{}, idents: map[ID]UserIdentifier{}, creds: map[ID]PasswordCredential{}}}
	accounts, err := NewAccountCreation(signupProofs, svc.passwords, txns.store, txns, svc.proofs.now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := accounts.CompleteSignup(ctx, CompleteSignupInput{SignupProof: raw}); !errors.Is(err, errInvalidSignupProof) {
		t.Fatalf("err = %v", err)
	}
}

func assertResetIntent(t *testing.T, ev outbox.NewEvent, challengeID ID, channel contracts.Channel, locale, corr string) {
	t.Helper()
	if ev.EventType != contracts.IntentEventType || ev.EventVersion != contracts.IntentEventVersion {
		t.Fatalf("event type/version = %s/%d", ev.EventType, ev.EventVersion)
	}
	if ev.AggregateType != resetAggregateType || ev.AggregateID != challengeID.String() {
		t.Fatalf("aggregate = %s/%s", ev.AggregateType, ev.AggregateID)
	}
	intent, err := contracts.DecodeIntent(ev.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if intent.Purpose != contracts.PurposeSecurity || intent.TemplateCode != contracts.TemplateIdentityPasswordReset {
		t.Fatalf("template/purpose = %s/%s", intent.TemplateCode, intent.Purpose)
	}
	if intent.Channel != channel || intent.Locale != locale {
		t.Fatalf("channel/locale = %s/%s", intent.Channel, intent.Locale)
	}
	if intent.Recipient.Kind != contracts.RecipientVerificationChallenge || intent.Recipient.ID != challengeID.String() {
		t.Fatalf("recipient = %+v", intent.Recipient)
	}
	if corr != "" && intent.CorrelationID != corr {
		t.Fatalf("correlation = %q", intent.CorrelationID)
	}
	body := strings.ToLower(string(ev.Payload))
	if strings.Contains(body, "otp") || strings.Contains(body, `"token"`) || strings.Contains(body, "secret") {
		t.Fatal("outbox payload must not carry secrets")
	}
}

func issueResetProofFor(t *testing.T, svc *PasswordReset, user User, email string) string {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	putVerifiedIdent(t, svc, user.ID, IdentifierEmail, email, now)
	started, err := svc.StartPasswordReset(ctx, StartPasswordResetInput{
		Kind: IdentifierEmail, Destination: email, Locale: contracts.LocaleEN,
	})
	if err != nil {
		t.Fatal(err)
	}
	secret, err := svc.challenges.ResolveDeliverySecret(ctx, started.ChallengeID)
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.VerifyPasswordReset(ctx, VerifyPasswordResetInput{ChallengeID: started.ChallengeID, Code: secret})
	if err != nil {
		t.Fatal(err)
	}
	return got.ResetProof
}

func putVerifiedIdent(t *testing.T, svc *PasswordReset, userID ID, kind IdentifierKind, canonical string, now time.Time) {
	t.Helper()
	idents := svc.identifiers.(*Identifiers).store.(*memIdentifierStore)
	if _, ok := idents.users[userID]; !ok {
		idents.users[userID] = User{ID: userID, CreatedAt: now, UpdatedAt: now}
	}
	verified := now
	ident := UserIdentifier{
		ID: mustID(t), UserID: userID, Kind: kind, ValueCanonical: canonical,
		VerifiedAt: &verified, CreatedAt: now,
	}
	if err := idents.InsertIdentifier(context.Background(), ident); err != nil {
		t.Fatal(err)
	}
}

func newTestPasswordReset(t *testing.T) (*PasswordReset, *memChallengeStore, *memIntentEnqueuer, *memResetTransactor, User) {
	t.Helper()
	svc, store, intents, txns, world, _, _ := newTestPasswordResetFull(t)
	user := world.putUser(t, User{})
	idents := svc.identifiers.(*Identifiers).store.(*memIdentifierStore)
	idents.users[user.ID] = user
	return svc, store, intents, txns, user
}

func newTestPasswordResetComplete(t *testing.T) (*PasswordReset, *Sessions, *memStore, *memHotCache, User) {
	t.Helper()
	svc, _, _, _, world, sessions, hot := newTestPasswordResetFull(t)
	user := world.putUser(t, User{})
	idents := svc.identifiers.(*Identifiers).store.(*memIdentifierStore)
	idents.users[user.ID] = user
	return svc, sessions, world, hot, user
}

func newTestPasswordResetFull(t *testing.T) (*PasswordReset, *memChallengeStore, *memIntentEnqueuer, *memResetTransactor, *memStore, *Sessions, *memHotCache) {
	t.Helper()
	now := func() time.Time { return time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC) }
	chStore := newMemChallengeStore()
	inc := &memIssuanceCounter{}
	ch, err := NewChallenges(chStore, testChallengePolicy(), mustLimiter(t, inc), mustProtector(t), now)
	if err != nil {
		t.Fatal(err)
	}
	intents := &memIntentEnqueuer{keys: make(map[string]struct{})}
	idents := newMemIdentifierStore()
	identifiers, err := NewIdentifiers(idents, now)
	if err != nil {
		t.Fatal(err)
	}
	world := newMemStore()
	passwords, err := NewPasswords(world, testPasswordPolicy(), now)
	if err != nil {
		t.Fatal(err)
	}
	proofs, err := NewResetProofs(world, ResetProofPolicy{TTL: 15 * time.Minute}, now)
	if err != nil {
		t.Fatal(err)
	}
	hot := newMemHotCache()
	sessions, err := NewSessions(world, SessionPolicy{Idle: time.Hour, Absolute: 24 * time.Hour}, hot, SessionCachePolicy{}, now)
	if err != nil {
		t.Fatal(err)
	}
	txns := &memResetTransactor{world: world, challenges: chStore, intents: intents}
	svc, err := NewPasswordReset(ch, identifiers, txns, intents, proofs, passwords, world, sessions)
	if err != nil {
		t.Fatal(err)
	}
	return svc, chStore, intents, txns, world, sessions, hot
}

type memResetTransactor struct {
	world      *memStore
	challenges *memChallengeStore
	intents    *memIntentEnqueuer
	begins     int
	last       *memResetTx
}

func (t *memResetTransactor) Begin(context.Context) (transaction, error) {
	t.begins++
	tx := &memResetTx{
		world:      t.world,
		challenges: t.challenges,
		intents:    t.intents,
		consumed:   make(map[ID]PasswordResetProof),
		creds:      make(map[ID]PasswordCredential),
		sessions:   make(map[ID]Session),
		users:      make(map[ID]User),
	}
	t.last = tx
	return tx, nil
}

type memResetTx struct {
	world        *memStore
	challenges   *memChallengeStore
	intents      *memIntentEnqueuer
	consumed     map[ID]PasswordResetProof
	creds        map[ID]PasswordCredential
	sessions     map[ID]Session
	users        map[ID]User
	stagedCh     []VerificationChallenge
	stagedMat    []VerificationMaterial
	stagedEvents []outbox.NewEvent
	committed    bool
	rolled       bool
}

func (tx *memResetTx) Exec(context.Context, string, ...any) (int64, error) {
	if tx.committed || tx.rolled {
		return 0, errUnavailable
	}
	return 1, nil
}

func (tx *memResetTx) stageChallenge(ch VerificationChallenge, mat VerificationMaterial) {
	tx.stagedCh = append(tx.stagedCh, cloneChallenge(ch))
	tx.stagedMat = append(tx.stagedMat, cloneMaterial(mat))
}

func (tx *memResetTx) stageEvent(in outbox.NewEvent) {
	tx.stagedEvents = append(tx.stagedEvents, in)
}

func (tx *memResetTx) Commit(context.Context) error {
	if tx.committed || tx.rolled {
		return errUnavailable
	}
	if tx.challenges != nil {
		tx.challenges.mu.Lock()
		for i, ch := range tx.stagedCh {
			tx.challenges.rows[ch.ID] = cloneChallenge(ch)
			tx.challenges.materials[ch.ID] = cloneMaterial(tx.stagedMat[i])
		}
		tx.challenges.mu.Unlock()
	}
	if tx.intents != nil {
		tx.intents.apply(tx.stagedEvents)
	}
	if tx.world != nil {
		for id, p := range tx.consumed {
			cloned := p
			cloned.TokenHash = cloneBytes(p.TokenHash)
			tx.world.resetProofs[id] = cloned
		}
		for id, c := range tx.creds {
			tx.world.creds[id] = clonePassword(c)
		}
		for id, s := range tx.sessions {
			tx.world.sessions[id] = s
		}
		for id, u := range tx.users {
			tx.world.users[id] = u
		}
	}
	tx.committed = true
	return nil
}

func (tx *memResetTx) Rollback(context.Context) error {
	if tx.committed {
		return nil
	}
	tx.rolled = true
	tx.consumed = nil
	tx.creds = nil
	tx.sessions = nil
	tx.users = nil
	tx.stagedCh = nil
	tx.stagedMat = nil
	tx.stagedEvents = nil
	return nil
}

func (m *memStore) InsertResetProof(_ context.Context, proof PasswordResetProof) error {
	if m.resetProofs == nil {
		m.resetProofs = make(map[ID]PasswordResetProof)
	}
	for _, existing := range m.resetProofs {
		if existing.ChallengeID == proof.ChallengeID || bytes.Equal(existing.TokenHash, proof.TokenHash) {
			return errInvalidResetProof
		}
	}
	cloned := proof
	cloned.TokenHash = cloneBytes(proof.TokenHash)
	m.resetProofs[proof.ID] = cloned
	return nil
}

func (m *memStore) GetResetProof(_ context.Context, id ID) (PasswordResetProof, error) {
	p, ok := m.resetProofs[id]
	if !ok {
		return PasswordResetProof{}, errNotFound
	}
	cloned := p
	cloned.TokenHash = cloneBytes(p.TokenHash)
	return cloned, nil
}

func (m *memStore) ConsumeResetProofByTokenHash(_ context.Context, tx transaction, tokenHash []byte, now time.Time) (PasswordResetProof, error) {
	atx, ok := tx.(*memResetTx)
	if !ok || atx.world != m {
		return PasswordResetProof{}, errUnavailable
	}
	for id, p := range m.resetProofs {
		if !bytes.Equal(p.TokenHash, tokenHash) {
			continue
		}
		if _, staged := atx.consumed[id]; staged {
			return PasswordResetProof{}, errResetProofConsumed
		}
		if p.Purpose != PasswordResetProofPasswordReset {
			return PasswordResetProof{}, errInvalidResetProof
		}
		if err := p.rejectUnusable(now); err != nil {
			return PasswordResetProof{}, err
		}
		consumed := now
		p.ConsumedAt = &consumed
		cloned := p
		cloned.TokenHash = cloneBytes(p.TokenHash)
		atx.consumed[id] = cloned
		return cloned, nil
	}
	return PasswordResetProof{}, errInvalidResetProof
}

func (m *memStore) GetUserTx(_ context.Context, tx transaction, id ID) (User, error) {
	atx, ok := tx.(*memResetTx)
	if !ok || atx.world != m {
		return User{}, errUnavailable
	}
	if u, ok := atx.users[id]; ok {
		return u, nil
	}
	u, ok := m.users[id]
	if !ok {
		return User{}, errNotFound
	}
	return u, nil
}

func (m *memStore) GetPasswordCredentialTx(_ context.Context, tx transaction, userID ID) (PasswordCredential, error) {
	atx, ok := tx.(*memResetTx)
	if !ok || atx.world != m {
		return PasswordCredential{}, errUnavailable
	}
	if c, ok := atx.creds[userID]; ok {
		return clonePassword(c), nil
	}
	c, ok := m.creds[userID]
	if !ok {
		return PasswordCredential{}, errNotFound
	}
	return clonePassword(c), nil
}

func (m *memStore) UpsertPasswordCredentialTx(_ context.Context, tx transaction, credential PasswordCredential) error {
	atx, ok := tx.(*memResetTx)
	if !ok || atx.world != m {
		return errUnavailable
	}
	if m.failUpsert {
		return errUnavailable
	}
	atx.creds[credential.UserID] = clonePassword(credential)
	return nil
}

func (m *memStore) RevokeSessionsForUserTx(_ context.Context, tx transaction, userID ID, at time.Time) (int64, error) {
	atx, ok := tx.(*memResetTx)
	if !ok || atx.world != m {
		return 0, errUnavailable
	}
	u, ok := m.users[userID]
	if !ok {
		return 0, errNotFound
	}
	for id, s := range m.sessions {
		if s.UserID != userID || s.RevokedAt != nil {
			continue
		}
		revoked := at
		s.RevokedAt = &revoked
		atx.sessions[id] = s
	}
	u.SessionEpoch++
	u.UpdatedAt = at
	atx.users[userID] = u
	return u.SessionEpoch, nil
}

func (m *memStore) GetPasswordCredential(_ context.Context, userID ID) (PasswordCredential, error) {
	c, ok := m.creds[userID]
	if !ok {
		return PasswordCredential{}, errNotFound
	}
	return clonePassword(c), nil
}

func (m *memStore) UpsertPasswordCredential(_ context.Context, credential PasswordCredential) error {
	if m.creds == nil {
		m.creds = make(map[ID]PasswordCredential)
	}
	m.creds[credential.UserID] = clonePassword(credential)
	return nil
}

func (m *memStore) DisablePasswordCredential(_ context.Context, userID ID, at time.Time) error {
	c, ok := m.creds[userID]
	if !ok {
		return errNotFound
	}
	disabled := at
	c.DisabledAt = &disabled
	c.UpdatedAt = at
	m.creds[userID] = c
	return nil
}

func assertResetProofConsumedOnce(t *testing.T, m *memStore, raw string) {
	t.Helper()
	secret, err := decodeSessionToken(raw)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := HashSessionSecret(secret)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, p := range m.resetProofs {
		if bytes.Equal(p.TokenHash, hash) && p.ConsumedAt != nil {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("consumed proofs = %d, want 1", n)
	}
}

var (
	_ resetProofStore    = (*memStore)(nil)
	_ resetCompleteStore = (*memStore)(nil)
	_ passwordStore      = (*memStore)(nil)
	_ sessionStore       = (*memStore)(nil)
)
