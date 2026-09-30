package commerce

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"core/packages/outbox"
)

const DefaultCheckoutSessionLifetime = 30 * time.Minute

const (
	CheckoutSessionStatusOpen      = "open"
	CheckoutSessionStatusFinalized = "finalized"
	CheckoutSessionStatusExpired   = "expired"
)

type ShippingAddress struct {
	RecipientName string  `json:"recipient_name"`
	AddressLine1  string  `json:"address_line_1"`
	AddressLine2  *string `json:"address_line_2,omitempty"`
	City          string  `json:"city"`
	Region        *string `json:"region,omitempty"`
	PostalCode    *string `json:"postal_code,omitempty"`
	CountryCode   string  `json:"country_code"`
}

type CheckoutSession struct {
	ID                          string          `json:"id"`
	StoreID                     string          `json:"store_id"`
	CartID                      string          `json:"cart_id"`
	CustomerID                  *string         `json:"customer_id,omitempty"`
	Status                      string          `json:"status"`
	ExpiresAt                   time.Time       `json:"expires_at"`
	ShippingAddressSnapshot     json.RawMessage `json:"shipping_address_snapshot,omitempty"`
	ContactEmail                *string         `json:"contact_email,omitempty"`
	FinalizeFingerprint         *string         `json:"-"`
	GuestOrderAccessTokenDigest []byte          `json:"-"`
	FinalizedAt                 *time.Time      `json:"finalized_at,omitempty"`
	CreatedAt                   time.Time       `json:"created_at"`
	UpdatedAt                   time.Time       `json:"updated_at"`
}

type MarketplaceAttributionInput struct {
	SellerListingID  string  `json:"seller_listing_id"`
	StoreID          string  `json:"store_id,omitempty"`
	MarketCode       string  `json:"market_code,omitempty"`
	SourceCollection *string `json:"source_collection,omitempty"`
}

type FinalizeRequest struct {
	SessionID       string
	ShippingAddress ShippingAddress
	ContactEmail    string
	Attribution     *MarketplaceAttributionInput
}

type CheckoutDecision struct {
	SessionID   string
	Status      string
	Fingerprint string
	Replay      bool
}

type fingerprintLine struct {
	SellerListingID        string `json:"seller_listing_id"`
	SKUID                  string `json:"sku_id"`
	Quantity               int64  `json:"quantity"`
	ExpectedUnitPriceMinor int64  `json:"expected_unit_price_minor"`
	ExpectedCurrencyCode   string `json:"expected_currency_code"`
}

type canonicalFinalizeInput struct {
	CheckoutSessionID string                       `json:"checkout_session_id"`
	CartID            string                       `json:"cart_id"`
	CustomerID        *string                      `json:"customer_id"`
	ShippingAddress   ShippingAddress              `json:"shipping_address"`
	ContactEmail      string                       `json:"contact_email"`
	Attribution       *MarketplaceAttributionInput `json:"attribution,omitempty"`
	CartLines         []fingerprintLine            `json:"cart_lines"`
}

