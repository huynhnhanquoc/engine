package host_test

import (
	"crypto/tls"
	"encoding/pem"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/kitwork/engine2/host"
)

// clientAuthority of app.database (host/port.go; docs/KITWORK_KITDB.md câu 13.11, 08/11/2026). The rule on the wire, a
// client of another machine presents a certificate of these authorities named for its user, besides its password, is
// tried in engine kitdb/pgwire TestClientCertificates.

// clientAuthority, read from the manifest's folder: the port's server takes its authorities, as its log line says; a
// client of this machine still comes in without a certificate. A file that does not exist stops the port before it
// listens; the option is the path of a file.
//
// CONTROL (08/11/2026): with port.go not giving the authorities to the server, "the log: …, want it to name the
// client authority"; with the path not read from the manifest's folder, "the port: <nil>, app.database({
// clientAuthority }): pgwire: client authority: open tls\clients.pem: …".
func TestTheDatabasePortClientAuthority(t *testing.T) {
	manifest := func(port int, option string) string {
		return `import * as app from "kitwork/application";

app.database(".database", { port: ` + strconv.Itoa(port) + `, user: "admin", password: "pw", tls: { certificate: "tls/db.pem", key: "tls/db.key" }, clientAuthority: ` + option + ` });
app.run();
`
	}
	authority := newTestAuthority(t)
	certificate, key := authority.issue(t, "db.example")
	clients := newTestAuthority(t)
	folder := write(t, map[string]string{
		"application.work":              manifest(freePort(t), `"tls/clients.pem"`),
		"tls/db.pem":                    certificate,
		"tls/db.key":                    key,
		"tls/clients.pem":               string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: clients.certificate.Raw})),
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
	if !strings.Contains(log.String(), "a client of another machine presents a certificate of "+filepath.Join(folder, "tls", "clients.pem")) {
		t.Errorf("the log: %s, want it to name the client authority", log)
	}
	client, err := pgConnectTLS(address.String(), &tls.Config{ServerName: "db.example", RootCAs: authority.pool}, "admin", "pw", "shop")
	if err != nil {
		t.Fatalf("a client of this machine without a certificate: %v", err)
	}
	client.Close()

	missing := write(t, map[string]string{
		"application.work":              manifest(freePort(t), `"tls/none.pem"`),
		"tls/db.pem":                    certificate,
		"tls/db.key":                    key,
		".database/shop/kitwork.db.sql": "",
	})
	missingRead, err := host.ReadWithLog(filepath.Join(missing, "application.work"), &lines{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = missingRead.Close() })
	if address, err := missingRead.DatabasePort(); err == nil || !strings.Contains(err.Error(), "clientAuthority") {
		t.Errorf("a clientAuthority file that does not exist: %v, %v, want the port stopped naming it", address, err)
	}

	for _, option := range []string{`""`, `true`, `{ file: "a.pem" }`} {
		folder := write(t, map[string]string{"application.work": manifest(5999, option)})
		if _, err := host.ReadWithLog(filepath.Join(folder, "application.work"), &lines{}); err == nil ||
			!strings.Contains(err.Error(), "app.database({ clientAuthority })") {
			t.Errorf("clientAuthority: %s: %v, want an error naming it", option, err)
		}
	}
}
