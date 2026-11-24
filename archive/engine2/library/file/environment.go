package file

import (
	"os"
	"strconv"

	"github.com/kitwork/engine2/core/value"
)

// Environment is what file.env(…) gives: read by name only (docs/KITWORK_ENVIRONMENT.md mục 3).
// env.PORT is the key's value coerced, null when no file sets it or sets it empty; env.require("KEY")
// is the value, and a key it misses is one boot error that lists every missing key. A struct and not an
// object, so a program can only name keys, never list them: no Object.keys(env), no for…of. A key named
// require is read with env.require("require").
type Environment struct {
	values map[string]entry
	// system: the manifest's, where the operating system's variable of a key wins over every file.
	system bool
	// require records a missing key at boot; false once the boot is over (File.Require).
	require func(key string) bool
}

// MissingMember is env.KEY: every name the struct has no method of is a key.
func (e *Environment) MissingMember(name string) value.Value {
	item, ok := e.lookup(name)
	if !ok {
		return value.Null()
	}
	return item.coerced()
}

// coerced is the entry's value: the one its file's read made, or a variable of the system's coerced now.
func (item entry) coerced() value.Value {
	if item.value.IsUndefined() {
		return coerce(item)
	}
	return item.value
}

func (e *Environment) lookup(name string) (entry, bool) {
	if e.system {
		if text, ok := os.LookupEnv(name); ok && text != "" {
			return entry{text: text}, true // a variable of the system is never quoted
		}
	}
	item, ok := e.values[name]
	return item, ok
}

// Require is env.require("KEY"): the key's value. A key no file sets is recorded while the boot runs
// (null here, the boot fails at its end naming every such key) and a NOT_FOUND failure after it.
func (e *Environment) Require(arguments ...value.Value) value.Value {
	if len(arguments) == 0 || !arguments[0].IsString() {
		return value.Fail(value.InvalidArgumentCode, "env.require(…) takes the key's name written as text: env.require(\"STRIPE_SECRET\")")
	}
	name := arguments[0].Text()
	if item, ok := e.lookup(name); ok {
		return item.coerced()
	}
	if e.require != nil && e.require(name) {
		return value.Null()
	}
	return value.Fail(value.NotFoundCode, "env.require("+strconv.Quote(name)+"): no file sets the key")
}
