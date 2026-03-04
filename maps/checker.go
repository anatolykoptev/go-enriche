// Package maps provides place status verification via map services.
package maps

import "context"

// PlaceStatus represents the operational status of a place.
type PlaceStatus string

const (
	PlaceOpen            PlaceStatus = "open"
	PlaceTemporaryClosed PlaceStatus = "temporarily_closed"
	PlacePermanentClosed PlaceStatus = "permanently_closed"
	PlaceNotFound        PlaceStatus = "not_found"
	PlaceUnknown         PlaceStatus = "unknown"
)

// OrgFetcher fetches a URL and returns rendered HTML (e.g. via headless browser).
type OrgFetcher func(ctx context.Context, url string) (string, error)

// OrgData holds structured business data extracted from a maps org page.
type OrgData struct {
	Status     PlaceStatus
	Name       string
	Address    string
	Phone      string
	Website    string
	Hours      string
	Rating     float64
	Categories []string
	Latitude   float64
	Longitude  float64
	MapURL     string
}

// CheckResult holds the result of a place status check.
type CheckResult struct {
	Status   PlaceStatus
	MapURL   string   // URL on the map service, if found
	RawTitle string   // title/snippet for debugging
	OrgData  *OrgData // populated when OrgFetcher is available
}

// IsClosed returns true if the place is permanently closed.
func (r *CheckResult) IsClosed() bool {
	return r.Status == PlacePermanentClosed
}

// IsTemporaryClosed returns true if the place is temporarily closed.
func (r *CheckResult) IsTemporaryClosed() bool {
	return r.Status == PlaceTemporaryClosed
}

// Checker verifies whether a place is open or closed
// by querying an external map service.
type Checker interface {
	Check(ctx context.Context, name, city string) (*CheckResult, error)
}
