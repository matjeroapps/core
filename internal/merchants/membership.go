package merchants

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

type MembershipStatus string

const (
	MembershipStatusActive    MembershipStatus = "active"
	MembershipStatusInvited   MembershipStatus = "invited"
	MembershipStatusSuspended MembershipStatus = "suspended"
)

type MerchantMembership struct {
	ID               uuid.UUID        `json:"id"`
	MerchantID       uuid.UUID        `json:"merchant_id"`
	PrincipalSubject string           `json:"principal_subject"`
	Status           MembershipStatus `json:"status"`
	CreatedAt        time.Time        `json:"created_at"`
	UpdatedAt        time.Time        `json:"updated_at"`
	Permissions      []string         `json:"permissions,omitempty"`
}

func NewMerchantMembership(merchantID uuid.UUID, principalSubject string) (*MerchantMembership, error) {
	if merchantID == uuid.Nil {
		return nil, errors.New("merchant_id is required")
	}
	if principalSubject == "" {
		return nil, errors.New("principal_subject is required")
	}
	now := time.Now().UTC()
	return &MerchantMembership{
		ID:               uuid.New(),
		MerchantID:       merchantID,
		PrincipalSubject: principalSubject,
		Status:           MembershipStatusActive,
		CreatedAt:        now,
		UpdatedAt:        now,
		Permissions:      make([]string, 0),
	}, nil
}

func (m *MerchantMembership) IsActive() bool {
	return m.Status == MembershipStatusActive
}
