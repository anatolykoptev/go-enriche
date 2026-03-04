package maps

import "testing"

func TestParseOrgPage_Full(t *testing.T) {
	html := []byte(`<html><body><script>
	{"name":"Государственный Эрмитаж","status":"open",
	 "address":{"formatted":"Дворцовая пл., 2, Санкт-Петербург"},
	 "phones":[{"formatted":"+7 (812) 710-90-79"}],
	 "hours":{"text":"вт-вс 10:30–18:00"},
	 "rating":{"score":4.8,"count":12000},
	 "coordinates":[59.939861,30.314621],
	 "urls":[{"value":"https://hermitagemuseum.org"}],
	 "rubrics":[{"name":"Музей"},{"name":"Достопримечательность"}]}
	</script></body></html>`)

	od := parseOrgPage(html)

	if od.Status != PlaceOpen {
		t.Errorf("status = %q, want %q", od.Status, PlaceOpen)
	}
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
	if od.Latitude != 59.939861 || od.Longitude != 30.314621 {
		t.Errorf("coords = (%f, %f)", od.Latitude, od.Longitude)
	}
	if od.Website != "https://hermitagemuseum.org" {
		t.Errorf("website = %q", od.Website)
	}
	if len(od.Categories) != 2 || od.Categories[0] != "Музей" {
		t.Errorf("categories = %v", od.Categories)
	}
}

func TestParseOrgPage_Closed(t *testing.T) {
	html := []byte(`{"status":"permanent-closed","name":"Old Place"}`)
	od := parseOrgPage(html)

	if od.Status != PlacePermanentClosed {
		t.Errorf("status = %q, want %q", od.Status, PlacePermanentClosed)
	}
	if od.Name != "Old Place" {
		t.Errorf("name = %q", od.Name)
	}
}

func TestParseOrgPage_Minimal(t *testing.T) {
	html := []byte(`<html><body>No JSON data here at all</body></html>`)
	od := parseOrgPage(html)

	if od.Status != "" {
		t.Errorf("status = %q, want empty", od.Status)
	}
	if od.Name != "" {
		t.Errorf("name = %q, want empty", od.Name)
	}
}

func TestParseOrgPage_DuplicateRubrics(t *testing.T) {
	html := []byte(`{"rubrics":[{"name":"Кафе"},{"name":"Кафе"},{"name":"Ресторан"}]}`)
	od := parseOrgPage(html)

	if len(od.Categories) != 2 {
		t.Errorf("categories = %v, want 2 unique", od.Categories)
	}
}

func TestExtractCoords_Invalid(t *testing.T) {
	re := reOrgCoords
	lat, lon := extractCoords(re, []byte(`"coordinates":["abc","def"]`))
	if lat != 0 || lon != 0 {
		t.Errorf("expected 0,0 for invalid coords, got %f,%f", lat, lon)
	}
}

func TestUnescapeJSON(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{`hello \"world\"`, `hello "world"`},
		{`path\\to`, `path\to`},
		{`url\/path`, `url/path`},
		{`line1\nline2`, "line1\nline2"},
		{`  trimmed  `, `trimmed`},
	}
	for _, tt := range tests {
		got := unescapeJSON(tt.input)
		if got != tt.want {
			t.Errorf("unescapeJSON(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
