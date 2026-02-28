package extract

import (
	"io"
	"net/url"
	"time"

	"github.com/markusmobius/go-trafilatura"
)

// TextResult holds the extracted text and metadata.
type TextResult struct {
	Content     string
	Title       string
	Author      string
	Description string
	Language    string
	SiteName    string
	Date        time.Time
	Image       string
}

// ExtractText extracts the main article text and metadata from HTML
// using go-trafilatura with fallback to readability and dom-distiller.
func ExtractText(r io.Reader, pageURL *url.URL) (*TextResult, error) {
	result, err := trafilatura.Extract(r, trafilatura.Options{
		OriginalURL:     pageURL,
		EnableFallback:  true,
		ExcludeComments: true,
		Focus:           trafilatura.FavorRecall,
	})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}

	return &TextResult{
		Content:     result.ContentText,
		Title:       result.Metadata.Title,
		Author:      result.Metadata.Author,
		Description: result.Metadata.Description,
		Language:    result.Metadata.Language,
		SiteName:    result.Metadata.Sitename,
		Date:        result.Metadata.Date,
		Image:       result.Metadata.Image,
	}, nil
}
