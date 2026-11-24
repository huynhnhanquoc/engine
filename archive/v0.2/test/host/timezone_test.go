package host_test

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	engine2 "github.com/kitwork/engine2"
)

// The zone is asked up the tiers, the nearest winning (slice 3 of root docs/KITWORK_MODULE_WORK.md mục 6, 17/11/2026):
// a schedule's own .timezone(…), then the app's module.timezone(…), then the host's app.timezone(…) of
// application.work, then UTC. kitwork check's map says where each zone comes from.

const zonedManifest = `import * as app from "kitwork/application";

app.root("apps");
app.timezone("Pacific/Kiritimati");
app.website({ hostname: "shop.test", port: 8080 });
app.run();
`

// An app whose module.work declares no zone counts in the host's app.timezone; one that declares its own counts in
// its own. Each says so on the map, with where the zone is written; a schedule without .timezone(…) counts in its
// app's, one with it in its own, and neither is said to count in UTC.
func TestTheZoneIsAskedUpTheTiers(t *testing.T) {
	answer := routerImport + "router.get((context) => context.text(new Date(2026, 0, 1).toISOString()));\n"
	folder := write(t, map[string]string{
		"application.work":                zonedManifest,
		"apps/host/host.test/router.work": answer,
		"apps/host/core/clock.work":       scheduleImport + "export const nhip = schedule.every(\"1m\").run((context) => {});\nexport const tokyo = schedule.daily(\"08:00\").timezone(\"Asia/Tokyo\").run((context) => {});\n",
		"apps/own/module.work":            zonedModule("Asia/Ho_Chi_Minh"),
		"apps/own/own.test/router.work":   answer,
	})
	b := boot(t, filepath.Join(folder, "application.work"))
	for hostname, want := range map[string]string{
		"host.test": "2025-12-31T10:00:00.000Z", // the host's, UTC+14
		"own.test":  "2025-12-31T17:00:00.000Z", // the app's own, UTC+7
	} {
		if got := b.get(t, hostname, "/"); got.body != want {
			t.Errorf("%s: %q, want the new year of its zone, %q", hostname, got.body, want)
		}
	}
	if strings.Contains(b.log.String(), "đếm theo UTC") {
		t.Errorf("a schedule of an app the host gives a zone is said to count in UTC:\n%s", b.log)
	}
	shown, err := engine2.CheckMap(filepath.Join(folder, "application.work"))
	if err != nil {
		t.Fatal(err)
	}
	plain := regexp.MustCompile(` +`).ReplaceAllString(shown, " ")
	for _, want := range []string{
		"múi giờ Pacific/Kiritimati application.work:4:4 (app.timezone, của host)",
		"múi giờ Asia/Ho_Chi_Minh module.work:3:7",
		`lịch nhip recurring every("1m") Pacific/Kiritimati core/clock.work:2:29 (của app: application.work:4:4 (app.timezone, của host))`,
		`lịch tokyo recurring daily("08:00") Asia/Tokyo core/clock.work:3:30 (.timezone của lịch)`,
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("the map lacks %q:\n%s", want, shown)
		}
	}
}

// With no tier declaring a zone, the app counts in UTC: the map says no tier declares one, and a schedule without
// .timezone(…) is said to count in UTC, naming each tier it could have read.
func TestNoTierDeclaringAZoneIsUTC(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work":                manifest,
		"apps/shop/core/clock.work":       scheduleImport + "export const nhip = schedule.every(\"1m\").run((context) => {});\n",
		"apps/shop/shop.test/router.work": says("shop"),
	})
	b := boot(t, filepath.Join(folder, "application.work"))
	contains(t, "the log", b.log.String(), "lịch nhip không viết .timezone(…), app không khai module.timezone(…), host không khai app.timezone(…): đếm theo UTC")
	shown, err := engine2.CheckMap(filepath.Join(folder, "application.work"))
	if err != nil {
		t.Fatal(err)
	}
	if plain := regexp.MustCompile(` +`).ReplaceAllString(shown, " "); !strings.Contains(plain, "múi giờ UTC (không tầng nào khai)") {
		t.Errorf("the map does not say no tier declares a zone:\n%s", shown)
	}
}

// app.timezone is written once, an IANA zone, before app.run().
func TestAppTimezoneIsWrittenRight(t *testing.T) {
	head := "import * as app from \"kitwork/application\";\n\napp.root(\"apps\");\napp.website({ hostname: \"shop.test\", port: 8080 });\n"
	for source, want := range map[string]string{
		head + "app.timezone(\"Asia/Ho_Chi_Minh\");\napp.timezone(\"UTC\");\napp.run();\n": "app.timezone(…) is declared twice: the first is at application.work:5:4",
		head + "app.timezone(\"Europe/Nowhere\");\napp.run();\n":                           "app.timezone(\"Europe/Nowhere\"): not an IANA zone name",
		head + "app.timezone(\"Local\");\napp.run();\n":                                    "the server's own clock (\"Local\") is no app's zone",
		head + "app.timezone(7);\napp.run();\n":                                            "app.timezone(…) takes an IANA zone name",
		head + "app.run();\napp.timezone(\"UTC\");\n":                                      "app.timezone(…) is written after app.run()",
	} {
		got := bootError(t, map[string]string{"application.work": source, "apps/shop/shop.test/router.work": says("shop")})
		if !strings.Contains(got, want) {
			t.Errorf("%q:\n got %q\nwant %q", source, got, want)
		}
	}
}
