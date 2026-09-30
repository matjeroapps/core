package commerce

import (
	"testing"
	"time"
)

func TestValidateOrderTransition_StateMatrix(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	deadline := now.Add(30 * time.Minute)

	tests := []struct {
		name                   string
		currentStatus          string
		authority              TransitionAuthority
		targetStatus           string
		confirmationDeadlineAt time.Time
		decisionNow            time.Time
		wantErr                bool
	}{
		// Creation -> Pending
		{
			name:                   "creation to pending - checkout authority",
			currentStatus:          "",
			authority:              AuthorityCheckout,
			targetStatus:           OrderStatusPending,
			confirmationDeadlineAt: deadline,
			decisionNow:            now,
			wantErr:                false,
		},
		{
			name:                   "creation to pending - wrong authority (seller)",
			currentStatus:          "",
			authority:              AuthoritySeller,
			targetStatus:           OrderStatusPending,
			confirmationDeadlineAt: deadline,
			decisionNow:            now,
			wantErr:                true,
		},

		// Pending -> Confirmed
		{
			name:                   "pending to confirmed - seller before deadline",
			currentStatus:          OrderStatusPending,
			authority:              AuthoritySeller,
			targetStatus:           OrderStatusConfirmed,
			confirmationDeadlineAt: deadline,
			decisionNow:            deadline.Add(-1 * time.Second),
			wantErr:                false,
		},
		{
			name:                   "pending to confirmed - seller exactly at deadline",
			currentStatus:          OrderStatusPending,
			authority:              AuthoritySeller,
			targetStatus:           OrderStatusConfirmed,
			confirmationDeadlineAt: deadline,
			decisionNow:            deadline,
			wantErr:                true,
		},
		{
			name:                   "pending to confirmed - seller after deadline",
			currentStatus:          OrderStatusPending,
			authority:              AuthoritySeller,
			targetStatus:           OrderStatusConfirmed,
			confirmationDeadlineAt: deadline,
			decisionNow:            deadline.Add(1 * time.Second),
			wantErr:                true,
		},
		{
			name:                   "pending to confirmed - wrong authority (customer)",
			currentStatus:          OrderStatusPending,
			authority:              AuthorityCustomer,
			targetStatus:           OrderStatusConfirmed,
			confirmationDeadlineAt: deadline,
			decisionNow:            now,
			wantErr:                true,
		},

		// Pending -> Cancelled
		{
			name:                   "pending to cancelled - customer",
			currentStatus:          OrderStatusPending,
			authority:              AuthorityCustomer,
			targetStatus:           OrderStatusCancelled,
			confirmationDeadlineAt: deadline,
			decisionNow:            now,
			wantErr:                false,
		},
		{
			name:                   "pending to cancelled - seller",
			currentStatus:          OrderStatusPending,
			authority:              AuthoritySeller,
			targetStatus:           OrderStatusCancelled,
			confirmationDeadlineAt: deadline,
			decisionNow:            now,
			wantErr:                false,
		},
		{
			name:                   "pending to cancelled - scheduler at deadline",
			currentStatus:          OrderStatusPending,
			authority:              AuthorityScheduler,
			targetStatus:           OrderStatusCancelled,
			confirmationDeadlineAt: deadline,
			decisionNow:            deadline,
			wantErr:                false,
		},
		{
			name:                   "pending to cancelled - scheduler after deadline",
			currentStatus:          OrderStatusPending,
			authority:              AuthorityScheduler,
			targetStatus:           OrderStatusCancelled,
			confirmationDeadlineAt: deadline,
			decisionNow:            deadline.Add(10 * time.Minute),
			wantErr:                false,
		},
		{
			name:                   "pending to cancelled - scheduler before deadline",
			currentStatus:          OrderStatusPending,
			authority:              AuthorityScheduler,
			targetStatus:           OrderStatusCancelled,
			confirmationDeadlineAt: deadline,
			decisionNow:            now,
			wantErr:                true,
		},

		// Confirmed -> Processing
		{
			name:                   "confirmed to processing - seller",
			currentStatus:          OrderStatusConfirmed,
			authority:              AuthoritySeller,
			targetStatus:           OrderStatusProcessing,
			confirmationDeadlineAt: deadline,
			decisionNow:            now,
			wantErr:                false,
		},
		{
			name:                   "confirmed to processing - wrong authority (customer)",
			currentStatus:          OrderStatusConfirmed,
			authority:              AuthorityCustomer,
			targetStatus:           OrderStatusProcessing,
			confirmationDeadlineAt: deadline,
			decisionNow:            now,
			wantErr:                true,
		},

		// Confirmed -> Cancelled
		{
			name:                   "confirmed to cancelled - seller",
			currentStatus:          OrderStatusConfirmed,
			authority:              AuthoritySeller,
			targetStatus:           OrderStatusCancelled,
			confirmationDeadlineAt: deadline,
			decisionNow:            now,
			wantErr:                false,
		},
		{
			name:                   "confirmed to cancelled - wrong authority (customer)",
			currentStatus:          OrderStatusConfirmed,
			authority:              AuthorityCustomer,
			targetStatus:           OrderStatusCancelled,
			confirmationDeadlineAt: deadline,
			decisionNow:            now,
			wantErr:                true,
		},

		// Processing -> ReadyForShipping
		{
			name:                   "processing to ready_for_shipping - seller",
			currentStatus:          OrderStatusProcessing,
			authority:              AuthoritySeller,
			targetStatus:           OrderStatusReadyForShipping,
			confirmationDeadlineAt: deadline,
			decisionNow:            now,
			wantErr:                false,
		},

		// Processing -> Cancelled
		{
			name:                   "processing to cancelled - seller",
			currentStatus:          OrderStatusProcessing,
			authority:              AuthoritySeller,
			targetStatus:           OrderStatusCancelled,
			confirmationDeadlineAt: deadline,
			decisionNow:            now,
			wantErr:                false,
		},

		// ReadyForShipping -> Shipped
		{
			name:                   "ready_for_shipping to shipped - seller",
			currentStatus:          OrderStatusReadyForShipping,
			authority:              AuthoritySeller,
			targetStatus:           OrderStatusShipped,
			confirmationDeadlineAt: deadline,
			decisionNow:            now,
			wantErr:                false,
		},
		{
			name:                   "ready_for_shipping to shipped - wrong authority (customer)",
			currentStatus:          OrderStatusReadyForShipping,
			authority:              AuthorityCustomer,
			targetStatus:           OrderStatusShipped,
			confirmationDeadlineAt: deadline,
			decisionNow:            now,
			wantErr:                true,
		},

		// Shipped -> Delivered
		{
			name:                   "shipped to delivered - seller",
			currentStatus:          OrderStatusShipped,
			authority:              AuthoritySeller,
			targetStatus:           OrderStatusDelivered,
			confirmationDeadlineAt: deadline,
			decisionNow:            now,
			wantErr:                false,
		},
		{
			name:                   "shipped to delivered - wrong authority (customer)",
			currentStatus:          OrderStatusShipped,
			authority:              AuthorityCustomer,
			targetStatus:           OrderStatusDelivered,
			confirmationDeadlineAt: deadline,
			decisionNow:            now,
			wantErr:                true,
		},

		// Terminal & unsupported transitions
		{
			name:                   "shipped to cancelled - invalid terminal transition",
			currentStatus:          OrderStatusShipped,
			authority:              AuthoritySeller,
			targetStatus:           OrderStatusCancelled,
			confirmationDeadlineAt: deadline,
			decisionNow:            now,
			wantErr:                true,
		},
		{
			name:                   "delivered to returned - invalid transition",
			currentStatus:          OrderStatusDelivered,
			authority:              AuthoritySeller,
			targetStatus:           OrderStatusReturned,
			confirmationDeadlineAt: deadline,
			decisionNow:            now,
			wantErr:                true,
		},
		{
			name:                   "cancelled to pending - invalid back transition",
			currentStatus:          OrderStatusCancelled,
			authority:              AuthoritySeller,
			targetStatus:           OrderStatusPending,
			confirmationDeadlineAt: deadline,
			decisionNow:            now,
			wantErr:                true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateOrderTransition(tt.currentStatus, tt.authority, tt.targetStatus, tt.confirmationDeadlineAt, tt.decisionNow)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateOrderTransition() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestOrder_ToPublic(t *testing.T) {
	now := time.Now().UTC()
	listingID := "listing-1"
	productID := "product-1"
	variantID := "variant-1"
	skuID := "sku-1"
	offerID := "offer-1"
	supplierID := "supplier-1"
	cost := int64(800)
	costCurrency := "USD"

	order := Order{
		ID:                          "order-1",
		OrderNumber:                 "#100001",
		StoreID:                     "store-1",
		MarketCode:                  "US",
		CustomerID:                  nil,
		CheckoutSessionID:           "session-1",
		Status:                      OrderStatusPending,
		CurrencyCode:                "USD",
		GuestOrderAccessTokenDigest: []byte("01234567890123456789012345678901"),
		SubtotalMinor:               1000,
		TotalMinor:                  1000,
		ConfirmationDeadlineAt:      now.Add(30 * time.Minute),
		AggregateVersion:            1,
		CreatedAt:                   now,
		UpdatedAt:                   now,
		Items: []OrderItem{
			{
				ID:                       "item-1",
				OrderID:                  "order-1",
				SellerListingID:          &listingID,
				ProductID:                &productID,
				VariantID:                &variantID,
				SKUID:                    &skuID,
				SupplierOfferID:          &offerID,
				SourceSupplierID:         &supplierID,
				FulfillmentLocationID:    "loc-1",
				InventoryReservationID:   "res-1",
				ProductTitleSnapshot:     "Test Product",
				SKUCodeSnapshot:          "TEST-SKU",
				UnitPriceMinor:           1000,
				CurrencyCode:             "USD",
				Quantity:                 1,
				LineTotalMinor:           1000,
				SupplierCostMinor:        &cost,
				SupplierCostCurrencyCode: &costCurrency,
				CreatedAt:                now,
			},
		},
	}

	pub := order.ToPublic()

	if pub.ID != order.ID || pub.OrderNumber != order.OrderNumber {
		t.Fatalf("unexpected public order identity fields")
	}
	if len(pub.Items) != 1 {
		t.Fatalf("expected 1 public item, got %d", len(pub.Items))
	}
	pubItem := pub.Items[0]
	if pubItem.ProductTitleSnapshot != "Test Product" || pubItem.LineTotalMinor != 1000 {
		t.Fatalf("unexpected public item fields")
	}

	// Verify internal fields are omitted from public DTO (implicitly checked by struct field absence)
}
