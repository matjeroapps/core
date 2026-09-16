package commerce

import "strings"

// StoreEntitlementPolicy manages maximum active stores per seller.
type StoreEntitlementPolicy struct {
	DefaultMaxActiveStores int
}

func NewStoreEntitlementPolicy(defaultMaxActive int) StoreEntitlementPolicy {
	if defaultMaxActive < 1 {
		defaultMaxActive = 1
	}
	return StoreEntitlementPolicy{
		DefaultMaxActiveStores: defaultMaxActive,
	}
}

func (p StoreEntitlementPolicy) EffectiveLimit() int {
	if p.DefaultMaxActiveStores < 1 {
		return 1
	}
	return p.DefaultMaxActiveStores
}

const (
	RoleOwner   = "owner"
	RoleManager = "manager"
	RoleStaff   = "staff"
)

func NormalizeRole(role string) string {
	r := strings.ToLower(strings.TrimSpace(role))
	switch r {
	case "seller_owner", "owner":
		return RoleOwner
	case "seller_manager", "manager":
		return RoleManager
	case "seller_staff", "staff":
		return RoleStaff
	default:
		return r
	}
}

func IsOwnerRole(role string) bool {
	return NormalizeRole(role) == RoleOwner
}

func IsManagerRole(role string) bool {
	r := NormalizeRole(role)
	return r == RoleOwner || r == RoleManager
}

func IsStaffRole(role string) bool {
	r := NormalizeRole(role)
	return r == RoleOwner || r == RoleManager || r == RoleStaff
}
