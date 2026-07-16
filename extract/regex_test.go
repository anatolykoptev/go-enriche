package extract

import (
	"regexp"
	"strings"
	"testing"
)

func TestRegexAddress_Russian(t *testing.T) {
	t.Parallel()
	text := "Контакты: Адрес: Невский проспект, 100, Санкт-Петербург"
	addr := regexAddress(text)
	if addr == nil {
		t.Fatal("expected address, got nil")
	}
	if *addr != "Невский проспект, 100, Санкт-Петербург" {
		t.Errorf("unexpected address: %s", *addr)
	}
}

func TestRegexAddress_English(t *testing.T) {
	t.Parallel()
	text := "Address: 123 Main Street, Springfield"
	addr := regexAddress(text)
	if addr == nil {
		t.Fatal("expected address, got nil")
	}
}

func TestRegexAddress_NotFound(t *testing.T) {
	t.Parallel()
	text := "No location information here."
	addr := regexAddress(text)
	if addr != nil {
		t.Errorf("expected nil, got %v", *addr)
	}
}

func TestRegexPhone_Russian(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plus7", "Телефон: +7 (812) 555-12-34", "+7 (812) 555-12-34"},
		{"eight", "Звоните: 8(495)123-45-67", "8(495)123-45-67"},
		{"compact", "тел. +79215551234", "+79215551234"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			phone := regexPhone(tt.input)
			if phone == nil {
				t.Fatal("expected phone, got nil")
			}
			if *phone != tt.want {
				t.Errorf("expected %q, got %q", tt.want, *phone)
			}
		})
	}
}

func TestRegexPhone_NotFound(t *testing.T) {
	t.Parallel()
	text := "No phone number here at all."
	phone := regexPhone(text)
	if phone != nil {
		t.Errorf("expected nil, got %v", *phone)
	}
}

func TestRegexPrice_Russian(t *testing.T) {
	t.Parallel()
	text := "Стоимость: от 500 до 1500 рублей"
	price := regexPrice(text)
	if price == nil {
		t.Fatal("expected price, got nil")
	}
}

func TestRegexPrice_English(t *testing.T) {
	t.Parallel()
	text := "Price: $25 per person"
	price := regexPrice(text)
	if price == nil {
		t.Fatal("expected price, got nil")
	}
}

func TestRegexPrice_NotFound(t *testing.T) {
	t.Parallel()
	text := "Nothing about cost here."
	price := regexPrice(text)
	if price != nil {
		t.Errorf("expected nil, got %v", *price)
	}
}

// TestRegexPrice_RejectsProseAfterKeyword guards issue #56: rePrice used to
// capture ANY 2-80 chars after "цена"/"стоимость"/"price", so marketing prose
// where the keyword is part of a sentence (not followed by a figure) leaked
// through as a garbage "price" fact. The regex must require a price-shaped
// token (digit, currency symbol, or от/до/free prefix + digit) right after
// the keyword, rejecting prose like "уборки за 30 минут".
func TestRegexPrice_RejectsProseAfterKeyword(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		text string
	}{
		{"prose with incidental digit", "Цена: уборки за 30 минут гарантированно!"},
		{"prose with seconds", "Цена: уборки за 10 секунд!"},
		{"schema tier symbol no digit", "Цена: ₽₽ за уборку"},
		{"tickets prose", "стоимость билетов ...: Ботанический сад"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			price := regexPrice(tt.text)
			if price != nil {
				t.Errorf("expected nil price for %q, got %q", tt.text, *price)
			}
		})
	}
}

// TestRegexPrice_AcceptsRealPrices ensures the tightened regex still captures
// legitimate price shapes — bare amount, range, currency-prefixed, free.
func TestRegexPrice_AcceptsRealPrices(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		text string
		want string // substring the capture must contain
	}{
		{"bare rubles", "Цена: 1500 руб.", "1500"},
		{"range rubles", "Стоимость: от 2300 до 4500 рублей", "2300"},
		{"dollar", "Price: $25 per person", "25"},
		{"from prefix", "Стоимость: от 200 рублей", "200"},
		{"free keyword", "Стоимость: бесплатно (0 руб)", "бесплатно"},
		{"range with symbol", "Цена: 1500-2500 ₽", "1500"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			price := regexPrice(tt.text)
			if price == nil {
				t.Fatalf("expected price for %q, got nil", tt.text)
			}
			if !strings.Contains(*price, tt.want) {
				t.Errorf("expected capture containing %q, got %q", tt.want, *price)
			}
		})
	}
}

func TestRegexSubmatch_WhitespaceOnly(t *testing.T) {
	t.Parallel()
	// Regression: regexSubmatch must return nil (not &"") when capture
	// group matches whitespace that trims to empty string.
	re := regexp.MustCompile(`test[:\s]+([^\n]{5,20})`)
	result := regexSubmatch(re, "test:      \n")
	if result != nil {
		t.Errorf("expected nil for whitespace-only capture, got %q", *result)
	}
}

func TestRegexMatch_WhitespaceOnly(t *testing.T) {
	t.Parallel()
	// Regression: regexMatch must return nil (not &"") when full match
	// trims to empty string.
	re := regexp.MustCompile(`\s{3,}`)
	result := regexMatch(re, "hello     world")
	if result != nil {
		t.Errorf("expected nil for whitespace-only match, got %q", *result)
	}
}
