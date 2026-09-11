package contracts

import (
	"testing"
)

func TestRolePermissionMappingIsExplicit(t *testing.T) {
	if !HasPermission([]Role{RoleModerator}, PermModerationReportRead) {
		t.Fatal("moderator report.read")
	}
	if HasPermission([]Role{RoleModerator}, PermModerationActionApprove) {
		t.Fatal("moderator must not inherit approve")
	}
	if !HasPermission([]Role{RoleSeniorModerator}, PermModerationAppealReview) {
		t.Fatal("senior appeal.review")
	}
	if HasPermission([]Role{RoleSupport}, PermModerationCaseWrite) {
		t.Fatal("support must not write cases")
	}
	if HasPermission([]Role{RoleSupport}, PermIdentityProfileRead) || HasPermission([]Role{RoleSupport}, PermListingsRead) || HasPermission([]Role{RoleSupport}, PermTrustRead) {
		t.Fatal("support must not read user/listing 360")
	}
	if !HasPermission([]Role{RoleModerator}, PermIdentityProfileRead) || !HasPermission([]Role{RoleModerator}, PermListingsRead) || !HasPermission([]Role{RoleModerator}, PermTrustRead) {
		t.Fatal("moderator should read user/listing 360")
	}
	if !HasPermission([]Role{RoleAdmin}, PermDisputesReview) || !HasPermission([]Role{RoleAdmin}, PermModerationActionApprove) {
		t.Fatal("admin union")
	}
	if _, ok := ParseRole("superuser"); ok {
		t.Fatal("unknown role")
	}
	if _, ok := ParsePermission("moderation.case.delete"); ok {
		t.Fatal("unknown permission")
	}
}
