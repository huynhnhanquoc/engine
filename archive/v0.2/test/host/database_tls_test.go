package host_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kitwork/engine2/host"
)

// The TLS of app.database's port (host/port.go DatabaseTLS; docs/KITWORK_KITDB.md câu 13.11, 08/11/2026).

// testAuthority signs the port's certificates of these tests; pool trusts it.
type testAuthority struct {
	certificate *x509.Certificate
	key         *ecdsa.PrivateKey
	pool        *x509.CertPool
}

func newTestAuthority(t *testing.T) *testAuthority {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test authority"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(certificate)
	return &testAuthority{certificate: certificate, key: key, pool: pool}
}

// issue is a certificate of name and its key, in PEM.
func (authority *testAuthority) issue(t *testing.T, name string) (certificate, key string) {
	t.Helper()
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: name}, DNSNames: []string{name},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, authority.certificate, &private.PublicKey, authority.key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))
}

// app.database({ tls: { certificate, key } }), the files read from the manifest's folder: the port answers an
// SSLRequest with S, and a client checking the certificate against its name and the authority reads through it,
// authenticated with SCRAM-SHA-256-PLUS bound to that certificate; a client asking for another name is refused by its
// own check; a client of this machine may still come in without TLS, with SCRAM-SHA-256.
//
// CONTROL (08/11/2026): with the server not given DatabaseTLS's config, "connect over TLS: the server answered the
// SSLRequest 'N': no TLS"; with the files not read from the manifest's folder, "the port: <nil>, app.database({ tls }):
// pgwire: TLS certificate: GetFileAttributesEx tls\db.pem: The system cannot find the path specified."; with pgwire
// keeping no certificate's hash (engine kitdb/pgwire startTLS), "over TLS the client authenticated with SCRAM-SHA-256,
// want SCRAM-SHA-256-PLUS, bound to the certificate".
func TestTheDatabasePortServesTLSFromItsFiles(t *testing.T) {
	authority := newTestAuthority(t)
	certificate, key := authority.issue(t, "db.example")
	port := freePort(t)
	folder := write(t, map[string]string{
		"application.work": `import * as app from "kitwork/application";

app.database(".database", { port: ` + strconv.Itoa(port) + `, user: "admin", password: "pw", tls: { certificate: "tls/db.pem", key: "tls/db.key" } });
app.run();
`,
		"tls/db.pem":                    certificate,
		"tls/db.key":                    key,
		".database/shop/kitwork.db.sql": "",
	})
	log := &lines{}
	read, err := host.ReadWithLog(filepath.Join(folder, "application.work"), log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = read.Close() })
	address, err := read.DatabasePort()
	if err != nil || address == nil {
		t.Fatalf("the port: %v, %v", address, err)
	}
	client, err := pgConnectTLS(address.String(), &tls.Config{ServerName: "db.example", RootCAs: authority.pool}, "admin", "pw", "shop")
	if err != nil {
		t.Fatalf("connect over TLS: %v\n%s", err, log)
	}
	if client.mechanism != "SCRAM-SHA-256-PLUS" {
		t.Errorf("over TLS the client authenticated with %s, want SCRAM-SHA-256-PLUS, bound to the certificate", client.mechanism)
	}
	if _, tag, err := client.query(`CREATE TABLE notes (id bigserial PRIMARY KEY, text text NOT NULL)`); err != nil || tag != "CREATE TABLE" {
		t.Fatalf("CREATE TABLE over TLS: %q, %v", tag, err)
	}
	if rows, _, err := client.query(`SELECT count(*) FROM notes`); err != nil || jsonText(rows) != `[["0"]]` {
		t.Errorf("SELECT over TLS: %s, %v", jsonText(rows), err)
	}
	client.Close()
	if _, err := pgConnectTLS(address.String(), &tls.Config{ServerName: "other.example", RootCAs: authority.pool}, "admin", "pw", "shop"); err == nil || !strings.Contains(err.Error(), "other.example") {
		t.Errorf("a name the certificate is not of: %v, want refused", err)
	}
	plain, err := pgConnect(address.String(), "admin", "pw", "shop")
	if err != nil {
		t.Fatalf("a client of this machine without TLS: %v", err)
	}
	if plain.mechanism != "SCRAM-SHA-256" {
		t.Errorf("without TLS the client authenticated with %s, want SCRAM-SHA-256", plain.mechanism)
	}
	plain.Close()
	if !strings.Contains(log.String(), "TLS: "+filepath.Join(folder, "tls", "db.pem")) {
		t.Errorf("the log: %s", log)
	}
}

