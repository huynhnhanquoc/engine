// Package module is "kitwork/module": the words of module.work, the file whose folder is an app's root, as go.mod
// says where a Go module begins (root docs/KITWORK_MODULE_WORK.md, slice 1, 17/11/2026; docs/module.md).
//
//	import * as module from "kitwork/module";
//
//	module.timezone("Asia/Ho_Chi_Minh");
//	module.routes((site) => site.domain);
//
// module.work runs once as the app boots, before any other file of it, and declares into Declared, which the host
// reads: the app's zone, and the function that gives each of the app's hostnames its folder. Each word is written
// once. A word module.work does not have is the boot's error at its line, saying the nearest one.
package module

import (
	"path"
	"strconv"
	"time"

	"github.com/kitwork/engine2/core/value"
)

// Declared is what module.work declares: the app's zone, nil for none (UTC), and the function of module.routes,
// nil when the app's pages are its root's. At is where each is written, for the error of a second one and for the
// errors of the function.
type Declared struct {
	Timezone   *time.Location
	TimezoneAt string
	Routes     *value.Lambda
	RoutesAt   string
}

// Module is "kitwork/module" as module.work sees it.
type Module struct {
	Declared *Declared
	// Where is the place of the call in progress, "shop/module.work:3:1".
	Where func() string
	// Open says module.work is running at boot: a function it kept (module.routes's) that calls a word later is
	// refused, the app's root being read once.
	Open func() bool
}

func argument(arguments []value.Value, i int) value.Value {
	if i < len(arguments) {
		return arguments[i]
	}
	return value.Undefined()
}

func (m *Module) unready(word string) (value.Value, bool) {
	if m.Declared == nil || m.Open == nil || !m.Open() {
		return value.Fail(value.UnsupportedCode, "module."+word+"(…) declares at the top level of module.work, read once as the app boots"), true
	}
	return value.Value{}, false
}

func (m *Module) where() string {
	if m.Where == nil {
		return "module.work"
	}
	return m.Where()
}

// Timezone is module.timezone(name): the app's zone, an IANA zone name, its clock (Date's parts, a route's cache, a
// schedule that writes no .timezone(…)). None declared is UTC. It was router.timezone(…) in the config.work of the
// app root until 17/11/2026.
func (m *Module) Timezone(arguments ...value.Value) value.Value {
	if failure, ok := m.unready("timezone"); ok {
		return failure
	}
	if m.Declared.Timezone != nil {
		return value.Fail(value.InvalidArgumentCode, "module.timezone(…) is declared twice: the first is at "+m.Declared.TimezoneAt)
	}
	name := argument(arguments, 0)
	if len(arguments) != 1 || !name.IsString() || name.Text() == "" || name.Text() == "Local" {
		return value.Fail(value.InvalidArgumentCode, "module.timezone(…) takes an IANA zone name, \"Asia/Ho_Chi_Minh\"; the server's own clock (\"Local\") is no app's zone")
	}
	location, err := time.LoadLocation(name.Text())
	if err != nil {
		return value.Fail(value.InvalidArgumentCode, "module.timezone("+strconv.Quote(name.Text())+"): not an IANA zone name")
	}
	m.Declared.Timezone = location
	m.Declared.TimezoneAt = m.where()
	return value.Undefined()
}

// Routes is module.routes((site) => …): the folder of each hostname the host gives the app, for an owner of several
// sites (root docs/KITWORK_MODULE_WORK.md mục 4). The host calls the function once for each hostname as the app boots,
// with { domain }, and the site of that name is the folder it gives, from the app's root: module.routes((site) =>
// site.domain) gives each name the folder of its name. An app that writes none has one site, its root.
func (m *Module) Routes(arguments ...value.Value) value.Value {
	if failure, ok := m.unready("routes"); ok {
		return failure
	}
	if m.Declared.Routes != nil {
		return value.Fail(value.InvalidArgumentCode, "module.routes(…) is declared twice: the first is at "+m.Declared.RoutesAt)
	}
	function := argument(arguments, 0).Lambda()
	if len(arguments) != 1 || function == nil || len(function.Params) != 1 || function.Rest {
		return value.Fail(value.InvalidArgumentCode, "module.routes(…) takes a function of one site, which gives the folder of its domain: module.routes((site) => site.domain)")
	}
	m.Declared.Routes = function
	m.Declared.RoutesAt = m.where()
	return value.Undefined()
}

// RoleFile says written, a file of the app from its root ("shop.test/router.work"), has a role of its own: module.work,
// router.work, config.work or design.work. A table, a job, a schedule, the rules of the queue and the schedules (slice 2
// of root docs/KITWORK_MODULE_WORK.md, 17/11/2026) and a component (slice 7, the same day; components.work had a role
// until then) are declared at the top level of any other .work file of the app, which the boot reads.
func RoleFile(written string) bool {
	switch path.Base(written) {
	case "module.work", "router.work", "config.work", "design.work":
		return true
	}
	return false
}

// MissingMember is a word module.work does not have: the boot's error at its line, saying the nearest word.
func (m *Module) MissingMember(name string) value.Value {
	text := "module has no word " + name
	if nearest := value.NearestName(name, value.Expose[Module]().Names()); nearest != "" {
		text += ": did you mean module." + nearest + "?"
	}
	return value.Fail(value.TypeCode, text)
}
