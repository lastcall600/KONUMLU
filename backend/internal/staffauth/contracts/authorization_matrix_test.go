package contracts

import "testing"

func TestRolePermissionMatrixCoversEveryConstant(t *testing.T) {
	roles := []Role{RoleModerator, RoleSeniorModerator, RoleSupport, RoleAdmin}
	perms := []Permission{
		PermModerationReportRead,
		PermModerationCaseRead,
		PermModerationCaseWrite,
		PermModerationActionApprove,
		PermModerationAppealReview,
		PermDisputesRead,
		PermDisputesReview,
		PermIdentityProfileRead,
		PermListingsRead,
		PermTrustRead,
	}
	if len(RolePermissions) != len(roles) {
		t.Fatalf("RolePermissions size = %d want %d", len(RolePermissions), len(roles))
	}
	for _, role := range roles {
		allowed := map[Permission]struct{}{}
		for _, p := range RolePermissions[role] {
			allowed[p] = struct{}{}
		}
		for _, perm := range perms {
			got := HasPermission([]Role{role}, perm)
			_, want := allowed[perm]
			if got != want {
				t.Errorf("role %s perm %s = %v want %v", role, perm, got, want)
			}
		}
	}
	if HasPermission(nil, PermListingsRead) {
		t.Fatal("empty roles must not grant")
	}
	if HasPermission([]Role{Role("superuser")}, PermListingsRead) {
		t.Fatal("unknown role must not grant")
	}
}
