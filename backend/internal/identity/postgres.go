package identity

import (
	"context"
	"errors"
	"time"

	"backend/internal/platform/db"
)

var (
	_ sessionStore           = (*PostgresStore)(nil)
	_ passkeyStore           = (*PostgresStore)(nil)
	_ ceremonyStore          = (*PostgresStore)(nil)
	_ passwordStore          = (*PostgresStore)(nil)
	_ identifierStore        = (*PostgresStore)(nil)
	_ challengeStore         = (*PostgresStore)(nil)
	_ signupProofStore       = (*PostgresStore)(nil)
	_ resetProofStore        = (*PostgresStore)(nil)
	_ resetCompleteStore     = (*PostgresStore)(nil)
	_ accountStore           = (*PostgresStore)(nil)
	_ activeIdentifierLookup = (*PostgresStore)(nil)
)

const identifierSelectCols = `id, user_id, kind, value_canonical, verified_at, created_at, revoked_at`

const challengeSelectCols = `id, kind, purpose, destination_canonical, token_hash, created_at, expires_at, consumed_at, failed_attempts, max_attempts`

const materialSelectCols = `challenge_id, key_id, nonce, ciphertext, created_at, destroyed_at`

const insertChallengeSQL = `
		INSERT INTO identity.verification_challenges (
			id, kind, purpose, destination_canonical, token_hash,
			created_at, expires_at, consumed_at, failed_attempts, max_attempts
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`

const insertMaterialSQL = `
		INSERT INTO identity.verification_material (
			challenge_id, key_id, nonce, ciphertext, created_at, destroyed_at
		) VALUES ($1, $2, $3, $4, $5, $6)`

const destroyMaterialSQL = `
		UPDATE identity.verification_material
		SET nonce = ''::bytea, ciphertext = ''::bytea, destroyed_at = $2
		WHERE challenge_id = $1 AND destroyed_at IS NULL`

const ceremonySelectCols = `id, kind, user_id, token_hash, session_data, created_at, expires_at, consumed_at`

const passkeySelectCols = `id, user_id, credential_id, public_key, sign_count, backup_eligible, backup_state, transports, created_at, last_used_at, revoked_at`

const passwordSelectCols = `user_id, password_hash, created_at, updated_at, disabled_at`

// PostgresStore persists Identity users, devices, sessions, and passkeys via the platform pool.
type PostgresStore struct {
	db *db.Pool
}

func NewPostgresStore(pool *db.Pool) *PostgresStore {
	return &PostgresStore{db: pool}
}

