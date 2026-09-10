package moderation

import (
	"strings"
	"time"
	"unicode"
)

const (
	EvidenceStaffNote         EvidenceType = "staff_note"
	EvidenceExternalReference EvidenceType = "external_reference"
	EvidenceInternalReference EvidenceType = "internal_reference"
	EvidenceSnapshotReference EvidenceType = "snapshot_reference"
)

type EvidenceType string

func ParseEvidenceType(raw string) (EvidenceType, error) {
	switch EvidenceType(strings.TrimSpace(raw)) {
	case EvidenceStaffNote, EvidenceExternalReference, EvidenceInternalReference, EvidenceSnapshotReference:
		return EvidenceType(strings.TrimSpace(raw)), nil
	default:
		return "", errInvalidEvidence
	}
}

// CaseEvidence is internal investigation material owned by Moderation.
// It is append-only: normal application paths do not update or delete rows.
// Closed cases still accept evidence, matching AddCaseNote / case-history policy.
type CaseEvidence struct {
	ID             ID
	CaseID         ID
	EvidenceType   EvidenceType
	Title          string
	Description    *string
	ReferenceValue *string
	ActorStaffID   *ID
	CreatedAt      time.Time
}

func (e CaseEvidence) Validate() error {
	if e.ID.IsZero() || e.CaseID.IsZero() {
		return errZeroID
	}
	if _, err := ParseEvidenceType(string(e.EvidenceType)); err != nil {
		return err
	}
	if _, err := NormalizeEvidenceTitle(e.Title); err != nil {
		return err
	}
	if e.Description != nil {
		if _, err := NormalizeEvidenceDescription(*e.Description); err != nil {
			return err
		}
	}
	if e.ReferenceValue != nil {
		if _, err := NormalizeEvidenceReference(*e.ReferenceValue); err != nil {
			return err
		}
	}
	if err := validateEvidenceFields(e.EvidenceType, e.ReferenceValue); err != nil {
		return err
	}
	if e.ActorStaffID != nil && e.ActorStaffID.IsZero() {
		return errZeroID
	}
	if e.CreatedAt.IsZero() {
		return errInvalidEvidence
	}
	return nil
}

type AddEvidenceInput struct {
	EvidenceType   EvidenceType
	Title          string
	Description    string
	ReferenceValue string
	ActorID        *ID
}

func NormalizeEvidenceTitle(raw string) (string, error) {
	title := strings.TrimSpace(raw)
	if title == "" {
		return "", errInvalidEvidence
	}
	if len(title) > MaxEvidenceTitleBytes {
		return "", errInvalidBody
	}
	if err := rejectSensitiveMaterial(title); err != nil {
		return "", err
	}
	return title, nil
}

func NormalizeEvidenceDescription(raw string) (*string, error) {
	body := strings.TrimSpace(raw)
	if body == "" {
		return nil, nil
	}
	if len(body) > MaxEvidenceDescriptionBytes {
		return nil, errInvalidBody
	}
	if err := rejectSensitiveMaterial(body); err != nil {
		return nil, err
	}
	return &body, nil
}

func NormalizeEvidenceReference(raw string) (*string, error) {
	body := strings.TrimSpace(raw)
	if body == "" {
		return nil, nil
	}
	if len(body) > MaxEvidenceReferenceBytes {
		return nil, errInvalidBody
	}
	if err := rejectSensitiveMaterial(body); err != nil {
		return nil, err
	}
	return &body, nil
}

func validateEvidenceFields(kind EvidenceType, reference *string) error {
	switch kind {
	case EvidenceStaffNote:
		if reference != nil {
			return errInvalidEvidence
		}
	case EvidenceExternalReference, EvidenceInternalReference, EvidenceSnapshotReference:
		if reference == nil {
			return errInvalidEvidence
		}
	default:
		return errInvalidEvidence
	}
	return nil
}

func rejectSensitiveMaterial(raw string) error {
	if strings.ContainsRune(raw, 0) {
		return errInvalidEvidence
	}
	for _, r := range raw {
		if r < 32 && r != '\n' && r != '\r' && r != '\t' {
			return errInvalidEvidence
		}
		if unicode.Is(unicode.Cc, r) && r != '\n' && r != '\r' && r != '\t' {
			return errInvalidEvidence
		}
	}
	lower := strings.ToLower(raw)
	if strings.Contains(lower, "authorization:") ||
		strings.Contains(lower, "cookie:") ||
		strings.Contains(lower, "set-cookie") ||
		strings.Contains(raw, "__Host-konumlu_session") ||
		strings.Contains(lower, "-----begin ") {
		return errInvalidEvidence
	}
	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(trimmed, "eyJ") {
		return errInvalidEvidence
	}
	return nil
}

func cloneEvidence(row CaseEvidence) CaseEvidence {
	out := row
	out.ActorStaffID = cloneOptionalID(row.ActorStaffID)
	if row.Description != nil {
		v := *row.Description
		out.Description = &v
	}
	if row.ReferenceValue != nil {
		v := *row.ReferenceValue
		out.ReferenceValue = &v
	}
	return out
}
