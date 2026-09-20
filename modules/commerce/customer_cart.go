package commerce

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"core/modules/catalog"
)

const (
	CartStatusActive      = "active"
	CartStatusCheckedOut  = "checked_out"
	CartStatusAbandoned   = "abandoned"
	CartStatusExpired     = "expired"
	CustomerStatusActive  = "active"
	CustomerStatusBlocked = "blocked"
)

var ErrCartExpired = errors.New("cart expired")

type Customer struct {
	ID               string    `json:"id"`
	StoreID          string    `json:"store_id"`
	MarketCode       string    `json:"market_code"`
	IdentityProvider *string   `json:"identity_provider,omitempty"`
	IdentitySubject  *string   `json:"identity_subject,omitempty"`
	Email            *string   `json:"email,omitempty"`
	DisplayName      *string   `json:"display_name,omitempty"`
	Status           string    `json:"status"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type CustomerAddress struct {
	ID            string    `json:"id"`
	CustomerID    string    `json:"customer_id"`
	StoreID       string    `json:"store_id"`
	Label         *string   `json:"label,omitempty"`
	RecipientName string    `json:"recipient_name"`
	Phone         *string   `json:"phone,omitempty"`
	AddressLine1  string    `json:"address_line_1"`
	AddressLine2  *string   `json:"address_line_2,omitempty"`
	City          string    `json:"city"`
	Region        *string   `json:"region,omitempty"`
	PostalCode    *string   `json:"postal_code,omitempty"`
	CountryCode   string    `json:"country_code"`
	IsDefault     bool      `json:"is_default"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type Cart struct {
	ID         string     `json:"id"`
	StoreID    string     `json:"store_id"`
	MarketCode string     `json:"market_code"`
	CustomerID *string    `json:"customer_id,omitempty"`
	Status     string     `json:"status"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	Items      []CartItem `json:"items"`
}

type CartItem struct {
	ID                     string    `json:"id"`
	CartID                 string    `json:"cart_id"`
	SellerListingID        string    `json:"seller_listing_id"`
	SKUID                  string    `json:"sku_id"`
	Quantity               int64     `json:"quantity"`
	ExpectedUnitPriceMinor int64     `json:"expected_unit_price_minor"`
	ExpectedCurrencyCode   string    `json:"expected_currency_code"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

func generateBearerToken() (string, string, error) {
	return generateCapability()
}

func generateCapability() (string, string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("generate capability: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	digest := sha256.Sum256([]byte(token))
	return token, hex.EncodeToString(digest[:]), nil
}

func bearerDigest(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

func (r Repository) CreateCustomer(ctx context.Context, storeID, marketCode string, identityProvider, identitySubject, email, displayName *string) (Customer, error) {
	if storeID == "" || marketCode == "" {
		return Customer{}, ErrInvalidInput
	}
	var customer Customer
	err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		id := uuid.NewString()
		err := tx.QueryRow(ctx, `
			INSERT INTO customers (id, store_id, market_code, identity_provider, identity_subject, email, display_name, status)
			VALUES ($1, $2, $3, $4, $5, $6, $7, 'active')
			RETURNING id, store_id, market_code, identity_provider, identity_subject, email, display_name, status, created_at, updated_at
		`, id, storeID, marketCode, identityProvider, identitySubject, email, displayName).Scan(
			&customer.ID, &customer.StoreID, &customer.MarketCode, &customer.IdentityProvider,
			&customer.IdentitySubject, &customer.Email, &customer.DisplayName, &customer.Status,
			&customer.CreatedAt, &customer.UpdatedAt,
		)
		return translatePGError(err, "create customer")
	})
	return customer, err
}

func (r Repository) CreateCustomerAddress(ctx context.Context, address CustomerAddress) (CustomerAddress, error) {
	if address.CustomerID == "" || address.StoreID == "" || strings.TrimSpace(address.RecipientName) == "" || strings.TrimSpace(address.AddressLine1) == "" || strings.TrimSpace(address.City) == "" || len(address.CountryCode) != 2 {
		return CustomerAddress{}, ErrInvalidInput
	}
	err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		id := uuid.NewString()
		return translatePGError(tx.QueryRow(ctx, `
			INSERT INTO customer_addresses (id, customer_id, store_id, label, recipient_name, phone, address_line_1, address_line_2, city, region, postal_code, country_code, is_default)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
			RETURNING id, created_at, updated_at
		`, id, address.CustomerID, address.StoreID, address.Label, address.RecipientName, address.Phone, address.AddressLine1, address.AddressLine2, address.City, address.Region, address.PostalCode, strings.ToUpper(address.CountryCode), address.IsDefault).Scan(&address.ID, &address.CreatedAt, &address.UpdatedAt), "create customer address")
	})
	return address, err
}

