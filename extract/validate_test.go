package extract

import (
	"strings"
	"testing"
)

func TestValidatePhone(t *testing.T) {
	valid := []struct {
		name  string
		phone string
	}{
		{"mobile spaced", "+7 (921) 555-12-34"},
		{"mobile 8 prefix", "8(911)123-45-67"},
		{"mobile compact", "+79215551234"},
		{"moscow 495", "+7 (495) 123-45-67"},
		{"moscow 499", "8(499)123-45-67"},
		{"spb 812", "+7 (812) 555-12-34"},
		{"novosibirsk 383", "+7 (383) 222-33-44"},
		{"mobile 900", "+79001234567"},
		{"mobile 999", "+79991234567"},
	}

	for _, tc := range valid {
		t.Run("valid/"+tc.name, func(t *testing.T) {
			if !ValidatePhone(tc.phone) {
				t.Errorf("expected valid: %s", tc.phone)
			}
		})
	}

	invalid := []struct {
		name  string
		phone string
	}{
		{"code 106", "81063196745"},
		{"code 068", "80684534804"},
		{"code 150", "+71501234567"},
		{"code 200", "+72001234567"},
		{"code 600", "+76001234567"},
		{"code 700", "+77001234567"},
		{"code 000", "+70001234567"},
		{"code 100", "+71001234567"},
		{"code 550", "+75501234567"},
		{"too short", "+7812555"},
		{"too long", "+781255512345"},
		{"no prefix", "9215551234"},
	}

	for _, tc := range invalid {
		t.Run("invalid/"+tc.name, func(t *testing.T) {
			if ValidatePhone(tc.phone) {
				t.Errorf("expected invalid: %s", tc.phone)
			}
		})
	}
}

func TestValidatePrice(t *testing.T) {
	valid := []struct {
		name  string
		price string
	}{
		{"digits only", "500"},
		{"range with currency", "от 500 до 1500 рублей"},
		{"rub short", "300 руб."},
		{"dollar sign", "$25"},
		{"free russian", "бесплатно"},
		{"zero", "0"},
		{"range with symbol", "1500-2500 ₽"},
		{"from price", "от 200 рублей"},
		{"currency symbols", "₽₽"},
	}

	for _, tc := range valid {
		t.Run("valid/"+tc.name, func(t *testing.T) {
			if !ValidatePrice(tc.price) {
				t.Errorf("expected valid: %s", tc.price)
			}
		})
	}

	invalid := []struct {
		name  string
		price string
	}{
		{"css inline", `not(:empty){margin-top:4px}.business-card-bko-view__products{padding:12px 0}.bus`},
		{"html tag", `<span class="price">500</span>`},
		{"long text", "и адрес. Мы могли отвезти в Сестрорецк начос за 200 рублей, при этом потратив в"},
		{"js code", "var price = 500;"},
		{"json", `{"price": 500, "currency": "RUB"}`},
		{"empty", ""},
		{"spaces", "   "},
		{"url", "https://example.com/price"},
		{"css class", ".price-block{display:none}"},
	}

	for _, tc := range invalid {
		t.Run("invalid/"+tc.name, func(t *testing.T) {
			if ValidatePrice(tc.price) {
				t.Errorf("expected invalid: %s", tc.price)
			}
		})
	}
}

func TestValidateAddress(t *testing.T) {
	valid := []struct {
		name string
		addr string
	}{
		{"russian street short", "ул. Пушкина, 10"},
		{"prospect full", "Невский проспект, 100"},
		{"prospect abbrev", "пр. Стачек, 45"},
		{"pereulok with city", "пер. Озерный, 7, Санкт-Петербург"},
		{"embankment", "наб. канала Грибоедова, 22"},
		{"highway", "Приморское ш., 427"},
		{"square", "пл. Островского, 6"},
		{"boulevard", "Конногвардейский б-р, 4"},
		{"korpus", "ул. Марата, 10, корп. 2"},
		{"english street", "123 Main Street, Springfield"},
		{"english st", "42 Baker St, London"},
	}

	for _, tc := range valid {
		t.Run("valid/"+tc.name, func(t *testing.T) {
			if !ValidateAddress(tc.addr) {
				t.Errorf("expected valid: %s", tc.addr)
			}
		})
	}

	invalid := []struct {
		name string
		addr string
	}{
		{"page title marketing", "Кафе «Nothing Fancy» Санкт-Петербург: бронирование, цены, меню, адрес"},
		{"listing marketing", "объекта: Красного Текстильщика д. 10-12, лит. У. Напрямую от собственника, без комиссии."},
		{"culture ref", "и т. д. на официальном сайте Культура.РФ"},
		{"css", "margin: 0 auto; padding: 10px"},
		{"too long", strings.Repeat("a", 200)},
		{"empty", ""},
		{"url", "https://example.com/address"},
		{"meta desc", "Бар «Пробирочная» Санкт-Петербург: бронирование, цены, меню, адрес и фото"},
		{"just city no street", "Санкт-Петербург"},
		{"ad text", "Аренда от собственника. Без комиссии. Офис 50 кв.м."},
	}

	for _, tc := range invalid {
		t.Run("invalid/"+tc.name, func(t *testing.T) {
			if ValidateAddress(tc.addr) {
				t.Errorf("expected invalid: %s", tc.addr)
			}
		})
	}
}
