package extract

import (
	"io"
	"net/url"
	"time"

	"github.com/markusmobius/go-trafilatura"
)

// ExtractDate extracts the publication date from HTML using go-trafilatura's
// metadata extraction (which internally uses go-htmldate).
// Returns nil if no date found.
func ExtractDate(r io.Reader, pageURL *url.URL) *time.Time {
	result, err := trafilatura.Extract(r, trafilatura.Options{
		OriginalURL:    pageURL,
		EnableFallback: true,
	})
	if err != nil || result == nil {
		return nil
	}

	if result.Metadata.Date.IsZero() {
		return nil
	}
	d := result.Metadata.Date
	return &d
}
