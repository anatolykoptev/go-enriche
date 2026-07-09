package fetch

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"strings"
	"syscall"
	"testing"
	"time"
)

// --- isCertError: pure, network-free classification tests -----------------
//
// These pin isCertError's contract directly: it must fire for every genuine
// certificate-validation failure (hostname mismatch, untrusted CA, chain
// error, the Go 1.20+ CertificateVerificationError wrapper) and must NOT
// fire for a connection-level failure (refused, DNS, timeout) or a non-cert
// TLS transport error -- those must keep returning StatusUnreachable
// unchanged, per the task's hard requirement that only a TRUE cert error
// triggers the fallback.
func TestIsCertError(t *testing.T) {
	t.Parallel()
	leafErr := errors.New("x509: certificate signed by unknown authority")
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"hostname mismatch", x509.HostnameError{Certificate: &x509.Certificate{}, Host: "p45.su"}, true},
		{"unknown authority", x509.UnknownAuthorityError{}, true},
		{"certificate invalid (expired)", x509.CertificateInvalidError{Cert: &x509.Certificate{}, Reason: x509.Expired}, true},
		{"tls.CertificateVerificationError wrapping x509 error", &tls.CertificateVerificationError{Err: leafErr}, true},
		{"wrapped hostname error (url.Error-style)", fmt.Errorf("Get %q: %w", "https://p45.su/x", x509.HostnameError{Certificate: &x509.Certificate{}, Host: "p45.su"}), true},
		{"connection refused", syscall.ECONNREFUSED, false},
		{"dns failure", &net.DNSError{Err: "no such host", Name: "nonexistent.invalid", IsNotFound: true}, false},
		{"context deadline exceeded", context.DeadlineExceeded, false},
		{"non-cert TLS transport error", tls.RecordHeaderError{Msg: "first record does not look like a TLS handshake"}, false},
		{"generic error", errors.New("boom"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isCertError(tt.err); got != tt.want {
				t.Errorf("isCertError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// --- httpFallbackURL: pure URL-rewrite tests --------------------------------

func TestHTTPFallbackURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		rawURL string
		want   string
		wantOK bool
	}{
		{"https no port", "https://p45.su/contacts", "http://p45.su/contacts", true},
		{"https explicit port preserved", "https://p45.su:8443/contacts?x=1", "http://p45.su:8443/contacts?x=1", true},
		{"https path+query preserved", "https://example.com/a/b?c=d&e=f", "http://example.com/a/b?c=d&e=f", true},
		{"already http: no fallback", "http://p45.su/contacts", "", false},
		{"unparseable: no fallback", "://not-a-url", "", false},
		{"non-http scheme: no fallback", "ftp://p45.su/x", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := httpFallbackURL(tt.rawURL)
			if ok != tt.wantOK {
				t.Fatalf("httpFallbackURL(%q) ok = %v, want %v", tt.rawURL, ok, tt.wantOK)
			}
			if got != tt.want {
				t.Errorf("httpFallbackURL(%q) = %q, want %q", tt.rawURL, got, tt.want)
			}
		})
	}
}

// --- dual-protocol test listener --------------------------------------------
//
// dualListener wraps a real net.Listener and, per accepted connection, peeks
// the first byte to route a TLS ClientHello (record type 0x16) into a real
// tls.Server handshake and everything else to plain HTTP -- letting ONE
// listener address serve BOTH a broken-cert HTTPS endpoint and a working
// plain-HTTP endpoint. This mirrors the real p45.su case (HTTPS cert broken,
// HTTP reachable) on the exact SAME host:port, which is what doFetch's
// same-host, scheme-only http fallback (httpFallbackURL) targets -- a
// same-port plain listener next to it on a different port would not
// exercise the real fallback URL doFetch actually builds.
type dualListener struct {
	net.Listener
	tlsConfig *tls.Config
}

const tlsHandshakeRecordType = 0x16

func (d *dualListener) Accept() (net.Conn, error) {
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
	pc := &peekedConn{Conn: conn, r: br}
	if first[0] == tlsHandshakeRecordType {
		return tls.Server(pc, d.tlsConfig), nil
	}
	return pc, nil
}

// peekedConn re-exposes a bufio.Reader-buffered net.Conn so the one Peek'd
// byte in dualListener.Accept isn't lost to the next reader (http.Server or
// tls.Server) downstream.
type peekedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *peekedConn) Read(p []byte) (int, error) { return c.r.Read(p) }

