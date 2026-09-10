package moderation

import (
	"strings"
	"time"
)

const (
	ActionTypeNoAction ActionType = "no_action"
	ActionTypeWarning  ActionType = "warning"
	ActionTypeRestrict ActionType = "restrict"
	ActionTypeSuspend  ActionType = "suspend"
	ActionTypeRemove   ActionType = "remove"

	ActionStatusProposed  ActionStatus = "proposed"
	ActionStatusApproved  ActionStatus = "approved"
	ActionStatusExecuted  ActionStatus = "executed"
	ActionStatusCancelled ActionStatus = "cancelled"

	ActionReasonNoViolation        ActionReasonCode = "no_violation"
	ActionReasonPolicyViolation    ActionReasonCode = "policy_violation"
	ActionReasonRepeatedViolation  ActionReasonCode = "repeated_violation"
	ActionReasonSafetyRisk         ActionReasonCode = "safety_risk"
	ActionReasonProhibitedContent  ActionReasonCode = "prohibited_content"
	ActionReasonOther              ActionReasonCode = "other"

	HistoryActionProposed  CaseHistoryKind = "action_proposed"
	HistoryActionApproved  CaseHistoryKind = "action_approved"
	HistoryActionExecuted  CaseHistoryKind = "action_executed"
	HistoryActionCancelled CaseHistoryKind = "action_cancelled"
)

type ActionType string

func ParseActionType(raw string) (ActionType, error) {
	switch ActionType(strings.TrimSpace(raw)) {
	case ActionTypeNoAction, ActionTypeWarning, ActionTypeRestrict, ActionTypeSuspend, ActionTypeRemove:
		return ActionType(strings.TrimSpace(raw)), nil
	default:
		return "", errInvalidAction
	}
}

type ActionStatus string

func ParseActionStatus(raw string) (ActionStatus, error) {
	switch ActionStatus(strings.TrimSpace(raw)) {
	case ActionStatusProposed, ActionStatusApproved, ActionStatusExecuted, ActionStatusCancelled:
		return ActionStatus(strings.TrimSpace(raw)), nil
	default:
		return "", errInvalidActionStatus
	}
}

func CanTransitionAction(from, to ActionStatus) bool {
	switch from {
	case ActionStatusProposed:
		return to == ActionStatusApproved || to == ActionStatusCancelled
	case ActionStatusApproved:
		return to == ActionStatusExecuted || to == ActionStatusCancelled
	default:
		return false
	}
}

func actionHistoryKind(to ActionStatus) (CaseHistoryKind, error) {
	switch to {
	case ActionStatusApproved:
		return HistoryActionApproved, nil
	case ActionStatusExecuted:
		return HistoryActionExecuted, nil
	case ActionStatusCancelled:
		return HistoryActionCancelled, nil
	default:
		return "", errInvalidActionStatus
	}
}

type ActionReasonCode string

func ParseActionReasonCode(raw string) (ActionReasonCode, error) {
	switch ActionReasonCode(strings.TrimSpace(raw)) {
	case ActionReasonNoViolation, ActionReasonPolicyViolation, ActionReasonRepeatedViolation,
		ActionReasonSafetyRisk, ActionReasonProhibitedContent, ActionReasonOther:
		return ActionReasonCode(strings.TrimSpace(raw)), nil
	default:
		return "", errInvalidAction
	}
}

// CaseAction is a Moderation-owned staff decision. Status may change along the
// allowed lifecycle; target, type, reason, and rationale are immutable after create.
type CaseAction struct {
	ID           ID
	CaseID       ID
	TargetType   TargetType
	TargetID     ID
	ActionType   ActionType
	Status       ActionStatus
	ReasonCode   ActionReasonCode
	Rationale    *string
	ActorStaffID *ID
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (a CaseAction) Validate() error {
	if a.ID.IsZero() || a.CaseID.IsZero() || a.TargetID.IsZero() {
		return errZeroID
	}
	if _, err := ParseTargetType(string(a.TargetType)); err != nil {
		return err
	}
	if _, err := ParseActionType(string(a.ActionType)); err != nil {
		return err
	}
	if _, err := ParseActionStatus(string(a.Status)); err != nil {
		return err
	}
	if _, err := ParseActionReasonCode(string(a.ReasonCode)); err != nil {
		return err
	}
	if a.Rationale != nil {
		if _, err := NormalizeActionRationale(*a.Rationale); err != nil {
			return err
		}
	}
	if a.ActorStaffID != nil && a.ActorStaffID.IsZero() {
		return errZeroID
	}
	if a.CreatedAt.IsZero() || a.UpdatedAt.IsZero() {
		return errInvalidAction
	}
	return nil
}

type CreateActionInput struct {
	TargetType TargetType
	TargetID   ID
	ActionType ActionType
	ReasonCode ActionReasonCode
	Rationale  string
	ActorID    *ID
}

type ActionTransitionInput struct {
	To      ActionStatus
	ActorID *ID
}

func NormalizeActionRationale(raw string) (*string, error) {
	body := strings.TrimSpace(raw)
	if body == "" {
		return nil, nil
	}
	if len(body) > MaxActionRationaleBytes {
		return nil, errInvalidBody
	}
	if err := rejectSensitiveMaterial(body); err != nil {
		return nil, err
	}
	return &body, nil
}

func cloneAction(row CaseAction) CaseAction {
	out := row
	out.ActorStaffID = cloneOptionalID(row.ActorStaffID)
	if row.Rationale != nil {
		v := *row.Rationale
		out.Rationale = &v
	}
	return out
}

func actionMatchesCaseSubject(row Case, targetType TargetType, targetID ID) bool {
	return row.SubjectType == targetType && row.SubjectID == targetID
}