func (p *PostgresStore) GetUser(ctx context.Context, id ID) (User, error) {
	if p.db == nil {
		return User{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		SELECT id, created_at, updated_at, disabled_at, deleted_at, session_epoch
		FROM identity.users
		WHERE id = $1`, id)
	var u User
	if err := row.Scan(&u.ID, &u.CreatedAt, &u.UpdatedAt, &u.DisabledAt, &u.DeletedAt, &u.SessionEpoch); err != nil {
		return User{}, mapDBErr(err)
	}
	return u, nil
}

func (p *PostgresStore) GetDevice(ctx context.Context, id ID) (Device, error) {
	if p.db == nil {
		return Device{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		SELECT id, user_id, created_at, last_seen_at, revoked_at
		FROM identity.devices
		WHERE id = $1`, id)
	var d Device
	if err := row.Scan(&d.ID, &d.UserID, &d.CreatedAt, &d.LastSeenAt, &d.RevokedAt); err != nil {
		return Device{}, mapDBErr(err)
	}
	return d, nil
}

func (p *PostgresStore) InsertSession(ctx context.Context, session Session) error {
	if p.db == nil {
		return errUnavailable
	}
	_, err := p.db.Exec(ctx, `
		INSERT INTO identity.sessions (
			id, user_id, device_id, token_hash,
			created_at, last_seen_at, idle_expires_at, absolute_expires_at, revoked_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		session.ID, session.UserID, session.DeviceID, session.TokenHash,
		session.CreatedAt, session.LastSeenAt, session.IdleExpiresAt, session.AbsoluteExpiresAt, session.RevokedAt,
	)
	return mapDBErr(err)
}

func (p *PostgresStore) GetSession(ctx context.Context, id ID) (Session, error) {
	if p.db == nil {
		return Session{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		SELECT id, user_id, device_id, token_hash,
			created_at, last_seen_at, idle_expires_at, absolute_expires_at, revoked_at
		FROM identity.sessions
		WHERE id = $1`, id)
	return scanSession(row)
}

func (p *PostgresStore) GetSessionByTokenHash(ctx context.Context, tokenHash []byte) (Session, error) {
	if p.db == nil {
		return Session{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		SELECT id, user_id, device_id, token_hash,
			created_at, last_seen_at, idle_expires_at, absolute_expires_at, revoked_at
		FROM identity.sessions
		WHERE token_hash = $1`, tokenHash)
	return scanSession(row)
}

func (p *PostgresStore) UpdateSessionActivity(ctx context.Context, id ID, lastSeenAt, idleExpiresAt time.Time) error {
	if p.db == nil {
		return errUnavailable
	}
	n, err := p.db.Exec(ctx, `
		UPDATE identity.sessions
		SET last_seen_at = $2, idle_expires_at = $3
		WHERE id = $1
		  AND revoked_at IS NULL
		  AND idle_expires_at > $2
		  AND absolute_expires_at > $2`, id, lastSeenAt, idleExpiresAt)
	if err != nil {
		return mapDBErr(err)
	}
	if n == 0 {
		session, gerr := p.GetSession(ctx, id)
		if gerr != nil {
			return mapLookupErr(gerr, errUnauthenticated)
		}
		if session.RevokedAt != nil {
			return errSessionRevoked
		}
		return errUnauthenticated
	}
	return nil
}

func (p *PostgresStore) RevokeSession(ctx context.Context, id ID, at time.Time) error {
	if p.db == nil {
		return errUnavailable
	}
	_, err := p.db.Exec(ctx, `
		UPDATE identity.sessions
		SET revoked_at = $2
		WHERE id = $1 AND revoked_at IS NULL`, id, at)
	return mapDBErr(err)
}

func (p *PostgresStore) RevokeSessionsForUser(ctx context.Context, userID ID, at time.Time) (int64, error) {
	if p.db == nil {
		return 0, errUnavailable
	}
	tx, err := p.db.Begin(ctx)
	if err != nil {
		return 0, mapDBErr(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
		UPDATE identity.sessions
		SET revoked_at = $2
		WHERE user_id = $1 AND revoked_at IS NULL`, userID, at); err != nil {
		return 0, mapDBErr(err)
	}
	row := tx.QueryRow(ctx, `
		UPDATE identity.users
		SET session_epoch = session_epoch + 1, updated_at = $2
		WHERE id = $1
		RETURNING session_epoch`, userID, at)
	var epoch int64
	if err := row.Scan(&epoch); err != nil {
		return 0, mapDBErr(err)
	}
	if epoch < 0 {
		return 0, errUnavailable
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, mapDBErr(err)
	}
	return epoch, nil
}

func (p *PostgresStore) RevokeSessionsForUserTx(ctx context.Context, tx transaction, userID ID, at time.Time) (int64, error) {
	if tx == nil {
		return 0, errUnavailable
	}
	if userID.IsZero() {
		return 0, errZeroID
	}
	if _, err := tx.Exec(ctx, `
		UPDATE identity.sessions
		SET revoked_at = $2
		WHERE user_id = $1 AND revoked_at IS NULL`, userID, at); err != nil {
		return 0, mapDBErr(err)
	}
	row := txQueryRow(tx, ctx, `
		UPDATE identity.users
		SET session_epoch = session_epoch + 1, updated_at = $2
		WHERE id = $1
		RETURNING session_epoch`, userID, at)
	var epoch int64
	if err := row.Scan(&epoch); err != nil {
		return 0, mapDBErr(err)
	}
	if epoch < 0 {
		return 0, errUnavailable
	}
	return epoch, nil
}

func (p *PostgresStore) ListSessionsForUser(ctx context.Context, userID ID) ([]Session, error) {
	if p.db == nil {
		return nil, errUnavailable
	}
	if userID.IsZero() {
		return nil, errZeroID
	}
	rows, err := p.db.Query(ctx, `
		SELECT id, user_id, device_id, token_hash,
			created_at, last_seen_at, idle_expires_at, absolute_expires_at, revoked_at
		FROM identity.sessions
		WHERE user_id = $1
		ORDER BY created_at DESC, id DESC`, userID)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer rows.Close()
	out := make([]Session, 0)
	for rows.Next() {
		s, scanErr := scanSession(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, mapDBErr(err)
	}
	return out, nil
}

func (p *PostgresStore) RevokeOtherSessionsForUser(ctx context.Context, userID, keepSessionID ID, at time.Time) ([][]byte, error) {
	if p.db == nil {
		return nil, errUnavailable
	}
	if userID.IsZero() || keepSessionID.IsZero() {
		return nil, errZeroID
	}
	rows, err := p.db.Query(ctx, `
		UPDATE identity.sessions
		SET revoked_at = $3
		WHERE user_id = $1 AND id <> $2 AND revoked_at IS NULL
		RETURNING token_hash`, userID, keepSessionID, at)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer rows.Close()
	out := make([][]byte, 0)
	for rows.Next() {
		var hash []byte
		if scanErr := rows.Scan(&hash); scanErr != nil {
			return nil, mapDBErr(scanErr)
		}
		out = append(out, cloneBytes(hash))
	}
	if err := rows.Err(); err != nil {
		return nil, mapDBErr(err)
	}
	return out, nil
}

func (p *PostgresStore) InsertPasskey(ctx context.Context, credential PasskeyCredential) error {
	if p.db == nil {
		return errUnavailable
	}
	_, err := p.db.Exec(ctx, `
		INSERT INTO identity.passkey_credentials (
			id, user_id, credential_id, public_key, sign_count,
			backup_eligible, backup_state, transports, created_at, last_used_at, revoked_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		credential.ID, credential.UserID, credential.CredentialID, credential.PublicKey, credential.SignCount,
		credential.BackupEligible, credential.BackupState, credential.Transports,
		credential.CreatedAt, credential.LastUsedAt, credential.RevokedAt,
	)
	return mapDBErr(err)
}

func (p *PostgresStore) GetPasskey(ctx context.Context, id ID) (PasskeyCredential, error) {
	if p.db == nil {
		return PasskeyCredential{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		SELECT `+passkeySelectCols+`
		FROM identity.passkey_credentials
		WHERE id = $1`, id)
	return scanPasskey(row)
}

func (p *PostgresStore) GetPasskeyByCredentialID(ctx context.Context, credentialID []byte) (PasskeyCredential, error) {
	if p.db == nil {
		return PasskeyCredential{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		SELECT `+passkeySelectCols+`
		FROM identity.passkey_credentials
		WHERE credential_id = $1`, credentialID)
	return scanPasskey(row)
}

func (p *PostgresStore) GetActivePasskeyByCredentialID(ctx context.Context, credentialID []byte) (PasskeyCredential, error) {
	if p.db == nil {
		return PasskeyCredential{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		SELECT `+passkeySelectCols+`
		FROM identity.passkey_credentials
		WHERE credential_id = $1 AND revoked_at IS NULL`, credentialID)
	return scanPasskey(row)
}

func (p *PostgresStore) ListActivePasskeysForUser(ctx context.Context, userID ID) ([]PasskeyCredential, error) {
	if p.db == nil {
		return nil, errUnavailable
	}
	rows, err := p.db.Query(ctx, `
		SELECT `+passkeySelectCols+`
		FROM identity.passkey_credentials
		WHERE user_id = $1 AND revoked_at IS NULL
		ORDER BY created_at, id`, userID)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer rows.Close()

	out := make([]PasskeyCredential, 0)
	for rows.Next() {
		c, scanErr := scanPasskey(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, mapDBErr(err)
	}
	return out, nil
}

func (p *PostgresStore) UpdatePasskeySuccessfulUse(ctx context.Context, id ID, signCount int64, backupState bool, lastUsedAt time.Time) error {
	if p.db == nil {
		return errUnavailable
	}
	n, err := p.db.Exec(ctx, `
		UPDATE identity.passkey_credentials
		SET sign_count = $2, backup_state = $3, last_used_at = $4
		WHERE id = $1
		  AND revoked_at IS NULL
		  AND sign_count >= 0
		  AND $2 >= 0
		  AND (sign_count = 0 OR $2 >= sign_count)`,
		id, signCount, backupState, lastUsedAt,
	)
	if err != nil {
		return mapDBErr(err)
	}
	if n == 0 {
		return errNotFound
	}
	return nil
}

func (p *PostgresStore) RevokePasskey(ctx context.Context, id ID, at time.Time) error {
	if p.db == nil {
		return errUnavailable
	}
	_, err := p.db.Exec(ctx, `
		UPDATE identity.passkey_credentials
		SET revoked_at = $2
		WHERE id = $1 AND revoked_at IS NULL`, id, at)
	return mapDBErr(err)
}

type scanner interface {
	Scan(dest ...any) error
}

func (p *PostgresStore) GetPasswordCredential(ctx context.Context, userID ID) (PasswordCredential, error) {
	if p.db == nil {
		return PasswordCredential{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		SELECT `+passwordSelectCols+`
		FROM identity.password_credentials
		WHERE user_id = $1`, userID)
	return scanPassword(row)
}

func (p *PostgresStore) UpsertPasswordCredential(ctx context.Context, credential PasswordCredential) error {
	if p.db == nil {
		return errUnavailable
	}
	_, err := p.db.Exec(ctx, `
		INSERT INTO identity.password_credentials (
			user_id, password_hash, created_at, updated_at, disabled_at
		) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (user_id) DO UPDATE SET
			password_hash = EXCLUDED.password_hash,
			updated_at = EXCLUDED.updated_at,
			disabled_at = EXCLUDED.disabled_at`,
		credential.UserID, credential.PasswordHash, credential.CreatedAt, credential.UpdatedAt, credential.DisabledAt,
	)
	return mapDBErr(err)
}

func (p *PostgresStore) GetUserTx(ctx context.Context, tx transaction, id ID) (User, error) {
	if tx == nil {
		return User{}, errUnavailable
	}
	if id.IsZero() {
		return User{}, errZeroID
	}
	row := txQueryRow(tx, ctx, `
		SELECT id, created_at, updated_at, disabled_at, deleted_at, session_epoch
		FROM identity.users
		WHERE id = $1`, id)
	var u User
	if err := row.Scan(&u.ID, &u.CreatedAt, &u.UpdatedAt, &u.DisabledAt, &u.DeletedAt, &u.SessionEpoch); err != nil {
		return User{}, mapDBErr(err)
	}
	return u, nil
}

func (p *PostgresStore) GetPasswordCredentialTx(ctx context.Context, tx transaction, userID ID) (PasswordCredential, error) {
	if tx == nil {
		return PasswordCredential{}, errUnavailable
	}
	row := txQueryRow(tx, ctx, `
		SELECT `+passwordSelectCols+`
		FROM identity.password_credentials
		WHERE user_id = $1`, userID)
	return scanPassword(row)
}

func (p *PostgresStore) UpsertPasswordCredentialTx(ctx context.Context, tx transaction, credential PasswordCredential) error {
	if tx == nil {
		return errUnavailable
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO identity.password_credentials (
			user_id, password_hash, created_at, updated_at, disabled_at
		) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (user_id) DO UPDATE SET
			password_hash = EXCLUDED.password_hash,
			updated_at = EXCLUDED.updated_at,
			disabled_at = EXCLUDED.disabled_at`,
		credential.UserID, credential.PasswordHash, credential.CreatedAt, credential.UpdatedAt, credential.DisabledAt,
	)
	return mapDBErr(err)
}

func (p *PostgresStore) DisablePasswordCredential(ctx context.Context, userID ID, at time.Time) error {
	if p.db == nil {
		return errUnavailable
	}
	n, err := p.db.Exec(ctx, `
		UPDATE identity.password_credentials
		SET disabled_at = $2, updated_at = $2
		WHERE user_id = $1`, userID, at)
	if err != nil {
		return mapDBErr(err)
	}
	if n == 0 {
		return errNotFound
	}
	return nil
}

func (p *PostgresStore) InsertIdentifier(ctx context.Context, identifier UserIdentifier) error {
	if p.db == nil {
		return errUnavailable
	}
	_, err := p.db.Exec(ctx, `
		INSERT INTO identity.user_identifiers (
			id, user_id, kind, value_canonical, verified_at, created_at, revoked_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		identifier.ID, identifier.UserID, string(identifier.Kind), identifier.ValueCanonical,
		identifier.VerifiedAt, identifier.CreatedAt, identifier.RevokedAt,
	)
	if errors.Is(err, db.ErrConflict) {
		return errIdentifierConflict
	}
	return mapDBErr(err)
}

func (p *PostgresStore) GetIdentifier(ctx context.Context, id ID) (UserIdentifier, error) {
	if p.db == nil {
		return UserIdentifier{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		SELECT `+identifierSelectCols+`
		FROM identity.user_identifiers
		WHERE id = $1`, id)
	return scanIdentifier(row)
}

func (p *PostgresStore) GetActiveIdentifier(ctx context.Context, kind IdentifierKind, valueCanonical string) (UserIdentifier, error) {
	if p.db == nil {
		return UserIdentifier{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		SELECT `+identifierSelectCols+`
		FROM identity.user_identifiers
		WHERE kind = $1 AND value_canonical = $2 AND revoked_at IS NULL`, kind, valueCanonical)
	return scanIdentifier(row)
}

func (p *PostgresStore) ListActiveIdentifiersForUser(ctx context.Context, userID ID) ([]UserIdentifier, error) {
	if p.db == nil {
		return nil, errUnavailable
	}
	rows, err := p.db.Query(ctx, `
		SELECT `+identifierSelectCols+`
		FROM identity.user_identifiers
		WHERE user_id = $1 AND revoked_at IS NULL
		ORDER BY created_at, id`, userID)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer rows.Close()

	out := make([]UserIdentifier, 0)
	for rows.Next() {
		ident, scanErr := scanIdentifier(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, ident)
	}
	if err := rows.Err(); err != nil {
		return nil, mapDBErr(err)
	}
	return out, nil
}

func (p *PostgresStore) MarkIdentifierVerified(ctx context.Context, id ID, at time.Time) error {
	if p.db == nil {
		return errUnavailable
	}
	n, err := p.db.Exec(ctx, `
		UPDATE identity.user_identifiers
		SET verified_at = COALESCE(verified_at, $2)
		WHERE id = $1 AND revoked_at IS NULL`, id, at)
	if err != nil {
		return mapDBErr(err)
	}
	if n == 0 {
		return errNotFound
	}
	return nil
}

func (p *PostgresStore) RevokeIdentifier(ctx context.Context, id ID, at time.Time) error {
	if p.db == nil {
		return errUnavailable
	}
	n, err := p.db.Exec(ctx, `
		UPDATE identity.user_identifiers
		SET revoked_at = $2
		WHERE id = $1 AND revoked_at IS NULL`, id, at)
	if err != nil {
		return mapDBErr(err)
	}
	if n == 0 {
		return errNotFound
	}
	return nil
}

func (p *PostgresStore) InsertChallenge(ctx context.Context, challenge VerificationChallenge) error {
	if p.db == nil {
		return errUnavailable
	}
	_, err := p.db.Exec(ctx, insertChallengeSQL,
		challenge.ID, string(challenge.Kind), string(challenge.Purpose), challenge.DestinationCanonical, challenge.TokenHash,
		challenge.CreatedAt, challenge.ExpiresAt, challenge.ConsumedAt, challenge.FailedAttempts, challenge.MaxAttempts,
	)
	return mapDBErr(err)
}

func (p *PostgresStore) Begin(ctx context.Context) (transaction, error) {
	if p.db == nil {
		return nil, errUnavailable
	}
	tx, err := p.db.Begin(ctx)
	if err != nil {
		return nil, mapDBErr(err)
	}
	return tx, nil
}

func (p *PostgresStore) InsertChallengeAndMaterial(ctx context.Context, exec txExecer, challenge VerificationChallenge, material VerificationMaterial) error {
	if exec == nil && p.db == nil {
		return errUnavailable
	}
	if err := material.Validate(); err != nil || !material.Active() || material.ChallengeID != challenge.ID {
		return errInvalidChallenge
	}
	if exec != nil {
		return execChallengeAndMaterial(ctx, exec, challenge, material)
	}
	if p.db == nil {
		return errUnavailable
	}
	tx, err := p.db.Begin(ctx)
	if err != nil {
		return mapDBErr(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := execChallengeAndMaterial(ctx, tx, challenge, material); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return mapDBErr(err)
	}
	return nil
}

func execChallengeAndMaterial(ctx context.Context, exec txExecer, challenge VerificationChallenge, material VerificationMaterial) error {
	if exec == nil {
		return errUnavailable
	}
	if _, err := exec.Exec(ctx, insertChallengeSQL,
		challenge.ID, string(challenge.Kind), string(challenge.Purpose), challenge.DestinationCanonical, challenge.TokenHash,
		challenge.CreatedAt, challenge.ExpiresAt, challenge.ConsumedAt, challenge.FailedAttempts, challenge.MaxAttempts,
	); err != nil {
		return mapDBErr(err)
	}
	if _, err := exec.Exec(ctx, insertMaterialSQL,
		material.ChallengeID, material.KeyID, material.Nonce, material.Ciphertext, material.CreatedAt, material.DestroyedAt,
	); err != nil {
		return mapDBErr(err)
	}
	return nil
}

func (p *PostgresStore) GetActiveMaterial(ctx context.Context, challengeID ID) (VerificationMaterial, error) {
	if p.db == nil {
		return VerificationMaterial{}, errUnavailable
	}
	if challengeID.IsZero() {
		return VerificationMaterial{}, errZeroID
	}
	row := p.db.QueryRow(ctx, `
		SELECT `+materialSelectCols+`
		FROM identity.verification_material
		WHERE challenge_id = $1 AND destroyed_at IS NULL`, challengeID)
	return scanMaterial(row)
}

func (p *PostgresStore) DestroyMaterial(ctx context.Context, challengeID ID, at time.Time) error {
	if p.db == nil {
		return errUnavailable
	}
	if challengeID.IsZero() {
		return errZeroID
	}
	n, err := p.db.Exec(ctx, destroyMaterialSQL, challengeID, at)
	if err != nil {
		return mapDBErr(err)
	}
	if n == 0 {
		return errNotFound
	}
	return nil
}

func (p *PostgresStore) GetChallenge(ctx context.Context, id ID) (VerificationChallenge, error) {
	if p.db == nil {
		return VerificationChallenge{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		SELECT `+challengeSelectCols+`
		FROM identity.verification_challenges
		WHERE id = $1`, id)
	return scanChallenge(row)
}

func (p *PostgresStore) ConsumeChallenge(ctx context.Context, id ID, now time.Time) (VerificationChallenge, error) {
	if p.db == nil {
		return VerificationChallenge{}, errUnavailable
	}
	tx, err := p.db.Begin(ctx)
	if err != nil {
		return VerificationChallenge{}, mapDBErr(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	row := tx.QueryRow(ctx, `
		UPDATE identity.verification_challenges
		SET consumed_at = $2
		WHERE id = $1
		  AND consumed_at IS NULL
		  AND expires_at > $2
		  AND failed_attempts < max_attempts
		RETURNING `+challengeSelectCols,
		id, now,
	)
	ch, err := scanChallenge(row)
	if err != nil {
		if !errors.Is(err, errNotFound) {
			return VerificationChallenge{}, err
		}
		return classifyUnconsumedChallenge(p.db.QueryRow(ctx, `
			SELECT `+challengeSelectCols+`
			FROM identity.verification_challenges
			WHERE id = $1`, id), now)
	}
	if _, err := tx.Exec(ctx, destroyMaterialSQL, id, now); err != nil {
		return VerificationChallenge{}, mapDBErr(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return VerificationChallenge{}, mapDBErr(err)
	}
	return ch, nil
}

func (p *PostgresStore) IncrementFailedAttempt(ctx context.Context, id ID, now time.Time) (int, error) {
	if p.db == nil {
		return 0, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		UPDATE identity.verification_challenges
		SET failed_attempts = failed_attempts + 1
		WHERE id = $1
		  AND consumed_at IS NULL
		  AND expires_at > $2
		  AND failed_attempts < max_attempts
		RETURNING failed_attempts`,
		id, now,
	)
	var n int
	if err := row.Scan(&n); err != nil {
		mapped := mapDBErr(err)
		if !errors.Is(mapped, errNotFound) {
			return 0, mapped
		}
		_, classErr := classifyUnconsumedChallenge(p.db.QueryRow(ctx, `
			SELECT `+challengeSelectCols+`
			FROM identity.verification_challenges
			WHERE id = $1`, id), now)
		return 0, classErr
	}
	return n, nil
}

func classifyUnconsumedChallenge(row scanner, now time.Time) (VerificationChallenge, error) {
	ch, err := scanChallenge(row)
	if err != nil {
		if errors.Is(err, errNotFound) {
			return VerificationChallenge{}, errInvalidChallenge
		}
		return VerificationChallenge{}, err
	}
	if reject := ch.rejectUnusable(now); reject != nil {
		return VerificationChallenge{}, reject
	}
	return VerificationChallenge{}, errUnavailable
}

func scanChallenge(row scanner) (VerificationChallenge, error) {
	var c VerificationChallenge
	var kind, purpose string
	if err := row.Scan(
		&c.ID, &kind, &purpose, &c.DestinationCanonical, &c.TokenHash,
		&c.CreatedAt, &c.ExpiresAt, &c.ConsumedAt, &c.FailedAttempts, &c.MaxAttempts,
	); err != nil {
		return VerificationChallenge{}, mapDBErr(err)
	}
	c.Kind = IdentifierKind(kind)
	c.Purpose = ChallengePurpose(purpose)
	c.TokenHash = cloneBytes(c.TokenHash)
	return c, nil
}

func scanMaterial(row scanner) (VerificationMaterial, error) {
	var m VerificationMaterial
	if err := row.Scan(
		&m.ChallengeID, &m.KeyID, &m.Nonce, &m.Ciphertext, &m.CreatedAt, &m.DestroyedAt,
	); err != nil {
		return VerificationMaterial{}, mapDBErr(err)
	}
	m.Nonce = cloneBytes(m.Nonce)
	m.Ciphertext = cloneBytes(m.Ciphertext)
	return m, nil
}

func (p *PostgresStore) InsertCeremony(ctx context.Context, state CeremonyState) error {
	if p.db == nil {
		return errUnavailable
	}
	_, err := p.db.Exec(ctx, `
		INSERT INTO identity.webauthn_ceremonies (
			id, kind, user_id, token_hash, session_data, created_at, expires_at, consumed_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		state.ID, string(state.Kind), state.UserID, state.TokenHash, state.SessionData,
		state.CreatedAt, state.ExpiresAt, state.ConsumedAt,
	)
	return mapDBErr(err)
}

func (p *PostgresStore) ConsumeCeremonyByTokenHash(ctx context.Context, tokenHash []byte, now time.Time) (CeremonyState, error) {
	if p.db == nil {
		return CeremonyState{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		UPDATE identity.webauthn_ceremonies
		SET consumed_at = $2
		WHERE token_hash = $1
		  AND consumed_at IS NULL
		  AND expires_at > $2
		RETURNING `+ceremonySelectCols,
		tokenHash, now,
	)
	state, err := scanCeremony(row)
	if err == nil {
		return state, nil
	}
	if !errors.Is(err, errNotFound) {
		return CeremonyState{}, err
	}
	return classifyUnconsumedCeremony(p.db.QueryRow(ctx, `
		SELECT `+ceremonySelectCols+`
		FROM identity.webauthn_ceremonies
		WHERE token_hash = $1`, tokenHash), now)
}

func classifyUnconsumedCeremony(row scanner, now time.Time) (CeremonyState, error) {
	state, err := scanCeremony(row)
	if err != nil {
		if errors.Is(err, errNotFound) {
			return CeremonyState{}, errInvalidCeremony
		}
		return CeremonyState{}, err
	}
	if state.ConsumedAt != nil {
		return CeremonyState{}, errCeremonyConsumed
	}
	if !now.Before(state.ExpiresAt) {
		return CeremonyState{}, errCeremonyExpired
	}
	return CeremonyState{}, errUnavailable
}

func scanCeremony(row scanner) (CeremonyState, error) {
	var s CeremonyState
	var kind string
	if err := row.Scan(
		&s.ID, &kind, &s.UserID, &s.TokenHash, &s.SessionData,
		&s.CreatedAt, &s.ExpiresAt, &s.ConsumedAt,
	); err != nil {
		return CeremonyState{}, mapDBErr(err)
	}
	s.Kind = CeremonyKind(kind)
	s.TokenHash = cloneBytes(s.TokenHash)
	s.SessionData = cloneBytes(s.SessionData)
	return s, nil
}

func scanIdentifier(row scanner) (UserIdentifier, error) {
	var i UserIdentifier
	var kind string
	if err := row.Scan(
		&i.ID, &i.UserID, &kind, &i.ValueCanonical, &i.VerifiedAt, &i.CreatedAt, &i.RevokedAt,
	); err != nil {
		return UserIdentifier{}, mapDBErr(err)
	}
	i.Kind = IdentifierKind(kind)
	return i, nil
}

func scanPassword(row scanner) (PasswordCredential, error) {
	var c PasswordCredential
	if err := row.Scan(&c.UserID, &c.PasswordHash, &c.CreatedAt, &c.UpdatedAt, &c.DisabledAt); err != nil {
		return PasswordCredential{}, mapDBErr(err)
	}
	return c, nil
}

func scanPasskey(row scanner) (PasskeyCredential, error) {
	var c PasskeyCredential
	if err := row.Scan(
		&c.ID, &c.UserID, &c.CredentialID, &c.PublicKey, &c.SignCount,
		&c.BackupEligible, &c.BackupState, &c.Transports, &c.CreatedAt, &c.LastUsedAt, &c.RevokedAt,
	); err != nil {
		return PasskeyCredential{}, mapDBErr(err)
	}
	c.CredentialID = cloneBytes(c.CredentialID)
	c.PublicKey = cloneBytes(c.PublicKey)
	c.Transports = cloneStrings(c.Transports)
	return c, nil
}

func cloneBytes(in []byte) []byte {
	if in == nil {
		return nil
	}
	out := make([]byte, len(in))
	copy(out, in)
	return out
}

func cloneStrings(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func scanSession(row scanner) (Session, error) {
	var s Session
	if err := row.Scan(
		&s.ID, &s.UserID, &s.DeviceID, &s.TokenHash,
		&s.CreatedAt, &s.LastSeenAt, &s.IdleExpiresAt, &s.AbsoluteExpiresAt, &s.RevokedAt,
	); err != nil {
		return Session{}, mapDBErr(err)
	}
	return s, nil
}

const signupProofSelectCols = `id, challenge_id, kind, destination_canonical, purpose, token_hash, created_at, expires_at, consumed_at`

func (p *PostgresStore) InsertSignupProof(ctx context.Context, proof SignupProof) error {
	if p.db == nil {
		return errUnavailable
	}
	if err := proof.Validate(); err != nil {
		return err
	}
	_, err := p.db.Exec(ctx, `
		INSERT INTO identity.signup_proofs (
			id, challenge_id, kind, destination_canonical, purpose, token_hash,
			created_at, expires_at, consumed_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		proof.ID, proof.ChallengeID, string(proof.Kind), proof.DestinationCanonical, string(proof.Purpose),
		proof.TokenHash, proof.CreatedAt, proof.ExpiresAt, proof.ConsumedAt,
	)
	if errors.Is(err, db.ErrConflict) {
		return errInvalidSignupProof
	}
	return mapDBErr(err)
}

func (p *PostgresStore) GetSignupProof(ctx context.Context, id ID) (SignupProof, error) {
	if p.db == nil {
		return SignupProof{}, errUnavailable
	}
	if id.IsZero() {
		return SignupProof{}, errZeroID
	}
	row := p.db.QueryRow(ctx, `
		SELECT `+signupProofSelectCols+`
		FROM identity.signup_proofs
		WHERE id = $1`, id)
	return scanSignupProof(row)
}

func (p *PostgresStore) ConsumeSignupProofByTokenHash(ctx context.Context, tx transaction, tokenHash []byte, now time.Time) (SignupProof, error) {
	if tx == nil {
		return SignupProof{}, errUnavailable
	}
	if len(tokenHash) != TokenHashSize {
		return SignupProof{}, errInvalidSignupProof
	}
	row := txQueryRow(tx, ctx, `
		UPDATE identity.signup_proofs
		SET consumed_at = $2
		WHERE token_hash = $1
		  AND consumed_at IS NULL
		  AND expires_at > $2
		  AND purpose = $3
		RETURNING `+signupProofSelectCols,
		tokenHash, now, string(SignupProofSignup),
	)
	proof, err := scanSignupProof(row)
	if err == nil {
		return proof, nil
	}
	if !errors.Is(err, errNotFound) {
		return SignupProof{}, err
	}
	return classifyUnconsumedSignupProof(txQueryRow(tx, ctx, `
		SELECT `+signupProofSelectCols+`
		FROM identity.signup_proofs
		WHERE token_hash = $1`, tokenHash), now)
}

func classifyUnconsumedSignupProof(row scanner, now time.Time) (SignupProof, error) {
	proof, err := scanSignupProof(row)
	if err != nil {
		if errors.Is(err, errNotFound) {
			return SignupProof{}, errInvalidSignupProof
		}
		return SignupProof{}, err
	}
	if proof.Purpose != SignupProofSignup {
		return SignupProof{}, errInvalidSignupProof
	}
	if reject := proof.rejectUnusable(now); reject != nil {
		return SignupProof{}, reject
	}
	return SignupProof{}, errUnavailable
}

func (p *PostgresStore) GetActiveIdentifierTx(ctx context.Context, tx transaction, kind IdentifierKind, valueCanonical string) (UserIdentifier, error) {
	if tx == nil {
		return UserIdentifier{}, errUnavailable
	}
	row := txQueryRow(tx, ctx, `
		SELECT `+identifierSelectCols+`
		FROM identity.user_identifiers
		WHERE kind = $1 AND value_canonical = $2 AND revoked_at IS NULL`, kind, valueCanonical)
	return scanIdentifier(row)
}

func (p *PostgresStore) InsertUserTx(ctx context.Context, tx transaction, user User) error {
	if tx == nil {
		return errUnavailable
	}
	if !user.EligibleForSession() || user.CreatedAt.IsZero() || user.UpdatedAt.IsZero() {
		return errUnavailable
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO identity.users (id, created_at, updated_at, disabled_at, deleted_at)
		VALUES ($1, $2, $3, $4, $5)`,
		user.ID, user.CreatedAt, user.UpdatedAt, user.DisabledAt, user.DeletedAt,
	)
	return mapDBErr(err)
}

func (p *PostgresStore) InsertIdentifierTx(ctx context.Context, tx transaction, identifier UserIdentifier) error {
	if tx == nil {
		return errUnavailable
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO identity.user_identifiers (
			id, user_id, kind, value_canonical, verified_at, created_at, revoked_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		identifier.ID, identifier.UserID, string(identifier.Kind), identifier.ValueCanonical,
		identifier.VerifiedAt, identifier.CreatedAt, identifier.RevokedAt,
	)
	if errors.Is(err, db.ErrConflict) {
		return errIdentifierConflict
	}
	return mapDBErr(err)
}

func (p *PostgresStore) InsertPasswordCredentialTx(ctx context.Context, tx transaction, credential PasswordCredential) error {
	if tx == nil {
		return errUnavailable
	}
	if !credential.Active() {
		return errInvalidPassword
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO identity.password_credentials (
			user_id, password_hash, created_at, updated_at, disabled_at
		) VALUES ($1, $2, $3, $4, $5)`,
		credential.UserID, credential.PasswordHash, credential.CreatedAt, credential.UpdatedAt, credential.DisabledAt,
	)
	return mapDBErr(err)
}

type txRowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) db.Row
}

func txQueryRow(tx transaction, ctx context.Context, sql string, args ...any) scanner {
	q, ok := tx.(txRowQuerier)
	if !ok {
		return errScanRow{err: errUnavailable}
	}
	return q.QueryRow(ctx, sql, args...)
}

type errScanRow struct {
	err error
}

func (r errScanRow) Scan(dest ...any) error {
	return r.err
}

func scanSignupProof(row scanner) (SignupProof, error) {
	var p SignupProof
	var kind, purpose string
	if err := row.Scan(
		&p.ID, &p.ChallengeID, &kind, &p.DestinationCanonical, &purpose, &p.TokenHash,
		&p.CreatedAt, &p.ExpiresAt, &p.ConsumedAt,
	); err != nil {
		return SignupProof{}, mapDBErr(err)
	}
	p.Kind = IdentifierKind(kind)
	p.Purpose = SignupProofPurpose(purpose)
	p.TokenHash = cloneBytes(p.TokenHash)
	return p, nil
}

const resetProofSelectCols = `id, user_id, challenge_id, purpose, token_hash, created_at, expires_at, consumed_at`

func (p *PostgresStore) InsertResetProof(ctx context.Context, proof PasswordResetProof) error {
	if p.db == nil {
		return errUnavailable
	}
	if err := proof.Validate(); err != nil {
		return err
	}
	_, err := p.db.Exec(ctx, `
		INSERT INTO identity.password_reset_proofs (
			id, user_id, challenge_id, purpose, token_hash,
			created_at, expires_at, consumed_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		proof.ID, proof.UserID, proof.ChallengeID, string(proof.Purpose),
		proof.TokenHash, proof.CreatedAt, proof.ExpiresAt, proof.ConsumedAt,
	)
	if errors.Is(err, db.ErrConflict) {
		return errInvalidResetProof
	}
	return mapDBErr(err)
}

func (p *PostgresStore) GetResetProof(ctx context.Context, id ID) (PasswordResetProof, error) {
	if p.db == nil {
		return PasswordResetProof{}, errUnavailable
	}
	if id.IsZero() {
		return PasswordResetProof{}, errZeroID
	}
	row := p.db.QueryRow(ctx, `
		SELECT `+resetProofSelectCols+`
		FROM identity.password_reset_proofs
		WHERE id = $1`, id)
	return scanResetProof(row)
}

func (p *PostgresStore) ConsumeResetProofByTokenHash(ctx context.Context, tx transaction, tokenHash []byte, now time.Time) (PasswordResetProof, error) {
	if tx == nil {
		return PasswordResetProof{}, errUnavailable
	}
	if len(tokenHash) != TokenHashSize {
		return PasswordResetProof{}, errInvalidResetProof
	}
	row := txQueryRow(tx, ctx, `
		UPDATE identity.password_reset_proofs
		SET consumed_at = $2
		WHERE token_hash = $1
		  AND consumed_at IS NULL
		  AND expires_at > $2
		  AND purpose = $3
		RETURNING `+resetProofSelectCols,
		tokenHash, now, string(PasswordResetProofPasswordReset),
	)
	proof, err := scanResetProof(row)
	if err == nil {
		return proof, nil
	}
	if !errors.Is(err, errNotFound) {
		return PasswordResetProof{}, err
	}
	return classifyUnconsumedResetProof(txQueryRow(tx, ctx, `
		SELECT `+resetProofSelectCols+`
		FROM identity.password_reset_proofs
		WHERE token_hash = $1`, tokenHash), now)
}

func classifyUnconsumedResetProof(row scanner, now time.Time) (PasswordResetProof, error) {
	proof, err := scanResetProof(row)
	if err != nil {
		if errors.Is(err, errNotFound) {
			return PasswordResetProof{}, errInvalidResetProof
		}
		return PasswordResetProof{}, err
	}
	if proof.Purpose != PasswordResetProofPasswordReset {
		return PasswordResetProof{}, errInvalidResetProof
	}
	if reject := proof.rejectUnusable(now); reject != nil {
		return PasswordResetProof{}, reject
	}
	return PasswordResetProof{}, errUnavailable
}

func scanResetProof(row scanner) (PasswordResetProof, error) {
	var p PasswordResetProof
	var purpose string
	if err := row.Scan(
		&p.ID, &p.UserID, &p.ChallengeID, &purpose, &p.TokenHash,
		&p.CreatedAt, &p.ExpiresAt, &p.ConsumedAt,
	); err != nil {
		return PasswordResetProof{}, mapDBErr(err)
	}
	p.Purpose = PasswordResetProofPurpose(purpose)
	p.TokenHash = cloneBytes(p.TokenHash)
	return p, nil
}

func mapDBErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, db.ErrNoRows) {
		return errNotFound
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return errUnavailable
}
