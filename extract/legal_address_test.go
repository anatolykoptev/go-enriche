package extract

import (
	"strings"
	"testing"
)

// TestIsLegalAddress_Classifier is the STRING-based discriminator battery. The
// NEGATIVE cases are the load-bearing ones: a real venue address must NOT be
// classified as legal (else it gets demoted out of the venue slot and the card's
// map link breaks — the false-demote regression the reviewer flagged). The
// string classifier fires ONLY on a strong explicit legal marker
// (юридическ/реквизит/ИНН/ОГРН/ОГРНИП/КПП/entity-form). литера / лит. / помещение
// / a postal index are NOT signals — they appear in normal venue addresses. A
// markerless registration-format string (e.g. Игора's «…литера А, помещение 91,
// 199178») is classified VENUE by string and is caught as LEGAL only via
// PROVENANCE when it is the address of a schema.org/Organization block (see
// TestExtractFacts_OrgAddressIsLegal_ProvenanceArm).
func TestIsLegalAddress_Classifier(t *testing.T) {
	t.Parallel()
	legal := []struct{ name, addr string }{
		{"INN bundled", "г. Москва, ул. Тверская, 1, ИНН 7701234567"},
		{"OGRN bundled", "Невский проспект, 28, ОГРН 1027801234567"},
		{"OGRNIP bundled", "ул. Ленина, 5, ОГРНИП 320784700123456"},
		{"KPP bundled", "ул. Мира, 7, КПП 784001001"},
		{"yuridicheskiy label", "Юридический адрес: ул. Ленина, 5"},
		{"rekvizity label", "Реквизиты: ул. Ленина, 5, литера А"},
		{"OOO company form", "ООО «Игора Драйв», Приозерское шоссе, 3"},
		{"OOO mid-line after comma", "Приозерское шоссе, 3, ООО «Ромашка»"},
		// MAJOR fix: \bао\b / \bип\b never matched (RE2 \b is ASCII-only, can't
		// bind a Cyrillic token). The Cyrillic-safe boundary makes ИП/АО classify.
		{"IP prefix (Cyrillic boundary)", "ИП Иванов, ул. Садовая, 5"},
		{"AO mid-line after comma", "ул. Садовая, 5, АО «Энергия»"},
		{"IP after comma", "ул. Садовая, 5, ИП Петров"},
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
		// --- The reviewer-named false-demote negative controls (RED on the old
		// литера-substring classifier, GREEN now). литера / лит. are NOT legal
		// signals: they appear in ubiquitous SPb visiting addresses, and the
		// substring «литер» even matches the STREET NAME «Литераторов».
		{"venue litera A (Marshala Zhukova)", "ул. Маршала Жукова, 28, литера А"},
		{"street name contains litera substring", "улица Литераторов, 19"},
		{"venue lit. abbrev (Kamennoostrovsky)", "Каменноостровский пр., 42, лит. А"},
		{"venue litera B", "ул. Садовая, 10, литера Б"},
		{"venue lit. B abbrev", "ул. Садовая, 10, лит. Б"},
		// A bare postal index must NOT classify a venue address as legal.
		{"venue with postal index", "Невский проспект, 28, Санкт-Петербург, 191186"},
		{"venue with index, no markers", "ул. Марата, 84, Санкт-Петербург, 191002"},
		// помещение / офис / оф. occur on REAL venue suites — not legal signals.
		{"venue office suite", "Невский проспект, 28, корпус 2, офис 5"},
		{"venue pomeshchenie", "пр. Мира, 100, помещение 5Н"},
		{"venue ofis abbrev", "ул. Рубинштейна, 3, оф. 12"},
		// Игора's seat string by itself (no ИНН/entity) is VENUE by STRING — it
		// is caught as legal only by Organization PROVENANCE, not this function.
		{"igora seat string alone (no entity marker)", "11-я В.О. линия, дом № 38, литера А, помещение 91, Санкт-Петербург, 199178"},
		// A street whose name embeds a company-form token's letters must not match
		// (Cyrillic-boundary guard: «Заозёрная» contains «ао», «Липецкая» «ип»).
		{"street name embeds ao letters", "Заозёрная улица, 12"},
		{"street name embeds ip letters", "Липецкая улица, 7"},
	}
	for _, tc := range venue {
		if isLegalAddress(tc.addr) {
			t.Errorf("isLegalAddress(%q) = true, want false (venue must not be demoted) [%s]", tc.addr, tc.name)
		}
	}
}

