package coreapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"core/internal/merchants"
	"core/internal/serviceauth"
	"core/internal/testdb"
	"core/modules/commerce"
)

func TestMerchantStoreCreateBootstrapsSellerProfileForRetailMerchant(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://commerce:commerce@localhost:5432/commerce?sslmode=disable"
	}
	db := testdb.Open(t, dsn)
	migrationNames := []string{
		"000001_event_delivery_foundation",
		"000002_market_reference_data",
		"000003_commerce_domain_schema",
		"000008_storefront_revisions",
		"000034_merchant_identity_foundation",
		"000035_seller_supplier_merchant_linkage",
		"000036_merchant_profile_cardinality",
	}
	migrationPaths := make([]string, 0, len(migrationNames))
	for _, name := range migrationNames {
		migrationPaths = append(migrationPaths, filepath.Join("..", "..", "migrations", name+".up.sql"))
	}
	testdb.ApplyMigrations(t, db, migrationPaths...)

	ctx := context.Background()
	merchantRepo := merchants.NewPostgresRepository(db.Pool)
	merchantSvc := merchants.NewService(merchantRepo)
	merchant, err := merchantSvc.CreateMerchant(ctx, "ONBOARD-STORE", "Onboarding Store Merchant", merchants.CapabilityTypeRetail)
	if err != nil {
		t.Fatalf("create merchant: %v", err)
	}
	subject := "merchant-store-owner-subject"
	if _, err := merchantSvc.AddMember(ctx, merchant.ID, subject, []string{merchants.PermissionRetailStoresManage}); err != nil {
		t.Fatalf("add merchant member: %v", err)
	}

	handler := newTestRouter(Dependencies{
		Commerce:           commerce.NewService(commerce.NewRepository(db.Pool)),
		Repo:               commerce.NewRepository(db.Pool),
		MerchantAuthorizer: merchants.NewAuthorizer(merchantRepo, merchants.AuthModeCanonical),
	})

	body := bytes.NewBufferString(`{"market_code":"EG","code":"mega-store","name":"Mega Store","status":"draft"}`)
	req := authenticatedRequest(t, http.MethodPost, "/internal/v1/merchants/"+merchant.ID.String()+"/stores", string(serviceauth.CallerSeller), testSellerToken)
	req.Header.Set(serviceauth.HeaderSubject, subject)
	req.Body = ioNopCloser{body}
	rec := doRequest(t, handler, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var store commerce.Store
	if err := json.NewDecoder(rec.Body).Decode(&store); err != nil {
		t.Fatalf("decode store: %v", err)
	}
	if store.Code != "mega-store" || store.Name != "Mega Store" || store.MarketCode != "EG" {
		t.Fatalf("unexpected store: %+v", store)
	}

	sellerID, err := commerce.NewService(commerce.NewRepository(db.Pool)).ResolveSellerIDForSubject(ctx, subject)
	if err != nil {
		t.Fatalf("merchant store creation should create legacy seller membership: %v", err)
	}
	if sellerID != store.SellerID {
		t.Fatalf("resolved seller %s, store seller %s", sellerID, store.SellerID)
	}
}

type ioNopCloser struct {
	*bytes.Buffer
}

func (c ioNopCloser) Close() error { return nil }
