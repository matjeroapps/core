package coreapi

// Feature 034 variant lifecycle HTTP tests: exercise the real internal Seller
// variant routes end to end (update with attribute values, reference-protected
// delete, idempotent archive) against the shared P5.8 contract env.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"core/internal/serviceauth"

	"github.com/google/uuid"

	"core/modules/commerce"
)

type variantLifecycleEnv struct {
	p58ContractEnv
	productID string
	colorID   string
	redID     string
	blueID    string
	sizeID    string
	smallID   string
}

func newVariantLifecycleEnv(t *testing.T) variantLifecycleEnv {
	t.Helper()
	env := variantLifecycleEnv{p58ContractEnv: setupP58Contract(t)}
	ctx := context.Background()
	for _, migration := range []string{
		"000041_catalog_variant_options_and_sku_specs.up.sql",
		"000045_product_variant_management.up.sql",
	} {
		content, err := os.ReadFile(filepath.Join("..", "..", "migrations", migration))
		if err != nil {
			t.Fatalf("read %s: %v", migration, err)
		}
		// Both migrations are idempotent (IF NOT EXISTS / guarded), so reapplying
		// alongside the P5.8 base fixture is safe.
		if _, err := env.db.Pool.Exec(ctx, string(content)); err != nil {
			t.Fatalf("apply %s: %v", migration, err)
		}
	}

	productRec := env.do(t, "POST", "/internal/v1/stores/"+env.storeID+"/products", StoreProductCreateRequest{
		Slug:         "variant-lifecycle-" + uuid.NewString(),
		Translations: []commerce.ProductTranslation{{Locale: "en", Name: "Variant Lifecycle"}},
	})
	if productRec.Code != 201 {
		t.Fatalf("create product status = %d, body %s", productRec.Code, productRec.Body.String())
	}
	var productBody struct {
		Product struct {
			ID string `json:"id"`
		} `json:"product"`
	}
	if err := json.Unmarshal(productRec.Body.Bytes(), &productBody); err != nil {
		t.Fatalf("decode product response: %v", err)
	}
	env.productID = productBody.Product.ID

	color, err := env.repo.CreateAttribute(ctx, "color-"+uuid.NewString(), "active")
	if err != nil {
		t.Fatalf("create color attribute: %v", err)
	}
	red, err := env.repo.CreateAttributeValue(ctx, color.ID, "red", "active")
	if err != nil {
		t.Fatalf("create red value: %v", err)
	}
	blue, err := env.repo.CreateAttributeValue(ctx, color.ID, "blue", "active")
	if err != nil {
		t.Fatalf("create blue value: %v", err)
	}
	size, err := env.repo.CreateAttribute(ctx, "size-"+uuid.NewString(), "active")
	if err != nil {
		t.Fatalf("create size attribute: %v", err)
	}
	small, err := env.repo.CreateAttributeValue(ctx, size.ID, "small", "active")
	if err != nil {
		t.Fatalf("create small value: %v", err)
	}
	env.colorID, env.redID, env.blueID, env.sizeID, env.smallID = color.ID, red.ID, blue.ID, size.ID, small.ID

	return env
}

func seedVariantHTTPReference(t *testing.T, env variantLifecycleEnv, skuID string) {
	t.Helper()
	locID := uuid.NewString()
	if _, err := env.db.Pool.Exec(context.Background(), `
		INSERT INTO fulfillment_locations (id, store_id, market_code, code, name, location_type, status)
		VALUES ($1, $2, 'EG', $3, 'Loc', 'warehouse', 'active')
	`, locID, env.storeID, "loc-"+uuid.NewString()); err != nil {
		t.Fatalf("seed fulfillment location: %v", err)
	}
	if _, err := env.db.Pool.Exec(context.Background(), `
		INSERT INTO inventory_snapshots (id, sku_id, fulfillment_location_id, on_hand_qty, reserved_qty, version)
		VALUES ($1, $2, $3, 10, 0, 1)
	`, uuid.NewString(), skuID, locID); err != nil {
		t.Fatalf("seed inventory snapshot: %v", err)
	}
}

