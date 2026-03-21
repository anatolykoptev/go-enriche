package extract

import "testing"

func TestExtractSnippetFacts_Address(t *testing.T) {
	t.Parallel()
	text := "Кафе Рога и Копыта: адрес ул. Ленина, 42, Москва"
	var facts Facts
	ExtractSnippetFacts(text, &facts)
	if facts.Address == nil {
		t.Fatal("expected address from snippet")
	}
	if *facts.Address != "ул. Ленина, 42, Москва" {
		t.Errorf("got %q", *facts.Address)
	}
}

func TestExtractSnippetFacts_Phone(t *testing.T) {
	t.Parallel()
	text := "Контакт: +7 (812) 555-12-34, ежедневно с 10 до 22"
	var facts Facts
	ExtractSnippetFacts(text, &facts)
	if facts.Phone == nil {
		t.Fatal("expected phone from snippet")
	}
}

func TestExtractSnippetFacts_Price(t *testing.T) {
	t.Parallel()
	text := "Средний чек: цена 500-1500 руб."
	var facts Facts
	ExtractSnippetFacts(text, &facts)
	if facts.Price == nil {
		t.Fatal("expected price from snippet")
	}
}

func TestExtractSnippetFacts_DoesNotOverwrite(t *testing.T) {
	t.Parallel()
	existing := "ул. Пушкина, 1"
	facts := Facts{Address: &existing}
	text := "адрес ул. Ленина, 42"
	ExtractSnippetFacts(text, &facts)
	if *facts.Address != "ул. Пушкина, 1" {
		t.Errorf("should not overwrite existing, got %q", *facts.Address)
	}
}

func TestExtractSnippetFacts_MultipleFields(t *testing.T) {
	t.Parallel()
	text := "Ресторан Теремок\nадрес ул. Невского, 28\nТелефон: +7 (495) 123-45-67\nцена от 300 руб."
	var facts Facts
	ExtractSnippetFacts(text, &facts)
	if facts.Address == nil {
		t.Error("expected address")
	}
	if facts.Phone == nil {
		t.Error("expected phone")
	}
	if facts.Price == nil {
		t.Error("expected price")
	}
}

func TestExtractSnippetFacts_EmptyText(t *testing.T) {
	t.Parallel()
	var facts Facts
	ExtractSnippetFacts("", &facts)
	if facts.Address != nil || facts.Phone != nil || facts.Price != nil {
		t.Error("expected all nil for empty text")
	}
}

func TestExtractSnippetFacts_NilFacts(t *testing.T) {
	t.Parallel()
	// Should not panic.
	ExtractSnippetFacts("адрес ул. Ленина, 1", nil)
}

func TestExtractSnippetFacts_RejectsJunkAddress(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		text string
	}{
		{"title junk", "подробное описание, адрес и фото"},
		{"meta junk", "адрес на карте, режимы работы, интересные факты"},
		{"price list", "адрес, цены, как пройти, режим работы"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var facts Facts
			ExtractSnippetFacts(tt.text, &facts)
			if facts.Address != nil {
				t.Errorf("expected nil address for junk input %q, got %q", tt.text, *facts.Address)
			}
		})
	}
}

func TestExtractSnippetFacts_RejectsJunkPrice(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		text string
	}{
		{"title junk", "стоимость билетов ...: Ботанический сад"},
		{"meta junk", "режим работы и стоимость билетов, как добраться"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var facts Facts
			ExtractSnippetFacts(tt.text, &facts)
			if facts.Price != nil {
				t.Errorf("expected nil price for junk input %q, got %q", tt.text, *facts.Price)
			}
		})
	}
}

func TestExtractSnippetFacts_AcceptsRealAddress(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		text string
		want string
	}{
		{"street", "адрес: ул. Профессора Попова, 2", "ул. Профессора Попова, 2"},
		{"prospect", "адрес Невский пр., 28", "Невский пр., 28"},
		{"island", "адрес: Елагин остров, 4", "Елагин остров, 4"},
		{"city prefix", "адрес: город Петергоф, ул. Разводная, 2", "город Петергоф, ул. Разводная, 2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var facts Facts
			ExtractSnippetFacts(tt.text, &facts)
			if facts.Address == nil {
				t.Fatalf("expected address for %q", tt.text)
			}
			if *facts.Address != tt.want {
				t.Errorf("got %q, want %q", *facts.Address, tt.want)
			}
		})
	}
}

func TestExtractSnippetFacts_RejectsGarbagePhone(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		text string
	}{
		{"random digits", "Контакт: 81063196745, ежедневно"},
		{"invalid area", "Телефон: 80684534804"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var facts Facts
			ExtractSnippetFacts(tt.text, &facts)
			if facts.Phone != nil {
				t.Errorf("expected nil phone for %q, got %q", tt.text, *facts.Phone)
			}
		})
	}
}

func TestExtractSnippetFacts_AcceptsRealPrice(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		text string
	}{
		{"rubles", "стоимость: от 500 до 1500 рублей"},
		{"rub short", "цена 300 руб."},
		{"free", "стоимость: бесплатно (0 руб)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var facts Facts
			ExtractSnippetFacts(tt.text, &facts)
			if facts.Price == nil {
				t.Fatalf("expected price for %q", tt.text)
			}
		})
	}
}