// generateSelfSignedCert builds a self-signed leaf certificate for dnsNames,
// returning both the tls.Certificate (to serve) and an *x509.CertPool
// trusting it (for a test client that needs to get PAST chain validation
// and hit ONLY a hostname mismatch -- see TestFetch_TLSCertMismatch_*).
func generateSelfSignedCert(t *testing.T, dnsNames ...string) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: dnsNames[0]},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:              dnsNames,
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

// startDualProtoServer binds addr and serves plainBody over plain HTTP and
// tlsCfg over TLS on the SAME listener (see dualListener). Returns the base
// "https://host:port" URL (the primary attempt doFetch makes) and a closer.
func startDualProtoServer(t *testing.T, addr string, tlsCfg *tls.Config, plainHandler http.Handler) (baseURL string, closeFn func()) {
	t.Helper()
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", addr)
	if err != nil {
		t.Fatalf("listen %s: %v", addr, err)
	}
	d := &dualListener{Listener: ln, tlsConfig: tlsCfg}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		plainHandler.ServeHTTP(w, r)
	})
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	go srv.Serve(d)                                                //nolint:errcheck
	return "https://" + ln.Addr().String(), func() { srv.Close() } //nolint:errcheck
}

// --- behavioral test: cert error recovers real content via the fallback ----
//
// The certificate is signed for "wrong-host.example" -- NOT the loopback
// address doFetch actually dials -- and the test client's Transport trusts
// the leaf as a root (so chain validation PASSES), isolating the failure to
// a pure x509.HostnameError, exactly the "cert issued for
// 000h01.westcall.spb.ru" ground-truth case in the task. Uses WithClient
// (this test proves RECOVERY, not SSRF-preservation -- see the security
// test below for that, which uses the REAL guarded default).
//
// Before this change: doFetch's primary HTTPS request errors, doFetch
// returns {Status: StatusUnreachable} immediately, HTML is empty -- the RED
// state (run this test against a build without the fallback: it fails all
// three assertions below).
func TestFetch_TLSCertMismatch_RecoversViaHTTPFallback(t *testing.T) {
	t.Parallel()
	const wantBody = "recovered contacts page content"
	cert, pool := generateSelfSignedCert(t, "wrong-host.example")
	tlsCfg := &tls.Config{Certificates: []tls.Certificate{cert}} //nolint:gosec // test server cert, not a production TLS config
	plain := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(wantBody))
	})
	baseURL, closeFn := startDualProtoServer(t, "127.0.0.1:0", tlsCfg, plain)
	defer closeFn()

	client := &http.Client{
		Timeout:   DefaultTimeout,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}, //nolint:gosec // test-only trust anchor for the self-signed leaf above
	}
	f := NewFetcher(WithClient(client))

	result, err := f.Fetch(context.Background(), baseURL+"/contacts")
	if err != nil {
		t.Fatalf("unexpected Fetch error: %v", err)
	}
	if result.Status != StatusActive {
		t.Fatalf("Status = %s, want %s (fallback should have recovered the page)", result.Status, StatusActive)
	}
	if !strings.Contains(result.HTML, wantBody) {
		t.Errorf("HTML = %q, want it to contain %q (the plain-HTTP fallback response)", result.HTML, wantBody)
	}
	if !result.TLSFallbackUsed {
		t.Error("TLSFallbackUsed = false, want true -- a cert-error recovery must be tagged low-trust")
	}
}

// TestFetch_TLSCertMismatch_FallbackFails_ReportsDistinctStatus proves the
// OTHER half of StatusTLSInvalid's contract: when the cert error fires but
// the plain-HTTP fallback ALSO fails at the connection level (nothing
// listening in plain HTTP), doFetch must NOT silently collapse back to the
// generic StatusUnreachable -- it must report the distinct StatusTLSInvalid
// so a downstream consumer can tell "this is specifically a broken cert"
// apart from "this looks fully dead".
//
// Uses dropPlainListener rather than a bare tls.NewListener: net/http's
// server has a built-in "Client sent an HTTP request to an HTTPS server"
// 400 diagnostic for exactly this byte pattern (confirmed empirically) --
// that IS a real response, which would make the fallback "succeed" at the
// network layer (Status=Unreachable, TLSFallbackUsed=true, the OTHER
// legitimate outcome already covered by processResponse's normal
// >=400-status handling). This test instead models a target with NOTHING
// listening in plain HTTP at all -- the connection is dropped, not answered.
func TestFetch_TLSCertMismatch_FallbackFails_ReportsDistinctStatus(t *testing.T) {
	t.Parallel()
	cert, pool := generateSelfSignedCert(t, "wrong-host.example")
	tlsCfg := &tls.Config{Certificates: []tls.Certificate{cert}} //nolint:gosec // test server cert
	baseURL, closeFn := startDropPlainServer(t, "127.0.0.1:0", tlsCfg)
	defer closeFn()

	client := &http.Client{
		Timeout:   DefaultTimeout,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}, //nolint:gosec // test-only trust anchor
	}
	f := NewFetcher(WithClient(client))

	result, err := f.Fetch(context.Background(), baseURL+"/contacts")
	if err != nil {
		t.Fatalf("unexpected Fetch error: %v", err)
	}
	if result.Status != StatusTLSInvalid {
		t.Errorf("Status = %s, want %s (cert error, fallback also failed)", result.Status, StatusTLSInvalid)
	}
	if result.TLSFallbackUsed {
		t.Error("TLSFallbackUsed = true, want false -- no content was actually recovered")
	}
}