func TestUpdateVariantWithAttributesHTTP(t *testing.T) {
	env := newVariantLifecycleEnv(t)
	ctx := context.Background()

	created, _, err := env.service.CreateVariantWithAttributesAndSKU(ctx, p58Subject, env.storeID, env.productID, "red-small", "active", "SKU-RED-S", "", []commerce.VariantAttributeMapping{{AttributeID: env.colorID, AttributeValueID: env.redID}, {AttributeID: env.sizeID, AttributeValueID: env.smallID}}, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("create variant: %v", err)
	}

	// Unreferenced update through the real handler replaces the combination.
	rec := env.do(t, "PUT", variantPath(env.storeID, env.productID, created.ID), VariantCreateRequest{
		Code:   "blue-small",
		Status: "active",
		AttributeValues: []commerce.VariantAttributeMapping{
			{AttributeID: env.colorID, AttributeValueID: env.blueID},
			{AttributeID: env.sizeID, AttributeValueID: env.smallID},
		},
	})
	if rec.Code != 200 {
		t.Fatalf("unreferenced variant update status = %d, body %s", rec.Code, rec.Body.String())
	}
	var updated commerce.VariantWithDetails
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode update response: %v", err)
	}
	if updated.Code != "blue-small" || len(updated.AttributeValues) != 2 {
		t.Fatalf("unreferenced update response = %+v", updated)
	}
	for _, detail := range updated.AttributeValues {
		if detail.AttributeID == env.colorID && detail.AttributeValueID == env.redID {
			t.Fatalf("old combination still present after HTTP update: %+v", updated.AttributeValues)
		}
	}

	// Referenced update returns the actionable 409 conflict envelope.
	var skuID string
	if err := env.db.Pool.QueryRow(ctx, `SELECT id FROM skus WHERE variant_id = $1`, created.ID).Scan(&skuID); err != nil {
		t.Fatalf("query sku: %v", err)
	}
	seedVariantHTTPReference(t, env, skuID)
	rec = env.do(t, "PUT", variantPath(env.storeID, env.productID, created.ID), VariantCreateRequest{
		Code:   "red-small-again",
		Status: "active",
		AttributeValues: []commerce.VariantAttributeMapping{
			{AttributeID: env.colorID, AttributeValueID: env.redID},
			{AttributeID: env.sizeID, AttributeValueID: env.smallID},
		},
	})
	if rec.Code != 409 {
		t.Fatalf("referenced variant update status = %d, body %s", rec.Code, rec.Body.String())
	}
	var errBody struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	if errBody.Error.Code != "conflict" || errBody.Error.Message == "" {
		t.Fatalf("referenced update envelope = %+v, want actionable conflict", errBody.Error)
	}
}

