package fetch

import "net/http"

// PageStatus represents the availability status of a fetched page.
type PageStatus string

const (
	StatusActive      PageStatus = "active"
	StatusNotFound    PageStatus = "not_found"
	StatusRedirect    PageStatus = "redirect"
	StatusUnreachable PageStatus = "unreachable"
	StatusWebsiteDown PageStatus = "website_down"
)

// FetchResult is the output of a page fetch.
type FetchResult struct {
	HTML       string
	Status     PageStatus
	FinalURL   string
	StatusCode int
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
