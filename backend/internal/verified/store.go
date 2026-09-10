package verified

import (
	"context"
	"time"

	"backend/internal/platform/outbox"
)

type appointmentStore interface {
	InsertAppointment(ctx context.Context, appt Appointment) error
	GetAppointment(ctx context.Context, id ID) (Appointment, error)
	ListAppointmentsForUser(ctx context.Context, userID ID, limit int) ([]Appointment, error)
	UpdateAppointmentStatus(ctx context.Context, id ID, fromStatus, toStatus string, updatedAt time.Time) (Appointment, error)
	CreateFlowWithChallenge(ctx context.Context, flow VerificationFlow, ch Challenge, now time.Time) error
	GetFlow(ctx context.Context, id ID) (VerificationFlow, error)
	ListFlowsForUser(ctx context.Context, userID ID, listingID ID, limit int) ([]VerificationFlow, error)
	ReplaceChallenge(ctx context.Context, ch Challenge, now time.Time) error
	GetChallenge(ctx context.Context, id ID) (Challenge, error)
	ListChallenges(ctx context.Context, appointmentID ID) ([]Challenge, error)
	GetInteraction(ctx context.Context, id ID) (VerifiedInteraction, error)
	GetInteractionByAppointment(ctx context.Context, appointmentID ID) (VerifiedInteraction, error)
	CompleteVerification(ctx context.Context, appointmentID ID, tokenHash []byte, interaction VerifiedInteraction, now time.Time, enqueue func(ctx context.Context, exec outbox.Execer, completed VerifiedInteraction) error) (VerifiedInteraction, error)
	CompleteFlowVerification(ctx context.Context, flowID ID, tokenHash []byte, interactionID ID, now time.Time, enqueue func(ctx context.Context, exec outbox.Execer, completed VerifiedInteraction) error) (VerifiedInteraction, error)
}