func TestDeleteAndArchiveVariantHTTP(t *testing.T) {
	env := newVariantLifecycleEnv(t)
	ctx := context.Background()

	created, _, err := env.service.CreateVariantWithAttributesAndSKU(ctx, p58Subject, env.storeID, env.productID, "red-small", "active", "SKU-RED-S", "", []commerce.VariantAttributeMapping{{AttributeID: env.colorID, AttributeValueID: env.redID}}, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("create variant: %v", err)
	}
	var skuID string
	if err := env.db.Pool.QueryRow(ctx, `SELECT id FROM skus WHERE variant_id = $1`, created.ID).Scan(&skuID); err != nil {
		t.Fatalf("query sku: %v", err)
	}
	seedVariantHTTPReference(t, env, skuID)

	path := variantPath(env.storeID, env.productID, created.ID)
	if rec := env.do(t, "DELETE", path, nil); rec.Code != 409 {
		t.Fatalf("referenced variant delete status = %d, body %s", rec.Code, rec.Body.String())
	}
	var count int
	if err := env.db.Pool.QueryRow(ctx, `SELECT count(*) FROM variants WHERE id = $1`, created.ID).Scan(&count); err != nil {
		t.Fatalf("query variant after blocked delete: %v", err)
	}
	if count != 1 {
		t.Fatalf("blocked delete removed variant rows: %d", count)
	}

	for attempt := 1; attempt <= 2; attempt++ {
		rec := env.do(t, "POST", path+"/archive", nil)
		if rec.Code != 200 {
			t.Fatalf("archive attempt %d status = %d, body %s", attempt, rec.Code, rec.Body.String())
		}
	}
	var variantStatus, skuStatus string
	if err := env.db.Pool.QueryRow(ctx, `SELECT status FROM variants WHERE id = $1`, created.ID).Scan(&variantStatus); err != nil {
		t.Fatalf("query variant after archive: %v", err)
	}
	if err := env.db.Pool.QueryRow(ctx, `SELECT status FROM skus WHERE id = $1`, skuID).Scan(&skuStatus); err != nil {
		t.Fatalf("query sku after archive: %v", err)
	}
	if variantStatus != "archived" || skuStatus != "inactive" {
		t.Fatalf("archive result variant=%q sku=%q", variantStatus, skuStatus)
	}
	if err := env.db.Pool.QueryRow(ctx, `SELECT count(*) FROM skus WHERE variant_id = $1`, created.ID).Scan(&count); err != nil {
		t.Fatalf("count historical skus: %v", err)
	}
	if count != 1 {
		t.Fatalf("archive dropped historical sku rows: %d", count)
	}
}

func variantPath(storeID, productID, variantID string) string {
	return "/internal/v1/stores/" + storeID + "/products/" + productID + "/variants/" + variantID
}

// variantDoAs mirrors p58ContractEnv.do but controls the forwarded subject
// header explicitly, so the same route can be exercised with a missing
// subject, a foreign subject, or a non-manager member.
func (e variantLifecycleEnv) variantDoAs(t *testing.T, subject, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set(serviceauth.HeaderService, "seller")
	req.Header.Set("Authorization", "Bearer "+testSellerToken)
	if subject != "" {
		req.Header.Set(serviceauth.HeaderSubject, subject)
	}
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	return rec
}

func decodeErrorEnvelope(t *testing.T, rec *httptest.ResponseRecorder) (code, message string) {
	t.Helper()
	var envelope struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode error envelope: %v (body %q)", err, rec.Body.String())
	}
	return envelope.Error.Code, envelope.Error.Message
}

