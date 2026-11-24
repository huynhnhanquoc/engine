package component

import (
	"strconv"

	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/module"
)

// The server's half of an app's components (docs/component.md): component.define("shop-cart", fn) at the top level of
// any plain .work file of the app declares the logic of the tag <shop-cart>, a function of context returning
// context.bind({ … }) or context.sync({ … }).bind({ … }), for every site of the app (17/11/2026, slice 7 of root
// docs/KITWORK_MODULE_WORK.md; until then in components.work at a site's root or in .shared/components/). The page that
// draws the tag runs the function and draws the tag's interface with the names it returns (library/ui).

// Definition is one component.define: the tag's name, its function, where it is written (file:line:column from the
// app's root) and the file that writes it, "/"-separated from the app's root, beside which its interface <name>.ui is.
type Definition struct {
	Name     string
	Function *value.Lambda
	At       string
	File     string
}

// Definitions are the components of an app, in the order the boot declared them, one a name.
type Definitions struct {
	byName map[string]*Definition
	list   []*Definition
}

// NewDefinitions are an app's components, none yet.
func NewDefinitions() *Definitions {
	return &Definitions{byName: map[string]*Definition{}}
}

// Lookup is the component named name; nil when none is defined.
func (d *Definitions) Lookup(name string) *Definition {
	if d == nil {
		return nil
	}
	return d.byName[name]
}

// Empty reports that the app declares no component.
func (d *Definitions) Empty() bool { return d == nil || len(d.list) == 0 }

// Len is how many components the app declares; Since those declared after the first n.
func (d *Definitions) Len() int { return len(d.list) }
func (d *Definitions) Since(n int) []*Definition {
	return d.list[n:]
}

// List is every component, in the order declared.
func (d *Definitions) List() []*Definition {
	if d == nil {
		return nil
	}
	return d.list
}

// Module is "kitwork/component" as one file sees it.
type Module struct {
	// App is the app's components: the host's pass that loads the app's work declares into it (Loading); any other
	// run of a file that declares one, a page's that imports it, finds its own there by its place
	App     *Definitions
	Loading bool
	// Declaring is the pass that declares the app's tables, which passes over a component
	Declaring bool
	// Where is the place of the call in progress, Written the file whose top level writes it; Open that the file runs
	// as the app boots
	Where   func() string
	Written func() string
	Open    func() bool
}

// Freeze: a module is never written once made.
func (m *Module) Freeze() {}

// MissingMember is a word kitwork/component does not have.
func (m *Module) MissingMember(name string) value.Value {
	return value.Fail(value.TypeCode, "kitwork/component has no "+name+": component.define(\"shop-cart\", (context) => …) declares a component, component.alias() reads the page's aliases (docs/component.md)")
}

// Define is component.define(name, fn): the logic of the tag <name>, a function of context answering with
// context.bind({ … }) (docs/component.md mục 2, 3), declared at the top level of a plain .work file of the app.
func (m *Module) Define(arguments ...value.Value) value.Value {
	written := ""
	if m.Written != nil {
		written = m.Written()
	}
	if m.Open == nil || !m.Open() || module.RoleFile(written) {
		return value.Fail(value.InvalidArgumentCode, "component.define(…) khai ở cấp file của một file .work thường của app (không phải router.work, config.work, design.work, module.work), lúc app boot: component.define(\"shop-cart\", (context) => …) trong core/components.work, giao diện dùng lại core/shop-cart.ui cạnh nó (docs/component.md mục 2)")
	}
	name := argument(arguments, 0)
	if !name.IsString() {
		return value.Fail(value.InvalidArgumentCode, "component.define(name, fn) takes the tag's name first, a string: component.define(\"shop-cart\", (context) => …)")
	}
	if problem := tagProblem(name.Text()); problem != "" {
		return value.Fail(value.InvalidArgumentCode, "component.define("+strconv.Quote(name.Text())+", …): "+problem)
	}
	function := argument(arguments, 1).Lambda()
	if len(arguments) != 2 || function == nil {
		return value.Fail(value.InvalidArgumentCode, "component.define("+strconv.Quote(name.Text())+", fn) takes the component's function, (context) => { …; return context.bind({ … }); }")
	}
	at := ""
	if m.Where != nil {
		at = m.Where()
	}
	first := m.App.Lookup(name.Text())
	switch {
	case m.Declaring:
		return value.Undefined() // the pass that declares the tables: the components are the next pass's
	case first != nil && first.At == at:
		return value.Undefined() // the same declaration, run again by a file that imports its file
	case first != nil:
		return value.Fail(value.InvalidArgumentCode, "component.define("+strconv.Quote(name.Text())+", …) at "+at+" again: it is defined at "+first.At+"; one definition a name in the app")
	case !m.Loading || m.App == nil:
		return value.Fail(value.InvalidArgumentCode, "component.define("+strconv.Quote(name.Text())+", …) at "+at+": the boot declares the components of the plain .work files it reads, and not of this one, in a folder of _ or .: move it to a folder it reads, core/")
	}
	definition := &Definition{Name: name.Text(), Function: function, At: at, File: written}
	m.App.byName[name.Text()] = definition
	m.App.list = append(m.App.list, definition)
	return value.Undefined()
}

// Alias is component.alias(): the page's aliases run in the browser, which engine2 does not serve yet.
func (m *Module) Alias(arguments ...value.Value) value.Value {
	return value.Fail(value.UnsupportedCode, "component.alias() is not served by engine2 yet: the aliases of a page run in the browser")
}

// tagProblem is why name is no tag of a site's component, "" when it is one: lower-case words of letters and
// digits joined by single dashes, at least two, the first beginning with a letter. A name without a dash is
// Kitwork's own (dropdown, dialog, tabs).
func tagProblem(name string) string {
	if name == "" {
		return "the name is empty"
	}
	dashes := 0
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
			if i == 0 {
				return "a tag begins with a letter"
			}
		case c == '-':
			if i == 0 || i == len(name)-1 || name[i-1] == '-' {
				return "a tag is words joined by single dashes, \"shop-cart\""
			}
			dashes++
		default:
			return "a tag is lower-case letters, digits and dashes, \"shop-cart\""
		}
	}
	if dashes == 0 {
		return "a site's component has a dash in its name, \"" + name + "-…\": a name without one is Kitwork's own (dropdown, dialog, tabs)"
	}
	return ""
}

func argument(arguments []value.Value, i int) value.Value {
	if i < len(arguments) {
		return arguments[i]
	}
	return value.Undefined()
}
