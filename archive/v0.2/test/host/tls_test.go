package host_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kitwork/engine2/host"
)

// tlsManifest is a manifest serving shop.test and fresh.test, its website written as website.
func tlsManifest(website string) string {
	return `import * as app from "kitwork/application";

app.root("apps");
app.website(` + website + `);
`
}

// app.website's tls (library/application/tls.go): AutoSSL with nothing written, false for no HTTPS, or
// { email }; anything else, a misspelt key among them, stops the read naming the option.
func TestTheWebsitesTLSOption(t *testing.T) {
	read := func(website string) (*host.Host, error) {
		folder := write(t, map[string]string{"application.work": tlsManifest(website)})
		return host.ReadWithLog(filepath.Join(folder, "application.work"), io.Discard)
	}
	for _, want := range []struct {
		website   string
		automatic bool
		email     string
	}{
		{`"shop.test"`, true, ""},
		{`{ hostname: "shop.test" }`, true, ""},
		{`{ hostname: "shop.test", tls: true }`, true, ""},
		{`{ hostname: "shop.test", tls: false }`, false, ""},
		{`{ hostname: "shop.test", tls: { email: "admin@shop.test" } }`, true, "admin@shop.test"},
	} {
		h, err := read(want.website)
		if err != nil {
			t.Errorf("app.website(%s): %v", want.website, err)
			continue
		}
		if got := h.Manifest.Website.TLS; got.Automatic != want.automatic || got.Email != want.email {
			t.Errorf("app.website(%s): tls %+v, want automatic %v, email %q", want.website, got, want.automatic, want.email)
		}
	}
	for _, refused := range []struct{ website, says string }{
		{`{ hostname: "shop.test", tls: "yes" }`, "app.website({ tls })"},
		{`{ hostname: "shop.test", tls: { mail: "admin@shop.test" } }`, "not an option of tls"},
		{`{ hostname: "shop.test", tls: { email: "admin" } }`, "an address"},
		{`{ hostname: "shop.test", tls: { email: 42 } }`, "an address"},
	} {
		if _, err := read(refused.website); err == nil || !strings.Contains(err.Error(), refused.says) {
			t.Errorf("app.website(%s): %v, want a failure saying %q", refused.website, err, refused.says)
		}
	}
}

// selfSigned is a certificate for name and its key, as autocert keeps one in its cache: the key's PEM,
// then the certificate's. The serial tells it apart.
func selfSigned(t *testing.T, name string, serial int64) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject:      pkix.Name{CommonName: name},
		DNSNames:     []string{name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
	}
	certificate, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyBytes, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	out := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})
	return append(out, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate})...)
}

// hello is a TLS handshake for name from a client that takes an ECDSA certificate, as every browser does.
func hello(name string) *tls.ClientHelloInfo {
	return &tls.ClientHelloInfo{
		ServerName:       name,
		CipherSuites:     []uint16{tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256},
		SupportedCurves:  []tls.CurveID{tls.CurveP256},
		SignatureSchemes: []tls.SignatureScheme{tls.ECDSAWithP256AndSHA256},
	}
}