func TestVariantRouteAuthorizationHTTP(t *testing.T) {
	env := newVariantLifecycleEnv(t)
	ctx := context.Background()

	var sellerID string
	if err := env.db.Pool.QueryRow(ctx, `SELECT seller_id FROM stores WHERE id = $1`, env.storeID).Scan(&sellerID); err != nil {
		t.Fatalf("query seller id: %v", err)
	}
	staffSubject := "subject-staff-" + uuid.NewString()
	if _, err := env.repo.CreateSellerMember(ctx, sellerID, staffSubject, "staff", "active"); err != nil {
		t.Fatalf("create staff member: %v", err)
	}
	createPath := "/internal/v1/stores/" + env.storeID + "/products/" + env.productID + "/variants"
	body := VariantCreateRequest{Code: "red-small", Status: "active"}

	// Missing subject header: the handler must refuse before touching data.
	rec := env.variantDoAs(t, "", "POST", createPath, body)
	if rec.Code != 401 {
		t.Fatalf("missing subject status = %d, body %s", rec.Code, rec.Body.String())
	}
	if code, _ := decodeErrorEnvelope(t, rec); code != "unauthorized" {
		t.Fatalf("missing subject envelope code = %q, want unauthorized", code)
	}

	// A subject that is not a member of any seller is forbidden, not found.
	outsider := "subject-outsider-" + uuid.NewString()
	rec = env.variantDoAs(t, outsider, "POST", createPath, body)
	if rec.Code != 403 {
		t.Fatalf("outsider subject status = %d, body %s", rec.Code, rec.Body.String())
	}
	if code, _ := decodeErrorEnvelope(t, rec); code != "forbidden" {
		t.Fatalf("outsider envelope code = %q, want forbidden", code)
	}

	// A staff member of the correct seller is not a seller manager: the
	// variant write must be refused with 403 on every mutating route.
	for _, tc := range []struct {
		name, method, path string
		body               any
	}{
		{"create", "POST", createPath, VariantCreateRequest{Code: "red-small", Status: "active"}},
		{"update", "PUT", variantPath(env.storeID, env.productID, uuid.NewString()), VariantCreateRequest{Code: "red-small", Status: "active"}},
		{"delete", "DELETE", variantPath(env.storeID, env.productID, uuid.NewString()), nil},
		{"archive", "POST", variantPath(env.storeID, env.productID, uuid.NewString()) + "/archive", nil},
		{"create-sku", "POST", variantPath(env.storeID, env.productID, uuid.NewString()) + "/skus", SKUCreateRequest{Code: "SKU-X", Status: "active"}},
	} {
		rec := env.variantDoAs(t, staffSubject, tc.method, tc.path, tc.body)
		if rec.Code != 403 {
			t.Fatalf("%s by staff member status = %d, body %s", tc.name, rec.Code, rec.Body.String())
		}
		if code, _ := decodeErrorEnvelope(t, rec); code != "forbidden" {
			t.Fatalf("%s by staff member envelope code = %q, want forbidden", tc.name, code)
		}
	}
	var variantCount int
	if err := env.db.Pool.QueryRow(ctx, `SELECT count(*) FROM variants WHERE product_id = $1`, env.productID).Scan(&variantCount); err != nil {
		t.Fatalf("count variants: %v", err)
	}
	if variantCount != 0 {
		t.Fatalf("rejected subjects must not write variants, found %d", variantCount)
	}
}

func TestVariantRoutePathIsolationHTTP(t *testing.T) {
	env := newVariantLifecycleEnv(t)
	ctx := context.Background()

	// The same store-owned product addressed via another seller's store is
	// equally out of scope: the path must isolate per seller and store, and the
	// subject belongs to yet another seller, so the resolved answer is 404
	// rather than a seller-identity leak. (A second active store for seller A
	// is blocked by the store entitlement limit.)
	otherSeller, err := env.repo.CreateSeller(ctx, "seller-"+uuid.NewString(), "Other Seller", "active", nil)
	if err != nil {
		t.Fatalf("create second seller: %v", err)
	}
	otherStore, err := env.repo.CreateStore(ctx, otherSeller.ID, "EG", "store-"+uuid.NewString(), "Other Store", "active", nil)
	if err != nil {
		t.Fatalf("create second store: %v", err)
	}
	productRec := env.do(t, "POST", "/internal/v1/stores/"+env.storeID+"/products", StoreProductCreateRequest{
		Slug:         "variant-isolation-" + uuid.NewString(),
		Translations: []commerce.ProductTranslation{{Locale: "en", Name: "Variant Isolation"}},
	})
	if productRec.Code != 201 {
		t.Fatalf("create second product status = %d, body %s", productRec.Code, productRec.Body.String())
	}
	var productBody struct {
		Product struct {
			ID string `json:"id"`
		} `json:"product"`
	}
	if err := json.Unmarshal(productRec.Body.Bytes(), &productBody); err != nil {
		t.Fatalf("decode second product response: %v", err)
	}
	otherProductID := productBody.Product.ID

	created, _, err := env.service.CreateVariantWithAttributesAndSKU(ctx, p58Subject, env.storeID, env.productID, "red-small", "active", "SKU-RED-S", "", []commerce.VariantAttributeMapping{{AttributeID: env.colorID, AttributeValueID: env.redID}}, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("create variant: %v", err)
	}

	// Variant of product A addressed under product B of the same store: 404.
	rec := env.do(t, "PUT", variantPath(env.storeID, otherProductID, created.ID), VariantCreateRequest{Code: "blue-small", Status: "active"})
	if rec.Code != 404 {
		t.Fatalf("wrong product path status = %d, body %s", rec.Code, rec.Body.String())
	}
	if code, _ := decodeErrorEnvelope(t, rec); code != "not_found" {
		t.Fatalf("wrong product envelope code = %q, want not_found", code)
	}
	if rec := env.do(t, "DELETE", variantPath(env.storeID, otherProductID, created.ID), nil); rec.Code != 404 {
		t.Fatalf("wrong product delete status = %d, body %s", rec.Code, rec.Body.String())
	}

	// The same store-owned product addressed via a second store of the same
	// seller is equally out of scope: the path must isolate per store.
	for _, method := range []string{"PUT", "DELETE"} {
		path := variantPath(otherStore.ID, env.productID, created.ID)
		rec := env.variantDoAs(t, p58Subject, method, path, map[string]string{"code": "blue-small", "status": "active"})
		if rec.Code != 404 {
			t.Fatalf("%s wrong store path status = %d, body %s", method, rec.Code, rec.Body.String())
		}
		if code, _ := decodeErrorEnvelope(t, rec); code != "not_found" {
			t.Fatalf("%s wrong store envelope code = %q, want not_found", method, code)
		}
	}
	rec = env.do(t, "POST", variantPath(otherStore.ID, env.productID, created.ID)+"/archive", nil)
	if rec.Code != 404 {
		t.Fatalf("archive wrong store path status = %d, body %s", rec.Code, rec.Body.String())
	}

	// Every refused path change must leave the variant untouched.
	var code, status string
	if err := env.db.Pool.QueryRow(ctx, `SELECT code, status FROM variants WHERE id = $1`, created.ID).Scan(&code, &status); err != nil {
		t.Fatalf("query variant after path isolation attempts: %v", err)
	}
	if code != "red-small" || status != "active" {
		t.Fatalf("path-mismatched requests mutated the variant: %q/%q", code, status)
	}
}

