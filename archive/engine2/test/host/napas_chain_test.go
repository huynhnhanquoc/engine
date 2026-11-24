package host_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// Each chain a file starts from its napas import is its own, as `import * as napas` is a namespace no
// call changes: a file's `shop` and `other` chains, and a request's chain, never share a field
// (05/11/2026). Until then the import was a builder while the file booted, so every file-level chain
// wrote into the import itself: shop's QR named other's bank and account, and every request's chain
// carried shop's receiver and note.
func TestEachChainFromTheNapasImportIsItsOwn(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work": manifest,
		"apps/shop/shop.test/router.work": "import * as napas from \"kitwork/napas\";\n" + routerImport + `const shop = napas.bank("vcb", "0123456789").receiver("SHOPNAME").info("BOOTNOTE");
const other = napas.bank("acb", "5555555555");
router.get((context) => context.text([shop.payload(), other.payload(), napas.bank("tcb", "999").amount(5).payload()].join(" ")));
`,
	})
	app := boot(t, filepath.Join(folder, "application.work"))
	got := app.get(t, "shop.test", "/")
	parts := strings.Split(got.body, " ")
	if got.status != 200 || len(parts) != 3 {
		t.Fatalf("GET /: %d %q", got.status, got.body)
	}
	shop, other, request := parts[0], parts[1], parts[2]
	if !strings.Contains(shop, "9704360110"+"0123456789") || !strings.Contains(shop, "SHOPNAME") || !strings.Contains(shop, "BOOTNOTE") {
		t.Errorf("shop's chain: %s, want vcb's BIN with shop's account, receiver and note", shop)
	}
	if !strings.Contains(other, "9704160110"+"5555555555") || strings.Contains(other, "SHOPNAME") || strings.Contains(other, "BOOTNOTE") {
		t.Errorf("other's chain: %s, want acb's BIN and account alone", other)
	}
	if strings.Contains(request, "SHOPNAME") || strings.Contains(request, "BOOTNOTE") || strings.Contains(request, "5555555555") {
		t.Errorf("a request's chain carries the file's: %s", request)
	}
}

// A chain a file makes at its top level is a template two chains may branch from, each its own, as
// docs/napas.md mục 2 says every step returns a new copy (05/11/2026): `const store =
// napas.receiver(…).city(…)`, then store.bank("vcb", …) and store.bank("acb", …). Until then a step wrote
// into the Napas it was called on unless the host had frozen it, which it does only once the file has run:
// hanoi and saigon were one struct, === each other, and the Hanoi QR paid into acb's account 2222.
func TestATemplateAFileMakesBranchesIntoChainsOfTheirOwn(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work": manifest,
		"apps/shop/shop.test/router.work": "import * as napas from \"kitwork/napas\";\n" + routerImport + `const store = napas.receiver("CUA HANG KITWORK").city("HCM");
const hanoi = store.bank("vcb", "1111");
const saigon = store.bank("acb", "2222");
router.get((context) => {
  const base = napas.receiver("SHOP");
  const a = base.bank("vcb", "3333");
  const b = base.bank("acb", "4444");
  return context.text([hanoi === saigon, hanoi.amount(150000).payload(), saigon.amount(150000).payload(), a === b, a.payload(), b.payload()].join("|"));
});
`,
	})
	app := boot(t, filepath.Join(folder, "application.work"))
	got := app.get(t, "shop.test", "/")
	parts := strings.Split(got.body, "|")
	if got.status != 200 || len(parts) != 6 {
		t.Fatalf("GET /: %d %q", got.status, got.body)
	}
	if parts[0] != "false" || parts[3] != "false" {
		t.Errorf("hanoi === saigon is %s and a === b is %s, want two chains each", parts[0], parts[3])
	}
	for i, want := range map[int]string{1: "970436" + "0104" + "1111", 2: "970416" + "0104" + "2222", 4: "970436" + "0104" + "3333", 5: "970416" + "0104" + "4444"} {
		if !strings.Contains(parts[i], want) {
			t.Errorf("payload %d: %s, want its own bank and account %s", i, parts[i], want)
		}
	}
	if !strings.Contains(parts[1], "CUA HANG KITWORK") || !strings.Contains(parts[2], "CUA HANG KITWORK") {
		t.Errorf("the branches lost the template's receiver: %s %s", parts[1], parts[2])
	}
}
