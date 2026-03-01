package enriche

import "testing"

func TestTruncateRunes_Short(t *testing.T) {
	t.Parallel()
	if got := truncateRunes("hello", 10); got != "hello" {
		t.Errorf("got %q", got)
	}
}

func TestTruncateRunes_Exact(t *testing.T) {
	t.Parallel()
	if got := truncateRunes("hello", 5); got != "hello" {
		t.Errorf("got %q", got)
	}
}

func TestTruncateRunes_Truncate(t *testing.T) {
	t.Parallel()
	if got := truncateRunes("hello world", 5); got != "hello" {
		t.Errorf("got %q, want %q", got, "hello")
	}
}

func TestTruncateRunes_Cyrillic(t *testing.T) {
	t.Parallel()
	// "Привет мир" = 10 runes
	if got := truncateRunes("Привет мир", 6); got != "Привет" {
		t.Errorf("got %q, want %q", got, "Привет")
	}
}

func TestTruncateRunes_WordBoundary(t *testing.T) {
	t.Parallel()
	// Truncate at 8 runes: "Привет м" → last space at byte position after "Привет" → "Привет"
	if got := truncateRunes("Привет мир", 8); got != "Привет" {
		t.Errorf("got %q, want %q", got, "Привет")
	}
}

func TestTruncateRunes_Zero(t *testing.T) {
	t.Parallel()
	if got := truncateRunes("hello", 0); got != "hello" {
		t.Errorf("zero maxRunes should not truncate, got %q", got)
	}
}

func TestTruncateRunes_Empty(t *testing.T) {
	t.Parallel()
	if got := truncateRunes("", 10); got != "" {
		t.Errorf("got %q", got)
	}
}
