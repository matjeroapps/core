package inventory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Repository struct {
	pool DBPool
}

func NewRepository(pool DBPool) Repository {
	return Repository{pool: pool}
}

func (r Repository) AdjustStoreInventory(ctx context.Context, params AdjustParams, fingerprint string) (*AdjustmentResult, error) {
	if r.pool == nil {
		return nil, errors.New("database pool is not initialized")
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// 1. Verify location belongs to the store
	var locID string
	err = tx.QueryRow(ctx, `
		SELECT id FROM fulfillment_locations 
		WHERE id = $1 AND store_id = $2
	`, params.LocationID, params.StoreID).Scan(&locID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("check location: %w", err)
	}

	// 2. Verify SKU is attached to this store (via seller_listing or store-owned product)
	var matchedSKUID string
	err = tx.QueryRow(ctx, `
		SELECT s.id 
		FROM skus s 
		JOIN variants v ON v.id = s.variant_id 
		LEFT JOIN seller_listings sl ON sl.product_id = v.product_id AND sl.store_id = $1
		LEFT JOIN seller_products sp ON sp.product_id = v.product_id
		LEFT JOIN stores st ON st.id = $1 AND st.seller_id = sp.seller_id
		WHERE s.id = $2 AND (sl.id IS NOT NULL OR st.id IS NOT NULL)
		LIMIT 1
	`, params.StoreID, params.SKUID).Scan(&matchedSKUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("check sku store listing: %w", err)
	}

	// 3. Idempotency replay check
	if strings.TrimSpace(params.IdempotencyKey) != "" {
		var (
			existingID, existingSnapID, existingReason, existingLocID, existingSKUID string
			existingDelta, existingOnHand, existingReserved                         int64
			existingFP                                                              sql.NullString
			existingUpdatedAt                                                       time.Time
		)
		err = tx.QueryRow(ctx, `
			SELECT im.id, im.inventory_snapshot_id, im.quantity_delta, im.on_hand_qty, im.reserved_qty,
			       im.reason, im.request_fingerprint, snap.fulfillment_location_id, snap.sku_id, snap.updated_at
			FROM inventory_movements im
			JOIN inventory_snapshots snap ON snap.id = im.inventory_snapshot_id
			WHERE im.idempotency_key = $1
			LIMIT 1
		`, params.IdempotencyKey).Scan(
			&existingID, &existingSnapID, &existingDelta, &existingOnHand, &existingReserved,
			&existingReason, &existingFP, &existingLocID, &existingSKUID, &existingUpdatedAt,
		)
		if err == nil {
			if existingFP.Valid && existingFP.String != fingerprint {
				return nil, ErrIdempotencyConflict
			}
			return &AdjustmentResult{
				SnapshotID:            existingSnapID,
				FulfillmentLocationID: existingLocID,
				SKUID:                 existingSKUID,
				OnHandQty:             existingOnHand,
				ReservedQty:           existingReserved,
				AvailableQty:          existingOnHand - existingReserved,
				QuantityDelta:         existingDelta,
				MovementID:            existingID,
				ReasonCode:            existingReason,
				UpdatedAt:             existingUpdatedAt,
			}, nil
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("check idempotency replay: %w", err)
		}
	}

	// 4. Pessimistic lock on snapshot row
	var (
		snapshotID           string
		currentOnHand        int64
		currentReserved      int64
		version              int64
		updatedAt            time.Time
		newOnHand            int64
		delta                int64
		movementType         = "adjustment"
	)

	if params.TargetQty != nil {
		movementType = "cycle_count"
	}

	err = tx.QueryRow(ctx, `
		SELECT id, on_hand_qty, reserved_qty, version, updated_at
		FROM inventory_snapshots
		WHERE fulfillment_location_id = $1 AND sku_id = $2
		FOR UPDATE
	`, params.LocationID, params.SKUID).Scan(&snapshotID, &currentOnHand, &currentReserved, &version, &updatedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		// Snapshot does not exist yet
		if params.TargetQty != nil {
			delta = *params.TargetQty
			newOnHand = *params.TargetQty
		} else {
			delta = *params.QtyDelta
			newOnHand = *params.QtyDelta
		}

		if newOnHand < 0 {
			return nil, ErrInsufficientInventory
		}

		snapshotID = uuid.NewString()
		err = tx.QueryRow(ctx, `
			INSERT INTO inventory_snapshots (
				id, fulfillment_location_id, sku_id, on_hand_qty, reserved_qty, version, created_at, updated_at
			)
			VALUES ($1, $2, $3, $4, 0, 1, now(), now())
			RETURNING updated_at
		`, snapshotID, params.LocationID, params.SKUID, newOnHand).Scan(&updatedAt)
		if err != nil {
			return nil, fmt.Errorf("insert initial inventory snapshot: %w", err)
		}
		currentReserved = 0
	} else if err != nil {
		return nil, fmt.Errorf("lock inventory snapshot: %w", err)
	} else {
		// Snapshot exists
		if params.TargetQty != nil {
			delta = *params.TargetQty - currentOnHand
		} else {
			delta = *params.QtyDelta
		}

		newOnHand = currentOnHand + delta
		if newOnHand < currentReserved || newOnHand < 0 {
			return nil, ErrInsufficientInventory
		}

		if delta != 0 {
			err = tx.QueryRow(ctx, `
				UPDATE inventory_snapshots
				SET on_hand_qty = on_hand_qty + $2,
				    version = version + 1,
				    updated_at = now()
				WHERE id = $1
				RETURNING updated_at
			`, snapshotID, delta).Scan(&updatedAt)
			if err != nil {
				return nil, fmt.Errorf("update inventory snapshot: %w", err)
			}
		}
	}

	// 5. Insert movement record
	movementID := uuid.NewString()
	reasonText := strings.TrimSpace(params.ReasonCode)
	if strings.TrimSpace(params.Note) != "" {
		reasonText = fmt.Sprintf("%s: %s", reasonText, strings.TrimSpace(params.Note))
	}

	var ikParam, fpParam *string
	if strings.TrimSpace(params.IdempotencyKey) != "" {
		trimmedIK := strings.TrimSpace(params.IdempotencyKey)
		ikParam = &trimmedIK
	}
	if strings.TrimSpace(fingerprint) != "" {
		trimmedFP := strings.TrimSpace(fingerprint)
		fpParam = &trimmedFP
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO inventory_movements (
			id, inventory_snapshot_id, movement_type, quantity_delta, on_hand_qty, reserved_qty,
			reason, principal_subject, correlation_id, idempotency_key, request_fingerprint, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, now())
	`, movementID, snapshotID, movementType, delta, newOnHand, currentReserved,
		reasonText, params.Subject, params.CorrelationID, ikParam, fpParam)
	if err != nil {
		return nil, fmt.Errorf("insert inventory movement: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit inventory adjustment: %w", err)
	}

	return &AdjustmentResult{
		SnapshotID:            snapshotID,
		FulfillmentLocationID: params.LocationID,
		SKUID:                 params.SKUID,
		OnHandQty:             newOnHand,
		ReservedQty:           currentReserved,
		AvailableQty:          newOnHand - currentReserved,
		QuantityDelta:         delta,
		MovementID:            movementID,
		ReasonCode:            params.ReasonCode,
		UpdatedAt:             updatedAt,
	}, nil
}