// TestSetAddressFact_Routing verifies the per-slot fill-if-nil routing for the
// STRING-based path: a strong-marker legal candidate fills LegalAddress, a venue
// candidate fills Address, and a page that supplies BOTH populates BOTH slots.
func TestSetAddressFact_Routing(t *testing.T) {
	t.Parallel()

	t.Run("legal (entity marker) routes to LegalAddress, leaves Address nil", func(t *testing.T) {
		t.Parallel()
		var f Facts
		setAddressFact(&f, "ООО «Ромашка», ул. Ленина, 5, литера А, помещение 3")
		if f.Address != nil {
			t.Errorf("Address = %q, want nil (legal must not occupy the venue slot)", *f.Address)
		}
		if f.LegalAddress == nil || *f.LegalAddress != "ООО «Ромашка», ул. Ленина, 5, литера А, помещение 3" {
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

	t.Run("litera-only candidate routes to Address (venue), NOT demoted", func(t *testing.T) {
		t.Parallel()
		var f Facts
		setAddressFact(&f, "ул. Маршала Жукова, 28, литера А")
		if f.LegalAddress != nil {
			t.Errorf("LegalAddress = %q, want nil — литера alone must NOT demote a venue", *f.LegalAddress)
		}
		if f.Address == nil || *f.Address != "ул. Маршала Жукова, 28, литера А" {
			t.Errorf("Address = %v, want the venue address (litera is not a legal signal)", f.Address)
		}
	})

	t.Run("both candidates populate both slots", func(t *testing.T) {
		t.Parallel()
		var f Facts
		setAddressFact(&f, "Приозерское шоссе, 3, к. 1")                    // venue
		setAddressFact(&f, "ИНН 7801321150, 11-я В.О. линия, 38, литера А") // legal (ИНН marker)
		if f.Address == nil || *f.Address != "Приозерское шоссе, 3, к. 1" {
			t.Errorf("Address = %v, want the venue address", f.Address)
		}
		if f.LegalAddress == nil || *f.LegalAddress != "ИНН 7801321150, 11-я В.О. линия, 38, литера А" {
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

// TestSetOrgAddressFact_AlwaysLegal verifies the PROVENANCE arm: an
// Organization-sourced address ALWAYS fills LegalAddress, even when its string
// carries no legal marker (no ИНН/ОГРН/entity) — the Игора-seat shape.
func TestSetOrgAddressFact_AlwaysLegal(t *testing.T) {
	t.Parallel()
	var f Facts
	// No ИНН/entity in the string — string classification would call this VENUE.
	setOrgAddressFact(&f, "11-я В.О. линия, дом № 38, литера А, помещение 91, Санкт-Петербург, 199178")
	if f.Address != nil {
		t.Errorf("Address = %q, want nil — an Organization address is legal by provenance", *f.Address)
	}
	if f.LegalAddress == nil || !strings.Contains(*f.LegalAddress, "В.О. линия") {
		t.Errorf("LegalAddress = %v, want the Organization legal seat", f.LegalAddress)
	}
}

// TestExtractFacts_OrgAddressIsLegal_ProvenanceArm is the empirically-grounded
// end-to-end test built from the LIVE drive-igora.ru/contacts DOM shape: the
// legal seat is the streetAddress of a hidden schema.org/Organization block,
// while the visible venue address is in <address class="contacts__address">. The
// legal seat string carries NO ИНН/ОГРН — it is caught ONLY by Organization
// provenance — yet must NOT occupy the venue Address slot (which holds the card's
// map link). This is the positive Игора case the reviewer required to stay green.
func TestExtractFacts_OrgAddressIsLegal_ProvenanceArm(t *testing.T) {
	t.Parallel()
	// Mirrors drive-igora.ru/contacts: a display:none Organization microdata block
	// (the registered seat) + a visible <address> venue block.
	html := `<!DOCTYPE html><html lang="ru"><body>
<address class="contacts__address"><p>Россия, Ленинградская область, Приозерский район, Приозерское шоссе д.3 к.5</p></address>
<div style="display:none" itemscope itemtype="http://schema.org/Organization">
  <meta itemprop="name" content="Игора" />
  <div itemprop="address" itemscope itemtype="http://schema.org/PostalAddress">
    <span itemprop="streetAddress">11-я В.О. линия, дом № 38, литера А, помещение 91</span>
    <span itemprop="addressLocality">Санкт-Петербург</span>
    <span itemprop="addressCountry">Россия</span>
    <span itemprop="postalCode">199178</span>
  </div>
</div>
</body></html>`
	facts := ExtractFacts(html, "https://drive-igora.ru/contacts/")
	if facts.LegalAddress == nil {
		t.Fatalf("LegalAddress = nil, want the В.О. линия Organization legal seat")
	}
	if !strings.Contains(*facts.LegalAddress, "В.О. линия") {
		t.Errorf("LegalAddress = %q, want the В.О. линия legal seat", *facts.LegalAddress)
	}
	if facts.Address == nil {
		t.Fatalf("Address = nil, want the Приозерское venue address in the map slot")
	}
	if !strings.Contains(*facts.Address, "Приозерское") {
		t.Errorf("Address = %q, want the Приозерское venue address", *facts.Address)
	}
}

// TestExtractFacts_VenueLiteraNotDemoted is the end-to-end negative control: a
// /contacts page whose ONLY address is a real venue visiting address that
// happens to contain «литера» (with NO Organization block and NO entity marker)
// keeps the venue slot — it must NOT be demoted to LegalAddress. This is the
// broader regression the reviewer flagged: a litera-bearing venue with no maps
// address would otherwise lose its map link entirely.
func TestExtractFacts_VenueLiteraNotDemoted(t *testing.T) {
	t.Parallel()
	html := `<!DOCTYPE html><html lang="ru"><body><div class="contacts">
<address>ул. Маршала Жукова, 28, литера А</address>
</div></body></html>`
	facts := ExtractFacts(html, "https://example.ru/contacts/")
	if facts.LegalAddress != nil {
		t.Errorf("LegalAddress = %q, want nil (a litera-bearing venue must not be demoted)", *facts.LegalAddress)
	}
	if facts.Address == nil || !strings.Contains(*facts.Address, "Маршала Жукова") {
		t.Fatalf("Address = %v, want the venue address in the venue slot (holds the map link)", facts.Address)
	}
}

// TestExtractFacts_VenueOnlyAddressNotDemoted is the extract-layer negative
// control: a /contacts page with ONLY a plain venue address (no markers) keeps
// the venue slot.
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

// TestExtractFacts_BareOrgVenueAddress_NoCoSignal_StaysVenue is the MAKE-OR-BREAK
// negative control the reviewer demanded (PROBE A): a bare schema.org/Organization
// whose streetAddress is in fact the venue's visiting address — NO separate Place
// block, NO ИНН/ОГРН/legalName anywhere, NO other address source. The Organization
// itemtype ALONE must NOT demote that address to LegalAddress, or the venue loses
// its map slot (the false-demote class, via the provenance signal instead of the
// литера substring). Before the corroborant narrowing this was RED (the org-
// provenance arm routed EVERY Organization streetAddress to LegalAddress
// unconditionally); after it, the markerless lone Org address STAYS venue.
func TestExtractFacts_BareOrgVenueAddress_NoCoSignal_StaysVenue(t *testing.T) {
	t.Parallel()
	html := `<!DOCTYPE html><html lang="ru"><body>
<div itemscope itemtype="http://schema.org/Organization">
  <meta itemprop="name" content="Кафе Уют" />
  <div itemprop="address" itemscope itemtype="http://schema.org/PostalAddress">
    <span itemprop="streetAddress">Невский проспект, 28</span>
    <span itemprop="addressLocality">Санкт-Петербург</span>
  </div>
</div>
</body></html>`
	facts := ExtractFacts(html, "https://kafe-uyut.ru/")
	if facts.Address == nil || !strings.Contains(*facts.Address, "Невский") {
		t.Fatalf("Address = %v, want the venue address in the map slot (a markerless lone Organization address must NOT be demoted)", facts.Address)
	}
	if facts.LegalAddress != nil {
		t.Errorf("LegalAddress = %q, want nil (no legal co-signal: no ИНН/ОГРН/legalName, no distinct venue address)", *facts.LegalAddress)
	}
}

// TestExtractFacts_LocalBusinessVenueAddress_StaysVenue is the load-bearing
// REGRESSION GUARD the reviewer flagged as untested: a schema.org/LocalBusiness
// block routes via FirstPlace → the STRING arm, so its (markerless) address stays
// in the venue slot. The parser type-set split (placeTypes routes LocalBusiness to
// FirstPlace; only Organization/Corporation/GovernmentOrganization route to
// FirstOrganization → the provenance arm) is what keeps a LocalBusiness address out
// of the legal-by-provenance path. This guard locks that invariant in: a future
// astappiev/microdata bump to a subtype-aware IsOfType, or a placeTypes / Org
// type-set edit, must not silently collapse LocalBusiness into the org-provenance
// arm and demote a venue address.
func TestExtractFacts_LocalBusinessVenueAddress_StaysVenue(t *testing.T) {
	t.Parallel()
	html := `<!DOCTYPE html><html lang="ru"><body>
<div itemscope itemtype="http://schema.org/LocalBusiness">
  <meta itemprop="name" content="Ресторан Волна" />
  <div itemprop="address" itemscope itemtype="http://schema.org/PostalAddress">
    <span itemprop="streetAddress">Литейный проспект, 55, корпус 2</span>
    <span itemprop="addressLocality">Санкт-Петербург</span>
  </div>
</div>
</body></html>`
	facts := ExtractFacts(html, "https://volna.ru/")
	if facts.Address == nil || !strings.Contains(*facts.Address, "Литейный") {
		t.Fatalf("Address = %v, want the venue address in the map slot (a LocalBusiness address must stay venue)", facts.Address)
	}
	if facts.LegalAddress != nil {
		t.Errorf("LegalAddress = %q, want nil (LocalBusiness routes via the Place string arm, never the org-provenance arm)", *facts.LegalAddress)
	}
}
