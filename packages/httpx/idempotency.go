package httpx

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const HeaderIdempotencyKey = "Idempotency-Key"

type IdempotencyRecordStatus string

const (
	IdempotencyStatusInProgress IdempotencyRecordStatus = "in_progress"
	IdempotencyStatusCompleted  IdempotencyRecordStatus = "completed"
)

type IdempotencyRecord struct {
	Key             string                  `json:"key"`
	Scope           string                  `json:"scope"`
	RequestHash     string                  `json:"request_hash"`
	Status          IdempotencyRecordStatus `json:"status"`
	ResponseStatus  int                     `json:"response_status"`
	ResponseHeaders http.Header             `json:"response_headers"`
	ResponseBody    []byte                  `json:"response_body"`
	CreatedAt       time.Time               `json:"created_at"`
	ExpiresAt       time.Time               `json:"expires_at"`
}

type IdempotencyStore interface {
	LockKey(ctx context.Context, key, scope, requestHash string, ttl time.Duration) (existing *IdempotencyRecord, acquired bool, err error)
	SaveResponse(ctx context.Context, key, scope string, responseStatus int, headers http.Header, body []byte, ttl time.Duration) error
}

type MemoryIdempotencyStore struct {
	mu      sync.Mutex
	records map[string]*IdempotencyRecord
}

func NewMemoryIdempotencyStore() *MemoryIdempotencyStore {
	return &MemoryIdempotencyStore{
		records: make(map[string]*IdempotencyRecord),
	}
}

func (m *MemoryIdempotencyStore) LockKey(ctx context.Context, key, scope, requestHash string, ttl time.Duration) (*IdempotencyRecord, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	fullKey := scope + ":" + key
	now := time.Now().UTC()

	rec, ok := m.records[fullKey]
	if ok {
		if now.Before(rec.ExpiresAt) {
			return rec, false, nil
		}
	}

	newRec := &IdempotencyRecord{
		Key:         key,
		Scope:       scope,
		RequestHash: requestHash,
		Status:      IdempotencyStatusInProgress,
		CreatedAt:   now,
		ExpiresAt:   now.Add(ttl),
	}
	m.records[fullKey] = newRec
	return nil, true, nil
}

func (m *MemoryIdempotencyStore) SaveResponse(ctx context.Context, key, scope string, responseStatus int, headers http.Header, body []byte, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	fullKey := scope + ":" + key
	rec, ok := m.records[fullKey]
	if !ok {
		now := time.Now().UTC()
		rec = &IdempotencyRecord{
			Key:       key,
			Scope:     scope,
			CreatedAt: now,
		}
		m.records[fullKey] = rec
	}

	rec.Status = IdempotencyStatusCompleted
	rec.ResponseStatus = responseStatus
	rec.ResponseHeaders = headers.Clone()
	rec.ResponseBody = append([]byte(nil), body...)
	rec.ExpiresAt = time.Now().UTC().Add(ttl)
	return nil
}

func ComputeRequestHash(r *http.Request, body []byte) string {
	h := sha256.New()
	h.Write([]byte(r.Method))
	h.Write([]byte(":"))
	h.Write([]byte(r.URL.Path))
	h.Write([]byte(":"))
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

func IdempotencyMiddleware(store IdempotencyStore, ttl time.Duration) func(http.Handler) http.Handler {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Idempotency applies to mutating requests (POST, PUT, PATCH, DELETE)
			if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			key := strings.TrimSpace(r.Header.Get(HeaderIdempotencyKey))
			if key == "" {
				next.ServeHTTP(w, r)
				return
			}

			bodyBytes, err := io.ReadAll(r.Body)
			if err != nil {
				WriteError(w, http.StatusBadRequest, "invalid_argument", "unable to read request body")
				return
			}
			r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

			reqHash := ComputeRequestHash(r, bodyBytes)
			scope := r.URL.Path

			existing, acquired, err := store.LockKey(r.Context(), key, scope, reqHash, ttl)
			if err != nil {
				WriteError(w, http.StatusInternalServerError, "internal_error", "idempotency store error")
				return
			}

			if !acquired && existing != nil {
				if existing.RequestHash != reqHash {
					WriteError(w, http.StatusConflict, "idempotency_payload_mismatch", "idempotency key reused with different request payload")
					return
				}
				if existing.Status == IdempotencyStatusInProgress {
					WriteError(w, http.StatusConflict, "idempotency_conflict", "request with idempotency key is currently processing")
					return
				}
				if existing.Status == IdempotencyStatusCompleted {
					for k, vv := range existing.ResponseHeaders {
						for _, v := range vv {
							w.Header().Add(k, v)
						}
					}
					w.Header().Set("X-Idempotency-Replay", "true")
					w.WriteHeader(existing.ResponseStatus)
					_, _ = w.Write(existing.ResponseBody)
					return
				}
			}

			rec := &responseRecorder{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
				headers:        make(http.Header),
				body:           &bytes.Buffer{},
			}

			next.ServeHTTP(rec, r)

			// Copy captured headers to actual writer if not already written
			for k, vv := range rec.headers {
				for _, v := range vv {
					if w.Header().Get(k) == "" {
						w.Header().Add(k, v)
					}
				}
			}

			_ = store.SaveResponse(r.Context(), key, scope, rec.statusCode, rec.Header(), rec.body.Bytes(), ttl)
		})
	}
}

type responseRecorder struct {
	http.ResponseWriter
	statusCode int
	headers    http.Header
	body       *bytes.Buffer
}

func (r *responseRecorder) Header() http.Header {
	return r.ResponseWriter.Header()
}

func (r *responseRecorder) WriteHeader(code int) {
	r.statusCode = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	r.body.Write(b)
	return r.ResponseWriter.Write(b)
}

func SerializeHeader(h http.Header) ([]byte, error) {
	return json.Marshal(h)
}

func DeserializeHeader(data []byte) (http.Header, error) {
	var h http.Header
	err := json.Unmarshal(data, &h)
	return h, err
}
