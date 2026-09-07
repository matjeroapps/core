-- Phase 6: Shipping Foundation Schema (shipments, shipment_items, shipment_events)

CREATE TABLE shipments (
    id UUID PRIMARY KEY,
    order_id UUID NOT NULL REFERENCES orders (id) ON DELETE RESTRICT,
    fulfillment_location_id UUID NOT NULL REFERENCES fulfillment_locations (id) ON DELETE RESTRICT,
    status TEXT NOT NULL,
    tracking_number TEXT,
    shipping_cost_minor BIGINT NOT NULL DEFAULT 0 CHECK (shipping_cost_minor >= 0),
    cod_amount_minor BIGINT NOT NULL DEFAULT 0 CHECK (cod_amount_minor >= 0),
    currency CHAR(3) NOT NULL REFERENCES currencies (code),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT shipments_status_check
        CHECK (status IN (
            'PENDING',
            'PROCESSING',
            'READY_FOR_PICKUP',
            'SHIPPED',
            'OUT_FOR_DELIVERY',
            'DELIVERED',
            'FAILED',
            'RETURNED'
        ))
);

CREATE INDEX shipments_order_id_idx ON shipments (order_id);
CREATE INDEX shipments_status_idx ON shipments (status);

CREATE TABLE shipment_items (
    id UUID PRIMARY KEY,
    shipment_id UUID NOT NULL REFERENCES shipments (id) ON DELETE CASCADE,
    order_item_id UUID NOT NULL REFERENCES order_items (id) ON DELETE RESTRICT,
    quantity BIGINT NOT NULL CHECK (quantity > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX shipment_items_shipment_id_idx ON shipment_items (shipment_id);

CREATE TABLE shipment_events (
    id UUID PRIMARY KEY,
    shipment_id UUID NOT NULL REFERENCES shipments (id) ON DELETE CASCADE,
    status TEXT NOT NULL,
    notes TEXT,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX shipment_events_shipment_id_idx ON shipment_events (shipment_id, occurred_at ASC);
