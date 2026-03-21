package extract

import "testing"

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