// AutoSSL (host/certificates.go) serves HTTPS on 443 and Let's Encrypt's challenge on 80, and asks for a
// certificate only for a site of the host: a handshake for shop.test is answered from .certificates/
// beside the manifest, one for fresh.test (a site with no certificate yet) asks the authority, and one
// for unknown.test, a name no folder has, never reaches it. Port 80 sends every other request to https.
// The authority is a server of the test's, which counts what it is asked and answers 404: no test reaches
// Let's Encrypt.
func TestAutoSSLAsksACertificateOnlyForASiteOfTheHost(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work":                 tlsManifest(`{ hostname: "shop.test", tls: { email: "admin@shop.test" } }`),
		"apps/shop/shop.test/router.work":  routerImport + "router.get((context) => context.text(\"shop\"));\n",
		"apps/shop/fresh.test/router.work": routerImport + "router.get((context) => context.text(\"fresh\"));\n",
	})
	cached := selfSigned(t, "shop.test", 4242)
	if err := os.MkdirAll(filepath.Join(folder, ".certificates"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, ".certificates", "shop.test"), cached, 0o600); err != nil {
		t.Fatal(err)
	}
	var asked atomic.Int64
	authority := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		http.NotFound(w, r)
	}))
	t.Cleanup(authority.Close)

	h, err := host.ReadWithLog(filepath.Join(folder, "application.work"), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Boot(); err != nil {
		t.Fatal(err)
	}
	h.CertificateAuthority = authority.URL + "/directory"
	secure, challenge := h.TLSServers()
	if secure == nil || challenge == nil {
		t.Fatalf("TLSServers: %v %v, want HTTPS and the challenge's server", secure, challenge)
	}
	if secure.Addr != ":443" || challenge.Addr != ":80" {
		t.Errorf("HTTPS on %q, the challenge on %q; want :443 and :80", secure.Addr, challenge.Addr)
	}
	config := secure.TLSConfig
	if config.MinVersion < tls.VersionTLS12 || !slices.Contains(config.NextProtos, "h2") || !slices.Contains(config.NextProtos, "acme-tls/1") {
		t.Errorf("TLS: min version %x, protocols %v; want TLS 1.2 at least, h2 and acme-tls/1", config.MinVersion, config.NextProtos)
	}

	certificate, err := config.GetCertificate(hello("shop.test"))
	if err != nil {
		t.Fatalf("a handshake for shop.test: %v, want its certificate from .certificates/", err)
	}
	leaf, err := x509.ParseCertificate(certificate.Certificate[0])
	if err != nil || leaf.SerialNumber.Int64() != 4242 {
		t.Errorf("shop.test's certificate: serial %v (%v), want the one in .certificates/, 4242", leaf.SerialNumber, err)
	}
	if asked.Load() != 0 {
		t.Errorf("a site with a certificate asked the authority %d times, want none", asked.Load())
	}

	if _, err := config.GetCertificate(hello("unknown.test")); err == nil {
		t.Error("a handshake for unknown.test got a certificate, want none: no site is named so")
	}
	if asked.Load() != 0 {
		t.Errorf("a name no site has asked the authority %d times, want none", asked.Load())
	}

	if _, err := config.GetCertificate(hello("fresh.test")); err == nil {
		t.Error("fresh.test got a certificate from an authority that answers 404")
	}
	if asked.Load() == 0 {
		t.Error("fresh.test, a site with no certificate yet, did not ask the authority")
	}

	recorder := httptest.NewRecorder()
	challenge.Handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "http://shop.test/menu?day=2", nil))
	if recorder.Code != http.StatusFound || recorder.Header().Get("Location") != "https://shop.test/menu?day=2" {
		t.Errorf("GET http://shop.test/menu?day=2 on port 80: %d to %q, want 302 to https://shop.test/menu?day=2", recorder.Code, recorder.Header().Get("Location"))
	}
	recorder = httptest.NewRecorder()
	challenge.Handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "http://shop.test/menu", nil))
	if recorder.Code != http.StatusBadRequest {
		t.Errorf("POST http://shop.test/menu on port 80: %d, want 400: a write is never sent over plain HTTP", recorder.Code)
	}
}

