package host

import (
	"errors"
	"net/http"
	"path"
	"path/filepath"

	"github.com/kitwork/engine2/core/runtime"
	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/router"
	"github.com/kitwork/engine2/library/ui"
)

// pageComponents runs the site's components a page draws (library/ui, sitecomponent.go): each tag's function with the
// inputs written on it, a request's energy each run, on a VM of the site taken at the first run and given back once
// the page is drawn. A call of a work of one of them (bridge.go) names its instance: that one runs with the vars
// the browser held, and its context is kept for the work.
type pageComponents struct {
	site *Site
	r    *http.Request
	work *worker
	// call is the instance a call of a work names, nil when the page is drawn for a visitor
	call *instanceCall
	// draw is the page drawn for a tab, which notes each instance's names and the keys the runs read; vars are the
	// vars each instance held, by place, when the tab asks for its page again (tab.go)
	draw *pageDraw
	vars map[string]value.Value
}

// instanceCall is the instance a call names, and what its run left once the page was drawn: its context, the
// names it bound, whether the page drew it.
type instanceCall struct {
	tag, place string
	vars       value.Value
	context    *router.Context
	names      value.Value
	found      bool
	// failure is how its run failed, which the call answers as its own (a var of another kind is the call's 400)
	failure value.Value
}

// pageComponents are the components of r's page; nil when the site and its app define none.
func (s *Site) pageComponents(r *http.Request) *pageComponents {
	if s.components == nil || s.components.Empty() {
		return nil
	}
	return &pageComponents{site: s, r: r}
}

func (p *pageComponents) Defines(tag string) bool { return p.site.components.Lookup(tag) != nil }

// Interface is the file of the interface of the component tag beside the file that declares it, <tag>.ui in its folder
// (slice 7 of root docs/KITWORK_MODULE_WORK.md, 17/11/2026), there or not; "" when the app declares no such component.
func (p *pageComponents) Interface(tag string) string {
	definition := p.site.components.Lookup(tag)
	if definition == nil || definition.File == "" {
		return ""
	}
	return filepath.Join(p.site.AppRoot, filepath.FromSlash(path.Dir(definition.File)), tag+".ui")
}

// Run runs the function of the component tag with inputs for its instance at place, and gives the names its
// context.bind({ … }) binds, with those of the sync before it, and its works and vars when it has either.
func (p *pageComponents) Run(tag string, inputs value.Value, place string) (value.Value, *ui.Bridge, error) {
	definition := p.site.components.Lookup(tag)
	context := router.NewComponentContext(p.r, inputs)
	called := p.call != nil && p.call.tag == tag && p.call.place == place
	if called {
		context.Calling(p.call.vars)
	} else if vars, ok := p.vars[place]; ok {
		context.Calling(vars)
	}
	result, ok := p.run(definition.Function, []value.Value{value.Struct(context)})
	if !ok {
		return value.Value{}, nil, errors.New("the function of component.define(\"" + tag + "\", …) has no program")
	}
	if result.IsInvalid() {
		if called {
			p.call.failure = result
		}
		return value.Value{}, nil, errors.New(result.Failure())
	}
	response, answered := context.Answer(result)
	if !answered || response.Word() != "bind" {
		return value.Value{}, nil, errors.New("component.define(\"" + tag + "\", …) at " + definition.At + " returned " + describe(result) +
			": a component answers with return context.bind({ … }), or context.sync({ … }).bind({ … }) (docs/component.md mục 3.2)")
	}
	if extra := response.Extra(); extra != "" {
		return value.Value{}, nil, errors.New("component.define(\"" + tag + "\", …) at " + definition.At + " wrote context." + extra +
			"(…): a component answers with the names it binds; a status, a header, a cookie, a title are the page's")
	}
	names, _, _, _ := response.Bound()
	if called {
		p.call.context, p.call.names, p.call.found = context, names, true
	}
	bridge := bridgeOf(names, context.Held(), response.Live())
	if bridge != nil && p.draw != nil {
		p.draw.note(place, names, bridge.Live)
	}
	return names, bridge, nil
}

// run runs fn with arguments on the VM of the page's components, taken at the first run, with a request's energy.
func (p *pageComponents) run(fn *value.Lambda, arguments []value.Value) (value.Value, bool) {
	program, ok := runtime.ProgramFromRef(fn.Program)
	if !ok {
		return value.Value{}, false
	}
	if p.work == nil {
		p.work, _ = p.site.machines.Get().(*worker)
		if p.work == nil {
			p.work = &worker{machine: runtime.New(program)}
		}
		p.work.deadline.start(p.r.Context(), defaultRequestTimeout)
		if p.draw != nil {
			p.work.deadline.reads = p.draw.reads // what the instances read is the tab's too
		}
	}
	machine := p.work.machine
	machine.FastReset(program, p.site.globals)
	machine.Builtins = p.site.builtins
	machine.MaxEnergy = p.site.energy
	machine.MaxBytes = p.site.made
	machine.Pool = p.site.pool
	machine.Context = &p.work.deadline
	return machine.ExecuteLambda(fn, arguments), true
}

// release gives the VM back, the page drawn.
func (p *pageComponents) release() {
	if p.work == nil {
		return
	}
	p.work.machine.ResetForPool()
	p.work.deadline.stop()
	p.site.machines.Put(p.work)
	p.work = nil
}
