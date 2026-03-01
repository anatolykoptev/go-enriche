package maps

import (
	"context"
	"io"
	"testing"
)

// mockDoer implements BrowserDoer for testing.
type mockDoer struct {
	response []byte
	status   int
	err      error
}

func (m *mockDoer) Do(_, _ string, _ map[string]string, _ io.Reader) ([]byte, map[string]string, int, error) {
	return m.response, nil, m.status, m.err
}

func newTestChecker(html string) *YandexMaps {
	y, _ := NewYandexMaps("", WithYandexDoer(&mockDoer{
		response: []byte(html),
		status:   200,
	}))
	return y
}

func TestCheck_PermanentlyClosed(t *testing.T) {
	html := `<html><body>
	<li class="serp-item">
	  <a href="https://yandex.ru/maps/org/test_cafe/12345/">Test Cafe</a>
	  <h2>Закрыто навсегда: Test Cafe, кафе — Яндекс Карты</h2>
	  <div class="OrganicText">Test Cafe в Санкт-Петербурге закрыто навсегда</div>
	</li>
	</body></html>`

	y := newTestChecker(html)
	result, err := y.Check(context.Background(), "Test Cafe", "Санкт-Петербург")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != PlacePermanentClosed {
		t.Errorf("expected PlacePermanentClosed, got %q", result.Status)
	}
	if !result.IsClosed() {
		t.Error("IsClosed() should return true")
	}
}

func TestCheck_TemporaryClosed(t *testing.T) {
	html := `<html><body>
	<li class="serp-item">
	  <a href="https://yandex.ru/maps/org/spa_center/67890/">SPA Center</a>
	  <h2>SPA Center — Яндекс Карты</h2>
	  <div class="OrganicText">Временно закрыто. SPA Center на ремонте</div>
	</li>
	</body></html>`

	y := newTestChecker(html)
	result, err := y.Check(context.Background(), "SPA Center", "Москва")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != PlaceTemporaryClosed {
		t.Errorf("expected PlaceTemporaryClosed, got %q", result.Status)
	}
}

func TestCheck_Open(t *testing.T) {
	html := `<html><body>
	<li class="serp-item">
	  <a href="https://yandex.ru/maps/org/bolshoi_theatre/11111/">Bolshoi Theatre</a>
	  <h2>Большой театр — Яндекс Карты</h2>
	  <div class="OrganicText">Большой театр, Москва. Режим работы: 10:00-21:00</div>
	</li>
	</body></html>`

	y := newTestChecker(html)
	result, err := y.Check(context.Background(), "Большой театр", "Москва")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != PlaceOpen {
		t.Errorf("expected PlaceOpen, got %q", result.Status)
	}
}

func TestCheck_NotFound(t *testing.T) {
	html := `<html><body>
	<li class="serp-item">
	  <a href="https://example.com/something">Unrelated result</a>
	  <div class="OrganicText">Some unrelated text</div>
	</li>
	</body></html>`

	y := newTestChecker(html)
	result, err := y.Check(context.Background(), "Nonexistent Place", "Москва")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != PlaceNotFound {
		t.Errorf("expected PlaceNotFound, got %q", result.Status)
	}
}

func TestBuildQuery(t *testing.T) {
	q := buildQuery("Кафе Пушкин", "Москва")
	if q != `site:yandex.ru/maps/org "Кафе Пушкин" Москва` {
		t.Errorf("unexpected query: %s", q)
	}
}

func TestBuildQuery_NoCity(t *testing.T) {
	q := buildQuery("Кафе Пушкин", "")
	if q != `site:yandex.ru/maps/org "Кафе Пушкин"` {
		t.Errorf("unexpected query: %s", q)
	}
}
