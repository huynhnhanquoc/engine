package host_test

import (
	"crypto/ed25519"
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

// channelBinding of app.database (host/port.go; docs/KITWORK_KITDB.md câu 13.11, 08/11/2026). The rule on the wire,
// a client of another machine taken with SCRAM-SHA-256-PLUS only, is tried in engine kitdb/pgwire TestSCRAMPlusRequired.

// channelBinding: "require": the port's server requires SCRAM-SHA-256-PLUS of a client of another machine, as its log
// line says; a client of this machine still comes in, with PLUS over TLS and with SCRAM-SHA-256 without TLS. A
// certificate file signed without a hash (Ed25519) is refused before the port listens; the option takes "prefer" or
// "require" only.
//
// CONTROL (08/11/2026): with port.go not giving the requirement to the server, "the log: …, want it to say
// SCRAM-SHA-256-PLUS is required"; with DatabaseTLS not checking the certificate, "Ed25519 with binding required:
// <nil>, want the certificate refused".
func TestTheDatabasePortRequiresChannelBinding(t *testing.T) {
	manifest := func(port int, option string) string {
		return `import * as app from "kitwork/application";

app.database(".database", { port: ` + strconv.Itoa(port) + `, user: "admin", password: "pw", tls: { certificate: "tls/db.pem", key: "tls/db.key" }, channelBinding: ` + option + ` });
app.run();
`
	}
	authority := newTestAuthority(t)
	certificate, key := authority.issue(t, "db.example")
	folder := write(t, map[string]string{
		"application.work":              manifest(freePort(t), `"require"`),
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
	if !strings.Contains(log.String(), "SCRAM-SHA-256-PLUS required of a client of another machine") {
		t.Errorf("the log: %s, want it to say SCRAM-SHA-256-PLUS is required", log)
	}
	client, err := pgConnectTLS(address.String(), &tls.Config{ServerName: "db.example", RootCAs: authority.pool}, "admin", "pw", "shop")
	if err != nil {
		t.Fatalf("a client of this machine over TLS: %v", err)
	}
	if client.mechanism != "SCRAM-SHA-256-PLUS" {
		t.Errorf("over TLS the client authenticated with %s", client.mechanism)
	}
	client.Close()
	plain, err := pgConnect(address.String(), "admin", "pw", "shop")
	if err != nil {
		t.Fatalf("a client of this machine without TLS: %v", err)
	}
	plain.Close()

	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(3), Subject: pkix.Name{CommonName: "db.example"}, DNSNames: []string{"db.example"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, private.Public(), private)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	edFolder := write(t, map[string]string{
		"application.work":              manifest(freePort(t), `"require"`),
		"tls/db.pem":                    string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		"tls/db.key":                    string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})),
		".database/shop/kitwork.db.sql": "",
	})
	edRead, err := host.ReadWithLog(filepath.Join(edFolder, "application.work"), &lines{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = edRead.Close() })
	if address, err := edRead.DatabasePort(); err == nil || !strings.Contains(err.Error(), "is signed without a hash") {
		t.Errorf("Ed25519 with binding required: %v, %v, want the certificate refused", address, err)
	}

	for _, option := range []string{`"disable"`, `true`, `"Require"`} {
		folder := write(t, map[string]string{"application.work": manifest(5999, option)})
		if _, err := host.ReadWithLog(filepath.Join(folder, "application.work"), &lines{}); err == nil ||
			!strings.Contains(err.Error(), `"prefer" (the default`) {
			t.Errorf("channelBinding: %s: %v, want the two values named", option, err)
		}
	}
}