// seedVariantOrderReference builds the minimal live-order fixture (cart →
// checkout session → order → reservation → order item) that pins an order_item
// row to the variant's SKU, mirroring the production order aggregate schema.
func seedVariantOrderReference(t *testing.T, env variantLifecycleEnv, skuID string) (orderID, orderItemID string) {
	t.Helper()
	ctx := context.Background()

	var snapshotID, locID string
	if err := env.db.Pool.QueryRow(ctx, `SELECT id, fulfillment_location_id FROM inventory_snapshots WHERE sku_id = $1 ORDER BY created_at DESC LIMIT 1`, skuID).Scan(&snapshotID, &locID); err != nil {
		t.Fatalf("query snapshot for order fixture: %v", err)
	}
	reservationID := uuid.NewString()
	if _, err := env.db.Pool.Exec(ctx, `
		INSERT INTO inventory_reservations (id, inventory_snapshot_id, quantity, status, reservation_token)
		VALUES ($1, $2, 1, 'active', $3)
	`, reservationID, snapshotID, "res-token-"+reservationID); err != nil {
		t.Fatalf("seed inventory reservation: %v", err)
	}

	cartID := uuid.NewString()
	if _, err := env.db.Pool.Exec(ctx, `
		INSERT INTO carts (id, store_id, market_code, cart_token_digest, status)
		VALUES ($1, $2, 'EG', $3, 'active')
	`, cartID, env.storeID, "token-"+cartID); err != nil {
		t.Fatalf("seed cart: %v", err)
	}
	sessionID := uuid.NewString()
	digest := make([]byte, 32)
	if _, err := env.db.Pool.Exec(ctx, `
		INSERT INTO checkout_sessions (id, store_id, cart_id, status, expires_at, guest_order_access_token_digest)
		VALUES ($1, $2, $3, 'open', $4, $5)
	`, sessionID, env.storeID, cartID, time.Now().Add(time.Hour), digest); err != nil {
		t.Fatalf("seed checkout session: %v", err)
	}
	orderID = uuid.NewString()
	if _, err := env.db.Pool.Exec(ctx, `
		INSERT INTO orders (id, order_number, store_id, market_code, checkout_session_id, status, currency_code, subtotal_minor, total_minor, confirmation_deadline_at, guest_order_access_token_digest)
		VALUES ($1, $2, $3, 'EG', $4, 'confirmed', 'EGP', 1000, 1000, $5, $6)
	`, orderID, "#"+uuid.NewString(), env.storeID, sessionID, time.Now().Add(time.Hour), digest); err != nil {
		t.Fatalf("seed order: %v", err)
	}
	orderItemID = uuid.NewString()
	if _, err := env.db.Pool.Exec(ctx, `
		INSERT INTO order_items (id, order_id, variant_id, sku_id, fulfillment_location_id, inventory_reservation_id, product_title_snapshot, sku_code_snapshot, unit_price_minor, currency_code, quantity, line_total_minor)
		VALUES ($1, $2, (SELECT variant_id FROM skus WHERE id = $3), $3, $4, $5, 'Variant Lifecycle', (SELECT code FROM skus WHERE id = $3), 500, 'EGP', 2, 1000)
	`, orderItemID, orderID, skuID, locID, reservationID); err != nil {
		t.Fatalf("seed order item: %v", err)
	}
	return orderID, orderItemID
}