// ComputeFinalizeFingerprint serializes a typed, field-ordered semantic
// request. It deliberately does not normalize address/contact values beyond
// the validation contract, so only explicitly equal semantic inputs replay.
func ComputeFinalizeFingerprint(session CheckoutSession, cart Cart, request FinalizeRequest) (string, error) {
	if session.ID == "" || cart.ID == "" || request.SessionID != session.ID || request.ShippingAddress.RecipientName == "" || request.ShippingAddress.AddressLine1 == "" || request.ShippingAddress.City == "" || len(request.ShippingAddress.CountryCode) != 2 || strings.TrimSpace(request.ContactEmail) == "" {
		return "", ErrInvalidInput
	}
	lines := make([]fingerprintLine, 0, len(cart.Items))
	for _, item := range cart.Items {
		if item.SellerListingID == "" || item.SKUID == "" || item.Quantity <= 0 || item.ExpectedUnitPriceMinor < 0 || item.ExpectedCurrencyCode == "" {
			return "", ErrInvalidInput
		}
		lines = append(lines, fingerprintLine{
			SellerListingID: item.SellerListingID, SKUID: item.SKUID, Quantity: item.Quantity,
			ExpectedUnitPriceMinor: item.ExpectedUnitPriceMinor, ExpectedCurrencyCode: item.ExpectedCurrencyCode,
		})
	}
	// Cart loading is deterministic, but sorting here makes the primitive safe
	// for callers that construct a Cart in memory or load it through another store.
	sortFingerprintLines(lines)
	payload, err := json.Marshal(canonicalFinalizeInput{
		CheckoutSessionID: session.ID,
		CartID:            cart.ID,
		CustomerID:        session.CustomerID,
		ShippingAddress:   request.ShippingAddress,
		ContactEmail:      request.ContactEmail,
		Attribution:       request.Attribution,
		CartLines:         lines,
	})
	if err != nil {
		return "", fmt.Errorf("canonicalize finalize request: %w", err)
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func sortFingerprintLines(lines []fingerprintLine) {
	for i := 1; i < len(lines); i++ {
		for j := i; j > 0 && (lines[j].SellerListingID < lines[j-1].SellerListingID || (lines[j].SellerListingID == lines[j-1].SellerListingID && lines[j].SKUID < lines[j-1].SKUID)); j-- {
			lines[j], lines[j-1] = lines[j-1], lines[j]
		}
	}
}

func (r Repository) CreateCheckoutSession(ctx context.Context, storeID, cartToken string, customerID *string, lifetime time.Duration) (CheckoutSession, string, error) {
	if storeID == "" || strings.TrimSpace(cartToken) == "" {
		return CheckoutSession{}, "", ErrInvalidInput
	}
	if lifetime == 0 {
		lifetime = DefaultCheckoutSessionLifetime
	}
	if lifetime < 0 || lifetime < time.Second {
		return CheckoutSession{}, "", ErrInvalidInput
	}
	rawCapability, digestHex, err := generateCapability()
	if err != nil {
		return CheckoutSession{}, "", err
	}
	digest, err := hex.DecodeString(digestHex)
	if err != nil {
		return CheckoutSession{}, "", fmt.Errorf("decode capability digest: %w", err)
	}
	var session CheckoutSession
	err = r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		cart, err := r.getCart(ctx, tx, storeID, bearerDigest(cartToken))
		if err != nil {
			return err
		}
		if cart.Status != CartStatusActive {
			return ErrCartExpired
		}
		if customerID != nil && (cart.CustomerID == nil || *customerID != *cart.CustomerID) {
			return ErrConflict
		}
		if err := ensureCheckoutAcceptingTx(ctx, tx, storeID); err != nil {
			return err
		}
		id := uuid.NewString()
		err = tx.QueryRow(ctx, `
			INSERT INTO checkout_sessions (id, store_id, cart_id, customer_id, status, expires_at, guest_order_access_token_digest)
			VALUES ($1, $2, $3, $4, 'open', clock_timestamp() + ($5 * interval '1 second'), $6)
			RETURNING id, store_id, cart_id, customer_id, status, expires_at, shipping_address_snapshot, contact_email, finalize_fingerprint, guest_order_access_token_digest, finalized_at, created_at, updated_at
		`, id, storeID, cart.ID, customerID, int64(lifetime/time.Second), digest).Scan(checkoutSessionScanArgs(&session)...)
		return translatePGError(err, "create checkout session")
	})
	if err != nil {
		return CheckoutSession{}, "", err
	}
	return session, rawCapability, nil
}

func checkoutSessionScanArgs(session *CheckoutSession) []any {
	return []any{&session.ID, &session.StoreID, &session.CartID, &session.CustomerID, &session.Status, &session.ExpiresAt, &session.ShippingAddressSnapshot, &session.ContactEmail, &session.FinalizeFingerprint, &session.GuestOrderAccessTokenDigest, &session.FinalizedAt, &session.CreatedAt, &session.UpdatedAt}
}

