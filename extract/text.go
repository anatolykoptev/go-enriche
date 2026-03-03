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
// Optional TextOption parameters control output format.
func ExtractText(r io.Reader, pageURL *url.URL, opts ...TextOption) (*TextResult, error) {
	cfg := textConfig{format: FormatText}
	for _, o := range opts {
		o(&cfg)
	}

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

	content := result.ContentText
	if cfg.format == FormatMarkdown {
		if md := renderContentNodeAsMarkdown(result); md != "" {
			content = md
		}
	}

	return &TextResult{
		Content:     content,
		Title:       result.Metadata.Title,
		Author:      result.Metadata.Author,
		Description: result.Metadata.Description,
		Language:    result.Metadata.Language,
		SiteName:    result.Metadata.Sitename,
		Date:        result.Metadata.Date,
		Image:       result.Metadata.Image,
	}, nil
}