func TestDeleteOrderReferencedVariantHTTP(t *testing.T) {
	env := newVariantLifecycleEnv(t)
	ctx := context.Background()

	created, _, err := env.service.CreateVariantWithAttributesAndSKU(ctx, p58Subject, env.storeID, env.productID, "red-small", "active", "SKU-RED-S", "", []commerce.VariantAttributeMapping{{AttributeID: env.colorID, AttributeValueID: env.redID}}, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("create variant: %v", err)
	}
	var skuID string
	if err := env.db.Pool.QueryRow(ctx, `SELECT id FROM skus WHERE variant_id = $1`, created.ID).Scan(&skuID); err != nil {
		t.Fatalf("query sku: %v", err)
	}
	// The invariant repository treats inventory and order references alike, so
	// the snapshot also present here keeps both reference surfaces loaded.
	seedVariantHTTPReference(t, env, skuID)
	seedVariantOrderReference(t, env, skuID)

	// A live order_item referencing the variant blocks the permanent delete
	// with the same resource_in_use contract as inventory references.
	rec := env.do(t, "DELETE", variantPath(env.storeID, env.productID, created.ID), nil)
	if rec.Code != 409 {
		t.Fatalf("order-referenced delete status = %d, body %s", rec.Code, rec.Body.String())
	}
	if code, _ := decodeErrorEnvelope(t, rec); code != "resource_in_use" {
		t.Fatalf("order-referenced delete envelope code = %q, want resource_in_use", code)
	}
	var variantCount, skuCount, itemCount int
	if err := env.db.Pool.QueryRow(ctx, `SELECT count(*) FROM variants WHERE id = $1`, created.ID).Scan(&variantCount); err != nil {
		t.Fatalf("count variants after blocked delete: %v", err)
	}
	if err := env.db.Pool.QueryRow(ctx, `SELECT count(*) FROM skus WHERE variant_id = $1`, created.ID).Scan(&skuCount); err != nil {
		t.Fatalf("count skus after blocked delete: %v", err)
	}
	if err := env.db.Pool.QueryRow(ctx, `SELECT count(*) FROM order_items WHERE variant_id = $1`, created.ID).Scan(&itemCount); err != nil {
		t.Fatalf("count order items after blocked delete: %v", err)
	}
	if variantCount != 1 || skuCount != 1 || itemCount != 1 {
		t.Fatalf("blocked delete must preserve variant, sku, and order history: variant=%d sku=%d order_items=%d", variantCount, skuCount, itemCount)
	}

	// Referenced archive stays available while the delete stays forbidden.
	if rec := env.do(t, "POST", variantPath(env.storeID, env.productID, created.ID)+"/archive", nil); rec.Code != 200 {
		t.Fatalf("order-referenced archive status = %d, body %s", rec.Code, rec.Body.String())
	}
	var variantStatus string
	if err := env.db.Pool.QueryRow(ctx, `SELECT status FROM variants WHERE id = $1`, created.ID).Scan(&variantStatus); err != nil {
		t.Fatalf("query variant status after archive: %v", err)
	}
	if variantStatus != "archived" {
		t.Fatalf("order-referenced archive status = %q, want archived", variantStatus)
	}
}

