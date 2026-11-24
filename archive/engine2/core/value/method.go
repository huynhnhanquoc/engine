package value

import (
	"context"
	"reflect"
	"sort"
	"sync"
	"unicode"
)

// What a program reaches on a value is every exported method with one of two signatures, on Value itself
// and on a module's struct. On a struct the signature says which it is (04/11/2026): a method of no
// parameter, func (*T) X() Value, is a PROPERTY, which a read calls (request.url); a method of the
// signature (...Value) Value is a METHOD, which a call calls (napas.bank("vcb", "1")) and a read gives as a
// function bound to the struct, so `const bank = napas.bank` runs nothing until bank(…) does. Until then a
// read called any (...Value) Value method with no argument. Expose lists them once, at boot, with
// reflect; after that a call is one map lookup and a direct Go call (the lookup key is the receiver's
// type, read by reflect.TypeOf, a pointer read, not a reflection). A struct whose type has the Go method
// MissingMember(name string) Value answers the other names too (missingMember). This file is the only one
// that uses reflect.

// Methods is what a program reaches on one Go type: its methods and its properties, by the name the
// program writes. A table is built once per type and shared by every value of that type.
type Methods struct {
	name       string
	methods    map[string]hostMethod
	properties map[string]func(receiver any) Value
	// requestBound says the type belongs to one request (RequestBound): a method read off it is too
	requestBound bool
}

// hostMethod is a struct's method: call, and for a method that makes much from little (its Go signature takes a
// Budget first: Response.Json, a JSON column written), within, which a run calls with its budget (InvokeWithin).
type hostMethod struct {
	call   func(receiver any, arguments []Value) Value
	within func(receiver any, budget Budget, arguments []Value) Value
}

