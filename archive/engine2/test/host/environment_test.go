package host_test

import (
	"path/filepath"
	"testing"
)

// A .env value is coerced by its canonical form (docs/KITWORK_ENVIRONMENT.md mục 4): a number only when
// writing it out again gives the same text, true and false in any case booleans, anything quoted a text, an
// empty value no value (null). A later file wins key by key; an empty value in it leaves the earlier one.
func TestAnEnvironmentValueIsCoercedByItsCanonicalForm(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work": manifest,
		"apps/shop/app.env": "INT=42\nNEGATIVE=-7\nDECIMAL=3.14\nUPPER=TRUE\nLOWER=false\nZIP=0123\nPHONE=+84912345678\n" +
			"EXPONENT=1e3\nVERSION=1.10\nINFINITY=Infinity\nPLUS_INFINITY=+Inf\nNAN=NaN\nNEGATIVE_ZERO=-0\nWORD=kit\n" +
			"QUOTED=\"42\"\nSINGLE='1.10'\nEMPTY=\nexport EXPORTED=yes\n# COMMENT=no\nOVER=app\nKEPT=app\n",
		"apps/shop/shop.test/site.env": "OVER=site\nKEPT=\n",
		"apps/shop/shop.test/router.work": `import * as router from "kitwork/router";
import * as file from "kitwork/file";

const env = file.env("app/app.env", "./site.env");
const read = (value) => typeof value + ":" + JSON.stringify(value);

router.get((context) => context.json({
  INT: read(env.INT), NEGATIVE: read(env.NEGATIVE), DECIMAL: read(env.DECIMAL), UPPER: read(env.UPPER),
  LOWER: read(env.LOWER), ZIP: read(env.ZIP), PHONE: read(env.PHONE), EXPONENT: read(env.EXPONENT),
  VERSION: read(env.VERSION), INFINITY: read(env.INFINITY), PLUS_INFINITY: read(env.PLUS_INFINITY),
  NAN: read(env.NAN), NEGATIVE_ZERO: read(env.NEGATIVE_ZERO), WORD: read(env.WORD), QUOTED: read(env.QUOTED),
  SINGLE: read(env.SINGLE), EMPTY: read(env.EMPTY), EXPORTED: read(env.EXPORTED), COMMENT: read(env.COMMENT),
  OVER: read(env.OVER), KEPT: read(env.KEPT), NONE: read(env.NONE), required: env.require("INT")
}));
`,
	})
	app := boot(t, filepath.Join(folder, "application.work"))
	var got map[string]any
	decode(t, app.get(t, "shop.test", "/").body, &got)
	for key, want := range map[string]any{
		"INT": "number:42", "NEGATIVE": "number:-7", "DECIMAL": "number:3.14", "UPPER": "boolean:true",
		"LOWER": "boolean:false", "ZIP": `string:"0123"`, "PHONE": `string:"+84912345678"`, "EXPONENT": `string:"1e3"`,
		"VERSION": `string:"1.10"`, "INFINITY": `string:"Infinity"`, "PLUS_INFINITY": `string:"+Inf"`, "NAN": `string:"NaN"`,
		"NEGATIVE_ZERO": `string:"-0"`, "WORD": `string:"kit"`, "QUOTED": `string:"42"`, "SINGLE": `string:"1.10"`,
		"EMPTY": "object:null", "EXPORTED": `string:"yes"`, "COMMENT": "object:null", "OVER": `string:"site"`,
		"KEPT": `string:"app"`, "NONE": "object:null", "required": float64(42),
	} {
		if got[key] != want {
			t.Errorf("%s: %v, want %v", key, got[key], want)
		}
	}
}
