package moderation

import (
	"strings"
	"time"
)

const (
	AppealStatusSubmitted   AppealStatus = "submitted"
	AppealStatusUnderReview AppealStatus = "under_review"
	AppealStatusAccepted    AppealStatus = "accepted"
	AppealStatusRejected    AppealStatus = "rejected"
	AppealStatusWithdrawn   AppealStatus = "withdrawn"

	HistoryAppealSubmitted          CaseHistoryKind = "appeal_submitted"
	HistoryAppealReviewStarted      CaseHistoryKind = "appeal_review_started"
	HistoryAppealAccepted           CaseHistoryKind = "appeal_accepted"
	HistoryAppealRejected           CaseHistoryKind = "appeal_rejected"
	HistoryAppealWithdrawn          CaseHistoryKind = "appeal_withdrawn"
	HistoryAppealRestorationApplied CaseHistoryKind = "appeal_restoration_applied"
)

type AppealStatus string

func ParseAppealStatus(raw string) (AppealStatus, error) {
	switch AppealStatus(strings.TrimSpace(raw)) {
	case AppealStatusSubmitted, AppealStatusUnderReview, AppealStatusAccepted, AppealStatusRejected, AppealStatusWithdrawn:
		return AppealStatus(strings.TrimSpace(raw)), nil
	default:
		return "", errInvalidAppealStatus
	}
}

func (s AppealStatus) IsActive() bool {
	return s == AppealStatusSubmitted || s == AppealStatusUnderReview
}

func (s AppealStatus) IsTerminal() bool {
	return s == AppealStatusAccepted || s == AppealStatusRejected || s == AppealStatusWithdrawn
}

func CanTransitionAppeal(from, to AppealStatus) bool {
	switch from {
	case AppealStatusSubmitted:
		return to == AppealStatusUnderReview || to == AppealStatusWithdrawn
	case AppealStatusUnderReview:
		return to == AppealStatusAccepted || to == AppealStatusRejected
	default:
		return false
	}
}

func CanStaffTransitionAppeal(from, to AppealStatus) bool {
	return (from == AppealStatusSubmitted && to == AppealStatusUnderReview) ||
		(from == AppealStatusUnderReview && (to == AppealStatusAccepted || to == AppealStatusRejected))
}

func appealHistoryKind(to AppealStatus) (CaseHistoryKind, error) {
	switch to {
	case AppealStatusUnderReview:
		return HistoryAppealReviewStarted, nil
	case AppealStatusAccepted:
		return HistoryAppealAccepted, nil
	case AppealStatusRejected:
		return HistoryAppealRejected, nil
	case AppealStatusWithdrawn:
		return HistoryAppealWithdrawn, nil
	default:
		return "", errInvalidAppealStatus
	}
}

// Appeal is a Moderation-owned challenge of a recorded case action.
// Accepting a listing restrict/remove appeal clears Listings moderation_state
// through contract before the appeal is marked accepted. Owner listing status
// is unchanged. Accepting a public_profile restrict/remove appeal clears only
// Identity public-profile moderation_state. no_action/warning accepts restore nothing.
type Appeal struct {
	ID               ID
	ActionID         ID
	CaseID           ID
	AppellantUserID  ID
	Statement        string
	Status           AppealStatus
	CreatedAt        time.Time
	UpdatedAt        time.Time
	DecidedAt        *time.Time
	DecidedByStaffID *ID
}

func (a Appeal) Validate() error {
	if a.ID.IsZero() || a.ActionID.IsZero() || a.CaseID.IsZero() || a.AppellantUserID.IsZero() {
		return errZeroID
	}
	if _, err := ParseAppealStatus(string(a.Status)); err != nil {
		return err
	}
	if _, err := NormalizeAppealStatement(a.Statement); err != nil {
		return err
	}
	if a.DecidedByStaffID != nil && a.DecidedByStaffID.IsZero() {
		return errZeroID
	}
	if a.CreatedAt.IsZero() || a.UpdatedAt.IsZero() {
		return errInvalidAppeal
	}
	if a.Status.IsTerminal() && a.DecidedAt == nil {
		return errInvalidAppeal
	}
	if !a.Status.IsTerminal() && a.DecidedAt != nil {
		return errInvalidAppeal
	}
	if a.Status != AppealStatusAccepted && a.Status != AppealStatusRejected && a.DecidedByStaffID != nil {
		return errInvalidAppeal
	}
	return nil
}

type CreateAppealInput struct {
	ActionID  ID
	Statement string
}

type AppealTransitionInput struct {
	To      AppealStatus
	ActorID *ID
}

func NormalizeAppealStatement(raw string) (string, error) {
	body := strings.TrimSpace(raw)
	if body == "" {
		return "", errInvalidBody
	}
	if len(body) > MaxAppealStatementBytes {
		return "", errInvalidBody
	}
	return body, nil
}

func cloneAppeal(row Appeal) Appeal {
	out := row
	out.DecidedByStaffID = cloneOptionalID(row.DecidedByStaffID)
	if row.DecidedAt != nil {
		v := *row.DecidedAt
		out.DecidedAt = &v
	}
	return out
}
