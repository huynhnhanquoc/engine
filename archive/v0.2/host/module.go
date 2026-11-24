package host

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/kitwork/engine2/core/compiler"
	"github.com/kitwork/engine2/core/runtime"
	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/module"
	"github.com/kitwork/engine2/library/router"
)

// An app's root is the folder of its module.work (docs/module.md; root docs/KITWORK_MODULE_WORK.md, slice 1, 17/11/2026),
// as go.mod says where a Go module begins. It runs first as the app boots, importing kitwork/module alone, and says the
// app's zone (module.timezone) and, for an owner of several sites, the folder of each hostname the host gives the app
// (module.routes). Before it, apps/<identity>/ was an app by its place, each folder under it a site by its name, and
// the zone was router.timezone(…) in the config.work of the app root.

// bootModule runs the module.work of the app identity at appRoot and gives what it declares. An app without one does not
// boot: the folder of the apps holds apps, and an app says it is one.
func (h *Host) bootModule(appRoot, identity string, requirements *Requirements) (*module.Declared, error) {
	file := filepath.Join(appRoot, "module.work")
	if !isFile(file) {
		return nil, fmt.Errorf("%s has no module.work: an app's root is the folder of its module.work, import * as module from \"kitwork/module\"; a site in a folder of each hostname is module.routes((site) => site.domain) (docs/module.md)", identity)
	}
	lead := "[" + identity + "] "
	notice := func(text string) { h.log.Led(lead, text) }
	globals, slots := builtinsOf(h.log, lead, time.UTC)
	declared := &module.Declared{}
	kitwork := &Kitwork{folder: appRoot, scope: router.NewScope(router.LevelApp, nil, notice), file: "module.work", notice: notice, requirements: requirements, declared: declared}
	if err := h.bootFile(appRoot, file, globals, slots, kitwork); err != nil {
		return nil, err
	}
	return declared, nil
}

// route is the folder module.routes gives hostname, "/"-separated from the app's root: the function called once, as
// the app boots, with { domain }, on a VM of its own, within what a run of the host spends and makes. What it gives is a
// folder of the app below its root, each step of it no dot folder and no underscore one, which are no site's (hidden),
// not a link; anything else is that hostname's error, which closes its site alone.
func (h *Host) route(declared *module.Declared, appRoot, identity, hostname string, zone *time.Location) (string, error) {
	called := "module.routes(…) at " + declared.RoutesAt + ", for " + hostname
	program, ok := runtime.ProgramFromRef(declared.Routes.Program)
	if !ok {
		return "", fmt.Errorf("%s: the function has no program", called)
	}
	globals, slots := builtinsOf(h.log, "["+identity+"] ", zone)
	machine := runtime.New(program)
	machine.FastReset(program, globals)
	machine.Builtins = withKitwork(slots, &Kitwork{folder: appRoot, notice: func(string) {}})
	machine.MaxEnergy = h.runEnergy()
	machine.MaxBytes = h.runBytes()
	site := value.Object()
	site.Set("domain", value.String(hostname))
	result := machine.ExecuteLambda(declared.Routes, []value.Value{site})
	if result.IsInvalid() {
		return "", fmt.Errorf("%s: %s", called, result.Failure())
	}
	if !result.IsString() {
		return "", fmt.Errorf("%s gives %s, not a folder: the function gives the name of the site's folder, from the app's root", called, result.Kind())
	}
	folder := result.Text()
	if problem := folderProblem(folder); problem != "" {
		return "", fmt.Errorf("%s gives %s, which is no site's folder: %s", called, strconv.Quote(folder), problem)
	}
	info, err := os.Lstat(filepath.Join(appRoot, filepath.FromSlash(folder)))
	switch {
	case err != nil:
		return "", fmt.Errorf("%s gives %s, a folder the app does not have", called, strconv.Quote(folder))
	case info.Mode()&os.ModeSymlink != 0:
		return "", fmt.Errorf("%s gives %s, a link: a site's folder is one of the app's own", called, strconv.Quote(folder))
	case !info.IsDir():
		return "", fmt.Errorf("%s gives %s, a file, not a folder", called, strconv.Quote(folder))
	}
	return folder, nil
}

