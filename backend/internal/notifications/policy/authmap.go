package policy

const (
	AuthLoginSuccess           = "auth.login.success"
	AuthLoginFailed            = "auth.login.failed"
	AuthLogout                 = "auth.logout"
	AuthSessionRevoked         = "auth.session.revoked"
	AuthSessionsRevokedOthers  = "auth.sessions.revoked_others"
	AuthPasswordResetCompleted = "auth.password_reset.completed"
	AuthPasskeyAdded           = "auth.passkey.added"
	AuthPasskeyRemoved         = "auth.passkey.removed"
	AuthStepUpSuccess          = "auth.step_up.success"
	AuthStepUpFailed           = "auth.step_up.failed"
	AuthRateLimitTriggered     = "auth.rate_limit.triggered"
	AuthChallengeRequired      = "auth.challenge.required"
	AuthChallengeFailed        = "auth.challenge.failed"
)

type AuthNotifyPolicy struct {
	Notify    bool
	EventType EventType
	Reason    string
}

func MapAuthSecurityEvent(authEvent string) AuthNotifyPolicy {
	switch authEvent {
	case AuthLoginSuccess:
		return AuthNotifyPolicy{Notify: true, EventType: EventSecurityLoginNew, Reason: "new_session"}
	case AuthPasskeyAdded:
		return AuthNotifyPolicy{Notify: true, EventType: EventSecurityPasskeyAdded, Reason: "credential_change"}
	case AuthPasskeyRemoved:
		return AuthNotifyPolicy{Notify: true, EventType: EventSecurityPasskeyRemoved, Reason: "credential_change"}
	case AuthPasswordResetCompleted:
		return AuthNotifyPolicy{Notify: true, EventType: EventSecurityPasswordResetCompleted, Reason: "recovery_completed"}
	case AuthSessionRevoked, AuthSessionsRevokedOthers:
		return AuthNotifyPolicy{Notify: true, EventType: EventSecuritySessionsRevoked, Reason: "session_revoked"}
	case AuthLoginFailed:
		return AuthNotifyPolicy{Notify: false, Reason: "failed_attempt_not_user_notified"}
	case AuthLogout, AuthStepUpSuccess, AuthStepUpFailed, AuthRateLimitTriggered, AuthChallengeRequired, AuthChallengeFailed:
		return AuthNotifyPolicy{Notify: false, Reason: "operational_audit_only"}
	default:
		return AuthNotifyPolicy{Notify: false, Reason: "unmapped"}
	}
}
