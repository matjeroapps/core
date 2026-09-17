// Package catalog contains Core-owned catalog selection primitives shared by
// public read models and transactional Cart operations.
package catalog

// CanonicalListingSQL returns one deterministic, currently eligible Listing
// per Store/Product. The caller supplies $1 = store_id and $2 = market_code.
// Availability is deliberately returned as data rather than used to select a
// different, older Listing: the newest eligible commercial Listing remains the
// authority for price and source, while source-aware inventory decides whether
// that Listing is in stock.
const CanonicalListingSQL = `
SELECT DISTINCT ON (sl.product_id)
    sl.id AS listing_id,
    sl.store_id,
    sl.product_id,
    sl.supplier_offer_id,
    so.supplier_id,
    sl.market_code,
    m.currency_code AS market_currency,
    sl.created_at AS listing_created_at,
    slp.amount_minor AS price_minor,
    slp.currency_code AS price_currency,
    COALESCE(soa.is_available, true) AS supplier_available,
    p.slug AS product_slug,
    p.created_at AS product_created_at
FROM seller_listings sl
JOIN stores s
    ON s.id = sl.store_id
   AND s.market_code = sl.market_code
JOIN markets m
    ON m.code = sl.market_code
JOIN products p
    ON p.id = sl.product_id
   AND p.status = 'active'
JOIN seller_listing_prices slp
    ON slp.seller_listing_id = sl.id
   AND slp.is_current = true
LEFT JOIN supplier_offers so
    ON so.id = sl.supplier_offer_id
   AND so.market_code = sl.market_code
LEFT JOIN supplier_products sp
    ON sp.id = so.supplier_product_id
   AND sp.supplier_id = so.supplier_id
   AND sp.product_id = sl.product_id
LEFT JOIN supplier_offer_availability soa
    ON soa.supplier_offer_id = so.id
WHERE sl.store_id = $1
  AND sl.market_code = $2
  AND sl.status IN ('published', 'active')
  AND (
      sl.supplier_offer_id IS NULL
      OR (
          so.status = 'active'
          AND sp.id IS NOT NULL
      )
  )
ORDER BY sl.product_id, sl.created_at DESC, sl.id DESC`
