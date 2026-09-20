// Package serviceauth authenticates Matjero actor services calling the Core
// internal API.
//
// This is the initial service-to-service authentication mechanism required by
// ADR-017. It is deliberately minimal and independently replaceable: the
// middleware is the only place that knows how a caller proves its identity, so
// swapping it for ZITADEL client-credentials / OAuth2 M2M later does not change
// any application contract.
//
// Threat model: the internal API is reachable only from the private service
// network. Repository privacy is NOT part of the security model, and neither is
// the obscurity of the route namespace. Every request must present a per-caller
// secret.
package serviceauth

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"

	"core/packages/httpx"
)

// Caller identifies a Matjero actor service. It is carried on the request
// context after successful authentication so handlers can authorize per caller
// without re-parsing headers.
type Caller string

const (
	CallerAdmin    Caller = "admin"
	CallerPlatform Caller = "platform"
	CallerSeller   Caller = "seller"
	CallerSupplier Caller = "supplier"
)

// Header names forming the internal service authentication and actor-context
// contract. Actors must strip any client-supplied copy of these headers before
// setting trusted values themselves.
const (
	// HeaderService names the calling actor service.
	HeaderService = "X-Matjero-Service"
	// HeaderSubject carries the authenticated end-user subject forwarded by an
	// actor after it has validated the end-user's own credentials. Core resolves
	// business identity (seller/supplier) from this subject itself and never
	// trusts a caller-supplied business identifier.
	HeaderSubject = "X-Matjero-Subject"
	// HeaderStorefrontHost carries the trusted, normalized storefront host. Core
	// resolves the tenant from this value and ignores the request Host and any
	// X-Forwarded-Host entirely.
	HeaderStorefrontHost = "X-Matjero-Storefront-Host"
)

// ErrMissingCredentials is returned when a request carries no usable service
// credentials. It is intentionally generic: callers must not be able to
// distinguish "no header" from "wrong token" from "unknown caller".
var ErrMissingCredentials = errors.New("missing or invalid service credentials")

// Config holds the expected bearer token for each allowed caller. A caller with
// no configured token cannot authenticate.
type Config struct {
	Tokens map[Caller]string
}

// Enabled reports whether at least one caller token is configured. The internal
// API refuses to serve when no credentials exist, so a misconfigured deployment
// fails closed instead of exposing business data anonymously.
func (c Config) Enabled() bool {
	for _, token := range c.Tokens {
		if token != "" {
			return true
		}
	}
	return false
}

// CallersWithTokens lists the callers that have a configured token, sorted for
// deterministic startup logging.
func (c Config) CallersWithTokens() []Caller {
	callers := make([]Caller, 0, len(c.Tokens))
	for caller, token := range c.Tokens {
		if token != "" {
			callers = append(callers, caller)
		}
	}
	sortCallers(callers)
	return callers
}

type contextKey string

const callerKey contextKey = "service_caller"

// Middleware rejects any request that does not present a valid bearer token
// matching the caller named in X-Matjero-Service.
//
// Both the token and the caller name must match: a seller token presented with
// X-Matjero-Service: admin is rejected, so a compromised actor credential cannot
// be replayed to borrow another actor's authorization scope.
func Middleware(cfg Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			caller, err := Authenticate(r, cfg)
			if err != nil {
				// Deliberately identical for every failure mode. No token values,
				// caller names, or configuration details are echoed back.
				httpx.WriteError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
				return
			}
			next.ServeHTTP(w, r.WithContext(WithCaller(r.Context(), caller)))
		})
	}
}

// Authenticate validates the service credentials on a request and returns the
// authenticated caller.
func Authenticate(r *http.Request, cfg Config) (Caller, error) {
	caller := Caller(strings.TrimSpace(r.Header.Get(HeaderService)))
	if !caller.Valid() {
		return "", ErrMissingCredentials
	}

	expected, ok := cfg.Tokens[caller]
	if !ok || expected == "" {
		return "", ErrMissingCredentials
	}

	token, ok := bearerToken(r)
	if !ok {
		return "", ErrMissingCredentials
	}

	// Constant-time comparison: the token is a bearer secret, so a timing oracle
	// would let an attacker recover it byte by byte.
	if subtle.ConstantTimeCompare([]byte(token), []byte(expected)) != 1 {
		return "", ErrMissingCredentials
	}

	return caller, nil
}

// Valid reports whether the value names a known actor service.
func (c Caller) Valid() bool {
	switch c {
	case CallerAdmin, CallerPlatform, CallerSeller, CallerSupplier:
		return true
	default:
		return false
	}
}

func bearerToken(r *http.Request) (string, bool) {
	header := r.Header.Get("Authorization")
	token, ok := strings.CutPrefix(header, "Bearer ")
	if !ok {
		return "", false
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return "", false
	}
	return token, true
}

// WithCaller binds an authenticated caller to a context.
func WithCaller(ctx context.Context, caller Caller) context.Context {
	return context.WithValue(ctx, callerKey, caller)
}

// CallerFrom returns the authenticated caller, if any.
func CallerFrom(ctx context.Context) (Caller, bool) {
	caller, ok := ctx.Value(callerKey).(Caller)
	return caller, ok
}

// RequireCaller rejects requests not authenticated as one of the given callers.
// It composes with Middleware and lets a route group narrow authorization to a
// specific actor without re-reading headers.
func RequireCaller(next http.Handler, allowed ...Caller) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		caller, ok := CallerFrom(r.Context())
		if !ok {
			httpx.WriteError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
			return
		}
		for _, candidate := range allowed {
			if caller == candidate {
				next.ServeHTTP(w, r)
				return
			}
		}
		httpx.WriteError(w, http.StatusForbidden, "forbidden", "forbidden")
	})
}

// SubjectFrom returns the forwarded end-user subject. It is empty for callers
// that do not act on behalf of an end user (for example the admin service).
func SubjectFrom(r *http.Request) string {
	return strings.TrimSpace(r.Header.Get(HeaderSubject))
}

// StorefrontHostFrom returns the trusted storefront host forwarded by the actor.
func StorefrontHostFrom(r *http.Request) string {
	return strings.TrimSpace(r.Header.Get(HeaderStorefrontHost))
}

func sortCallers(callers []Caller) {
	for i := 1; i < len(callers); i++ {
		for j := i; j > 0 && callers[j] < callers[j-1]; j-- {
			callers[j], callers[j-1] = callers[j-1], callers[j]
		}
	}
}