func (r Repository) CreateCart(ctx context.Context, storeID, marketCode string, customerID *string) (Cart, string, error) {
	if storeID == "" || marketCode == "" {
		return Cart{}, "", ErrInvalidInput
	}
	rawToken, digest, err := generateBearerToken()
	if err != nil {
		return Cart{}, "", err
	}
	var cart Cart
	err = r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		id := uuid.NewString()
		err := tx.QueryRow(ctx, `
			INSERT INTO carts (id, store_id, market_code, customer_id, cart_token_digest, status)
			VALUES ($1, $2, $3, $4, $5, 'active')
			RETURNING id, store_id, market_code, customer_id, status, expires_at, created_at, updated_at
		`, id, storeID, marketCode, customerID, digest).Scan(&cart.ID, &cart.StoreID, &cart.MarketCode, &cart.CustomerID, &cart.Status, &cart.ExpiresAt, &cart.CreatedAt, &cart.UpdatedAt)
		return translatePGError(err, "create cart")
	})
	return cart, rawToken, err
}

func (r Repository) GetCartByToken(ctx context.Context, storeID, token string) (Cart, error) {
	if storeID == "" || strings.TrimSpace(token) == "" {
		return Cart{}, ErrInvalidInput
	}
	cart, err := r.getCart(ctx, nil, storeID, bearerDigest(token))
	if err != nil {
		return Cart{}, err
	}
	if err := loadCartItems(ctx, r.pool, &cart); err != nil {
		return Cart{}, err
	}
	return cart, nil
}

func (r Repository) GetCartByTokenAnyStore(ctx context.Context, token string) (Cart, error) {
	if strings.TrimSpace(token) == "" {
		return Cart{}, ErrInvalidInput
	}
	var cart Cart
	err := r.pool.QueryRow(ctx, `
		SELECT id, store_id, market_code, customer_id, status, expires_at, created_at, updated_at
		FROM carts
		WHERE cart_token_digest = $1
	`, bearerDigest(token)).Scan(&cart.ID, &cart.StoreID, &cart.MarketCode, &cart.CustomerID, &cart.Status, &cart.ExpiresAt, &cart.CreatedAt, &cart.UpdatedAt)
	if err != nil {
		return Cart{}, translatePGError(err, "get cart by token any store")
	}
	if err := loadCartItems(ctx, r.pool, &cart); err != nil {
		return Cart{}, err
	}
	return cart, nil
}

func (r Repository) getCart(ctx context.Context, tx pgx.Tx, storeID, digest string) (Cart, error) {
	query := `SELECT id, store_id, market_code, customer_id, status, expires_at, created_at, updated_at FROM carts WHERE store_id = $1 AND cart_token_digest = $2`
	var cart Cart
	var err error
	if tx != nil {
		err = tx.QueryRow(ctx, query+` FOR UPDATE`, storeID, digest).Scan(&cart.ID, &cart.StoreID, &cart.MarketCode, &cart.CustomerID, &cart.Status, &cart.ExpiresAt, &cart.CreatedAt, &cart.UpdatedAt)
	} else {
		err = r.pool.QueryRow(ctx, query, storeID, digest).Scan(&cart.ID, &cart.StoreID, &cart.MarketCode, &cart.CustomerID, &cart.Status, &cart.ExpiresAt, &cart.CreatedAt, &cart.UpdatedAt)
	}
	if err != nil {
		return Cart{}, translatePGError(err, "get cart")
	}
	return cart, nil
}

type resolvedCartListing struct {
	ListingID      string
	SKUID          string
	Price          int64
	Currency       string
	MarketCurrency string
}

func resolveCartListing(ctx context.Context, tx pgx.Tx, storeID, marketCode, skuID string) (resolvedCartListing, error) {
	var out resolvedCartListing
	err := tx.QueryRow(ctx, `
		WITH listing AS (`+catalog.CanonicalListingSQL+`)
		SELECT l.listing_id, $3, l.price_minor, l.price_currency, l.market_currency
		FROM listing l
		JOIN variants v ON v.product_id = l.product_id AND v.status = 'active'
		JOIN skus sk ON sk.variant_id = v.id AND sk.id = $3 AND sk.status = 'active'
	`, storeID, marketCode, skuID).Scan(&out.ListingID, &out.SKUID, &out.Price, &out.Currency, &out.MarketCurrency)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return resolvedCartListing{}, ErrNotFound
		}
		return resolvedCartListing{}, fmt.Errorf("resolve canonical cart listing: %w", err)
	}
	if out.Currency != out.MarketCurrency {
		return resolvedCartListing{}, ErrMarketMismatch
	}
	return out, nil
}

