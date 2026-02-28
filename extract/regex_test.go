package extract

import "testing"

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
