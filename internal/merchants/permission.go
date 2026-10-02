package merchants

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Permission Codes Taxonomy
const (
	// Merchant Scope
	PermissionMerchantManage = "merchant.manage"
	PermissionTeamManage     = "team.manage"

	// Retail Scope
	PermissionRetailStoresManage    = "retail.stores.manage"
	PermissionRetailCatalogManage   = "retail.catalog.manage"
	PermissionRetailOrdersManage    = "retail.orders.manage"
	PermissionRetailInventoryManage = "retail.inventory.manage"

	// Supply Scope
	PermissionSupplyCatalogManage   = "supply.catalog.manage"
	PermissionSupplyInventoryManage = "supply.inventory.manage"
	PermissionSupplyFulfillment     = "supply.fulfillment.manage"
	PermissionSupplyShippingManage  = "supply.shipping.manage"

	// Finance Scope
	PermissionFinanceView   = "finance.view"
	PermissionFinanceManage = "finance.manage"
)

type MembershipPermission struct {
	ID             uuid.UUID `json:"id"`
	MembershipID   uuid.UUID `json:"membership_id"`
	PermissionCode string    `json:"permission_code"`
	GrantedAt      time.Time `json:"granted_at"`
}

func NewMembershipPermission(membershipID uuid.UUID, permissionCode string) (*MembershipPermission, error) {
	if membershipID == uuid.Nil {
		return nil, errors.New("membership_id is required")
	}
	if permissionCode == "" {
		return nil, errors.New("permission_code is required")
	}
	return &MembershipPermission{
		ID:             uuid.New(),
		MembershipID:   membershipID,
		PermissionCode: permissionCode,
		GrantedAt:      time.Now().UTC(),
	}, nil
}
