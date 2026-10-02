package merchants

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrMerchantNotFound      = errors.New("merchant not found")
	ErrInvalidMerchantStatus = errors.New("invalid merchant status")
	ErrCapabilityNotFound    = errors.New("merchant capability not found")
	ErrCapabilitySuspended   = errors.New("merchant capability is suspended")
	ErrMembershipNotFound    = errors.New("merchant membership not found")
	ErrPermissionDenied      = errors.New("merchant permission denied")
	ErrOwnerMismatch         = errors.New("merchant owner mismatch across profiles")
)

type MerchantStatus string

const (
	MerchantStatusActive    MerchantStatus = "active"
	MerchantStatusSuspended MerchantStatus = "suspended"
)

type Merchant struct {
	ID        uuid.UUID      `json:"id"`
	Code      string         `json:"code"`
	LegalName string         `json:"legal_name"`
	Status    MerchantStatus `json:"status"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

func NewMerchant(code, legalName string) (*Merchant, error) {
	if code == "" || legalName == "" {
		return nil, errors.New("merchant code and legal name are required")
	}
	now := time.Now().UTC()
	return &Merchant{
		ID:        uuid.New(),
		Code:      code,
		LegalName: legalName,
		Status:    MerchantStatusActive,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

func (m *Merchant) Suspend() {
	m.Status = MerchantStatusSuspended
	m.UpdatedAt = time.Now().UTC()
}

func (m *Merchant) Reactivate() {
	m.Status = MerchantStatusActive
	m.UpdatedAt = time.Now().UTC()
}

func (m *Merchant) IsActive() bool {
	return m.Status == MerchantStatusActive
}