func (r Repository) EvaluateCheckoutSession(ctx context.Context, storeID string, request FinalizeRequest) (CheckoutDecision, error) {
	if storeID == "" || request.SessionID == "" {
		return CheckoutDecision{}, ErrInvalidInput
	}
	var decision CheckoutDecision
	err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var session CheckoutSession
		if err := tx.QueryRow(ctx, `
			SELECT id, store_id, cart_id, customer_id, status, expires_at, shipping_address_snapshot, contact_email, finalize_fingerprint, guest_order_access_token_digest, finalized_at, created_at, updated_at
			FROM checkout_sessions WHERE id = $1 AND store_id = $2 FOR UPDATE
		`, request.SessionID, storeID).Scan(checkoutSessionScanArgs(&session)...); err != nil {
			return translatePGError(err, "lock checkout session")
		}
		var cart Cart
		if err := tx.QueryRow(ctx, `
			SELECT id, store_id, market_code, customer_id, status, expires_at, created_at, updated_at
			FROM carts WHERE id = $1 AND store_id = $2 FOR UPDATE
		`, session.CartID, session.StoreID).Scan(&cart.ID, &cart.StoreID, &cart.MarketCode, &cart.CustomerID, &cart.Status, &cart.ExpiresAt, &cart.CreatedAt, &cart.UpdatedAt); err != nil {
			return translatePGError(err, "lock checkout cart")
		}
		var decisionNow time.Time
		if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&decisionNow); err != nil {
			return fmt.Errorf("capture checkout decision time: %w", err)
		}
		if err := ensureCheckoutAcceptingTx(ctx, tx, storeID); err != nil {
			return err
		}
		if session.Status == CheckoutSessionStatusFinalized && cart.Status != CartStatusCheckedOut {
			return ErrCheckoutCartInvariant
		}
		if session.Status == CheckoutSessionStatusOpen && cart.Status != CartStatusActive {
			return ErrConflict
		}
		if session.Status == CheckoutSessionStatusExpired || (session.Status == CheckoutSessionStatusOpen && !session.ExpiresAt.After(decisionNow)) {
			return ErrCheckoutExpired
		}
		if session.Status != CheckoutSessionStatusOpen && session.Status != CheckoutSessionStatusFinalized {
			return ErrCheckoutExpired
		}
		if len(session.GuestOrderAccessTokenDigest) != sha256.Size {
			return ErrInvalidInput
		}
		if err := loadCartItems(ctx, tx, &cart); err != nil {
			return err
		}
		fingerprint, err := ComputeFinalizeFingerprint(session, cart, request)
		if err != nil {
			return err
		}
		decision = CheckoutDecision{SessionID: session.ID, Status: session.Status, Fingerprint: fingerprint}
		if session.Status == CheckoutSessionStatusFinalized {
			if session.FinalizeFingerprint == nil || *session.FinalizeFingerprint != fingerprint {
				return ErrIdempotencyConflict
			}
			decision.Replay = true
		}
		return nil
	})
	return decision, err
}

var (
	testHookBeforeFinalizeCommit      func(ctx context.Context) error
	testHookAfterCommercialAcceptance func(ctx context.Context) error
)

type txPIDKey struct{}

func WithTxPIDNotifier(ctx context.Context, ch chan<- int32) context.Context {
	return context.WithValue(ctx, txPIDKey{}, ch)
}

func checkedMultiply(a, b int64) (int64, error) {
	if a == 0 || b == 0 {
		return 0, nil
	}
	res := a * b
	if res/b != a {
		return 0, ErrInvalidInput
	}
	return res, nil
}

func checkedAdd(a, b int64) (int64, error) {
	res := a + b
	if (b > 0 && res < a) || (b < 0 && res > a) {
		return 0, ErrInvalidInput
	}
	return res, nil
}

