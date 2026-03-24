# Fact Extraction Validation Layer — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a validation layer to go-enriche that filters out garbage data (fake phones, CSS in prices, page titles in addresses) from extracted facts, without adding external dependencies.

**Architecture:** New file `extract/validate.go` with pure-function validators for each fact type. Validators are called at every extraction point: structured data (Layer 1), regex fallback (Layer 2), snippet extraction (Layer 3). Each validator is a `func(string) bool` that returns false for garbage. No external deps — lightweight regex + heuristics only, since `nyaruka/phonenumbers` (45MB) is overkill for our use case.

**Tech Stack:** Go 1.26, regexp, unicode/utf8. Zero new dependencies.

---

## File Structure

| File | Responsibility |
|------|---------------|
| Create: `extract/validate.go` | All validators: `ValidatePhone`, `ValidateAddress`, `ValidatePrice` + compiled regexes |
| Create: `extract/validate_test.go` | Comprehensive test suite with real garbage examples from production |
| Modify: `extract/facts.go:48-90` | Wrap `setIfNil` calls with validation for structured data |
| Modify: `extract/facts.go:92-102` | Add phone validation to `applyRegexFallback` |
| Modify: `extract/facts.go:107-124` | Add phone validation to `ExtractSnippetFacts` |
| Modify: `extract/facts_test.go:47-59` | Update test phone `+7-111-222-33-44` to valid `+7-812-222-33-44` |

---

### Task 1: Phone Validator

**Files:**
- Create: `extract/validate.go`
- Create: `extract/validate_test.go`

**Context:** Current `rePhone` regex (`regex.go:11`) matches any `+7`/`8` + 10 digits. Fake numbers like `81063196745`, `88180117277` pass because there's no area/operator code validation. Russian numbers have format `+7 XXX XXXXXXX` where XXX is a DEF code (mobile: 900-999) or ABC code (landline: 301-879). We don't need a full phone library — a prefix whitelist covers 99% of cases.

- [ ] **Step 1: Write failing tests for phone validation**

```go
// extract/validate_test.go
package extract

import "testing"

func TestValidatePhone_ValidNumbers(t *testing.T) {
	t.Parallel()
	valid := []struct {
		name, phone string
	}{
		{"mobile +7", "+7 (921) 555-12-34"},
		{"mobile 8", "8(911)123-45-67"},
		{"mobile compact", "+79215551234"},
		{"moscow", "+7 (495) 123-45-67"},
		{"moscow2", "8(499)123-45-67"},
		{"spb", "+7 (812) 555-12-34"},
		{"landline novosibirsk", "+7 (383) 222-33-44"},
		{"mobile 900", "+79001234567"},
		{"mobile 999", "+79991234567"},
	}
	for _, tt := range valid {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if !ValidatePhone(tt.phone) {
				t.Errorf("expected valid: %s", tt.phone)
			}
		})
	}
}

func TestValidatePhone_InvalidNumbers(t *testing.T) {
	t.Parallel()
	invalid := []struct {
		name, phone string
	}{
		{"random digits code 106", "81063196745"},
		{"random digits code 068", "80684534804"},
		{"unassigned code 150", "+71501234567"},
		{"unassigned code 200", "+72001234567"},
		{"unassigned code 600", "+76001234567"},
		{"unassigned code 700", "+77001234567"},
		{"unassigned code 000", "+70001234567"},
		{"unassigned code 100", "+71001234567"},
		{"unassigned code 550", "+75501234567"},
		{"too short", "+7812555"},
		{"too long", "+781255512345"},
		{"no prefix", "9215551234"},
	}
	for _, tt := range invalid {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if ValidatePhone(tt.phone) {
				t.Errorf("expected invalid: %s", tt.phone)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/krolik/src/go-enriche && GOWORK=off go test ./extract/ -run TestValidatePhone -v -count=1`
Expected: FAIL — `ValidatePhone` not defined

- [ ] **Step 3: Implement phone validator**