// Which TLS the port answers with, beyond the loopback, before it listens: the website's certificates, a site's by the
// name the client connects with, out of .certificates/ beside the manifest; none for a name that is no site; an error
// with no website of AutoSSL; the loopback without files has none, as before.
//
// CONTROL (08/11/2026): with DatabaseTLS answering nil beyond the loopback, "0.0.0.0 with a website: <nil> (no TLS)";
// with every name taken for a site, "other.test, no site: a certificate (true), want refused".
func TestTheDatabasePortChoosesItsCertificates(t *testing.T) {
	authority := newTestAuthority(t)
	shop, shopKey := authority.issue(t, "shop.test")
	other, otherKey := authority.issue(t, "other.test")
	database := func(host string) string {
		return `app.database(".database", { host: "` + host + `", port: 5999, user: "admin", password: "pw" });`
	}
	folder := write(t, map[string]string{
		"application.work": `import * as app from "kitwork/application";

app.root("apps");
app.website({ hostname: "shop.test", port: 8080 });
` + database("0.0.0.0") + `
app.run();
`,
		"apps/shop/shop.test/router.work": routerImport,
		".certificates/shop.test":         shopKey + shop,
		".certificates/other.test":        otherKey + other,
	})
	read, err := host.ReadWithLog(filepath.Join(folder, "application.work"), &lines{})
	if err != nil {
		t.Fatal(err)
	}
	secure, said, err := read.DatabaseTLS()
	if err != nil || secure == nil {
		t.Fatalf("0.0.0.0 with a website: %v (no TLS), %v", secure, err)
	}
	if !strings.Contains(said, filepath.Join(folder, ".certificates")) {
		t.Errorf("it says %q, want the store", said)
	}
	if found, err := secure.GetCertificate(&tls.ClientHelloInfo{ServerName: "shop.test"}); err != nil || found == nil {
		t.Errorf("shop.test: %v", err)
	}
	if found, err := secure.GetCertificate(&tls.ClientHelloInfo{ServerName: "other.test"}); err == nil {
		t.Errorf("other.test, no site: a certificate (%v), want refused", found != nil)
	}

	for name, test := range map[string]struct{ manifest, says string }{
		"no website": {database("0.0.0.0") + "\napp.run();", "write tls: { certificate, key }"},
		"tls: false": {`app.root("apps"); app.website({ hostname: "shop.test", port: 8080, tls: false });` + "\n" + database("0.0.0.0") + "\napp.run();", "write tls: { certificate, key }"},
		"loopback":   {database("127.0.0.1") + "\napp.run();", ""},
	} {
		folder := write(t, map[string]string{
			"application.work":                "import * as app from \"kitwork/application\";\n" + test.manifest + "\n",
			"apps/shop/shop.test/router.work": routerImport,
		})
		read, err := host.ReadWithLog(filepath.Join(folder, "application.work"), &lines{})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		secure, _, err := read.DatabaseTLS()
		switch {
		case test.says == "" && (err != nil || secure != nil):
			t.Errorf("%s: %v, %v, want no TLS and no error", name, secure != nil, err)
		case test.says != "" && (err == nil || !strings.Contains(err.Error(), test.says)):
			t.Errorf("%s: %v, want %q", name, err, test.says)
		}
	}
}

// tls of app.database takes certificate and key, both, and nothing else.
func TestTheDatabasePortTLSOption(t *testing.T) {
	for option, says := range map[string]string{
		`{ certificate: "a.pem" }`:                        "{ certificate, key }",
		`{ certificate: "a.pem", key: "a.key", ca: "c" }`: "tls takes certificate and key",
		`true`:                               "{ certificate, key }",
		`{ certificate: " ", key: "a.key" }`: "the path of a file",
	} {
		folder := write(t, map[string]string{"application.work": `import * as app from "kitwork/application";
app.database(".database", { port: 5999, password: "pw", tls: ` + option + ` });
app.run();
`})
		_, err := host.ReadWithLog(filepath.Join(folder, "application.work"), &lines{})
		if err == nil || !strings.Contains(err.Error(), says) {
			t.Errorf("tls: %s: %v, want %q", option, err, says)
		}
	}
}
