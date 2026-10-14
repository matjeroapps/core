package coreapi

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"

	"core/modules/commerce"
	"core/packages/httpx"
)

func TestWritePublishNotReadyErrorIncludesReadinessReasons(t *testing.T) {
	rec := httptest.NewRecorder()
	err := fmt.Errorf("%w: At least one active Variant is required; Sellable inventory at an active store location is required", commerce.ErrPublishNotReady)

	writePublishNotReadyError(rec, err)

	if rec.Code != 422 {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	var body httpx.ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Error.Code != CodePublishNotReady {
		t.Fatalf("code = %q, want %q", body.Error.Code, CodePublishNotReady)
	}
	want := "At least one active Variant is required; Sellable inventory at an active store location is required"
	if body.Error.Message != want {
		t.Fatalf("message = %q, want %q", body.Error.Message, want)
	}
}

func TestWritePublishNotReadyErrorKeepsOtherErrorsMapped(t *testing.T) {
	rec := httptest.NewRecorder()
	writePublishNotReadyError(rec, commerce.ErrNotFound)

	if rec.Code != 404 {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	var body httpx.ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Error.Code != CodeNotFound {
		t.Fatalf("code = %q, want %q", body.Error.Code, CodeNotFound)
	}
}
