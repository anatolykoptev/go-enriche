package search

import "testing"

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
