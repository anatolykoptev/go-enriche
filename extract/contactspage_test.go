package extract

import "testing"

// TestDiscoverContactsPage_SlugFirst covers the primary slug-segment discovery
// across the multilingual table, the segment-not-substring rule, the
// about-family lower priority, and the off-origin / self-link rejections.
func TestDiscoverContactsPage_SlugFirst(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		home    string
		base    string
		want    string
		wantOK  bool
	}{
		{
			name:   "RU contacts slug",
			home:   `<a href="/контакты/">Контакты</a>`,
			base:   "https://venue.ru/",
			want:   "https://venue.ru/%D0%BA%D0%BE%D0%BD%D1%82%D0%B0%D0%BA%D1%82%D1%8B/",
			wantOK: true,
		},
		{
			name:   "EN contacts slug",
			home:   `<a href="/contacts/">Contacts</a>`,
			base:   "https://venue.com/",
			want:   "https://venue.com/contacts/",
			wantOK: true,
		},
		{
			name:   "translit kontakty slug",
			home:   `<a href="/kontakty">Контакты</a>`,
			base:   "https://venue.ru/",
			want:   "https://venue.ru/kontakty",
			wantOK: true,
		},
		{
			name:   "DE kontakt slug",
			home:   `<a href="/kontakt/">Kontakt</a>`,
			base:   "https://venue.de/",
			want:   "https://venue.de/kontakt/",
			wantOK: true,
		},
		{
			name:   "segment match not substring — contact-lenses-shop must NOT match",
			home:   `<a href="/contact-lenses-shop/">Линзы</a>`,
			base:   "https://venue.ru/",
			want:   "",
			wantOK: false,
		},
		{
			name:   "contact slug beats about slug",
			home:   `<a href="/about/">О нас</a><a href="/contacts/">Контакты</a>`,
			base:   "https://venue.ru/",
			want:   "https://venue.ru/contacts/",
			wantOK: true,
		},
		{
			name:   "about-family fallback when no contacts page",
			home:   `<a href="/about-us/">About</a>`,
			base:   "https://venue.com/",
			want:   "https://venue.com/about-us/",
			wantOK: true,
		},
		{
			name:   "off-origin contacts link rejected",
			home:   `<a href="https://other.com/contacts/">Контакты</a>`,
			base:   "https://venue.ru/",
			want:   "",
			wantOK: false,
		},
		{
			name:   "self-link to homepage rejected",
			home:   `<a href="/">Главная</a>`,
			base:   "https://venue.ru/",
			want:   "",
			wantOK: false,
		},
		{
			name:   "fragment-only contacts anchor rejected (homepage IS the contacts page)",
			home:   `<a href="#contacts">Контакты</a>`,
			base:   "https://venue.ru/",
			want:   "",
			wantOK: false,
		},
		{
			name:   "no contact link at all",
			home:   `<a href="/menu/">Меню</a><a href="/prices/">Цены</a>`,
			base:   "https://venue.ru/",
			want:   "",
			wantOK: false,
		},
		{
			name:   "CMS-numeric slug resolved via link text",
			home:   `<a href="/page/5">Контакты</a>`,
			base:   "https://venue.ru/",
			want:   "https://venue.ru/page/5",
			wantOK: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := DiscoverContactsPage(tc.home, tc.base)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v (got url %q)", ok, tc.wantOK, got)
			}
			if got != tc.want {
				t.Fatalf("url = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestExtractFacts_Email verifies the email field is wired end-to-end through
// ExtractFacts (the previously extracted-but-dropped firstMailto).
func TestExtractFacts_Email(t *testing.T) {
	t.Parallel()
	html := `<html><body>
<a href="mailto:salon@affresco.ru?subject=Hi">salon@affresco.ru</a>
</body></html>`
	f := ExtractFacts(html, "https://affresco.ru/contacts/")
	if f.Email == nil {
		t.Fatal("expected Email extracted, got nil")
	}
	if *f.Email != "salon@affresco.ru" {
		t.Fatalf("Email = %q, want salon@affresco.ru (query string must be stripped)", *f.Email)
	}
}

// TestExtractFacts_Email_AbsentLeavesNil verifies no email yields nil (not "").
func TestExtractFacts_Email_AbsentLeavesNil(t *testing.T) {
	t.Parallel()
	f := ExtractFacts(`<html><body><p>no email here</p></body></html>`, "https://x.ru/")
	if f.Email != nil {
		t.Fatalf("expected nil Email, got %q", *f.Email)
	}
}

// TestExtractFacts_Hours_OpeningHoursSpecification verifies the structured
// openingHoursSpecification array is parsed into a readable hours string (the
// previously-unparsed structured form).
func TestExtractFacts_Hours_OpeningHoursSpecification(t *testing.T) {
	t.Parallel()
	html := `<html><head>
<script type="application/ld+json">{"@context":"https://schema.org","@type":"LocalBusiness",
"name":"Кафе","openingHoursSpecification":[
{"@type":"OpeningHoursSpecification","dayOfWeek":["https://schema.org/Monday","https://schema.org/Friday"],"opens":"10:00","closes":"22:00"}]}</script>
</head><body></body></html>`
	f := ExtractFacts(html, "https://cafe.ru/")
	if f.Hours == nil {
		t.Fatal("expected Hours from openingHoursSpecification, got nil")
	}
	if got := *f.Hours; got != "Monday,Friday 10:00-22:00" {
		t.Fatalf("Hours = %q, want %q", got, "Monday,Friday 10:00-22:00")
	}
}

// TestExtractVisibleHours_RussianLabel verifies the visible RU «Часы работы»
// fallback when no structured hours exist.
func TestExtractVisibleHours_RussianLabel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		html string
		want string
	}{
		{
			name: "inline label and value",
			html: `<p>Часы работы: 10:00-22:00</p>`,
			want: "10:00-22:00",
		},
		{
			name: "split label/value siblings",
			html: `<div><span>Режим работы</span><span>ежедневно с 09:00 до 21:00</span></div>`,
			want: "ежедневно с 09:00 до 21:00",
		},
		{
			name: "label with marketing prose rejected",
			html: `<p>Режим работы нашей компании построен на заботе о клиентах</p>`,
			want: "",
		},
		{
			name: "no label",
			html: `<p>Просто текст без часов</p>`,
			want: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ExtractVisibleHours(tc.html); got != tc.want {
				t.Fatalf("ExtractVisibleHours = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestExtractFacts_VisibleHours_FillsWhenStructuredAbsent verifies the visible
// RU hours block reaches Facts.Hours when JSON-LD carried none.
func TestExtractFacts_VisibleHours_FillsWhenStructuredAbsent(t *testing.T) {
	t.Parallel()
	html := `<html><body><div class="contacts"><span>Часы работы</span><span>Пн-Пт 10:00-19:00</span></div></body></html>`
	f := ExtractFacts(html, "https://x.ru/")
	if f.Hours == nil {
		t.Fatal("expected visible hours filled, got nil")
	}
	if got := *f.Hours; got != "Пн-Пт 10:00-19:00" {
		t.Fatalf("Hours = %q, want %q", got, "Пн-Пт 10:00-19:00")
	}
}