func (r Repository) FinalizeCheckout(ctx context.Context, storeID string, request FinalizeRequest, correlationID string) (Order, error) {
	if strings.TrimSpace(storeID) == "" || strings.TrimSpace(request.SessionID) == "" {
		return Order{}, ErrInvalidInput
	}

	var finalizedOrder Order
	err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if ch, ok := ctx.Value(txPIDKey{}).(chan<- int32); ok && ch != nil {
			var pid int32
			if err := tx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err == nil {
				ch <- pid
			}
		}

		// 1. Lock Checkout Session FOR UPDATE (by session_id and store_id)
		var session CheckoutSession
		if err := tx.QueryRow(ctx, `
			SELECT id, store_id, cart_id, customer_id, status, expires_at, shipping_address_snapshot, contact_email, finalize_fingerprint, guest_order_access_token_digest, finalized_at, created_at, updated_at
			FROM checkout_sessions
			WHERE id = $1 AND store_id = $2
			FOR UPDATE
		`, request.SessionID, storeID).Scan(checkoutSessionScanArgs(&session)...); err != nil {
			return translatePGError(err, "lock checkout session")
		}

		// 2. Lock Parent Cart FOR UPDATE (by cart_id and store_id)
		var cart Cart
		if err := tx.QueryRow(ctx, `
			SELECT id, store_id, market_code, customer_id, status, expires_at, created_at, updated_at
			FROM carts
			WHERE id = $1 AND store_id = $2
			FOR UPDATE
		`, session.CartID, session.StoreID).Scan(&cart.ID, &cart.StoreID, &cart.MarketCode, &cart.CustomerID, &cart.Status, &cart.ExpiresAt, &cart.CreatedAt, &cart.UpdatedAt); err != nil {
			return translatePGError(err, "lock checkout cart")
		}

		// 3. Capture Session decision timestamp AFTER both critical locks
		var sessionDecisionNow time.Time
		if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&sessionDecisionNow); err != nil {
			return fmt.Errorf("capture checkout session decision time: %w", err)
		}
		sessionDecisionNow = sessionDecisionNow.UTC()

		if err := ensureCheckoutAcceptingTx(ctx, tx, storeID); err != nil {
			return err
		}

		// 4. Session / Cart State Rules
		if session.Status == CheckoutSessionStatusFinalized && cart.Status != CartStatusCheckedOut {
			return ErrCheckoutCartInvariant
		}
		if session.Status == CheckoutSessionStatusOpen && cart.Status != CartStatusActive {
			return ErrConflict
		}
		if session.Status == CheckoutSessionStatusExpired || (session.Status == CheckoutSessionStatusOpen && !session.ExpiresAt.After(sessionDecisionNow)) {
			return ErrCheckoutExpired
		}
		if session.Status != CheckoutSessionStatusOpen && session.Status != CheckoutSessionStatusFinalized {
			return ErrCheckoutExpired
		}

		// 5. Load Cart Items deterministically (by seller_listing_id ASC, sku_id ASC)
		if err := loadCartItems(ctx, tx, &cart); err != nil {
			return err
		}
		if len(cart.Items) == 0 {
			return ErrInvalidInput
		}
		for _, item := range cart.Items {
			if strings.TrimSpace(item.SellerListingID) == "" || strings.TrimSpace(item.SKUID) == "" || item.Quantity <= 0 || item.ExpectedUnitPriceMinor < 0 || strings.TrimSpace(item.ExpectedCurrencyCode) == "" {
				return ErrInvalidInput
			}
		}

		// 6. Compute Server Fingerprint
		fingerprint, err := ComputeFinalizeFingerprint(session, cart, request)
		if err != nil {
			return err
		}

		// 7. Finalized Replay
		if session.Status == CheckoutSessionStatusFinalized {
			if session.FinalizeFingerprint == nil || *session.FinalizeFingerprint != fingerprint {
				return ErrIdempotencyConflict
			}
			var existingOrderID string
			err := tx.QueryRow(ctx, `SELECT id FROM orders WHERE checkout_session_id = $1`, session.ID).Scan(&existingOrderID)
			if err != nil {
				return translatePGError(err, "get order for finalized session replay")
			}
			order, err := r.GetOrderByID(ctx, tx, storeID, existingOrderID)
			if err != nil {
				return err
			}
			finalizedOrder = order
			return nil
		}

		// 8. Validate Guest Finalization Payload
		if strings.TrimSpace(request.ShippingAddress.RecipientName) == "" ||
			strings.TrimSpace(request.ShippingAddress.AddressLine1) == "" ||
			strings.TrimSpace(request.ShippingAddress.City) == "" ||
			len(strings.TrimSpace(request.ShippingAddress.CountryCode)) != 2 ||
			strings.TrimSpace(request.ContactEmail) == "" {
			return ErrInvalidInput
		}

		// 9. Require Pre-Issued Guest Capability
		if len(session.GuestOrderAccessTokenDigest) != sha256.Size {
			return ErrInvalidInput
		}

		// 10. Preliminary Candidate Discovery & 11. Candidate Inventory Locking
		var candidateSnapshotIDs []string
		snapIDMap := make(map[string]struct{})

		for _, item := range cart.Items {
			rows, err := tx.Query(ctx, `
				SELECT s.id
				FROM inventory_snapshots s
				JOIN fulfillment_locations loc ON loc.id = s.fulfillment_location_id
				JOIN seller_listings sl ON sl.id = $1
				WHERE s.sku_id = $2
				  AND loc.status = 'active'
				  AND loc.market_code = $3
				  AND (
				      (sl.supplier_offer_id IS NULL AND loc.store_id = sl.store_id AND loc.supplier_id IS NULL)
				      OR (sl.supplier_offer_id IS NOT NULL AND loc.store_id IS NULL AND loc.supplier_id = (SELECT supplier_id FROM supplier_offers WHERE id = sl.supplier_offer_id))
				  )
			`, item.SellerListingID, item.SKUID, cart.MarketCode)
			if err != nil {
				return translatePGError(err, "discover candidate snapshots")
			}
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err != nil {
					rows.Close()
					return translatePGError(err, "scan candidate snapshot id")
				}
				if _, exists := snapIDMap[id]; !exists {
					snapIDMap[id] = struct{}{}
					candidateSnapshotIDs = append(candidateSnapshotIDs, id)
				}
			}
			rows.Close()
		}

		// Lock candidate Snapshots globally by ID ASC
		lockedSnapshots, err := lockSnapshotsByIDs(ctx, tx, candidateSnapshotIDs)
		if err != nil {
			return err
		}

		// 12. Lock / Allocate Store Order Sequence row
		orderNumber, err := r.AllocateOrderNumber(ctx, tx, storeID)
		if err != nil {
			return err
		}

		// 13. Final Authoritative Commercial Revalidation
		var storeStatus, storeMarketCode, storeMarketCurrency string
		if err := tx.QueryRow(ctx, `
			SELECT s.status, s.market_code, m.currency_code
			FROM stores s
			JOIN markets m ON m.code = s.market_code
			WHERE s.id = $1
		`, storeID).Scan(&storeStatus, &storeMarketCode, &storeMarketCurrency); err != nil {
			return translatePGError(err, "revalidate store")
		}
		if storeStatus != "active" || storeMarketCode != cart.MarketCode {
			return ErrListingUnavailable
		}

		type lineValidation struct {
			cartItem                 CartItem
			sellerListingID          string
			productID                string
			variantID                string
			supplierOfferID          *string
			sourceSupplierID         *string
			productTitle             string
			skuCode                  string
			retailPriceMinor         int64
			retailCurrencyCode       string
			supplierCostMinor        *int64
			supplierCostCurrencyCode *string
			eligibleCandidateSnaps   []InventorySnapshot
		}

		lineValidations := make([]lineValidation, 0, len(cart.Items))
		var subtotalMinor int64

		for _, item := range cart.Items {
			var lv lineValidation
			lv.cartItem = item

			var slStore_id, slProduct_id, slMarketCode, slStatus string
			var slSupplierOfferID *string
			var slpAmountMinor int64
			var slpCurrencyCode string

			err := tx.QueryRow(ctx, `
				SELECT sl.store_id, sl.product_id, sl.supplier_offer_id, sl.market_code, sl.status, slp.amount_minor, slp.currency_code
				FROM seller_listings sl
				JOIN seller_listing_prices slp ON slp.seller_listing_id = sl.id AND slp.is_current = true
				WHERE sl.id = $1 AND sl.store_id = $2
			`, item.SellerListingID, storeID).Scan(&slStore_id, &slProduct_id, &slSupplierOfferID, &slMarketCode, &slStatus, &slpAmountMinor, &slpCurrencyCode)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return ErrListingUnavailable
				}
				return translatePGError(err, "revalidate listing")
			}

			if (slStatus != "published" && slStatus != "active") || slStore_id != storeID || slMarketCode != cart.MarketCode {
				return ErrListingUnavailable
			}
			if slpAmountMinor != item.ExpectedUnitPriceMinor {
				return ErrPriceChanged
			}
			if slpCurrencyCode != item.ExpectedCurrencyCode {
				return ErrPriceChanged
			}
			if slpCurrencyCode != storeMarketCurrency {
				return ErrMarketMismatch
			}

			lv.sellerListingID = item.SellerListingID
			lv.productID = slProduct_id
			lv.supplierOfferID = slSupplierOfferID
			lv.retailPriceMinor = slpAmountMinor
			lv.retailCurrencyCode = slpCurrencyCode

			// Product / Variant / SKU status revalidation
			var pStatus, pSlug string
			err = tx.QueryRow(ctx, `SELECT status, slug FROM products WHERE id = $1`, slProduct_id).Scan(&pStatus, &pSlug)
			if err != nil || pStatus != "active" {
				return ErrListingUnavailable
			}

			var pName string
			_ = tx.QueryRow(ctx, `SELECT name FROM product_translations WHERE product_id = $1 ORDER BY locale ASC LIMIT 1`, slProduct_id).Scan(&pName)
			if strings.TrimSpace(pName) == "" {
				pName = pSlug
			}
			lv.productTitle = pName

			var vID, vStatus, vProductID, kStatus, kCode string
			err = tx.QueryRow(ctx, `
				SELECT v.id, v.status, v.product_id, k.status, k.code
				FROM skus k
				JOIN variants v ON v.id = k.variant_id
				WHERE k.id = $1
			`, item.SKUID).Scan(&vID, &vStatus, &vProductID, &kStatus, &kCode)
			if err != nil || vStatus != "active" || vProductID != slProduct_id || kStatus != "active" {
				return ErrListingUnavailable
			}
			lv.variantID = vID
			lv.skuCode = kCode

			// Supplier-backed rules
			if slSupplierOfferID != nil {
				var soSupplierID, soStatus, soMarketCode string
				var spID *string
				err := tx.QueryRow(ctx, `
					SELECT so.supplier_id, so.status, so.market_code, sp.id
					FROM supplier_offers so
					JOIN supplier_products sp ON sp.id = so.supplier_product_id AND sp.supplier_id = so.supplier_id AND sp.product_id = $2
					WHERE so.id = $1
				`, *slSupplierOfferID, slProduct_id).Scan(&soSupplierID, &soStatus, &soMarketCode, &spID)
				if err != nil || soStatus != "active" || soMarketCode != cart.MarketCode || spID == nil {
					return ErrListingUnavailable
				}
				lv.sourceSupplierID = &soSupplierID

				// Supplier offer price
				var sopAmountMinor int64
				var sopCurrencyCode string
				err = tx.QueryRow(ctx, `
					SELECT amount_minor, currency_code
					FROM supplier_offer_prices
					WHERE supplier_offer_id = $1 AND is_current = true
				`, *slSupplierOfferID).Scan(&sopAmountMinor, &sopCurrencyCode)
				if err != nil {
					return ErrListingUnavailable
				}
				if sopCurrencyCode != storeMarketCurrency {
					return ErrMarketMismatch
				}
				lv.supplierCostMinor = &sopAmountMinor
				lv.supplierCostCurrencyCode = &sopCurrencyCode

				// Supplier offer availability check
				var isAvailable bool
				err = tx.QueryRow(ctx, `
					SELECT is_available FROM supplier_offer_availability WHERE supplier_offer_id = $1
				`, *slSupplierOfferID).Scan(&isAvailable)
				if err != nil {
					if !errors.Is(err, pgx.ErrNoRows) {
						return translatePGError(err, "revalidate supplier offer availability")
					}
				} else if !isAvailable {
					return ErrListingUnavailable
				}
			}

			// Filter locked candidate snapshots for this item line
			var lineCandidates []InventorySnapshot
			for _, snap := range lockedSnapshots {
				if snap.SKUID != item.SKUID {
					continue
				}
				var locStatus string
				var locStoreID, locSupplierID *string
				var locMarketCode string
				err := tx.QueryRow(ctx, `SELECT status, store_id, supplier_id, market_code FROM fulfillment_locations WHERE id = $1`, snap.FulfillmentLocationID).Scan(&locStatus, &locStoreID, &locSupplierID, &locMarketCode)
				if err != nil || locStatus != "active" || locMarketCode != cart.MarketCode {
					continue
				}
				if slSupplierOfferID != nil {
					if locStoreID != nil || locSupplierID == nil || *locSupplierID != *lv.sourceSupplierID {
						continue
					}
				} else {
					if locStoreID == nil || *locStoreID != storeID || locSupplierID != nil {
						continue
					}
				}
				lineCandidates = append(lineCandidates, snap)
			}
			if len(lineCandidates) == 0 {
				return ErrInsufficientInventory
			}
			lv.eligibleCandidateSnaps = lineCandidates

			// Money arithmetic for line
			lineTotal, err := checkedMultiply(item.ExpectedUnitPriceMinor, item.Quantity)
			if err != nil || lineTotal < 0 {
				return ErrInvalidInput
			}
			subtotalMinor, err = checkedAdd(subtotalMinor, lineTotal)
			if err != nil || subtotalMinor < 0 {
				return ErrInvalidInput
			}

			lineValidations = append(lineValidations, lv)
		}

		// 18. Deterministic One-Location-Per-Line Allocation & 19. Cumulative Multi-Line Demand
		cumulativeAllocations := make(map[string]int64)
		type lineAllocation struct {
			validation     lineValidation
			selectedSnap   InventorySnapshot
			lineTotalMinor int64
		}
		allocations := make([]lineAllocation, 0, len(lineValidations))

		for _, lv := range lineValidations {
			selectedSnap, err := AllocateLineSnapshot(lv.eligibleCandidateSnaps, lv.cartItem.Quantity, cumulativeAllocations)
			if err != nil {
				return err
			}
			lineTotal, _ := checkedMultiply(lv.cartItem.ExpectedUnitPriceMinor, lv.cartItem.Quantity)
			cumulativeAllocations[selectedSnap.ID] += lv.cartItem.Quantity
			allocations = append(allocations, lineAllocation{
				validation:     lv,
				selectedSnap:   selectedSnap,
				lineTotalMinor: lineTotal,
			})
		}

		// 21. Capture Order Acceptance Timestamp AFTER final revalidation & snapshot locks
		var orderCreatedAt time.Time
		if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&orderCreatedAt); err != nil {
			return fmt.Errorf("capture order acceptance timestamp: %w", err)
		}
		orderCreatedAt = orderCreatedAt.UTC()

		if testHookAfterCommercialAcceptance != nil {
			if err := testHookAfterCommercialAcceptance(ctx); err != nil {
				return err
			}
		}

		// 22. Confirmation Deadline
		duration := r.OrderConfirmationDuration
		if duration <= 0 {
			duration = DefaultConfirmationDuration
		}
		confirmationDeadlineAt := orderCreatedAt.Add(duration)

		// 27. Create Held Reservations for each line
		orderItems := make([]OrderItem, 0, len(allocations))
		for _, alloc := range allocations {
			resParams := HoldReservationParams{
				SnapshotID:             alloc.selectedSnap.ID,
				Quantity:               alloc.validation.cartItem.Quantity,
				ReservationToken:       uuid.NewString(),
				ConfirmationDeadlineAt: confirmationDeadlineAt,
				DecisionNow:            orderCreatedAt,
			}
			reservation, err := r.HoldReservation(ctx, tx, resParams)
			if err != nil {
				return err
			}

			orderItem := OrderItem{
				ID:                       uuid.NewString(),
				SellerListingID:          &alloc.validation.sellerListingID,
				ProductID:                &alloc.validation.productID,
				VariantID:                &alloc.validation.variantID,
				SKUID:                    &alloc.validation.cartItem.SKUID,
				SupplierOfferID:          alloc.validation.supplierOfferID,
				SourceSupplierID:         alloc.validation.sourceSupplierID,
				FulfillmentLocationID:    alloc.selectedSnap.FulfillmentLocationID,
				InventoryReservationID:   reservation.ID,
				ProductTitleSnapshot:     alloc.validation.productTitle,
				SKUCodeSnapshot:          alloc.validation.skuCode,
				UnitPriceMinor:           alloc.validation.cartItem.ExpectedUnitPriceMinor,
				CurrencyCode:             alloc.validation.cartItem.ExpectedCurrencyCode,
				Quantity:                 alloc.validation.cartItem.Quantity,
				LineTotalMinor:           alloc.lineTotalMinor,
				SupplierCostMinor:        alloc.validation.supplierCostMinor,
				SupplierCostCurrencyCode: alloc.validation.supplierCostCurrencyCode,
				CreatedAt:                orderCreatedAt,
			}

			orderItems = append(orderItems, orderItem)
		}

		// 29. Create Order
		orderID := uuid.NewString()
		orderAddr := OrderAddress{
			ID:            uuid.NewString(),
			OrderID:       orderID,
			AddressType:   AddressTypeShipping,
			RecipientName: request.ShippingAddress.RecipientName,
			AddressLine1:  request.ShippingAddress.AddressLine1,
			AddressLine2:  request.ShippingAddress.AddressLine2,
			City:          request.ShippingAddress.City,
			Region:        request.ShippingAddress.Region,
			PostalCode:    request.ShippingAddress.PostalCode,
			CountryCode:   request.ShippingAddress.CountryCode,
			CreatedAt:     orderCreatedAt,
		}

		var orderAttribution *MarketplaceAttribution
		if request.Attribution != nil {
			attr := request.Attribution
			if strings.TrimSpace(attr.SellerListingID) == "" {
				return ErrInvalidInput
			}
			if attr.StoreID != "" && attr.StoreID != storeID {
				return ErrStoreMismatch
			}
			if attr.MarketCode != "" && strings.ToUpper(strings.TrimSpace(attr.MarketCode)) != cart.MarketCode {
				return ErrMarketMismatch
			}
			var matchesListing bool
			for _, item := range cart.Items {
				if item.SellerListingID == attr.SellerListingID {
					matchesListing = true
					break
				}
			}
			if !matchesListing {
				return ErrListingUnavailable
			}
			orderAttribution = &MarketplaceAttribution{
				ID:                uuid.NewString(),
				OrderID:           orderID,
				CheckoutSessionID: session.ID,
				StoreID:           storeID,
				MarketCode:        cart.MarketCode,
				SellerListingID:   attr.SellerListingID,
				SourceCollection:  attr.SourceCollection,
				CreatedAt:         orderCreatedAt,
			}
		}

		orderToCreate := Order{
			ID:                          orderID,
			OrderNumber:                 orderNumber,
			StoreID:                     storeID,
			MarketCode:                  cart.MarketCode,
			CustomerID:                  session.CustomerID,
			CheckoutSessionID:           session.ID,
			Status:                      OrderStatusPending,
			CurrencyCode:                storeMarketCurrency,
			GuestOrderAccessTokenDigest: session.GuestOrderAccessTokenDigest,
			SubtotalMinor:               subtotalMinor,
			TotalMinor:                  subtotalMinor,
			ConfirmationDeadlineAt:      confirmationDeadlineAt,
			AggregateVersion:            1,
			CreatedAt:                   orderCreatedAt,
			UpdatedAt:                   orderCreatedAt,
			Items:                       orderItems,
			Address:                     &orderAddr,
			Attribution:                 orderAttribution,
		}

		createdOrder, err := r.CreateOrder(ctx, tx, orderToCreate)
		if err != nil {
			return err
		}

		// 32. Initial Order Timeline
		timelineID := uuid.NewString()
		_, err = tx.Exec(ctx, `
			INSERT INTO order_timeline (id, order_id, from_status, to_status, actor_type, created_at)
			VALUES ($1, $2, NULL, $3, 'checkout', $4)
		`, timelineID, createdOrder.ID, OrderStatusPending, orderCreatedAt)
		if err != nil {
			return translatePGError(err, "insert order timeline for checkout")
		}

		// 33. OrderCreated Event & 35. Transactional Outbox
		envelope, err := NewOrderCreatedEvent(createdOrder, correlationID, "", orderCreatedAt)
		if err != nil {
			return err
		}
		if err := outbox.NewStore().Enqueue(ctx, tx, envelope); err != nil {
			return fmt.Errorf("enqueue OrderCreated event: %w", err)
		}

		// 36. Finalize Checkout Session
		addrJSON, err := json.Marshal(request.ShippingAddress)
		if err != nil {
			return fmt.Errorf("marshal shipping address snapshot: %w", err)
		}
		cmdTag, err := tx.Exec(ctx, `
			UPDATE checkout_sessions
			SET status = $1, finalize_fingerprint = $2, shipping_address_snapshot = $3, contact_email = $4, finalized_at = $5, updated_at = $5
			WHERE id = $6 AND store_id = $7 AND status = $8
		`, CheckoutSessionStatusFinalized, fingerprint, addrJSON, request.ContactEmail, orderCreatedAt, session.ID, storeID, CheckoutSessionStatusOpen)
		if err != nil {
			return translatePGError(err, "update checkout session to finalized")
		}
		if cmdTag.RowsAffected() != 1 {
			return ErrConflict
		}

		// 37. Checkout Cart
		cmdTag, err = tx.Exec(ctx, `
			UPDATE carts
			SET status = $1, updated_at = $2
			WHERE id = $3 AND store_id = $4 AND status = $5
		`, CartStatusCheckedOut, orderCreatedAt, cart.ID, storeID, CartStatusActive)
		if err != nil {
			return translatePGError(err, "update cart to checked_out")
		}
		if cmdTag.RowsAffected() != 1 {
			return ErrConflict
		}

		if testHookBeforeFinalizeCommit != nil {
			if err := testHookBeforeFinalizeCommit(ctx); err != nil {
				return err
			}
		}

		finalizedOrder = createdOrder
		return nil
	})

	if err != nil {
		return Order{}, err
	}
	return finalizedOrder, nil
}
