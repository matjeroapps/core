package merchants

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

type CapabilityType string

const (
	CapabilityTypeRetail CapabilityType = "RETAIL"
	CapabilityTypeSupply CapabilityType = "SUPPLY"
)

type CapabilityStatus string

const (
	CapabilityStatusInactive   CapabilityStatus = "inactive"
	CapabilityStatusActivating CapabilityStatus = "activating"
	CapabilityStatusActive     CapabilityStatus = "active"
	CapabilityStatusSuspended  CapabilityStatus = "suspended"
)

type MerchantCapability struct {
	ID             uuid.UUID        `json:"id"`
	MerchantID     uuid.UUID        `json:"merchant_id"`
	CapabilityType CapabilityType   `json:"capability_type"`
	Status         CapabilityStatus `json:"status"`
	ActivatedAt    *time.Time       `json:"activated_at,omitempty"`
	SuspendedAt    *time.Time       `json:"suspended_at,omitempty"`
	CreatedAt      time.Time        `json:"created_at"`
	UpdatedAt      time.Time        `json:"updated_at"`
}

func NewMerchantCapability(merchantID uuid.UUID, capType CapabilityType) (*MerchantCapability, error) {
	if merchantID == uuid.Nil {
		return nil, errors.New("merchant_id is required")
	}
	if capType != CapabilityTypeRetail && capType != CapabilityTypeSupply {
		return nil, errors.New("invalid capability_type")
	}
	now := time.Now().UTC()
	return &MerchantCapability{
		ID:             uuid.New(),
		MerchantID:     merchantID,
		CapabilityType: capType,
		Status:         CapabilityStatusInactive,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

func (c *MerchantCapability) Activate() {
	now := time.Now().UTC()
	c.Status = CapabilityStatusActive
	c.ActivatedAt = &now
	c.SuspendedAt = nil
	c.UpdatedAt = now
}

func (c *MerchantCapability) Suspend() {
	now := time.Now().UTC()
	c.Status = CapabilityStatusSuspended
	c.SuspendedAt = &now
	c.UpdatedAt = now
}

func (c *MerchantCapability) IsActive() bool {
	return c.Status == CapabilityStatusActive
}
