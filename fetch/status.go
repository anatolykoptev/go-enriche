package fetch

import "net/http"

// PageStatus represents the availability status of a fetched page.
type PageStatus string

const (
	StatusActive          PageStatus = "active"
	StatusNotFound        PageStatus = "not_found"
	StatusRedirect        PageStatus = "redirect"
	StatusUnreachable     PageStatus = "unreachable"
	StatusWebsiteDown     PageStatus = "website_down"
	StatusClosed          PageStatus = "closed"
	StatusTemporaryClosed PageStatus = "temporarily_closed"

	// StatusTLSInvalid marks a page whose HTTPS endpoint failed TLS
	// CERTIFICATE validation (hostname mismatch, untrusted/unknown CA, or
	// another x509 chain error -- see fetcher.go's isCertError) AND whose
	// one-retry plain-HTTP fallback (httpFallbackURL) ALSO failed to produce
	// a usable response. This is DISTINCT from StatusUnreachable: it tells a
	// downstream consumer (go-wp's classifyContactVerdict, a follow-up task)
	// that the origin's TLS setup is specifically broken, not that the site
	// is down/unreachable outright -- a fixable-cert case, not a dead-site
	// case, so it should not collapse to the same bare "unverifiable"
	// verdict a real outage does.
	//
	// When the fallback DOES recover a response, doFetch reports that
	// response's REAL status (usually StatusActive, but it can legitimately
	// be StatusNotFound/StatusRedirect/StatusUnreachable too, exactly as a
	// normal fetch would) with FetchResult.TLSFallbackUsed set instead of
	// this status -- see that field's doc comment for the go-wp trust
	// contract.
	StatusTLSInvalid PageStatus = "tls_invalid"
)

// FetchResult is the output of a page fetch.
type FetchResult struct {
	HTML       string
	Status     PageStatus
	FinalURL   string
	StatusCode int

	// TLSFallbackUsed is true when this result was recovered via doFetch's
	// ONE-retry plain-HTTP fallback after the primary HTTPS request failed
	// TLS certificate validation (see isCertError, fetcher.go). The content
	// is REAL -- fetched through the exact SAME SSRF-guarded client/transport
	// as every other request (see NewFetcher's doc comment; the fallback
	// request reuses doFetch's already-guarded *http.Client, never a raw
	// http.Get and never a process-global TLS relaxation) -- but the origin's
	// certificate could not be validated, so this data is LOWER TRUST than a
	// normal fetch.
	//
	// Contract for the go-wp follow-up (classifyContactVerdict /
	// Correctable gate): a field recovered from a TLSFallbackUsed result
	// MUST NOT be auto-applied to a paid/live card without human
	// confirmation -- mirrors enriche.Result.RenderSkipped's identical
	// fail-closed contract (types.go) for the exact same reason (a
	// wrong-cert page could serve attacker-controlled content).
	//
	// False (the zero value) for every fetch that did not go through this
	// fallback -- existing callers see no behavior change.
	TLSFallbackUsed bool
}

// IsTransient returns true if the result indicates a transient error worth retrying
// (connection failure, 502, 503, 504, 429).
func (fr *FetchResult) IsTransient() bool {
	if fr.Status != StatusUnreachable {
		return false
	}
	// StatusCode 0 means connection failed (timeout, DNS, etc.) — transient.
	return fr.StatusCode == 0 ||
		fr.StatusCode == http.StatusBadGateway ||
		fr.StatusCode == http.StatusServiceUnavailable ||
		fr.StatusCode == http.StatusGatewayTimeout ||
		fr.StatusCode == http.StatusTooManyRequests
}
