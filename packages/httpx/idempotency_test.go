package httpx

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestIdempotencyMiddleware(t *testing.T) {
	store := NewMemoryIdempotencyStore()
	middleware := IdempotencyMiddleware(store, 1*time.Hour)

	callCount := 0
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		WriteJSON(w, http.StatusCreated, map[string]string{"id": "order_123", "status": "created"})
	}))

	// Request 1: Initial creation
	body1 := []byte(`{"item_id":"prod_1","quantity":2}`)
	req1 := httptest.NewRequest(http.MethodPost, "/v1/orders", bytes.NewBuffer(body1))
	req1.Header.Set(HeaderIdempotencyKey, "idem-key-001")
	rec1 := httptest.NewRecorder()

	handler.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusCreated {
		t.Fatalf("req1 status = %d, want %d", rec1.Code, http.StatusCreated)
	}
	if callCount != 1 {
		t.Fatalf("callCount = %d, want 1", callCount)
	}

	// Request 2: Replay with identical key and identical payload -> Returns cached response without re-executing handler
	req2 := httptest.NewRequest(http.MethodPost, "/v1/orders", bytes.NewBuffer(body1))
	req2.Header.Set(HeaderIdempotencyKey, "idem-key-001")
	rec2 := httptest.NewRecorder()

	handler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusCreated {
		t.Fatalf("req2 status = %d, want %d", rec2.Code, http.StatusCreated)
	}
	if callCount != 1 {
		t.Errorf("callCount should still be 1 (replayed), got %d", callCount)
	}
	if rec2.Header().Get("X-Idempotency-Replay") != "true" {
		t.Errorf("expected X-Idempotency-Replay header")
	}

	// Request 3: Replay with identical key BUT DIFFERENT payload -> 409 Conflict
	body3 := []byte(`{"item_id":"prod_2","quantity":5}`)
	req3 := httptest.NewRequest(http.MethodPost, "/v1/orders", bytes.NewBuffer(body3))
	req3.Header.Set(HeaderIdempotencyKey, "idem-key-001")
	rec3 := httptest.NewRecorder()

	handler.ServeHTTP(rec3, req3)

	if rec3.Code != http.StatusConflict {
		t.Fatalf("req3 status = %d, want %d (Conflict)", rec3.Code, http.StatusConflict)
	}
}
