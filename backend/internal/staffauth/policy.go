package staffauth

import "backend/internal/staffauth/contracts"

type Policy struct{}

func DefaultPolicy() Policy {
	return Policy{}
}

func (Policy) Allows(p contracts.Principal, perm contracts.Permission) bool {
	if p.StaffID.IsZero() {
		return false
	}
	return contracts.HasPermission(p.Roles, perm)
}
