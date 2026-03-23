package search

import "testing"

func TestAggregateResults_Entries(t *testing.T) {
	results := []searchResult{
		{URL: "https://a.com/1", Title: "First", Content: "content 1"},
		{URL: "https://b.com/2", Title: "Second", Content: "content 2"},
		{URL: "https://a.com/1", Title: "First Dup", Content: "dup"},
	}
	sr := aggregateResults(results, 8)
	if len(sr.Entries) != 2 {
		t.Errorf("expected 2 entries (deduped), got %d", len(sr.Entries))
	}
	if sr.Entries[0].Title != "First" {
		t.Errorf("expected First, got %s", sr.Entries[0].Title)
	}
	if sr.Entries[0].URL != "https://a.com/1" {
		t.Errorf("expected https://a.com/1, got %s", sr.Entries[0].URL)
	}
}

func TestNormalizeURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		{"https://Example.COM/page#section", "https://example.com/page"},
		{"https://example.com/page/", "https://example.com/page"},
		{"HTTP://EXAMPLE.COM", "http://example.com"},
		{"https://www.Example.COM/page", "https://example.com/page"},
		{"https://WWW.example.com/page/", "https://example.com/page"},
		{"", ""},
	}
	for _, tt := range tests {
		got := normalizeURL(tt.input)
		if got != tt.want {
			t.Errorf("normalizeURL(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
