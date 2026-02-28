package fetch

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
