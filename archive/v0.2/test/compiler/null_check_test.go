package compiler_test

import (
	"strings"
	"testing"
)

// A value a library call may give as null, used where JavaScript throws on null, on a path that has not checked it,
// is a compile error (19/11/2026, decided 27/10/2026; docs/language.md mục 3.7; root docs/KITWORK_NULL_CHECK.md, slice
// 1). The calls: kitwork/file's read, text, bytes, json, info; a storage handle's get(); a database statement's first();
// a router door's context.request.file(name); safe(…)'s error. The uses: a member read or write, a method call, a
// destructuring, for…of, a spread into an array or a call. The checks are what JavaScript reads as one.
//
// CONTROL (19/11/2026): with checkNulls not called, every refused file compiled; with narrow proving nothing, every
// checked file was refused at its first read; with a let kept checked inside a function written after the check, the
// let read in a closure compiled; with a loop's later turns not seen, the let read before the body made it null compiled.
func TestAValueALibraryMayGiveAsNullIsCheckedBeforeUse(t *testing.T) {
	const file = "import * as file from \"kitwork/file\";\n"
	const storage = "import { memory } from \"kitwork/storage\";\n"
	const database = "import { connection } from \"kitwork/database\";\n"
	const router = "import * as router from \"kitwork/router\";\n"
	for source, want := range map[string]string{
		file + "const settings = file.json(\"s.json\");\nconst theme = settings.theme;":                                                    "router.work:3:15: settings có thể là null: file.json(…) trả null khi không có file; kiểm trước khi đọc (if (settings == null) return …), hay viết settings?.…",
		file + "const theme = file.read(\"s.json\").theme;":                                                                                "router.work:2:15: file.read(…) có thể là null: file.read(…) trả null khi không có file",
		file + "const { a } = file.json(\"s.json\");":                                                                                      "router.work:2:15: file.json(…) có thể là null",
		file + "const f = () => { for (const x of file.json(\"l.json\")) { } };":                                                           "router.work:2:35: file.json(…) có thể là null",
		file + "const all = [...file.json(\"l.json\")];":                                                                                   "router.work:2:17: file.json(…) có thể là null",
		file + "const info = file.info(\"a.txt\");\ninfo.size = 1;":                                                                        "router.work:3:1: info có thể là null",
		storage + "const cart = memory(\"cart\");\nconst f = () => { const c = cart.get(); return c.items; };":                             "router.work:3:48: c có thể là null: ….get() của kitwork/storage trả null khi chưa có hay đã hết hạn",
		storage + "const f = () => memory(\"cart\").segment(\"a\").get().items;":                                                           "router.work:2:17: ….get() có thể là null",
		database + "const db = connection();\nconst f = (id) => { const user = db.where((u) => u.id === id).first(); return user.name; };": "router.work:3:79: user có thể là null: ….first() của kitwork/database trả null khi không có dòng nào",
		router + "router.post((context) => { const avatar = context.request.file(\"a\"); return context.text(avatar.name); });":            "router.work:2:90: avatar có thể là null: context.request.file(…) trả null khi form không có file ấy",
		"const r = safe(() => 1);\nconst m = r.error.message;":                                                                             "router.work:2:11: r.error có thể là null: safe(…).error là null khi hàm chạy xong; kiểm trước khi đọc (if (r.error == null) return …), hay viết r.error?.…",
		"const code = safe(() => 1).error.code;":                                                                                           "router.work:1:14: safe(…).error có thể là null",
		"const { error } = safe(() => 1);\nconst m = error.message;":                                                                       "router.work:2:11: error có thể là null",
		file + "const f = (c) => { let x = {}; if (c) { x = file.json(\"a\"); } return x.y; };":                                            "router.work:2:70: x có thể là null",
		file + "const f = () => { let x = file.json(\"a\"); if (!x) return 0; const g = () => x.y; return g; };":                           "router.work:2:77: x có thể là null",
		file + "const f = () => { let x = {}; for (let i = 0; i < 2; i++) { const y = x.y; x = file.json(\"a\"); } };":                     "router.work:2:71: x có thể là null",
	} {
		if _, err := compileFile(t, source); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want the compile error %q", source, err, want)
		}
	}
	for _, source := range []string{
		file + "const settings = file.json(\"s.json\");\nconst f = () => { if (settings == null) return 0; return settings.theme; };",
		file + "const s = file.json(\"s.json\");\nconst a = s ? s.theme : 1;\nconst b = s && s.theme;\nconst c = s?.theme;\nconst d = (s ?? {}).theme;",
		file + "const s = file.json(\"s.json\");\nconst t = s ?? {};\nconst e = t.theme;\nif (s) { const g = s.theme; }",
		file + "const s = file.json(\"a\");\nconst o = file.json(\"b\");\nconst f = () => { if (!s || !o) return 0; return s.a + o.b; };",
		file + "const s = file.json(\"a\");\nconst f = () => { if (s === null) { return 0; } else { return s.a; } };",
		file + "const s = file.json(\"a\");\nconst f = () => { if (s !== null) { return s.a; } return 0; };",
		file + "const f = () => { const x = file.json(\"a\"); if (!x) return 0; const g = () => x.y; return g; };",
		file + "const f = () => { let x = file.json(\"a\"); if (x == null) { x = {}; } return x.y; };",
		file + "const t = JSON.stringify(file.json(\"a\"));\nconst u = String(file.read(\"b\"));\nconst m = { ...file.json(\"c\") };",
		"const r = safe(() => 1);\nconst a = r.error ? r.error.message : \"\";\nconst f = () => { if (r.error === null) return 0; return r.error.code; };",
		"const r = safe(() => 1);\nconst f = () => { if (!r.error) return r.value; return r.error.message; };",
		storage + "const f = () => memory(\"k\").get(() => 1).toFixed();\nconst g = () => (memory(\"k\").get() ?? 0) + 1;",
		"const file = { json: (p) => ({ a: 1 }) };\nconst a = file.json(\"x\").a;",
		"const safe = (f) => ({ error: { code: 1 } });\nconst c = safe(() => 1).error.code;",
		router + "router.post((context) => { const avatar = context.request.file(\"a\"); if (avatar == null) return context.text(\"\"); return context.text(avatar.name); });",
	} {
		if _, err := compileFile(t, source); err != nil {
			t.Errorf("%q: %v, want it compiled", source, err)
		}
	}
}