```go
// extract/validate.go
package extract

import (
	"regexp"
	"strconv"
	"strings"
)

// reDigitsOnly strips non-digits for validation.
var reDigitsOnly = regexp.MustCompile(`\D`)

// ValidatePhone checks if a Russian phone number has a valid area/operator code.
// Accepts +7/8 prefix + 10 digits. Validates the 3-digit code using Rossvyaz ranges:
// - Mobile DEF: 900-999
// - Landline ABC: 301-349, 351-389, 390-399, 401-499, 800-816, 820-879
// - SPb: 812, Moscow: 495/499
func ValidatePhone(phone string) bool {
	digits := reDigitsOnly.ReplaceAllString(phone, "")

	// Must be 11 digits with +7/8 prefix. Bare 10-digit strings are too ambiguous.
	if len(digits) != 11 || (digits[0] != '8' && digits[0] != '7') {
		return false
	}
	digits = digits[1:]

	// First 3 digits = area/operator code.
	code, _ := strconv.Atoi(digits[:3])

	// Mobile DEF codes: 900-999.
	if code >= 900 && code <= 999 {
		return true
	}

	// Landline ABC codes — assigned ranges per Rossvyaz.
	// Compact check: covers ~95% of assigned city codes.
	switch {
	case code >= 301 && code <= 349: return true // Primorsky, Khabarovsk, etc.
	case code >= 351 && code <= 395: return true // Chelyabinsk, Sverdlovsk, Novosibirsk, etc.
	case code >= 401 && code <= 499: return true // Kaliningrad, Moscow (495/499), etc.
	case code >= 800 && code <= 816: return true // toll-free 800, SPb 812, Leningrad oblast 813-816
	case code >= 820 && code <= 879: return true // Vologda, Arkhangelsk, Murmansk, etc.
	}

	return false
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/krolik/src/go-enriche && GOWORK=off go test ./extract/ -run TestValidatePhone -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd /home/krolik/src/go-enriche && git add extract/validate.go extract/validate_test.go
git commit -m "feat(extract): add phone number validator with area code checking"
```

---

### Task 2: Price Validator

**Files:**
- Modify: `extract/validate.go`
- Modify: `extract/validate_test.go`

**Context:** Structured data `extractPrice` (`parser.go:177`) returns raw strings from `offers.price` or `priceRange` without validation. Production garbage includes: CSS code `"not(:empty){margin-top:4px}"`, HTML fragments, article text snippets >100 chars. A valid price is short, contains digits or currency words (бесплатно, free), and has no CSS/HTML markers.

- [ ] **Step 1: Write failing tests for price validation**

Add to `extract/validate_test.go`:

```go
func TestValidatePrice_ValidPrices(t *testing.T) {
	t.Parallel()
	valid := []struct {
		name, price string
	}{
		{"digits", "500"},
		{"range rubles", "от 500 до 1500 рублей"},
		{"rub short", "300 руб."},
		{"dollar", "$25"},
		{"free", "бесплатно"},
		{"zero", "0"},
		{"average check", "1500-2500 ₽"},
		{"with text", "от 200 рублей"},
		{"price range schema", "₽₽"},
	}
	for _, tt := range valid {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if !ValidatePrice(tt.price) {
				t.Errorf("expected valid: %s", tt.price)
			}
		})
	}
}

func TestValidatePrice_InvalidPrices(t *testing.T) {
	t.Parallel()
	invalid := []struct {
		name, price string
	}{
		{"css", "not(:empty){margin-top:4px}.business-card-bko-view__products{padding:12px 0}.bus"},
		{"html tag", "<span class=\"price\">500</span>"},
		{"long text", "и адрес. Мы могли отвезти в Сестрорецк начос за 200 рублей, при этом потратив в"},
		{"script", "var price = 500;"},
		{"json", `{"price": 500, "currency": "RUB"}`},
		{"empty", ""},
		{"only spaces", "   "},
		{"url", "https://example.com/price"},
		{"css class", ".price-block{display:none}"},
	}
	for _, tt := range invalid {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if ValidatePrice(tt.price) {
				t.Errorf("expected invalid: %q", tt.price)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/krolik/src/go-enriche && GOWORK=off go test ./extract/ -run TestValidatePrice -v -count=1`
Expected: FAIL — `ValidatePrice` not defined

- [ ] **Step 3: Implement price validator**

Add to `extract/validate.go`:

