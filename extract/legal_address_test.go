package extract

import (
	"strings"
	"testing"
)

// TestIsLegalAddress_Classifier is the discriminator battery for the legal-vs-
// venue address classifier. The NEGATIVE cases are the load-bearing ones: a real
// venue address must NOT be classified as legal (else it gets demoted out of the
// venue slot and the card's map link breaks — the false-demote regression the
// reviewer flagged).
func TestIsLegalAddress_Classifier(t *testing.T) {
	t.Parallel()
	legal := []struct{ name, addr string }{
		{"drive-igora legal seat", "11-я В.О. линия, дом № 38, литера А, помещение 91, Санкт-Петербург, 199178"},
		{"INN bundled", "г. Москва, ул. Тверская, 1, ИНН 7701234567"},
		{"OGRN bundled", "Невский проспект, 28, ОГРН 1027801234567"},
		{"yuridicheskiy label", "Юридический адрес: ул. Ленина, 5"},
		{"litera only", "ул. Садовая, 10, литера Б"},
		{"litera abbrev", "ул. Садовая, 10, лит. Б"},
		{"litera + pomeshchenie (igora seat)", "11-я В.О. линия, 38, литера А, помещение 91"},
		{"OOO company form", "ООО «Игора Драйв», Приозерское шоссе, 3"},
	}
	for _, tc := range legal {
		if !isLegalAddress(tc.addr) {
			t.Errorf("isLegalAddress(%q) = false, want true [%s]", tc.addr, tc.name)
		}
	}

	venue := []struct{ name, addr string }{
		{"plain SPb street", "Невский проспект, 28"},
		{"street with house+building", "Литейный проспект, 55, корпус 2"},
		{"venue with metro", "Приозерское шоссе, 3, к. 1"},
		{"english street", "Nevsky Prospect, 28"},
		{"dom + korpus", "улица Восстания, дом 12, корпус 3"},
		// A bare postal index must NOT classify a venue address as legal — that
		// would false-demote a geo-correct venue address out of the map slot.
		{"venue with postal index", "Невский проспект, 28, Санкт-Петербург, 191186"},
		{"venue with index, no markers", "ул. Марата, 84, Санкт-Петербург, 191002"},
		// помещение / офис / оф. occur on REAL venue suites and are deliberately
		// not legal signals — a venue in an office suite must keep the map slot.
		{"venue office suite", "Невский проспект, 28, корпус 2, офис 5"},
		{"venue pomeshchenie", "пр. Мира, 100, помещение 5Н"},
		{"venue ofis abbrev", "ул. Рубинштейна, 3, оф. 12"},
	}
	for _, tc := range venue {
		if isLegalAddress(tc.addr) {
			t.Errorf("isLegalAddress(%q) = true, want false (venue must not be demoted) [%s]", tc.addr, tc.name)
		}
	}
}

// TestSetAddressFact_Routing verifies the per-slot fill-if-nil routing: a legal
// candidate fills LegalAddress, a venue candidate fills Address, and a page that
// supplies BOTH populates BOTH slots independently.
func TestSetAddressFact_Routing(t *testing.T) {
	t.Parallel()

	t.Run("legal routes to LegalAddress, leaves Address nil", func(t *testing.T) {
		t.Parallel()
		var f Facts
		setAddressFact(&f, "ул. Ленина, 5, литера А, помещение 3")
		if f.Address != nil {
			t.Errorf("Address = %q, want nil (legal must not occupy the venue slot)", *f.Address)
		}
		if f.LegalAddress == nil || *f.LegalAddress != "ул. Ленина, 5, литера А, помещение 3" {
			t.Errorf("LegalAddress = %v, want the legal address", f.LegalAddress)
		}
	})

	t.Run("venue routes to Address, leaves LegalAddress nil", func(t *testing.T) {
		t.Parallel()
		var f Facts
		setAddressFact(&f, "Приозерское шоссе, 3, к. 1")
		if f.LegalAddress != nil {
			t.Errorf("LegalAddress = %q, want nil", *f.LegalAddress)
		}
		if f.Address == nil || *f.Address != "Приозерское шоссе, 3, к. 1" {
			t.Errorf("Address = %v, want the venue address", f.Address)
		}
	})

	t.Run("both candidates populate both slots", func(t *testing.T) {
		t.Parallel()
		var f Facts
		setAddressFact(&f, "Приозерское шоссе, 3, к. 1")                          // venue first
		setAddressFact(&f, "11-я В.О. линия, 38, литера А, помещение 91, 199178") // legal second
		if f.Address == nil || *f.Address != "Приозерское шоссе, 3, к. 1" {
			t.Errorf("Address = %v, want the venue address", f.Address)
		}
		if f.LegalAddress == nil || *f.LegalAddress != "11-я В.О. линия, 38, литера А, помещение 91, 199178" {
			t.Errorf("LegalAddress = %v, want the legal address", f.LegalAddress)
		}
	})

	t.Run("each slot is fill-if-nil (first value wins per slot)", func(t *testing.T) {
		t.Parallel()
		var f Facts
		setAddressFact(&f, "Приозерское шоссе, 3") // venue
		setAddressFact(&f, "Невский проспект, 28") // 2nd venue — must NOT clobber
		if f.Address == nil || *f.Address != "Приозерское шоссе, 3" {
			t.Errorf("Address = %v, want the FIRST venue address (fill-if-nil)", f.Address)
		}
	})
}

// TestExtractFacts_LegalAddressElementSplit verifies the multi-<address> DOM
// case end-to-end through ExtractFacts: a /contacts page with a legal seat in one
// <address> and the venue in another must split across the two slots.
func TestExtractFacts_LegalAddressElementSplit(t *testing.T) {
	t.Parallel()
	html := `<!DOCTYPE html><html lang="ru"><body><div class="contacts">
<address>11-я В.О. линия, дом № 38, литера А, помещение 91, Санкт-Петербург, 199178</address>
<address>Приозерское шоссе, дом 3, корпус 5</address>
</div></body></html>`
	facts := ExtractFacts(html, "https://drive-igora.ru/contacts/")
	if facts.LegalAddress == nil {
		t.Fatalf("LegalAddress = nil, want the В.О. линия legal seat")
	}
	if !strings.Contains(*facts.LegalAddress, "В.О. линия") {
		t.Errorf("LegalAddress = %q, want the В.О. линия legal seat", *facts.LegalAddress)
	}
	if facts.Address == nil {
		t.Fatalf("Address = nil, want the Приозерское venue address")
	}
	if !strings.Contains(*facts.Address, "Приозерское") {
		t.Errorf("Address = %q, want the Приозерское venue address", *facts.Address)
	}
}

// TestExtractFacts_VenueOnlyAddressNotDemoted is the extract-layer negative
// control: a /contacts page with ONLY a real venue address (no legal markers)
// keeps the venue slot — it must NOT be demoted to LegalAddress.
func TestExtractFacts_VenueOnlyAddressNotDemoted(t *testing.T) {
	t.Parallel()
	html := `<!DOCTYPE html><html lang="ru"><body><div class="contacts">
<address>Невский проспект, 28</address>
</div></body></html>`
	facts := ExtractFacts(html, "https://example.ru/contacts/")
	if facts.LegalAddress != nil {
		t.Errorf("LegalAddress = %q, want nil (venue address must not be demoted)", *facts.LegalAddress)
	}
	if facts.Address == nil || !strings.Contains(*facts.Address, "Невский") {
		t.Fatalf("Address = %v, want the venue address in the venue slot", facts.Address)
	}
}
