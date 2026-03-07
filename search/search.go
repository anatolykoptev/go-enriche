// Package search provides external search context aggregation
// via direct scrapers (DDG, Startpage, Brave, Google) and other providers.
package search

import (
	"strings"

	"github.com/anatolykoptev/go-stealth/websearch"
)

const defaultMaxResults = 8

// searchResult is the internal generic search result used across all providers.
type searchResult struct {
	URL     string
	Title   string
	Content string
}

// toSearchResults converts websearch.Result slice to internal searchResult slice.
func toSearchResults(results []websearch.Result) []searchResult {
	out := make([]searchResult, 0, len(results))
	for _, r := range results {
		out = append(out, searchResult{
			URL:     r.URL,
			Title:   r.Title,
			Content: r.Content,
		})
	}
	return out
}

// aggregateResults deduplicates and builds context from generic search results.
func aggregateResults(results []searchResult, maxResults int) *SearchResult {
	var (
		contextParts []string
		sources      []string
		seen         = make(map[string]bool)
	)

	for _, r := range results {
		if len(sources) >= maxResults {
			break
		}

		norm := normalizeURL(r.URL)
		if norm == "" || seen[norm] {
			continue
		}
		seen[norm] = true

		sources = append(sources, r.URL)
		switch {
		case r.Title != "" && r.Content != "":
			contextParts = append(contextParts, r.Title+": "+r.Content)
		case r.Content != "":
			contextParts = append(contextParts, r.Content)
		case r.Title != "":
			contextParts = append(contextParts, r.Title)
		}
	}

	return &SearchResult{
		Context: strings.Join(contextParts, "\n\n"),
		Sources: sources,
	}
}
