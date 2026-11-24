package compiler_test

import (
	"strings"
	"testing"

	"github.com/kitwork/engine2/core/compiler"
	"github.com/kitwork/engine2/core/runtime"
)

// var (docs/language.md mục 2): the state of a screen, at the top of the function of router.view(…) or
// component.define(…) only, written as the let the run's context fills; anywhere else a compile error.
func TestVarStandsOnlyAtTheTopOfAViewOrAComponent(t *testing.T) {
	const router = "import * as router from \"kitwork/router\";\n"
	const component = "import * as component from \"kitwork/component\";\n"
	for _, source := range []string{
		router + `router.view((context) => { var q = ""; const set = () => { q = "x"; }; return context.bind({ q, set }); });`,
		router + `router.guard((context) => context.next()).view((context) => { var q; return context.bind({ q }); });`,
		"import * as r from \"kitwork/router\";\n" + `r.view((ctx) => { var q = 1; q = q + 1; return ctx.bind({ q }); });`,
		component + `const cart = (context) => { var items = []; return context.bind({ items }); };
component.define("shop-cart", cart);`,
		component + `component.define("shop-cart", (context) => { var items = []; var done = false; return context.bind({ items, done }); });`,
	} {
		bytecode, err := compiler.CompileSource(source)
		if err != nil {
			t.Errorf("%s: %v", source, err)
			continue
		}
		inspection, err := runtime.InspectProgram(bytecode.Program)
		if err != nil {
			t.Fatal(err)
		}
		filled := false
		for _, instruction := range inspection.Instructions {
			if strings.Contains(instruction.ConstantPreview, "var#") {
				filled = true
			}
		}
		if !filled {
			t.Errorf("%s: no var is filled by its context", source)
		}
	}
	for source, want := range map[string]string{
		`var x = 1;`: "1:1: var x ở cấp cao nhất của file",
		`const f = () => { var x = 1; return x; };`: "1:19: var x chỉ đứng ở cấp cao nhất của hàm router.view(…) hay component.define(…)",
		"import * as router from \"kitwork/router\";\n" + `router.view((context) => { if (true) { var x = 1; } return context.bind({}); });`:           "2:40: var x chỉ đứng ở cấp cao nhất",
		"import * as router from \"kitwork/router\";\n" + `router.view((context) => { const f = () => { var x = 1; }; return context.bind({ f }); });`: "2:46: var x chỉ đứng ở cấp cao nhất",
		"import * as router from \"kitwork/router\";\n" + `router.post((context) => { var x = 1; return context.bind({ x }); });`:                      "2:28: var x chỉ đứng ở cấp cao nhất",
		"import * as router from \"kitwork/router\";\n" + `router.view(() => { var x = 1; return 1; });`:                                               "2:21: var x là trạng thái của màn hình, giữ trong context của lần chạy: hàm cần tham số context",
		"import * as router from \"kitwork/router\";\n" + `router.view((context) => { var { a } = context; return context.bind({ a }); });`:            "2:28: var khai một tên mỗi dòng",
		`const router = { view: (f) => f }; router.view((context) => { var x = 1; });`:                                                                 "1:63: var x chỉ đứng ở cấp cao nhất",
		"import * as component from \"kitwork/component\";\n" + `component.define((context) => { var x = 1; });`:                                       "2:33: var x chỉ đứng ở cấp cao nhất",
	} {
		_, err := compiler.CompileSource(source)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want an error containing %q", source, err, want)
		}
	}
}
