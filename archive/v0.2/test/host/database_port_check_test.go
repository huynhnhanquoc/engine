package host_test

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The database stands by itself (Quốc, 07/11/2026): its own rules are written in SQL by whoever owns it, as on
// PostgreSQL, no struct asked. An admin makes a table with CHECKs through the port, a decimal's among them (its
// numbers are the value, the file compares the stored integer); a row outside one is 23514, and kitwork.db.sql writes
// the table back as it was written.
//
// CONTROL (07/11/2026): with the gateway keeping a decimal's CHECK numbers as written, the row of price 1000000 was
// refused (its stored 100000000 against 1000000), and kitwork.db.sql wrote the table as SQLite's.
func TestTheDatabasesOwnCheckIsWrittenInSQL(t *testing.T) {
	port := freePort(t)
	manifest := strings.Replace(manifest, "app.run();", `app.database(".database", { port: `+strconv.Itoa(port)+`, user: "admin", password: "pw" });
app.run();`, 1)
	folder := write(t, map[string]string{
		"application.work":                manifest,
		"apps/shop/shop.test/router.work": routerImport + "router.get((context) => context.text(\"ok\"));\n",
	})
	shop := filepath.Join(folder, ".database", "shop")
	if err := os.MkdirAll(shop, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shop, "kitwork.db.sql"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	app := boot(t, filepath.Join(folder, "application.work"))
	address, err := app.host.DatabasePort()
	if err != nil {
		t.Fatal(err)
	}
	client, err := pgConnect(address.String(), "admin", "pw", "shop")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if _, _, err := client.query(`CREATE TABLE items (id bigserial PRIMARY KEY, name text NOT NULL CHECK (length(name) >= 1), qty bigint NOT NULL CHECK (qty >= 0), price numeric(15, 2) NOT NULL CHECK (price >= 0 AND price <= 1000000))`); err != nil {
		t.Fatalf("CREATE TABLE with CHECKs: %v", err)
	}
	if _, _, err := client.query(`INSERT INTO items (name, qty, price) VALUES ('Cà phê', 1, 12.5), ('x', 0, 1000000)`); err != nil {
		t.Errorf("rows the CHECKs take: %v", err)
	}
	for _, row := range []string{
		`INSERT INTO items (name, qty, price) VALUES ('', 1, 1)`,
		`INSERT INTO items (name, qty, price) VALUES ('a', -1, 1)`,
		`INSERT INTO items (name, qty, price) VALUES ('a', 1, 1000000.01)`,
		`UPDATE items SET qty = qty - 5`,
	} {
		var wire *pgError
		if _, _, err := client.query(row); !errors.As(err, &wire) || wire.code != "23514" {
			t.Errorf("%s: %v, want 23514", row, err)
		}
	}
	structure, err := os.ReadFile(filepath.Join(shop, "kitwork.db") + ".sql")
	if err != nil || !strings.Contains(string(structure), "price numeric(15, 2) NOT NULL CHECK (price >= 0 AND price <= 1000000)") {
		t.Errorf("kitwork.db.sql writes the CHECK back as its values: %v\n%s", err, structure)
	}
}