// Names lists, in order, every name a program reaches on the type, its methods and its properties;
// Properties says which of them are properties. Documentation, tests and the compiler read them.
func (t *Methods) Names() []string {
	names := make([]string, 0, len(t.methods)+len(t.properties))
	for name := range t.methods {
		names = append(names, name)
	}
	for name := range t.properties {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Properties lists, in order, the names that are properties: read without a call, never called.
func (t *Methods) Properties() []string {
	names := make([]string, 0, len(t.properties))
	for name := range t.properties {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// missingMember is a struct that answers the names its type has no method or property of: the manifest's
// app, where an unknown app.x() prints one line at boot and the chain goes on (CLAUDE.md), and a .env
// read by key (env.PORT, any key, null when it has none). Its one method has neither of the two
// signatures, so it is Go's: a program never calls it by name. A type without it reads undefined for an
// unknown name and fails a call of one, as every struct did before (04/11/2026).
type missingMember interface {
	MissingMember(name string) Value
}

// internal is a struct that answers the methods the compiler writes, each a name with # in it a source never
// spells: a var of a view's function is context["var#"](…) (core/compiler, var.go). It is asked for every name its
// type has no method or property of, and answers the others as a type with none does. Its one method is Go's, as
// MissingMember is, so a program never calls it by name.
type internal interface {
	Internal(name string, arguments []Value) Value
}

// Invoke calls name on receiver, a *T of the table's T. A property called is what JavaScript makes of a
// getter called: the value it gives is called when it is a function, and else the call is a TYPE
// failure, "url is not a function". A name the type does not have is the receiver's MissingMember,
// called the same way, when the type has it.
func (t *Methods) Invoke(receiver any, name string, arguments []Value) Value {
	return t.InvokeWithin(receiver, nil, name, arguments)
}

// InvokeWithin is Invoke by a run whose budget is budget: a method that makes much from little is given it, and
// asks it as what it makes grows (Budget); any other is called as Invoke calls it.
func (t *Methods) InvokeWithin(receiver any, budget Budget, name string, arguments []Value) Value {
	if f, ok := t.methods[name]; ok {
		if budget == nil {
			return f.call(receiver, arguments)
		}
		var result Value
		if f.within != nil {
			result = f.within(receiver, budget, arguments)
		} else {
			result = f.call(receiver, arguments)
		}
		// the texts a module's method is handed, which it reads, cost the run's energy as a function's do (CallWithin)
		if spent := Spend(budget, HandedText(arguments)); spent.kind == invalid {
			return spent
		}
		return result
	}
	if read, ok := t.properties[name]; ok {
		return callRead(read(receiver), name, arguments)
	}
	if hook, ok := receiver.(internal); ok {
		return hook.Internal(name, arguments)
	}
	if missing, ok := receiver.(missingMember); ok {
		return callRead(missing.MissingMember(name), name, arguments)
	}
	return Fail(TypeCode, t.name+" has no method "+name)
}

// callRead calls what a read gave, as JavaScript calls what a getter returns: a function is called, a
// failure is itself, anything else is not a function.
func callRead(got Value, name string, arguments []Value) Value {
	switch got.kind {
	case function:
		return got.Call(arguments)
	case invalid:
		return got
	}
	return Fail(TypeCode, name+" is not a function")
}

// property is receiver.key read without a call: a property's value, a method as a function bound to
// receiver, the receiver's MissingMember for a name the type has neither of when the type has it, and
// else undefined. Two reads of one method of one type are one function to ===, as a class's method read
// off two instances is in JavaScript (owner, Equal).
func (t *Methods) property(receiver any, key string) Value {
	if read, ok := t.properties[key]; ok {
		return read(receiver)
	}
	method, ok := t.methods[key]
	if !ok {
		if missing, ok := receiver.(missingMember); ok {
			return missing.MissingMember(key)
		}
		return Value{}
	}
	bound := &callable{
		native:    func(arguments ...Value) Value { return method.call(receiver, arguments) },
		name:      key,
		prototype: host,
		owner:     t,
	}
	if method.within != nil {
		// read now and called later, it is still given the budget of the run that calls it (CallWithin)
		bound.within = func(budget Budget, arguments ...Value) Value { return method.within(receiver, budget, arguments) }
	}
	return Value{kind: function, data: bound}
}

// Expose reads T once: every exported method of *T (which includes T's own) with the signature
// (...Value) Value joins the table as a method, as does one with (Budget, ...Value) Value, which a run
// calls with its budget (InvokeWithin), and every one with the signature () Value as a property, each
// under its name with the first letter lowered (Bank → bank, ReceiverName → receiverName). A method with
// another signature is Go's and stays out.
func Expose[T any]() *Methods {
	pointer := reflect.TypeOf((*T)(nil))
	table := &Methods{
		name:         lowerFirst(pointer.Elem().Name()),
		methods:      map[string]hostMethod{},
		properties:   map[string]func(any) Value{},
		requestBound: pointer.Implements(reflect.TypeOf((*RequestBound)(nil)).Elem()),
	}
	for i := 0; i < pointer.NumMethod(); i++ {
		m := pointer.Method(i)
		if !m.Func.IsValid() {
			continue // an interface's method has no function to call
		}
		switch f := m.Func.Interface().(type) {
		case func(*T, ...Value) Value:
			table.methods[lowerFirst(m.Name)] = hostMethod{call: func(receiver any, arguments []Value) Value {
				t, ok := receiver.(*T)
				if !ok {
					return Fail(TypeCode, "receiver is not a "+pointer.Elem().Name())
				}
				return f(t, arguments...)
			}}
		case func(*T, Budget, ...Value) Value:
			// a method that makes much from little: given the budget of the run that calls it, nil when none does
			within := func(receiver any, budget Budget, arguments []Value) Value {
				t, ok := receiver.(*T)
				if !ok {
					return Fail(TypeCode, "receiver is not a "+pointer.Elem().Name())
				}
				return f(t, budget, arguments...)
			}
			table.methods[lowerFirst(m.Name)] = hostMethod{
				call:   func(receiver any, arguments []Value) Value { return within(receiver, nil, arguments) },
				within: within,
			}
		case func(*T) Value:
			table.properties[lowerFirst(m.Name)] = func(receiver any) Value {
				t, ok := receiver.(*T)
				if !ok {
					return Fail(TypeCode, "receiver is not a "+pointer.Elem().Name())
				}
				return f(t)
			}
		}
	}
	return table
}

// RequestMethod is, for a method read off a struct of one request without a call (const query =
// context.query), its name: the function belongs to the request as its struct does (RequestBound).
func (v Value) RequestMethod() (string, bool) {
	if f, ok := v.data.(*callable); ok && v.kind == function && f.owner != nil && f.owner.requestBound {
		return f.name, true
	}
	return "", false
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToLower(r[0])
	return string(r)
}

// hosts is the methods of every struct type a program has met, by the pointer type a Struct holds:
// Struct[T] exposes a type the first time it is built, so no module registers anything at boot.
var hosts sync.Map

// exposed makes sure T's methods are in hosts and returns them; after the first call per type it
// is one Load.
func exposed[T any]() *Methods {
	key := reflect.TypeOf((*T)(nil))
	if methods, ok := hosts.Load(key); ok {
		return methods.(*Methods)
	}
	methods, _ := hosts.LoadOrStore(key, Expose[T]())
	return methods.(*Methods)
}

func invokeHost(receiver any, budget Budget, name string, arguments []Value) Value {
	methods, ok := hosts.Load(reflect.TypeOf(receiver))
	if !ok {
		return Fail(TypeCode, "host type was never built with Struct: "+reflect.TypeOf(receiver).String())
	}
	return methods.(*Methods).InvokeWithin(receiver, budget, name, arguments)
}

// hostName is a struct's word for a person, in a failure text and in Kind: its type's name as the program
// reaches it ("napas", "request", "router"). Until 06/11/2026 every struct was "struct" there ("cannot set x on
// struct"), a word the program never wrote for a VietQR or a request, and since that day the word of
// kitwork/schema, whose struct is then the one value that says it.
func hostName(data any) string {
	if methods, ok := hosts.Load(reflect.TypeOf(data)); ok {
		return methods.(*Methods).name
	}
	return nameHost
}

// Keeper is a struct of a module that keeps functions the program gave it and calls them in its own methods: a
// schema's transform, written as the site booted and run by struct.parse(…) in a request (docs/schema.md). A
// function of a frozen file runs in a request only when it is called or handed to a method (runtime/freeze.go),
// and a kept function is neither: so the VM hands the struct over for each call of one of its methods
// (HandedTo), and the call runs on what HandOver returns, a copy whose kept functions went through hand, which
// makes one another VM made a stand-in running on the calling VM. The struct itself stays as it was, read by
// many runs at once (06/11/2026).
type Keeper interface {
	HandOver(hand func(Value) Value) Value
}

// Keeps reports a struct that keeps functions (Keeper), which the VM hands over before calling its method.
func (v Value) Keeps() bool {
	if v.kind != host {
		return false
	}
	_, ok := v.data.(Keeper)
	return ok
}

// HandedTo is v for one call of its method by a run whose hand is hand: a Keeper's HandOver, v itself for any
// other value.
func (v Value) HandedTo(hand func(Value) Value) Value {
	if keeper, ok := v.data.(Keeper); ok && v.kind == host {
		return keeper.HandOver(hand)
	}
	return v
}

// Attender is a struct of a module whose methods note what the run calling them carries in its context: a handle of
// kitwork/storage notes each key a page's run reads, for the tab that shows the page to be told when one changes
// (engine2/docs/view.md mục 3, slice 4). The VM gives it its context for each call of one of its methods
// (AttendedBy), and the call runs on what Attend returns; the struct itself stays as it was, read by many runs at
// once, as a Keeper's does.
type Attender interface {
	Attend(context context.Context) Value
}

// AttendedBy is v for one call of its method by a run whose context is context: an Attender's Attend, v itself for any
// other value and for a run with no context.
func (v Value) AttendedBy(context context.Context) Value {
	if attender, ok := v.data.(Attender); ok && v.kind == host && context != nil {
		return attender.Attend(context)
	}
	return v
}

// propertyOfHost is `v.key` on a struct (Methods.property): request.url, a method of no parameter,
// read without (); napas.bank, a method, as a function bound to the struct; undefined for any other name.
func propertyOfHost(receiver any, key string) Value {
	methods, ok := hosts.Load(reflect.TypeOf(receiver))
	if !ok {
		return Value{}
	}
	return methods.(*Methods).property(receiver, key)
}

// valueMethods is Value's own table: the (...Value) Value methods of javascript.go, stored as the
// variadic functions they are, so a call is one direct call with the receiver as a Value, not boxed.
var valueMethods = exposeValue()

func exposeValue() map[string]valueMethod {
	t := reflect.TypeOf(Value{})
	methods := map[string]valueMethod{}
	for i := 0; i < t.NumMethod(); i++ {
		m := t.Method(i)
		if f, ok := m.Func.Interface().(func(Value, ...Value) Value); ok {
			name := lowerFirst(m.Name)
			ofText, ofArray := workOf(name)
			methods[name] = valueMethod{call: f, hints: hintsOf(name), text: ofText, array: ofArray}
		}
	}
	return methods
}

// ValueMethods lists what a program can call on a value of any kind; a method answers for the kinds it is
// written for and fails with TYPE on the others.
func ValueMethods() []string {
	names := make([]string, 0, len(valueMethods))
	for name := range valueMethods {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
