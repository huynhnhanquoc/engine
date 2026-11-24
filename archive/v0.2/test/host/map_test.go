package host_test

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	engine2 "github.com/kitwork/engine2"
)

// kitwork check prints the map of every app (slice 2c of root docs/KITWORK_MODULE_WORK.md, 17/11/2026): what its
// module.work declares, its sites and the folder each serves, each page with its doors, its tables, jobs, schedules and
// the rules of its queue and schedules, each with the place that declares it, read from what the boot made. A closed
// app says why.
func TestCheckPrintsTheMapOfEachApp(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work": manifest,
		"apps/shop/module.work": moduleImport + "module.timezone(\"Asia/Ho_Chi_Minh\");\n" +
			"module.routes((site) => site.domain === \"vn.test\" ? \"sites/vn\" : site.domain);\n",
		"apps/shop/vn.test/.keep":         "",
		"apps/shop/sites/vn/router.work":  says("vn"),
		"apps/shop/shop.test/router.work": says("shop"),
		"apps/shop/shop.test/san-pham/router.work": routerImport + "router.post((context) => context.text(\"a\"));\n" +
			"router.get((context) => context.text(\"b\"));\n",
		"apps/shop/core/database.work":        declareHead + "const notes = struct({ id: id(), body: string() });\nexport const db = connection().tables({ notes });\n",
		"apps/shop/core/thanks.work":          queueImport + "export const thanks = queue.job((context) => {});\n",
		"apps/shop/core/clock.work":           scheduleImport + "export const nhip = schedule.every(\"1m\").run((context) => {});\n",
		"apps/shop/core/rules.work":           queueImport + "queue.parallel(2);\n",
		"apps/broken/broken.test/router.work": routerImport + "router.get((context) => context.text(\"a\")\n",
	})
	shown, err := engine2.CheckMap(filepath.Join(folder, "application.work"))
	if err == nil || !strings.Contains(err.Error(), "broken") {
		t.Fatalf("the check's error: %v", err)
	}
	space := regexp.MustCompile(` +`)
	plain := space.ReplaceAllString(shown, " ")
	for _, want := range []string{
		"app shop",
		"múi giờ Asia/Ho_Chi_Minh module.work:2:7",
		"routes module.routes module.work:3:7",
		"site shop.test shop.test/",
		"site vn.test sites/vn/",
		"trang shop.test / GET shop.test/router.work:2:7",
		"trang shop.test /san-pham GET shop.test/san-pham/router.work:3:7",
		"trang shop.test /san-pham POST shop.test/san-pham/router.work:2:7",
		"trang vn.test / GET sites/vn/router.work:2:7",
		"bảng notes connection() core/database.work:4:31",
		"job thanks core/thanks.work:2:28",
		`lịch nhip recurring every("1m") Asia/Ho_Chi_Minh core/clock.work:2:29`,
		"luật queue.parallel core/rules.work:2:6",
		"app broken",
		"site broken.test 503",
		"đóng broken/broken.test/router.work",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("the map lacks %q:\n%s", want, shown)
		}
	}
}
