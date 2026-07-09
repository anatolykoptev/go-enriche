package enriche

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/anatolykoptev/go-enriche/fetch"
)

// This file proves the fetch package's TLS-cert-error fallback (see
// fetch/tls_fallback_test.go for the mechanism-level tests) is actually
// WIRED into enriche.Result, not just implemented and unused. Two call
// sites read fetch.FetchResult.TLSFallbackUsed and OR it into
// Result.TLSFallbackUsed: the homepage leg (enriche_fetch.go,
// fetchAndExtract) and the contacts-subpage leg (enriche_contacts.go,
// fetchContactsHTML) — the same OR pattern RenderSkipped already uses
// across both legs. Each test below drives the REAL production function,
// not a copy, so reverting either wiring line makes the corresponding test
// fail (TLSFallbackUsed observed false where it must be true).

// dualListenerTLS + generateSelfSignedLeaf are a smaller, local copy of
// fetch/tls_fallback_test.go's dualListener/generateSelfSignedCert helpers
// (test-only scaffolding; not worth exporting across package boundaries for
// one file each side). See that file for the full design rationale.
type dualListenerTLS struct {
	net.Listener
	tlsConfig *tls.Config
}

const tlsHandshakeRecordType = 0x16

func (d *dualListenerTLS) Accept() (net.Conn, error) {
	conn, err := d.Listener.Accept()
	if err != nil {
		return nil, err
	}
	br := bufio.NewReader(conn)
	first, err := br.Peek(1)
	if err != nil {
		conn.Close() //nolint:errcheck
		return nil, err
	}
	pc := &peekedConnTLS{Conn: conn, r: br}
	if first[0] == tlsHandshakeRecordType {
		return tls.Server(pc, d.tlsConfig), nil
	}
	return pc, nil
}

type peekedConnTLS struct {
	net.Conn
	r *bufio.Reader
}

func (c *peekedConnTLS) Read(p []byte) (int, error) { return c.r.Read(p) }

func generateSelfSignedLeaf(t *testing.T, dnsName string) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: dnsName},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:              []string{dnsName},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: priv, Leaf: leaf}, pool
}

// startDualProtoTestServer serves plainBody over plain HTTP and a
// wrong-hostname cert over TLS on the SAME listener, so a cert-error primary
// request and its same-host plain-HTTP fallback both land on this one
// address. Returns the "https://host:port" base URL doFetch's primary
// attempt uses, the CertPool a caller's client must trust to isolate the
// failure to a pure hostname mismatch (see tlsFallbackFetcher), and a
// closer. Shared by both tests below.
func startDualProtoTestServer(t *testing.T, plainBody string) (baseURL string, pool *x509.CertPool, closeFn func()) {
	t.Helper()
	cert, pool := generateSelfSignedLeaf(t, "wrong-host.example")
	tlsCfg := &tls.Config{Certificates: []tls.Certificate{cert}} //nolint:gosec // test server cert
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	d := &dualListenerTLS{Listener: ln, tlsConfig: tlsCfg}
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(plainBody))
		}),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go srv.Serve(d)                                                      //nolint:errcheck
	return "https://" + ln.Addr().String(), pool, func() { srv.Close() } //nolint:errcheck
}

// tlsFallbackFetcher builds a fetch.Fetcher whose client trusts the given
// self-signed leaf as a root (so chain validation passes and the ONLY
// failure is the deliberate hostname mismatch — the p45.su ground-truth
// case), otherwise unguarded (WithClient escape hatch, same as
// testFetcher()) since these tests target the trust-tagging WIRING, not
// SSRF preservation (see fetch/tls_fallback_test.go for that).
func tlsFallbackFetcher(pool *x509.CertPool) *fetch.Fetcher {
	client := &http.Client{
		Timeout:   fetch.DefaultTimeout,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}, //nolint:gosec // test-only trust anchor
	}
	return fetch.NewFetcher(fetch.WithClient(client))
}

// TestEnrich_HomepageTLSCertError_TagsResultLowTrust proves the HOMEPAGE-leg
// wiring in enriche_fetch.go (fetchAndExtract): result.Status = fr.Status
// and result.TLSFallbackUsed = fr.TLSFallbackUsed, right after the retry
// fetch. Reverting the second assignment (or dropping it) makes this test
// fail with TLSFallbackUsed=false while Content is still recovered — the
// exact "written but not wired" gap this test exists to catch.
func TestEnrich_HomepageTLSCertError_TagsResultLowTrust(t *testing.T) {
	t.Parallel()
	const article = `<!DOCTYPE html><html><head><title>Клиника</title></head><body>
<article><h1>О клинике</h1><p>Мы оказываем медицинские услуги жителям города уже
пятнадцать лет, наши специалисты используют современное оборудование и подход,
ориентированный на пациента, чтобы забота о здоровье была доступна каждый день.</p>
</article></body></html>`
	baseURL, pool, closeFn := startDualProtoTestServer(t, article)
	defer closeFn()

	e := newTestEnricher(WithFetcher(tlsFallbackFetcher(pool)))
	result, err := e.Enrich(context.Background(), Item{Name: "Клиника", URL: baseURL, Mode: ModePlaces, SkipMapsCheck: true})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if result.Status != StatusActive {
		t.Fatalf("Result.Status = %s, want %s", result.Status, StatusActive)
	}
	if !result.TLSFallbackUsed {
		t.Error("Result.TLSFallbackUsed = false, want true — the homepage fetch recovered via the cert-error fallback")
	}
	if !strings.Contains(result.Content, "медицинские услуги") {
		t.Errorf("Result.Content = %q, want it to contain the recovered article text", result.Content)
	}
}

// TestFetchContactsHTML_TLSCertError_ReturnsTLSFallbackUsed is the direct,
// narrower counterpart for the CONTACTS-leg wiring (enriche_contacts.go):
// it calls the real fetchContactsHTML (same function resolveContactsPage
// calls) against the dual-protocol server and asserts its tlsFallbackUsed
// return value — the local computation the caller then ORs into
// Result.TLSFallbackUsed. Isolates the leg from contacts-page DISCOVERY
// (homepageMissingRichField / DiscoverContactsPage), which
// TestEnrich_HomepageTLSCertError_TagsResultLowTrust above does not
// exercise.
func TestFetchContactsHTML_TLSCertError_ReturnsTLSFallbackUsed(t *testing.T) {
	t.Parallel()
	const contactsBody = `<html><body>Тел: +7 (812) 111-22-33, часы работы 10-20, email info@example.com, адрес Невский пр., 1</body></html>`
	baseURL, pool, closeFn := startDualProtoTestServer(t, contactsBody)
	defer closeFn()

	e := New(WithFetcher(tlsFallbackFetcher(pool)))
	html, _, _, tlsFallbackUsed, _ := e.fetchContactsHTML(context.Background(), baseURL+"/contacts", Item{City: "Санкт-Петербург"})
	if html == "" {
		t.Fatal("fetchContactsHTML returned empty html — the cert-error fallback should have recovered the page")
	}
	if !tlsFallbackUsed {
		t.Error("tlsFallbackUsed = false, want true — the contacts-page raw fetch recovered via the cert-error fallback")
	}
}
