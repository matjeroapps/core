package inventory

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type Service struct {
	repo Repository
}

func NewService(repo Repository) Service {
	return Service{repo: repo}
}

func (s Service) AdjustStoreInventory(ctx context.Context, params AdjustParams) (*AdjustmentResult, error) {
	if _, err := uuid.Parse(strings.TrimSpace(params.StoreID)); err != nil {
		return nil, fmt.Errorf("%w: invalid store_id", ErrInvalidInput)
	}
	if _, err := uuid.Parse(strings.TrimSpace(params.LocationID)); err != nil {
		return nil, fmt.Errorf("%w: invalid fulfillment_location_id", ErrInvalidInput)
	}
	if _, err := uuid.Parse(strings.TrimSpace(params.SKUID)); err != nil {
		return nil, fmt.Errorf("%w: invalid sku_id", ErrInvalidInput)
	}

	// Validate dual-mode quantity specification: exactly one of QtyDelta or TargetQty
	if (params.QtyDelta == nil && params.TargetQty == nil) || (params.QtyDelta != nil && params.TargetQty != nil) {
		return nil, fmt.Errorf("%w: exactly one of qty_delta or target_qty must be specified", ErrInvalidInput)
	}

	if params.TargetQty != nil && *params.TargetQty < 0 {
		return nil, fmt.Errorf("%w: target_qty cannot be negative", ErrInvalidInput)
	}

	reasonCode := strings.TrimSpace(params.ReasonCode)
	if reasonCode == "" {
		if params.TargetQty != nil {
			reasonCode = ReasonCycleCountReconciliation
		} else {
			return nil, fmt.Errorf("%w: reason_code is required", ErrInvalidInput)
		}
	}

	switch reasonCode {
	case ReasonDamaged, ReasonReceivedStock, ReasonCycleCountReconciliation,
		ReasonTheftLoss, ReasonCustomerReturnManual, ReasonCorrection:
		// valid
	default:
		return nil, fmt.Errorf("%w: invalid reason_code '%s'", ErrInvalidInput, reasonCode)
	}
	params.ReasonCode = reasonCode

	var deltaStr, targetStr string
	if params.QtyDelta != nil {
		deltaStr = fmt.Sprintf("%d", *params.QtyDelta)
	} else {
		deltaStr = "nil"
	}
	if params.TargetQty != nil {
		targetStr = fmt.Sprintf("%d", *params.TargetQty)
	} else {
		targetStr = "nil"
	}

	fingerprint := fmt.Sprintf("store=%s;loc=%s;sku=%s;delta=%s;target=%s;reason=%s",
		params.StoreID, params.LocationID, params.SKUID, deltaStr, targetStr, params.ReasonCode)

	return s.repo.AdjustStoreInventory(ctx, params, fingerprint)
}
