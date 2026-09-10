package contracts

// Role is a V1 staff RBAC role. Roles are assigned by the identity provider,
// never by client-supplied headers.
type Role string

const (
	RoleModerator       Role = "moderator"
	RoleSeniorModerator Role = "senior_moderator"
	RoleSupport         Role = "support"
	RoleAdmin           Role = "admin"
)

func ParseRole(raw string) (Role, bool) {
	switch Role(raw) {
	case RoleModerator, RoleSeniorModerator, RoleSupport, RoleAdmin:
		return Role(raw), true
	default:
		return "", false
	}
}

// Permission is a coarse V1 staff permission. Do not expand into a large ACL catalog here.
type Permission string

const (
	PermModerationReportRead    Permission = "moderation.report.read"
	PermModerationCaseRead      Permission = "moderation.case.read"
	PermModerationCaseWrite     Permission = "moderation.case.write"
	PermModerationActionApprove Permission = "moderation.action.approve"
	PermModerationAppealReview  Permission = "moderation.appeal.review"
	PermDisputesRead            Permission = "disputes.read"
	PermDisputesReview          Permission = "disputes.review"
)

func ParsePermission(raw string) (Permission, bool) {
	switch Permission(raw) {
	case PermModerationReportRead, PermModerationCaseRead, PermModerationCaseWrite,
		PermModerationActionApprove, PermModerationAppealReview,
		PermDisputesRead, PermDisputesReview:
		return Permission(raw), true
	default:
		return "", false
	}
}

// RolePermissions is the explicit, testable V1 mapping. Admin is the union of all
// listed permissions; it is not a wildcard bypass in policy evaluation.
var RolePermissions = map[Role][]Permission{
	RoleModerator: {
		PermModerationReportRead,
		PermModerationCaseRead,
		PermModerationCaseWrite,
	},
	RoleSeniorModerator: {
		PermModerationReportRead,
		PermModerationCaseRead,
		PermModerationCaseWrite,
		PermModerationActionApprove,
		PermModerationAppealReview,
	},
	RoleSupport: {
		PermDisputesRead,
		PermDisputesReview,
	},
	RoleAdmin: {
		PermModerationReportRead,
		PermModerationCaseRead,
		PermModerationCaseWrite,
		PermModerationActionApprove,
		PermModerationAppealReview,
		PermDisputesRead,
		PermDisputesReview,
	},
}

func PermissionsFor(roles []Role) map[Permission]struct{} {
	out := make(map[Permission]struct{})
	for _, role := range roles {
		for _, perm := range RolePermissions[role] {
			out[perm] = struct{}{}
		}
	}
	return out
}

func HasPermission(roles []Role, perm Permission) bool {
	_, ok := PermissionsFor(roles)[perm]
	return ok
}
