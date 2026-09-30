package commerce

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

const (
	storeSettingCheckoutStatusKey      = "checkout_status"
	storeSettingMaintenanceMessageKey  = "checkout_maintenance_message"
	storeSettingCheckoutUpdatedByKey   = "checkout_state_updated_by"
	storeSettingCheckoutUpdatedAtKey   = "checkout_state_updated_at"
	maxCheckoutMaintenanceMessageRunes = 240
)

func (r Repository) GetStoreOperationalState(ctx context.Context, storeID string) (StoreOperationalState, error) {
	if strings.TrimSpace(storeID) == "" {
		return StoreOperationalState{}, ErrInvalidInput
	}

	var id string
	var settings map[string]any
	var settingsUpdatedAt *time.Time
	err := r.pool.QueryRow(ctx, `
		SELECT s.id, COALESCE(ss.settings, '{}'::jsonb), ss.updated_at
		FROM stores s
		LEFT JOIN store_settings ss ON ss.store_id = s.id
		WHERE s.id = $1
	`, storeID).Scan(&id, &settings, &settingsUpdatedAt)
	if err != nil {
		return StoreOperationalState{}, translatePGError(err, "get store operational state")
	}
	return operationalStateFromSettings(id, settings, settingsUpdatedAt), nil
}

func (r Repository) UpdateStoreOperationalState(ctx context.Context, storeID, checkoutStatus, maintenanceMessage, actor string) (StoreOperationalState, error) {
	if strings.TrimSpace(storeID) == "" || strings.TrimSpace(actor) == "" {
		return StoreOperationalState{}, ErrInvalidInput
	}
	status, err := normalizeCheckoutStatus(checkoutStatus)
	if err != nil {
		return StoreOperationalState{}, err
	}
	message := strings.TrimSpace(maintenanceMessage)
	if message == "" {
		message = DefaultCheckoutPausedMessage
	}
	if utf8.RuneCountInString(message) > maxCheckoutMaintenanceMessageRunes {
		return StoreOperationalState{}, ErrInvalidInput
	}

	var state StoreOperationalState
	err = r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var id string
		var settings map[string]any
		if err := tx.QueryRow(ctx, `
			SELECT s.id, COALESCE(ss.settings, '{}'::jsonb)
			FROM stores s
			LEFT JOIN store_settings ss ON ss.store_id = s.id
			WHERE s.id = $1
			FOR UPDATE OF s
		`, storeID).Scan(&id, &settings); err != nil {
			return translatePGError(err, "lock store operational state")
		}

		now := time.Now().UTC()
		nextSettings := normalizeSettings(settings)
		nextSettings[storeSettingCheckoutStatusKey] = status
		nextSettings[storeSettingMaintenanceMessageKey] = message
		nextSettings[storeSettingCheckoutUpdatedByKey] = strings.TrimSpace(actor)
		nextSettings[storeSettingCheckoutUpdatedAtKey] = now.Format(time.RFC3339Nano)

		if err := upsertJSONSettings(ctx, tx, `
			INSERT INTO store_settings (store_id, settings)
			VALUES ($1, $2)
			ON CONFLICT (store_id) DO UPDATE SET settings = EXCLUDED.settings, updated_at = now()
		`, id, nextSettings); err != nil {
			return err
		}
		state = operationalStateFromSettings(id, nextSettings, &now)
		return nil
	})
	if err != nil {
		return StoreOperationalState{}, err
	}
	return state, nil
}

func ensureCheckoutAcceptingTx(ctx context.Context, tx pgx.Tx, storeID string) error {
	var settings map[string]any
	err := tx.QueryRow(ctx, `SELECT settings FROM store_settings WHERE store_id = $1`, storeID).Scan(&settings)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil
		}
		return translatePGError(err, "read checkout operational state")
	}
	state := operationalStateFromSettings(storeID, settings, nil)
	if !state.CheckoutAccepting {
		return ErrCheckoutPaused
	}
	return nil
}

func operationalStateFromSettings(storeID string, settings map[string]any, fallbackUpdatedAt *time.Time) StoreOperationalState {
	status, err := normalizeCheckoutStatus(stringSetting(settings, storeSettingCheckoutStatusKey))
	if err != nil {
		status = StoreCheckoutStatusAccepting
	}
	message := strings.TrimSpace(stringSetting(settings, storeSettingMaintenanceMessageKey))
	if message == "" {
		message = DefaultCheckoutPausedMessage
	}
	updatedAt := timeSetting(settings, storeSettingCheckoutUpdatedAtKey)
	if updatedAt == nil {
		updatedAt = fallbackUpdatedAt
	}
	return StoreOperationalState{
		StoreID:            storeID,
		CheckoutStatus:     status,
		MaintenanceMessage: message,
		UpdatedBy:          strings.TrimSpace(stringSetting(settings, storeSettingCheckoutUpdatedByKey)),
		UpdatedAt:          updatedAt,
		CheckoutAccepting:  status == StoreCheckoutStatusAccepting,
	}
}

func normalizeCheckoutStatus(status string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", StoreCheckoutStatusAccepting:
		return StoreCheckoutStatusAccepting, nil
	case StoreCheckoutStatusPaused:
		return StoreCheckoutStatusPaused, nil
	default:
		return "", ErrInvalidInput
	}
}

func stringSetting(settings map[string]any, key string) string {
	value, ok := settings[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return ""
	}
}

func timeSetting(settings map[string]any, key string) *time.Time {
	raw := strings.TrimSpace(stringSetting(settings, key))
	if raw == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return nil
	}
	parsed = parsed.UTC()
	return &parsed
}