func TestVariantSKUReplaceActiveHTTP(t *testing.T) {
	env := newVariantLifecycleEnv(t)
	ctx := context.Background()

	created, _, err := env.service.CreateVariantWithAttributesAndSKU(ctx, p58Subject, env.storeID, env.productID, "red-small", "active", "SKU-OLD", "", []commerce.VariantAttributeMapping{{AttributeID: env.colorID, AttributeValueID: env.redID}}, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("create variant: %v", err)
	}
	otherProductRec := env.do(t, "POST", "/internal/v1/stores/"+env.storeID+"/products", StoreProductCreateRequest{
		Slug:         "variant-sku-" + uuid.NewString(),
		Translations: []commerce.ProductTranslation{{Locale: "en", Name: "Variant SKU"}},
	})
	var productBody struct {
		Product struct {
			ID string `json:"id"`
		} `json:"product"`
	}
	if err := json.Unmarshal(otherProductRec.Body.Bytes(), &productBody); err != nil {
		t.Fatalf("decode other product: %v", err)
	}
	skusPath := variantPath(env.storeID, env.productID, created.ID) + "/skus"

	// Creating an active SKU through the skus route replaces the previously
	// active one in one transaction: still exactly one active SKU.
	rec := env.do(t, "POST", skusPath, SKUCreateRequest{Code: "SKU-NEW", Status: "active"})
	if rec.Code != 201 {
		t.Fatalf("create replacement sku status = %d, body %s", rec.Code, rec.Body.String())
	}
	var newSKU commerce.SKU
	if err := json.Unmarshal(rec.Body.Bytes(), &newSKU); err != nil {
		t.Fatalf("decode new sku: %v", err)
	}
	if newSKU.Code != "SKU-NEW" || newSKU.Status != "active" {
		t.Fatalf("replacement sku = %+v", newSKU)
	}
	var skuCount, activeCount int
	if err := env.db.Pool.QueryRow(ctx, `SELECT count(*) FROM skus WHERE variant_id = $1`, created.ID).Scan(&skuCount); err != nil {
		t.Fatalf("count skus: %v", err)
	}
	if err := env.db.Pool.QueryRow(ctx, `SELECT count(*) FROM skus WHERE variant_id = $1 AND status = 'active'`, created.ID).Scan(&activeCount); err != nil {
		t.Fatalf("count active skus: %v", err)
	}
	if skuCount != 2 || activeCount != 1 {
		t.Fatalf("one-active invariant broken after replacement: total=%d active=%d", skuCount, activeCount)
	}

	// Store-scoped uniqueness still applies on this route: reusing the older
	// SKU code answers with the actionable store-catalog conflict.
	rec = env.do(t, "POST", skusPath, SKUCreateRequest{Code: "SKU-OLD", Status: "active"})
	if rec.Code != 409 {
		t.Fatalf("duplicate sku code status = %d, body %s", rec.Code, rec.Body.String())
	}
	if code, _ := decodeErrorEnvelope(t, rec); code != "conflict" {
		t.Fatalf("duplicate sku envelope code = %q, want conflict", code)
	}

	// A SKU route addressed at a variant of another product is 404.
	rec = env.do(t, "POST", variantPath(env.storeID, productBody.Product.ID, created.ID)+"/skus", SKUCreateRequest{Code: "SKU-OTHER", Status: "active"})
	if rec.Code != 404 {
		t.Fatalf("sku route wrong product status = %d, body %s", rec.Code, rec.Body.String())
	}
}
