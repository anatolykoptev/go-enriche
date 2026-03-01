package maps

import (
	"context"
	"errors"
	"fmt"
	"io"

	stealth "github.com/anatolykoptev/go-stealth"
)

const defaultStealthTimeout = 15

// BrowserDoer performs HTTP requests with browser-like TLS fingerprint.
// *stealth.BrowserClient satisfies this interface.
type BrowserDoer interface {
	Do(method, url string, headers map[string]string, body io.Reader) ([]byte, map[string]string, int, error)
}

// ProxyPoolProvider returns the next proxy URL for rotation.
type ProxyPoolProvider interface {
	Next() string
}

// YandexMaps checks place status by searching Yandex for Yandex Maps org listings
// and examining result titles/snippets for closure indicators.
type YandexMaps struct {
	bc        BrowserDoer
	proxyPool ProxyPoolProvider
}

// YandexOption configures YandexMaps.
type YandexOption func(*YandexMaps)

// WithYandexDoer overrides the default BrowserDoer (for testing).
func WithYandexDoer(bc BrowserDoer) YandexOption {
	return func(y *YandexMaps) { y.bc = bc }
}

// WithYandexProxyPool enables per-request proxy rotation.
func WithYandexProxyPool(pool ProxyPoolProvider) YandexOption {
	return func(y *YandexMaps) { y.proxyPool = pool }
}

// NewYandexMaps creates a Yandex Maps checker.
// Either proxyURL or WithYandexProxyPool must be provided.
func NewYandexMaps(proxyURL string, opts ...YandexOption) (*YandexMaps, error) {
	y := &YandexMaps{}
	for _, o := range opts {
		o(y)
	}
	if y.bc != nil {
		return y, nil
	}
	if proxyURL == "" && y.proxyPool == nil {
		return nil, errors.New("yandex: proxy URL or pool is required")
	}

	var stealthOpts []stealth.ClientOption
	stealthOpts = append(stealthOpts, stealth.WithTimeout(defaultStealthTimeout))
	if y.proxyPool != nil {
		stealthOpts = append(stealthOpts, stealth.WithProxyPool(y.proxyPool))
	} else {
		stealthOpts = append(stealthOpts, stealth.WithProxy(proxyURL))
	}

	bc, err := stealth.NewClient(stealthOpts...)
	if err != nil {
		return nil, fmt.Errorf("yandex: stealth client: %w", err)
	}
	y.bc = bc
	return y, nil
}

// Check queries Yandex Search for the place on yandex.ru/maps
// and detects closure signals in result titles/snippets.
func (y *YandexMaps) Check(ctx context.Context, name, city string) (*CheckResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	query := buildQuery(name, city)
	data, err := y.fetchSearchPage(query)
	if err != nil {
		return nil, fmt.Errorf("yandex: fetch: %w", err)
	}
	return parseResults(data, name), nil
}
