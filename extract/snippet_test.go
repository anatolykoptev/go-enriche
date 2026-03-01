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
