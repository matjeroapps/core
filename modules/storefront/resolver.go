package storefront

import (
	"net/http"
	"strings"

	"core/modules/commerce"
	"core/packages/config"
)

// NormalizeHost lowercases a host and strips any port and surrounding whitespace,
// then canonicalizes it through the shared commerce.NormalizeDomain routine so the
// read boundary (request host) and the write boundary (persisted domain) use one
// normalization function. It is the single normalization routine used for both
// request hosts and stored domain mappings so comparisons are stable and
// case-insensitive.
func NormalizeHost(host string) string {
	host = strings.TrimSpace(host)
	host = strings.ToLower(host)
	if i := strings.IndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	domain, err := commerce.NormalizeDomain(host)
	if err != nil {
		return ""
	}
	return domain
}

// DomainFromRequest extracts the trusted storefront domain from an HTTP request.
//
// The request Host header is authoritative by default. The X-Forwarded-Host
// header is only honored when the deployment explicitly trusts a reverse proxy
// (config.TrustedForwardedHost). This prevents hostname spoofing from untrusted
// clients: an attacker cannot supply a forwarded host to impersonate another
// tenant unless the platform is deployed behind a proxy that strips/overwrites
// that header.
func DomainFromRequest(r *http.Request, cfg config.Config) string {
	host := r.Host
	if cfg.TrustedForwardedHost {
		if fwd := r.Header.Get("X-Forwarded-Host"); fwd != "" {
			// Take the first host when multiple are comma-separated.
			if i := strings.IndexByte(fwd, ','); i >= 0 {
				fwd = fwd[:i]
			}
			host = fwd
		}
	}
	return NormalizeHost(host)
}
