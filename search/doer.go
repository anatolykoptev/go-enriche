package search

import (
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
// Compatible with stealth.ProxyPoolProvider and proxypool.ProxyPool.
type ProxyPoolProvider interface {
	Next() string
}

// ChromeHeaders returns browser-like HTTP headers for direct scraping.
func ChromeHeaders() map[string]string {
	return stealth.ChromeHeaders()
}
