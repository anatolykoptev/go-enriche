package search

import "context"

// SearchEntry is a single search result with metadata.
type SearchEntry struct {
	URL     string `json:"url"`
	Title   string `json:"title"`
	Snippet string `json:"snippet,omitempty"`
}

// SearchResult holds search context and source URLs.
type SearchResult struct {
	Context string        // concatenated title+content from top results
	Sources []string      // deduplicated source URLs
	Entries []SearchEntry // individual results with metadata
}

// Provider searches for external context about a topic.
type Provider interface {
	Search(ctx context.Context, query string, timeRange string) (*SearchResult, error)
}
