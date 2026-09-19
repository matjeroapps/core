package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimiter(t *testing.T) {
	limiter := NewMemoryRateLimiter(2, 5*time.Second)

	handler := RateLimitMiddleware(limiter, 2, 5*time.Second, func(r *http.Request) string {
		return "test_client"
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}))

	// Request 1: Allowed
	rec1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodGet, "/test", nil)
	handler.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusOK {
		t.Errorf("req1 status = %d, want %d", rec1.Code, http.StatusOK)
	}
	if rec1.Header().Get("X-RateLimit-Remaining") != "1" {
		t.Errorf("req1 X-RateLimit-Remaining = %q, want 1", rec1.Header().Get("X-RateLimit-Remaining"))
	}

	// Request 2: Allowed
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/test", nil)
	handler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Errorf("req2 status = %d, want %d", rec2.Code, http.StatusOK)
	}
	if rec2.Header().Get("X-RateLimit-Remaining") != "0" {
		t.Errorf("req2 X-RateLimit-Remaining = %q, want 0", rec2.Header().Get("X-RateLimit-Remaining"))
	}

	// Request 3: Blocked (429 Too Many Requests)
	rec3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodGet, "/test", nil)
	handler.ServeHTTP(rec3, req3)

	if rec3.Code != http.StatusTooManyRequests {
		t.Errorf("req3 status = %d, want %d", rec3.Code, http.StatusTooManyRequests)
	}
	if rec3.Header().Get("Retry-After") == "" {
		t.Error("req3 Retry-After header should be set")
	}
}
