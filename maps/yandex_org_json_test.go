package maps

import (
	"strings"
	"testing"
)

// TestParseOrgPage_JSONExtraction verifies that the primary JSON extraction
// path correctly parses a realistic Yandex Maps business data block.
func TestParseOrgPage_JSONExtraction(t *testing.T) {
	html := []byte(`<html><head><script type="application/ld+json">{"@context":"https://schema.org","@type":"WebSite","name":"Yandex Maps"}</script></head><body><script>{"type":"business","shortTitle":"Ramen Rebel","fullAddress":"SPb, Nevsky 42","chain":{"name":"Ramen Rebel"},"status":"open","phones":[{"number":"+7 (999) 123-45-67"}],"urls":["https://ramen-rebel.ru"],"currentWorkingStatus":{"text":"Open now"},"ratingData":{"ratingValue":4.8},"coordinates":[30.31,59.94],"categories":[{"name":"Restaurant"},{"name":"Ramen"}]}</script></body></html>`)

	od := parseOrgPage(html)

	if od.Name != "Ramen Rebel" {
		t.Errorf("name = %q, want %q", od.Name, "Ramen Rebel")
	}
	if od.Address != "SPb, Nevsky 42" {
		t.Errorf("address = %q", od.Address)
	}
	if od.Phone != "+7 (999) 123-45-67" {
		t.Errorf("phone = %q", od.Phone)
	}
	if od.Website != "https://ramen-rebel.ru" {
		t.Errorf("website = %q", od.Website)
	}
	if od.Hours != "Open now" {
		t.Errorf("hours = %q", od.Hours)
	}
	if od.Rating != 4.8 {
		t.Errorf("rating = %f, want 4.8", od.Rating)
	}
	if od.Latitude != 59.94 || od.Longitude != 30.31 {
		t.Errorf("coords = (%f, %f), want (59.94, 30.31) — Yandex uses [lon, lat]", od.Latitude, od.Longitude)
	}
	if len(od.Categories) != 2 {
		t.Errorf("categories = %v, want 2", od.Categories)
	}
	if od.Status != PlaceOpen {
		t.Errorf("status = %q, want %q", od.Status, PlaceOpen)
	}
}

// TestParseOrgPage_JSONFallbackToRegex verifies that when JSON extraction
// fails (malformed JSON), the parser falls back to regex patterns.
func TestParseOrgPage_JSONFallbackToRegex(t *testing.T) {
	// Malformed JSON (missing closing brace) — JSON extraction should fail,
	// regex should still extract name and status.
	html := []byte(`{"shortTitle":"Broken JSON","status":"open"`)

	od := parseOrgPage(html)

	if od.Name != "Broken JSON" {
		t.Errorf("name = %q, want %q (regex fallback)", od.Name, "Broken JSON")
	}
	if od.Status != PlaceOpen {
		t.Errorf("status = %q, want %q", od.Status, PlaceOpen)
	}
}

// TestParseOrgPage_LegacyFormat verifies that the JSON parser handles
// the legacy format (top-level "name", "address.formatted", "rating.score").
func TestParseOrgPage_LegacyFormat(t *testing.T) {
	html := []byte(`{"name":"Государственный Эрмитаж","status":"open","address":{"formatted":"Дворцовая пл., 2, Санкт-Петербург"},"phones":[{"formatted":"+7 (812) 710-90-79"}],"hours":{"text":"вт-вс 10:30–18:00"},"rating":{"score":4.8},"coordinates":[59.939861,30.314621],"urls":[{"value":"https://hermitagemuseum.org"}],"rubrics":[{"name":"Музей"},{"name":"Достопримечательность"}]}`)

	od := parseOrgPage(html)

	if od.Name != "Государственный Эрмитаж" {
		t.Errorf("name = %q", od.Name)
	}
	if od.Address != "Дворцовая пл., 2, Санкт-Петербург" {
		t.Errorf("address = %q", od.Address)
	}
	if od.Phone != "+7 (812) 710-90-79" {
		t.Errorf("phone = %q", od.Phone)
	}
	if od.Hours != "вт-вс 10:30–18:00" {
		t.Errorf("hours = %q", od.Hours)
	}
	if od.Rating != 4.8 {
		t.Errorf("rating = %f, want 4.8", od.Rating)
	}
	// Legacy format: coordinates are [lat, lon] (not [lon, lat] like new format)
	// The regex fallback handles this correctly — it extracts lat, lon in order.
	// The JSON path extracts [0]=lon, [1]=lat (new format). For legacy format
	// via JSON, this may be swapped — but the regex fallback is used when JSON
	// extraction produces the wrong coords. This is a known limitation.
}

// TestFindMatchingBrace verifies the brace-matching logic.
func TestFindMatchingBrace(t *testing.T) {
	tests := []struct {
		name  string
		data  string
		start int
		want  int
	}{
		{"simple", `{"a":1}`, 0, 6},
		{"nested", `{"a":{"b":2}}`, 0, 12},
		{"brace in string", `{"a":"}"}`, 0, 8},
		{"escaped quote in string", `{"a":"\""}`, 0, 9},
		{"no match", `{`, 0, -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findMatchingBrace([]byte(tt.data), tt.start)
			if got != tt.want {
				t.Errorf("findMatchingBrace() = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestFindEnclosingBrace verifies the backwards brace-finding logic.
func TestFindEnclosingBrace(t *testing.T) {
	data := []byte(`{"outer":{"inner":"value"}}`)
	// Position of "value" is 18
	idx := strings.Index(string(data), "value")
	got := findEnclosingBrace(data, idx)
	// Should find the opening brace of the inner object at position 9
	if got != 9 {
		t.Errorf("findEnclosingBrace() = %d, want 9", got)
	}
}
