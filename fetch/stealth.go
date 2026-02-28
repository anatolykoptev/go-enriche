package fetch

import (
	"net/http"

	stealth "github.com/anatolykoptev/go-stealth"
)

// StealthOption configures the stealth client.
type StealthOption func(*[]stealth.ClientOption)

// StealthWithTimeout sets the request timeout in seconds.
func StealthWithTimeout(seconds int) StealthOption {
	return func(opts *[]stealth.ClientOption) {
		*opts = append(*opts, stealth.WithTimeout(seconds))
	}
}

// StealthWithProxy sets an HTTP/SOCKS5 proxy URL.
func StealthWithProxy(proxyURL string) StealthOption {
	return func(opts *[]stealth.ClientOption) {
		*opts = append(*opts, stealth.WithProxy(proxyURL))
	}
}

// StealthWithProfile sets the TLS fingerprint profile.
func StealthWithProfile(profile stealth.TLSProfile) StealthOption {
	return func(opts *[]stealth.ClientOption) {
		*opts = append(*opts, stealth.WithProfile(profile))
	}
}

// StealthWithStdHTTP uses the stdlib net/http backend (no TLS fingerprinting).
func StealthWithStdHTTP() StealthOption {
	return func(opts *[]stealth.ClientOption) {
		*opts = append(*opts, stealth.WithStdHTTP())
	}
}

const defaultStealthTimeoutSec = 15

// NewStealthClient creates an *http.Client with TLS fingerprinting via go-stealth.
// The returned client can be passed to NewFetcher via WithClient.
func NewStealthClient(opts ...StealthOption) (*http.Client, error) {
	stealthOpts := []stealth.ClientOption{
		stealth.WithTimeout(defaultStealthTimeoutSec),
	}
	for _, o := range opts {
		o(&stealthOpts)
	}

	bc, err := stealth.NewClient(stealthOpts...)
	if err != nil {
		return nil, err
	}
	return bc.StdClient(), nil
}