// dropPlainListener is dualListener's sibling for a target with NOTHING
// listening in plain HTTP: it routes a real TLS ClientHello to tlsConfig
// (so the PRIMARY https request gets a genuine cert error), but silently
// closes -- rather than serving a response to -- any non-TLS connection,
// modeling doFetch's plain-HTTP fallback hitting dead air (connection reset)
// instead of net/http's own "sent HTTP to HTTPS server" diagnostic 400.
type dropPlainListener struct {
	net.Listener
	tlsConfig *tls.Config
}

func (d *dropPlainListener) Accept() (net.Conn, error) {
	for {
		conn, err := d.Listener.Accept()
		if err != nil {
			return nil, err
		}
		br := bufio.NewReader(conn)
		first, err := br.Peek(1)
		if err != nil {
			conn.Close() //nolint:errcheck
			continue
		}
		if first[0] == tlsHandshakeRecordType {
			return tls.Server(&peekedConn{Conn: conn, r: br}, d.tlsConfig), nil
		}
		conn.Close() //nolint:errcheck
	}
}

// startDropPlainServer serves ONLY the TLS side (via dropPlainListener) at
// addr; a plain-HTTP connection to the same host:port is accepted at the TCP
// layer, then immediately dropped.
func startDropPlainServer(t *testing.T, addr string, tlsCfg *tls.Config) (baseURL string, closeFn func()) {
	t.Helper()
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", addr)
	if err != nil {
		t.Fatalf("listen %s: %v", addr, err)
	}
	d := &dropPlainListener{Listener: ln, tlsConfig: tlsCfg}
	srv := &http.Server{
		Handler:           http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go srv.Serve(d)                                                //nolint:errcheck
	return "https://" + ln.Addr().String(), func() { srv.Close() } //nolint:errcheck
}

// --- security test: the fallback path stays SSRF-guarded -------------------
//
// This is the most important test in this file (see task spec): it proves
// the cert-error fallback does NOT bypass the SSRF guard. The primary HTTPS
// request reaches a REAL, non-blocked local address (firstUnblockedLocalIP,
// ssrf_test.go) and fails with a genuine cert error (self-signed, untrusted
// CA -- httptest's own default). doFetch's plain-HTTP fallback retry then
// hits the SAME listener (dualListener) and gets a 302 redirect to
// "http://127.0.0.1:1/blocked" -- an explicitly SSRF-blocked target. Uses
// the REAL guarded default (NewFetcher(), no WithClient escape hatch): if a
// future change threaded the fallback through an unguarded client instead
// of doFetch's already-guarded one, this test would start seeing the
// "should never be reached" body and go red.
func TestFetch_TLSCertError_FallbackRedirectToBlockedTarget_StaysBlocked(t *testing.T) {
	t.Parallel()
	hostIP := firstUnblockedLocalIP(t)

	cert, _ := generateSelfSignedCert(t, hostIP)                 // untrusted CA either way; hostname needn't match
	tlsCfg := &tls.Config{Certificates: []tls.Certificate{cert}} //nolint:gosec // test server cert
	redirectToBlocked := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://127.0.0.1:1/blocked", http.StatusFound)
	})
	baseURL, closeFn := startDualProtoServer(t, net.JoinHostPort(hostIP, "0"), tlsCfg, redirectToBlocked)
	defer closeFn()

	f := NewFetcher() // REAL guarded default, no escape hatch
	result, err := f.Fetch(context.Background(), baseURL+"/contacts")
	if err != nil {
		t.Fatalf("unexpected Fetch error: %v", err)
	}
	if result.Status == StatusActive {
		t.Fatalf("Status = %s, want NOT active -- the fallback's redirect target (127.0.0.1:1) must stay blocked", result.Status)
	}
	if strings.Contains(result.HTML, "blocked") {
		t.Errorf("HTML unexpectedly reached the redirect target's content: %q", result.HTML)
	}
}
