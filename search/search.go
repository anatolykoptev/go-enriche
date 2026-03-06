// Package search provides external search context aggregation
// via SearXNG and other providers.
package search

import "github.com/anatolykoptev/go-stealth/websearch"

// wsToInternal converts websearch.Result slice to internal searxngResult slice.
func wsToInternal(results []websearch.Result) []searxngResult {
	out := make([]searxngResult, 0, len(results))
	for _, r := range results {
		out = append(out, searxngResult{
			URL:     r.URL,
			Title:   r.Title,
			Content: r.Content,
		})
	}
	return out
}