func (r Repository) AddCartItem(ctx context.Context, storeID, token, skuID string, quantity int64) (Cart, error) {
	if storeID == "" || strings.TrimSpace(token) == "" || skuID == "" || quantity <= 0 || quantity > 10000 {
		return Cart{}, ErrInvalidInput
	}
	var cart Cart
	err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		cart, err = r.getCart(ctx, tx, storeID, bearerDigest(token))
		if err != nil {
			return err
		}
		if cart.Status != CartStatusActive {
			return ErrCartExpired
		}
		listing, err := resolveCartListing(ctx, tx, storeID, cart.MarketCode, skuID)
		if err != nil {
			return err
		}
		var existing int64
		err = tx.QueryRow(ctx, `SELECT quantity FROM cart_items WHERE cart_id = $1 AND seller_listing_id = $2 AND sku_id = $3`, cart.ID, listing.ListingID, skuID).Scan(&existing)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("read cart item: %w", err)
		}
		if err == nil && existing+quantity > 10000 {
			return ErrInvalidInput
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO cart_items (id, cart_id, seller_listing_id, sku_id, quantity, expected_unit_price_minor, expected_currency_code)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (cart_id, seller_listing_id, sku_id) DO UPDATE
			SET quantity = cart_items.quantity + EXCLUDED.quantity,
			    expected_unit_price_minor = EXCLUDED.expected_unit_price_minor,
			    expected_currency_code = EXCLUDED.expected_currency_code,
			    updated_at = now()
		`, uuid.NewString(), cart.ID, listing.ListingID, skuID, quantity, listing.Price, listing.Currency)
		if err != nil {
			return translatePGError(err, "add cart item")
		}
		return r.touchAndLoadCart(ctx, tx, &cart)
	})
	return cart, err
}

func (r Repository) UpdateCartItemQuantity(ctx context.Context, storeID, token, itemID string, quantity int64) (Cart, error) {
	if itemID == "" || quantity <= 0 || quantity > 10000 {
		return Cart{}, ErrInvalidInput
	}
	return r.mutateCartItem(ctx, storeID, token, itemID, func(ctx context.Context, tx pgx.Tx, cart Cart) error {
		_, err := tx.Exec(ctx, `UPDATE cart_items SET quantity = $1, updated_at = now() WHERE id = $2 AND cart_id = $3`, quantity, itemID, cart.ID)
		return translatePGError(err, "update cart item")
	})
}

func (r Repository) RemoveCartItem(ctx context.Context, storeID, token, itemID string) (Cart, error) {
	if itemID == "" {
		return Cart{}, ErrInvalidInput
	}
	return r.mutateCartItem(ctx, storeID, token, itemID, func(ctx context.Context, tx pgx.Tx, cart Cart) error {
		_, err := tx.Exec(ctx, `DELETE FROM cart_items WHERE id = $1 AND cart_id = $2`, itemID, cart.ID)
		return translatePGError(err, "remove cart item")
	})
}

func (r Repository) mutateCartItem(ctx context.Context, storeID, token, itemID string, mutate func(context.Context, pgx.Tx, Cart) error) (Cart, error) {
	var cart Cart
	err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		cart, err = r.getCart(ctx, tx, storeID, bearerDigest(token))
		if err != nil {
			return err
		}
		if cart.Status != CartStatusActive {
			return ErrCartExpired
		}
		if err := mutate(ctx, tx, cart); err != nil {
			return err
		}
		return r.touchAndLoadCart(ctx, tx, &cart)
	})
	return cart, err
}

func (r Repository) touchAndLoadCart(ctx context.Context, tx pgx.Tx, cart *Cart) error {
	if _, err := tx.Exec(ctx, `UPDATE carts SET updated_at = now() WHERE id = $1`, cart.ID); err != nil {
		return fmt.Errorf("touch cart: %w", err)
	}
	return loadCartItems(ctx, tx, cart)
}

type cartRowsQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func loadCartItems(ctx context.Context, querier cartRowsQuerier, cart *Cart) error {
	rows, err := querier.Query(ctx, `
		SELECT id, cart_id, seller_listing_id, sku_id, quantity, expected_unit_price_minor, expected_currency_code, created_at, updated_at
		FROM cart_items WHERE cart_id = $1 ORDER BY seller_listing_id ASC, sku_id ASC
	`, cart.ID)
	if err != nil {
		return fmt.Errorf("load cart items: %w", err)
	}
	defer rows.Close()
	cart.Items = make([]CartItem, 0)
	for rows.Next() {
		var item CartItem
		if err := rows.Scan(&item.ID, &item.CartID, &item.SellerListingID, &item.SKUID, &item.Quantity, &item.ExpectedUnitPriceMinor, &item.ExpectedCurrencyCode, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return fmt.Errorf("scan cart item: %w", err)
		}
		cart.Items = append(cart.Items, item)
	}
	return rows.Err()
}
