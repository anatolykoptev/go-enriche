package search

import "context"

// SearchResult holds search context and source URLs.
type SearchResult struct {
	Context string   // concatenated title+content from top results
	Sources []string // deduplicated source URLs
}

// Provider searches for external context about a topic.
type Provider interface {
	Search(ctx context.Context, query string, timeRange string) (*SearchResult, error)
}