// With allowLocal, or tls: false, the host serves no HTTPS: a machine of development has no certificate to
// ask for, and a host whose TLS ends before Kitwork has no use for one.
func TestNoHTTPSWithAllowLocalOrTLSFalse(t *testing.T) {
	for _, website := range []string{
		`{ hostname: "shop.test", allowLocal: true }`,
		`{ hostname: "shop.test", allowLocal: true, tls: { email: "admin@shop.test" } }`,
		`{ hostname: "shop.test", tls: false }`,
	} {
		folder := write(t, map[string]string{
			"application.work":                tlsManifest(website),
			"apps/shop/shop.test/router.work": routerImport + "router.get((context) => context.text(\"shop\"));\n",
		})
		h, err := host.ReadWithLog(filepath.Join(folder, "application.work"), io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		if err := h.Boot(); err != nil {
			t.Fatal(err)
		}
		if secure, challenge := h.TLSServers(); secure != nil || challenge != nil {
			t.Errorf("app.website(%s): HTTPS on %v, want none", website, secure.Addr)
		}
	}
}

// A host has one public door (14/11/2026). With AutoSSL it is HTTPS: the plain port of app.website listens on
// 127.0.0.1 alone, for the host's own calls, as app.database's port does; on every address it served every site
// without TLS beside the HTTPS that guards them. Without AutoSSL (allowLocal, tls: false) the plain port is the door,
// on every address. The manifest has no word to open it beside HTTPS.
//
// CONTROL: with the plain port on every address whatever TLS is, the AutoSSL host listened on :8123. Red.
func TestThePlainPortIsThisMachinesBesideAutoSSL(t *testing.T) {
	for website, want := range map[string]string{
		`{ hostname: "shop.test", port: 8123 }`:                                    "127.0.0.1:8123",
		`{ hostname: "shop.test", port: 8123, tls: { email: "admin@shop.test" } }`: "127.0.0.1:8123",
		`{ hostname: "shop.test", port: 8123, allowLocal: true }`:                  ":8123",
		`{ hostname: "shop.test", port: 8123, tls: false }`:                        ":8123",
	} {
		folder := write(t, map[string]string{
			"application.work":                tlsManifest(website),
			"apps/shop/shop.test/router.work": routerImport + "router.get((context) => context.text(\"shop\"));\n",
		})
		h, err := host.ReadWithLog(filepath.Join(folder, "application.work"), io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		if err := h.Boot(); err != nil {
			t.Fatal(err)
		}
		server, err := h.Server()
		if err != nil {
			t.Fatal(err)
		}
		if server.Addr != want {
			t.Errorf("app.website(%s): the plain port on %q, want %q", website, server.Addr, want)
		}
		_ = h.Close()
	}
}

// Every answer of AutoSSL's HTTPS carries HSTS for a day (14/11/2026): a page, and a hostname no site has, the browser
// keeping to HTTPS for the name it asked. Never over the plain port, nor with allowLocal or tls: false. A site's own
// header replaces it: one value, the site's.
//
// CONTROL: with the header not written, the HTTPS page had none; with it written whatever the connection, the plain
// port's answer and allowLocal's carried it. Each red.
func TestHTTPSCarriesHSTS(t *testing.T) {
	answer := func(website, hostname, path string, secure bool) []string {
		t.Helper()
		folder := write(t, map[string]string{
			"application.work":                    tlsManifest(website),
			"apps/shop/shop.test/router.work":     routerImport + "router.get((context) => context.text(\"shop\"));\n",
			"apps/shop/shop.test/own/router.work": routerImport + "router.get((context) => context.header(\"Strict-Transport-Security\", \"max-age=31536000; includeSubDomains\").text(\"own\"));\n",
		})
		h, err := host.ReadWithLog(filepath.Join(folder, "application.work"), io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		if err := h.Boot(); err != nil {
			t.Fatal(err)
		}
		defer h.Close()
		request := httptest.NewRequest(http.MethodGet, "http://"+hostname+path, nil)
		if secure {
			request.TLS = &tls.ConnectionState{}
		}
		recorder := httptest.NewRecorder()
		h.ServeHTTP(recorder, request)
		return recorder.Result().Header.Values("Strict-Transport-Security")
	}
	const autoSSL = `{ hostname: "shop.test", port: 8123 }`
	for _, each := range []struct {
		what, website, hostname, path string
		secure                        bool
		want                          string
	}{
		{"a page over HTTPS", autoSSL, "shop.test", "/", true, "max-age=86400"},
		{"no site over HTTPS", autoSSL, "unknown.test", "/", true, "max-age=86400"},
		{"a site's own", autoSSL, "shop.test", "/own", true, "max-age=31536000; includeSubDomains"},
		{"the plain port", autoSSL, "shop.test", "/", false, ""},
		{"allowLocal", `{ hostname: "shop.test", port: 8123, allowLocal: true }`, "shop.test", "/", true, ""},
		{"tls: false", `{ hostname: "shop.test", port: 8123, tls: false }`, "shop.test", "/", true, ""},
	} {
		got := answer(each.website, each.hostname, each.path, each.secure)
		if each.want == "" && len(got) != 0 || each.want != "" && (len(got) != 1 || got[0] != each.want) {
			t.Errorf("%s: Strict-Transport-Security %q, want %q", each.what, got, each.want)
		}
	}
}

// A host moving from the first engine keeps its certificates and its Let's Encrypt account: the first time AutoSSL
// serves, the files of the first engine's certs/ beside the manifest (autocert's, as engine2's) are copied to
// .certificates/, and a handshake for shop.test is answered from them without asking the authority; certs/ is left as
// it was, a folder in it is not copied, the log says so. Once .certificates/ is there it is engine2's own: certs/ is
// never read again (15/11/2026, round 2 of the 13/11 review: every name was asked for again, with a new account).
func TestAutoSSLKeepsTheFirstEnginesCertificates(t *testing.T) {
	files := map[string]string{
		"application.work":                tlsManifest(`{ hostname: "shop.test", tls: { email: "admin@shop.test" } }`),
		"apps/shop/shop.test/router.work": routerImport + "router.get((context) => context.text(\"shop\"));\n",
		"certs/acme_account+key":          "the first engine's account",
		"certs/old/kept":                  "a folder",
	}
	var asked atomic.Int64
	authority := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		http.NotFound(w, r)
	}))
	t.Cleanup(authority.Close)
	serving := func(folder string) (*tls.Config, string) {
		t.Helper()
		log := &lines{}
		h, err := host.ReadWithLog(filepath.Join(folder, "application.work"), log)
		if err != nil {
			t.Fatal(err)
		}
		if err := h.Boot(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = h.Close() })
		h.CertificateAuthority = authority.URL + "/directory"
		secure, _ := h.TLSServers()
		if secure == nil {
			t.Fatal("TLSServers: no HTTPS")
		}
		return secure.TLSConfig, log.String()
	}

	folder := write(t, files)
	cached := selfSigned(t, "shop.test", 4242)
	if err := os.WriteFile(filepath.Join(folder, "certs", "shop.test"), cached, 0o600); err != nil {
		t.Fatal(err)
	}
	config, said := serving(folder)
	certificate, err := config.GetCertificate(hello("shop.test"))
	if err != nil {
		t.Fatalf("a handshake for shop.test: %v, want the first engine's certificate", err)
	}
	if leaf, err := x509.ParseCertificate(certificate.Certificate[0]); err != nil || leaf.SerialNumber.Int64() != 4242 || asked.Load() != 0 {
		t.Errorf("shop.test's certificate: serial %v (%v), the authority asked %d times; want 4242 from certs/, none asked", leaf.SerialNumber, err, asked.Load())
	}
	for name, want := range map[string]string{"acme_account+key": "the first engine's account", "shop.test": string(cached)} {
		if got, err := os.ReadFile(filepath.Join(folder, ".certificates", name)); err != nil || string(got) != want {
			t.Errorf(".certificates/%s: %q (%v), want the first engine's", name, got, err)
		}
		if got, err := os.ReadFile(filepath.Join(folder, "certs", name)); err != nil || string(got) != want {
			t.Errorf("certs/%s: %q (%v), want it left as it was", name, got, err)
		}
	}
	if _, err := os.Stat(filepath.Join(folder, ".certificates", "old")); err == nil {
		t.Error("certs/old, a folder, was copied")
	}
	contains(t, "the log", said, "AutoSSL: 2 file(s) of the first engine's certs/", "copied to .certificates/")
	if strings.Contains(said, "is not copied") {
		t.Errorf("the log says a file was not copied:\n%s", said)
	}

	// engine2's own .certificates/, though empty: certs/ is not read, so shop.test asks the authority
	folder = write(t, files)
	if err := os.WriteFile(filepath.Join(folder, "certs", "shop.test"), cached, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(folder, ".certificates"), 0o700); err != nil {
		t.Fatal(err)
	}
	config, said = serving(folder)
	if _, err := config.GetCertificate(hello("shop.test")); err == nil || asked.Load() == 0 {
		t.Errorf("a handshake for shop.test beside engine2's own .certificates/: %v, the authority asked %d times; want it asked, certs/ unread", err, asked.Load())
	}
	if strings.Contains(said, "first engine") {
		t.Errorf("the log says certs/ was copied over engine2's own .certificates/:\n%s", said)
	}
}