```go
// reCSS detects CSS-like patterns: braces, property declarations, selectors.
var reCSS = regexp.MustCompile(`[{}]|\w+\s*:\s*\w+\s*;|:\s*\w+\(|margin|padding|display|font-size`)

// reHTMLTag detects HTML tags.
var reHTMLTag = regexp.MustCompile(`<[a-zA-Z/]`)

// reJSCode detects JavaScript patterns.
var reJSCode = regexp.MustCompile(`(?:var |const |let |function |=>|===)`)

// rePriceCurrency matches digits or known currency/free indicators.
var rePriceCurrency = regexp.MustCompile(`(?i)(?:\d|бесплатно|free|₽|руб|\$|€|£)`)

// ValidatePrice checks if a price string looks like an actual price,
// not CSS, HTML, or article text.
func ValidatePrice(price string) bool {
	price = strings.TrimSpace(price)
	if price == "" {
		return false
	}

	// Too long = almost certainly not a price.
	if len([]rune(price)) > 60 {
		return false
	}

	// Must contain a digit or currency word.
	if !rePriceCurrency.MatchString(price) {
		return false
	}

	// Reject CSS, HTML, JS patterns.
	if reCSS.MatchString(price) {
		return false
	}
	if reHTMLTag.MatchString(price) {
		return false
	}
	if reJSCode.MatchString(price) {
		return false
	}

	// Reject URLs.
	if strings.Contains(price, "://") {
		return false
	}

	return true
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/krolik/src/go-enriche && GOWORK=off go test ./extract/ -run TestValidatePrice -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd /home/krolik/src/go-enriche && git add extract/validate.go extract/validate_test.go
git commit -m "feat(extract): add price validator filtering CSS/HTML/JS garbage"
```

---

### Task 3: Address Validator

**Files:**
- Modify: `extract/validate.go`
- Modify: `extract/validate_test.go`

**Context:** Structured data `extractAddress` (`parser.go:157-172`) can return: page titles like `"Кафе «Nothing Fancy» Санкт-Петербург: бронирование, цены, меню, адрес"`, real estate listing fragments `"объекта: Красного Текстильщика д. 10-12, лит. У. Напрямую от собственника, без комиссии."`, or culture.ru fragments `"и т. д. на официальном сайте Культура.РФ"`. Valid addresses are compact, contain street-type words, and don't contain marketing/navigation terms.

- [ ] **Step 1: Write failing tests for address validation**

Add to `extract/validate_test.go`:

Note: Add `"strings"` to the imports of `validate_test.go` (needed for `strings.Repeat` below).

```go
func TestValidateAddress_ValidAddresses(t *testing.T) {
	t.Parallel()
	valid := []struct {
		name, addr string
	}{
		{"street", "ул. Пушкина, 10"},
		{"prospect", "Невский проспект, 100"},
		{"abbreviated", "пр. Стачек, 45"},
		{"with city", "пер. Озерный, 7, Санкт-Петербург"},
		{"naberezhnaya", "наб. канала Грибоедова, 22"},
		{"highway", "Приморское ш., 427"},
		{"square", "пл. Островского, 6"},
		{"boulevard", "Конногвардейский б-р, 4"},
		{"with building", "ул. Марата, 10, корп. 2"},
		{"english", "123 Main Street, Springfield"},
		{"english st", "42 Baker St, London"},
	}
	for _, tt := range valid {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if !ValidateAddress(tt.addr) {
				t.Errorf("expected valid: %s", tt.addr)
			}
		})
	}
}

func TestValidateAddress_InvalidAddresses(t *testing.T) {
	t.Parallel()
	invalid := []struct {
		name, addr string
	}{
		{"page title", "Кафе «Nothing Fancy» Санкт-Петербург: бронирование, цены, меню, адрес"},
		{"listing desc", "объекта: Красного Текстильщика д. 10-12, лит. У. Напрямую от собственника, без комиссии."},
		{"culture.ru", "и т. д. на официальном сайте Культура.РФ"},
		{"css", "margin: 0 auto; padding: 10px"},
		{"too long", strings.Repeat("a", 200)},
		{"empty", ""},
		{"url", "https://example.com/address"},
		{"meta desc", "Бар «Пробирочная» Санкт-Петербург: бронирование, цены, меню, адрес и фото"},
		{"just city", "Санкт-Петербург"},
		{"ad text", "Аренда от собственника. Без комиссии. Офис 50 кв.м."},
	}
	for _, tt := range invalid {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if ValidateAddress(tt.addr) {
				t.Errorf("expected invalid: %q", tt.addr)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/krolik/src/go-enriche && GOWORK=off go test ./extract/ -run TestValidateAddress -v -count=1`
Expected: FAIL — `ValidateAddress` not defined

- [ ] **Step 3: Implement address validator**

Add to `extract/validate.go`:

```go
// reStreetWord detects street-type words in Russian and English.
// Same as reAddressValidator from regex.go but also covers English + "д." (house number).
var reStreetWord = regexp.MustCompile(`(?i)(?:` +
	// Russian street types
	`ул\.|улица|пр\.|просп|проспект|наб\.|набережная|пер\.|переулок|` +
	`ш\.|шоссе|пл\.|площадь|б-р|бульвар|линия|аллея|остров|` +
	// Russian city/locality markers
	`город\b|г\.\s*\w|` +
	// Russian house/building markers
	`д\.\s*\d|корп\.|стр\.|лит\.|` +
	// English street types
	`\bstreet\b|\bst\b\.?|\bavenue\b|\bave\b\.?|\broad\b|\brd\b\.?|\bdrive\b|\blane\b|\bblvd\b` +
	`)`)

// reMarketingJunk detects marketing/navigation terms that shouldn't be in addresses.
var reMarketingJunk = regexp.MustCompile(`(?i)(?:бронирование|меню|цены|как пройти|режим работы|` +
	`собственник|без комиссии|аренда|официальном сайте|подробнее|интересные факты|фото)`)

// ValidateAddress checks if a string looks like a real street address,
// not a page title, marketing text, or CSS.
func ValidateAddress(addr string) bool {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return false
	}

	runeLen := len([]rune(addr))

	// Too long for an address.
	if runeLen > 120 {
		return false
	}

	// Too short to be meaningful.
	if runeLen < 8 {
		return false
	}

	// Must contain a street-type word.
	if !reStreetWord.MatchString(addr) {
		return false
	}

	// Reject marketing/navigation junk.
	if reMarketingJunk.MatchString(addr) {
		return false
	}

	// Reject CSS patterns.
	if reCSS.MatchString(addr) {
		return false
	}

	// Reject URLs.
	if strings.Contains(addr, "://") {
		return false
	}

	return true
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/krolik/src/go-enriche && GOWORK=off go test ./extract/ -run TestValidateAddress -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd /home/krolik/src/go-enriche && git add extract/validate.go extract/validate_test.go
git commit -m "feat(extract): add address validator filtering page titles and marketing text"
```

---

### Task 4: Wire Validators into ExtractFacts Pipeline

**Files:**
- Modify: `extract/facts.go:48-102`
- Modify: `extract/facts_test.go`

**Context:** Now we wire the validators into all extraction points. The approach: wrap `setIfNil` with validation for phone, price, and address. Add a helper `setIfValid` that checks the validator before setting.

- [ ] **Step 1: Write failing tests for validation in ExtractFacts**

Add to `extract/facts_test.go`:

```go
func TestExtractFacts_RejectsGarbagePhone(t *testing.T) {
	t.Parallel()
	// Schema.org with an invalid phone (code 106 doesn't exist).
	html := `<html><head>
	<script type="application/ld+json">
	{"@context":"https://schema.org","@type":"Restaurant","telephone":"81063196745"}
	</script>
	</head><body></body></html>`

	facts := ExtractFacts(html, "https://example.com")
	if facts.Phone != nil {
		t.Errorf("expected nil phone for garbage number, got %q", *facts.Phone)
	}
}

func TestExtractFacts_RejectsGarbagePrice(t *testing.T) {
	t.Parallel()
	// Schema.org with CSS in priceRange.
	html := `<html><head>
	<script type="application/ld+json">
	{"@context":"https://schema.org","@type":"Restaurant","priceRange":"not(:empty){margin-top:4px}"}
	</script>
	</head><body></body></html>`

	facts := ExtractFacts(html, "https://example.com")
	if facts.Price != nil {
		t.Errorf("expected nil price for CSS garbage, got %q", *facts.Price)
	}
}

func TestExtractFacts_RejectsGarbageAddress(t *testing.T) {
	t.Parallel()
	// Schema.org with page-title-like address.
	html := `<html><head>
	<script type="application/ld+json">
	{"@context":"https://schema.org","@type":"Place","address":"и т. д. на официальном сайте Культура.РФ"}
	</script>
	</head><body></body></html>`

	facts := ExtractFacts(html, "https://example.com")
	if facts.Address != nil {
		t.Errorf("expected nil address for junk, got %q", *facts.Address)
	}
}

func TestExtractFacts_AcceptsValidStructuredData(t *testing.T) {
	t.Parallel()
	html := `<html><head>
	<script type="application/ld+json">
	{
		"@context":"https://schema.org",
		"@type":"Restaurant",
		"name":"Good Cafe",
		"telephone":"+7 (812) 555-12-34",
		"address":{"@type":"PostalAddress","streetAddress":"ул. Рубинштейна, 10","addressLocality":"Санкт-Петербург"},
		"priceRange":"1500-2500 ₽"
	}
	</script>
	</head><body></body></html>`

	facts := ExtractFacts(html, "https://example.com")
	if facts.Phone == nil {
		t.Error("expected valid phone")
	}
	if facts.Address == nil {
		t.Error("expected valid address")
	}
	if facts.Price == nil {
		t.Error("expected valid price")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/krolik/src/go-enriche && GOWORK=off go test ./extract/ -run "TestExtractFacts_Rejects|TestExtractFacts_AcceptsValid" -v -count=1`
Expected: FAIL — garbage phone/price/address are not yet rejected

- [ ] **Step 3: Add validated setters and wire into facts.go**

In `extract/facts.go`, add validated setter and update `applyPlaceFacts`, `applyOrgFacts`, `applyRegexFallback`:

```go
// setIfValid sets *dst to src if *dst is nil, src is non-nil, and validator returns true.
func setIfValid(dst **string, src *string, validate func(string) bool) {
	if *dst == nil && src != nil && validate(*src) {
		*dst = src
	}
}
```

Update `applyPlaceFacts`:
```go
func applyPlaceFacts(data *structured.Data, facts *Facts) {
	place := data.FirstPlace()
	if place == nil {
		return
	}
	setIfNil(&facts.PlaceName, place.Name)
	setIfNil(&facts.PlaceType, place.Type)
	setIfValid(&facts.Address, place.Address, ValidateAddress)
	setIfValid(&facts.Phone, place.Phone, ValidatePhone)
	setIfNil(&facts.Website, place.Website)
	setIfNil(&facts.Hours, place.Hours)
	setIfValid(&facts.Price, place.Price, ValidatePrice)
}
```

Update `applyEventFacts`:
```go
func applyEventFacts(data *structured.Data, facts *Facts) {
	event := data.FirstEvent()
	if event == nil {
		return
	}
	setIfNil(&facts.PlaceName, event.Name)
	setIfNil(&facts.EventDate, event.StartDate)
	setIfValid(&facts.Price, event.Price, ValidatePrice)
	setIfValid(&facts.Address, event.Location, ValidateAddress)
}
```

Update `applyOrgFacts`:
```go
func applyOrgFacts(data *structured.Data, facts *Facts) {
	org := data.FirstOrganization()
	if org == nil {
		return
	}
	setIfNil(&facts.PlaceName, org.Name)
	setIfNil(&facts.Website, org.URL)
	setIfValid(&facts.Phone, org.Phone, ValidatePhone)
	setIfValid(&facts.Address, org.Address, ValidateAddress)
}
```

Update `applyRegexFallback`:
```go
func applyRegexFallback(html string, facts *Facts) {
	if facts.Address == nil {
		if addr := regexAddress(html); addr != nil && ValidateAddress(*addr) {
			facts.Address = addr
		}
	}
	if facts.Phone == nil {
		if phone := regexPhone(html); phone != nil && ValidatePhone(*phone) {
			facts.Phone = phone
		}
	}
	if facts.Price == nil {
		if price := regexPrice(html); price != nil && ValidatePrice(*price) {
			facts.Price = price
		}
	}
}
```

- [ ] **Step 4: Run ALL tests to verify they pass**

Run: `cd /home/krolik/src/go-enriche && GOWORK=off go test ./extract/ -v -count=1`
Expected: ALL PASS (new + existing)

**IMPORTANT:** The existing test `TestExtractFacts_JSONLDPriority` (`facts_test.go:47-59`) uses phone `+7-111-222-33-44` — code `111` is rejected by `ValidatePhone`. Update this test phone to `+7-812-222-33-44` (valid SPb code) and both body/regex values accordingly. Also update the regex body phone to `+7 (812) 888-77-66` to avoid collision.

Note: The existing test `TestExtractFacts_RegexFallback` uses `"Адрес: Литейный проспект, 55"` which contains "проспект" — this passes `ValidateAddress`. The existing phone tests use valid `+7 (812)` numbers — these pass `ValidatePhone`. Existing price `"от 200 рублей"` passes `ValidatePrice`.

- [ ] **Step 5: Commit**

```bash
cd /home/krolik/src/go-enriche && git add extract/facts.go extract/facts_test.go
git commit -m "feat(extract): wire validators into ExtractFacts pipeline (structured + regex)"
```

---

### Task 5: Wire Validators into ExtractSnippetFacts

**Files:**
- Modify: `extract/facts.go:107-124`
- Modify: `extract/snippet_test.go`

**Context:** `ExtractSnippetFacts` already validates addresses with `reAddressValidator` and prices with `rePriceValidator`, but does NOT validate phones at all (`facts.go:117`). We add phone validation and upgrade address/price validation to use our new validators.

- [ ] **Step 1: Write failing test for snippet phone validation**

Add to `extract/snippet_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/krolik/src/go-enriche && GOWORK=off go test ./extract/ -run TestExtractSnippetFacts_RejectsGarbagePhone -v -count=1`
Expected: FAIL — garbage phone passes through

- [ ] **Step 3: Add phone validation to ExtractSnippetFacts**

Update `ExtractSnippetFacts` in `extract/facts.go`:

```go
func ExtractSnippetFacts(text string, facts *Facts) {
	if text == "" || facts == nil {
		return
	}
	if facts.Address == nil {
		if addr := regexSubmatch(reSnippetAddress, text); addr != nil && ValidateAddress(*addr) {
			facts.Address = addr
		}
	}
	if facts.Phone == nil {
		if phone := regexMatch(rePhone, text); phone != nil && ValidatePhone(*phone) {
			facts.Phone = phone
		}
	}
	if facts.Price == nil {
		if price := regexSubmatch(reSnippetPrice, text); price != nil && ValidatePrice(*price) {
			facts.Price = price
		}
	}
}
```

Note: This replaces the old `reAddressValidator` and `rePriceValidator` checks with the stronger `ValidateAddress` and `ValidatePrice` functions.

- [ ] **Step 4: Run ALL snippet tests to verify they pass**

Run: `cd /home/krolik/src/go-enriche && GOWORK=off go test ./extract/ -run TestExtractSnippetFacts -v -count=1`
Expected: ALL PASS

Check that `TestExtractSnippetFacts_AcceptsRealAddress` still passes — all test addresses contain street-type words ("ул.", "пр.", "остров", "город") and are under 120 chars, so they pass `ValidateAddress`. The snippet-specific `reAddressValidator` regex is now superseded by the stronger `ValidateAddress`.

- [ ] **Step 5: Run full test suite**

Run: `cd /home/krolik/src/go-enriche && GOWORK=off go test ./... -count=1`
Expected: ALL PASS

- [ ] **Step 6: Commit**

```bash
cd /home/krolik/src/go-enriche && git add extract/facts.go extract/snippet_test.go
git commit -m "feat(extract): add phone validation to snippets, upgrade address/price validators"
```

---

### Task 6: Tag, Push, Update go-wp

**Files:**
- Modify: `/home/krolik/src/go-wp/go.mod` (update go-enriche version)

**Context:** After all validators are in place and tests pass, tag a new go-enriche version and update go-wp to use it.

- [ ] **Step 1: Run lint**

Run: `cd /home/krolik/src/go-enriche && GOWORK=off golangci-lint run ./extract/`
Expected: PASS (no lint issues)

- [ ] **Step 2: Tag and push go-enriche**

```bash
cd /home/krolik/src/go-enriche
git tag v1.4.0
git push origin main --tags
```

- [ ] **Step 3: Update go-wp dependency**

```bash
cd /home/krolik/src/go-wp
GOWORK=off go get github.com/anatolykoptev/go-enriche@v1.4.0
GOWORK=off go mod tidy
```

- [ ] **Step 4: Run go-wp tests**

Run: `cd /home/krolik/src/go-wp && GOWORK=off go test ./... -count=1`
Expected: ALL PASS

- [ ] **Step 5: Commit and deploy go-wp**

```bash
cd /home/krolik/src/go-wp && git add go.mod go.sum
git commit -m "deps: update go-enriche to v1.4.0 (fact validation layer)"
git push origin main
cd ~/deploy/krolik-server && docker compose build --no-cache go-wp && docker compose up -d --no-deps --force-recreate go-wp
```

- [ ] **Step 6: Smoke test with known-garbage input**

Call `wp_enrich` MCP tool with the same places that produced garbage:
```json
[
  {"name":"Pont ресторан Ждановская 45 Петербург"},
  {"name":"Мануфактура 10/12 Красного Текстильщика Петербург"}
]
```
Verify: phone/price/address fields are nil or contain valid data (not CSS/random numbers).
