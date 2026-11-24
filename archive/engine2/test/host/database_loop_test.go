package host_test

import (
	"net/http"
	"path/filepath"
	"testing"
)

// A statement's lambda reads a name from outside it wherever the name is declared (library/database, runtime.FreeValue):
// at the handler's top, in a block, and in the body of a for or a for…of, where a variable a closure reaches lives in
// the object the loop makes for each iteration (11/11/2026). Each statement reads its own iteration's value: a const
// of the body, the loop's variable, a counter, a variable of each of two loops one inside the other, and the loop's
// variable over a name of the handler it shadows.
//
// CONTROL (11/11/2026): before FreeValue read the objects of iterations, /for, /forof, /counter, /nested, /shadow
// and /where answered 500 "text không đọc được từ ngoài lambda", as the first engine never did.
func TestAStatementReadsTheNamesOfALoop(t *testing.T) {
	schema := `import { struct, id, string } from "kitwork/schema";
import { connection } from "kitwork/database";
const notes = struct({ id: id(), text: string() });
export const db = connection().tables({ notes });
`
	routes := []struct{ path, body, want string }{
		{"top", `const text = "top"; db.insert((notes) => ({ text })); return context.json(1);`, `1`},
		{"block", `if (true) { const text = "block"; db.insert((notes) => ({ text })); } return context.json(2);`, `2`},
		{"for", `for (let i = 0; i < 2; i++) { const text = "for" + i; db.insert((notes) => ({ text })); } return context.json(3);`, `3`},
		{"forof", `for (const text of ["of0", "of1"]) { db.insert((notes) => ({ text })); } return context.json(4);`, `4`},
		{"counter", `const found = []; for (let i = 1; i <= 3; i++) { found.push(db.list((notes) => notes.id === i)[0].text); } return context.json(found);`,
			`["top","block","for0"]`},
		{"nested", `for (const a of ["x", "y"]) { for (const b of ["1", "2"]) { const text = a + b; db.insert((notes) => ({ text })); } } return context.json(5);`, `5`},
		{"shadow", `const text = "outer"; for (const text of ["inner"]) { db.insert((notes) => ({ text })); } return context.json(6);`, `6`},
		{"where", `const counts = []; for (const text of ["for1", "of0", "y2", "nope"]) { counts.push(db.list((notes) => notes.text === text).length); } return context.json(counts);`,
			`[1,1,1,0]`},
		{"all", `return context.json(db.where((notes) => notes.id > 0).ascending((notes) => notes.id).list(100).map((note) => note.text));`,
			`["top","block","for0","for1","of0","of1","x1","x2","y1","y2","inner"]`},
	}
	files := map[string]string{"application.work": manifest, "apps/shop/core/schema/shop.work": schema}
	for _, route := range routes {
		files["apps/shop/shop.test/"+route.path+"/router.work"] = routerImport + `import { db } from "app/core/schema/shop.work";
router.get((context) => { ` + route.body + ` });
`
	}
	folder := write(t, files)
	if code, said := runMigrate(t, folder, true, ""); code != 0 {
		t.Fatalf("kitwork migrate: %d\n%s", code, said)
	}
	b := boot(t, filepath.Join(folder, "application.work"))
	for _, route := range routes {
		got := b.get(t, "shop.test", "/"+route.path)
		if got.status != http.StatusOK || got.body != route.want {
			t.Errorf("/%s: %d %s, want %s\n%s", route.path, got.status, got.body, route.want, b.log)
		}
	}
}
