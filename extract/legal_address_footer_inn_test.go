package extract

import (
	"strings"
	"testing"
)

// TestExtractFacts_FooterINN_VenueOrg_StaysVenue is the round-3 BLOCKER negative
// control. It reproduces the page-scope-corroborant-#2 false-demote: a venue
// /contacts page that prints footer requisites (ООО «…», ИНН …, ОГРН …) — which
// are near-universal on RU venue sites — alongside a BARE schema.org/Organization
// block carrying the venue's OWN visiting streetAddress, with NO separate Place
// block and NO distinct <address> venue element.
//
// Under the (removed) page-scope corroborant #2, the footer ИНН satisfied
// pageHasLegalEntityMarker(html) over the WHOLE page, so the lone Organization
// streetAddress was routed to LegalAddress and the venue lost its map slot — the
// same false-demote class as the литера-substring bug, via a third signal.
//
// The footer ИНН identifies a LEGAL ENTITY somewhere on the page; it does NOT
// prove the Organization block's streetAddress is the registered SEAT rather than
// the venue's visiting address. The only trustworthy corroborants are the
// IN-ITEM ones (org.HasLegalID / org.LegalName), the legal STRING marker on the
// address itself (corroborant #1), and a DISTINCT venue address present elsewhere
// (corroborant #4). None hold here, so the venue address MUST stay in the map slot.
func TestExtractFacts_FooterINN_VenueOrg_StaysVenue(t *testing.T) {
	t.Parallel()
	// A bare Organization block (no in-item ИНН/ОГРН/legalName) carrying the venue's
	// own visiting address, plus footer requisites printed as ordinary page text.
	// No separate Place/LocalBusiness block, no distinct <address> venue element.
	html := `<!DOCTYPE html><html lang="ru"><body>
<div itemscope itemtype="http://schema.org/Organization">
  <meta itemprop="name" content="Кафе Уют" />
  <div itemprop="address" itemscope itemtype="http://schema.org/PostalAddress">
    <span itemprop="streetAddress">Невский проспект, 28</span>
    <span itemprop="addressLocality">Санкт-Петербург</span>
  </div>
</div>
<footer><p>ООО «Уют», ИНН 7801234567, ОГРН 1117847123456</p></footer>
</body></html>`
	facts := ExtractFacts(html, "https://kafe-uyut.ru/")
	if facts.Address == nil || !strings.Contains(*facts.Address, "Невский") {
		t.Fatalf("Address = %v, want the venue address «Невский проспект, 28» in the map slot — a footer ИНН must NOT demote the venue's own Organization streetAddress", facts.Address)
	}
	if facts.LegalAddress != nil {
		t.Errorf("LegalAddress = %q, want nil — a page-scope footer ИНН/ОГРН is not a corroborant that the lone Organization streetAddress is the legal seat", *facts.LegalAddress)
	}
}
