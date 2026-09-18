package coreapi

import (
	"encoding/json"
	"net/http"

	"core/internal/serviceauth"
	"core/packages/httpx"
)

// decodeJSON decodes a request body into dst, writing the internal
// invalid_argument error when the body is not valid JSON.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		writeError(w, CodeInvalidArgument)
		return false
	}
	return true
}

// requireAdmin restricts a handler to the admin actor service. Platform
// moderation operations must not be reachable by a seller or supplier
// credential, even a valid one.
func requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	caller, ok := serviceauth.CallerFrom(r.Context())
	if !ok {
		writeError(w, CodeUnauthorized)
		return false
	}
	if caller != serviceauth.CallerAdmin {
		writeError(w, CodeForbidden)
		return false
	}
	return true
}

// writeStatus echoes the applied status for a status mutation.
func writeStatus(w http.ResponseWriter, status string) {
	httpx.WriteJSON(w, http.StatusOK, StatusResponse{Status: status})
}