// folderProblem is what keeps folder, a path module.routes gives, from being a site's folder below the app's root; ""
// when it can be one.
func folderProblem(folder string) string {
	if folder == "" {
		return "the app's root is the one site of an app that writes no module.routes"
	}
	if strings.ContainsAny(folder, "\\:") || strings.ContainsFunc(folder, func(r rune) bool { return r < ' ' || r == 0x7f }) {
		return "a folder from the app's root, its steps between \"/\", without \"\\\", \":\" or a control character"
	}
	for _, step := range strings.Split(folder, "/") {
		switch {
		case step == "":
			return "a folder from the app's root, without an empty step (a \"/\" at an end, two together)"
		case step == "." || step == "..":
			return "a folder below the app's root, straight down: no \".\" or \"..\""
		case hidden(step):
			return "a dot folder or an underscore one (" + step + ") is the app's own, no site's"
		}
	}
	return ""
}

// oldFolders is the error of an app at appRoot that keeps code in .shared/ or in .schedule/, the places of its tables,
// jobs, schedules and shared code until 17/11/2026 (slice 2b of root docs/KITWORK_MODULE_WORK.md), and of its components
// and their interfaces, .shared/components/, until slice 7 the same day: each file declares itself wherever it is, so
// the folders mean nothing more, and a dot folder is not read. The error names the first file and where it goes. nil
// when there is none.
func oldFolders(appRoot, identity string) error {
	for _, old := range []struct{ folder, why string }{
		{".shared", "code của app nằm trong thư mục app tự đặt tên, như core/, mỗi file tự khai ở cấp file (bảng, job, lịch, component, luật chung của hàng đợi và lịch); giao diện dùng lại <tên>.ui của component đặt cạnh file khai nó"},
		{".schedule", "lịch tự khai ở cấp file của một file .work thường, như core/; luật chung của lịch (schedule.keep, schedule.database) viết một lần ở một file như vậy"},
	} {
		folder := filepath.Join(appRoot, old.folder)
		if info, err := os.Stat(folder); err != nil || !info.IsDir() {
			continue
		}
		first := ""
		filepath.WalkDir(folder, func(file string, entry fs.DirEntry, err error) error {
			if err != nil || first != "" {
				return filepath.SkipDir
			}
			relative, _ := filepath.Rel(appRoot, file)
			relative = filepath.ToSlash(relative)
			if !entry.IsDir() {
				first = relative
			}
			return nil
		})
		if first == "" {
			continue
		}
		moved := "core/" + path.Base(first)
		if path.Base(first) == "config.work" {
			moved = "core/" + strings.TrimPrefix(path.Dir(first), ".") + ".work" // a file of the folder's rules, core/schedule.work
			moved = strings.ReplaceAll(moved, "/shared/", "/")
		}
		return fmt.Errorf("%s/%s: %s/ đã bỏ (17/11/2026): %s; dời file này, chẳng hạn %s, import bằng %q (engine2/docs/module.md mục 4)",
			identity, first, old.folder, old.why, moved, "app/"+moved)
	}
	return nil
}

// rulesInConfig is the error of a config.work of the app at appRoot that writes a rule of the queue or the schedules
// (writesRules): the rules were written in .shared/queue/config.work and .schedule/config.work until 17/11/2026, and a
// config.work has a role, a folder's rules, read only where a site's folder is: core/queue/config.work, the folder
// renamed, would be read by no one and its rule lost unsaid. The error names the file and where the rule goes; nil when
// no config.work writes one.
func rulesInConfig(appRoot, identity string) error {
	found := ""
	filepath.WalkDir(appRoot, func(file string, entry fs.DirEntry, err error) error {
		switch {
		case err != nil || found != "":
			return nil
		case entry.IsDir():
			if file != appRoot && (hidden(entry.Name()) || entry.Type()&fs.ModeSymlink != 0) {
				return filepath.SkipDir
			}
		case entry.Name() == "config.work" && writesRules(file):
			relative, _ := filepath.Rel(appRoot, file)
			found = filepath.ToSlash(relative)
		}
		return nil
	})
	if found == "" {
		return nil
	}
	return fmt.Errorf("%s/%s: luật chung của hàng đợi và lịch (queue.parallel, keep, database; schedule.keep, database, session) viết ở cấp file của một file .work thường, không trong config.work, file có vai: dời sang, chẳng hạn, core/queue.work (engine2/docs/module.md mục 4)", identity, found)
}

