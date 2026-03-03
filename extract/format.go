package extract

import (
	"bytes"
	"strings"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	trafilatura "github.com/markusmobius/go-trafilatura"
	"golang.org/x/net/html"
)

// Format controls the output format for extracted content.
type Format string

const (
	FormatText     Format = "text"
	FormatMarkdown Format = "markdown"
)

// TextOption configures ExtractText behavior.
type TextOption func(*textConfig)

type textConfig struct {
	format Format
}

// WithFormat sets the output format for text extraction.
func WithFormat(f Format) TextOption {
	return func(c *textConfig) { c.format = f }
}

// renderContentNode renders ContentNode to HTML string.
func renderContentNode(result *trafilatura.ExtractResult) string {
	if result.ContentNode == nil {
		return ""
	}
	var buf bytes.Buffer
	if err := html.Render(&buf, result.ContentNode); err != nil {
		return ""
	}
	return strings.TrimSpace(buf.String())
}

// renderContentNodeAsMarkdown renders ContentNode to markdown.
func renderContentNodeAsMarkdown(result *trafilatura.ExtractResult) string {
	raw := renderContentNode(result)
	if raw == "" {
		return ""
	}
	md, err := htmltomarkdown.ConvertString(raw)
	if err != nil || strings.TrimSpace(md) == "" {
		return ""
	}
	return strings.TrimSpace(md)
}