// zoneOf is the zone of an app whose module.work declares declared, asked up the tiers, the nearest winning (slice 3 of
// root docs/KITWORK_MODULE_WORK.md mục 6, 17/11/2026): its module.timezone(…), else the host's app.timezone(…), else
// UTC; and where it comes from, as kitwork check's map says it.
func (h *Host) zoneOf(declared *module.Declared) (*time.Location, string) {
	switch {
	case declared != nil && declared.Timezone != nil:
		return declared.Timezone, declared.TimezoneAt
	case h.Manifest != nil && h.Manifest.Timezone != nil:
		return h.Manifest.Timezone, h.Manifest.TimezoneAt + " (app.timezone, của host)"
	}
	return time.UTC, "(không tầng nào khai)"
}

// noName is the line of an app the host gives no hostname: it boots, and serves nothing.
func noName(routed bool) string {
	if routed {
		return "no hostname is the app's: no folder at its root has a local name (shop.test, shop.localhost), and the host's table of domains gives it none"
	}
	return "no hostname is the app's: its module.work writes no module.routes, so its one site is its root, served by the names the host's table of domains gives it, and it gives none"
}

// saidFolders says each folder at the root of the app identity, one of module.routes, that is named after a public
// hostname the table gives the app not, as the boot said of a site's folder before module.work: the name is not the
// app's, so no request reaches the folder by it.
func (h *Host) saidFolders(appRoot, identity string) {
	entries, err := os.ReadDir(appRoot)
	if err != nil {
		return
	}
	table := h.domains.Load()
	for _, entry := range entries {
		name := strings.ToLower(entry.Name())
		if !entry.IsDir() || hidden(entry.Name()) || !strings.Contains(name, ".") || hostnameProblem(name) != "" || localName(name) {
			continue
		}
		if why := h.refuses(table, identity, name); why != "" {
			h.log.Line("[" + identity + "] the site " + name + " is not opened: " + why)
		}
	}
}

// siteNamesBy are the hostnames the host gives the app identity at appRoot, by table, sorted: each the table gives it
// (its row not deleted, confirmed, sending it nowhere else), and, when its module.work writes module.routes, each folder
// at its root with a local name (shop.test, shop.localhost), which no one owns and needs no row. A name that is no
// hostname is none: it would lead the lines of the site in the host's log (hostnameProblem).
func (h *Host) siteNamesBy(table *domainTable, identity, appRoot string) []string {
	if h.oneApp {
		return h.oneAppNames(appRoot) // the only app: every name is its own, no table (alone.go)
	}
	var names []string
	if table != nil {
		for name := range table.rows {
			if hostnameProblem(name) == "" && table.refuses(identity, name) == "" {
				names = append(names, name)
			}
		}
	}
	if routed(appRoot) {
		if entries, err := os.ReadDir(appRoot); err == nil {
			for _, entry := range entries {
				name := strings.ToLower(entry.Name())
				if entry.IsDir() && !hidden(entry.Name()) && hostnameProblem(name) == "" && localName(name) && !slices.Contains(names, name) {
					names = append(names, name)
				}
			}
		}
	}
	slices.Sort(names)
	return names
}

// routed says the module.work of the app at appRoot writes module.routes, read from its words without running it: the
// word stands at the head of a statement of the file's top level (routerWords), so the words say what a run would.
func routed(appRoot string) bool {
	source, err := os.ReadFile(filepath.Join(appRoot, "module.work"))
	if err != nil {
		return false
	}
	parser := compiler.NewParser(compiler.NewLexerSource(string(source), "module.work"))
	program := parser.ParseProgram()
	if len(parser.Errors()) > 0 {
		return false
	}
	for _, statement := range program.Statements {
		expression, ok := statement.(*compiler.ExpressionStatement)
		if !ok {
			continue
		}
		if links, chain := moduleChain(expression.Expression, "module"); chain && links[len(links)-1].Method.Value == "routes" {
			return true
		}
	}
	return false
}
