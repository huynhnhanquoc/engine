package application

import (
	"slices"
	"strconv"
	"strings"

	"github.com/kitwork/engine2/core/value"
)

// Command is one command of app.commands({ name: function }): what `kitwork <name>` runs, in a process of its own
// with the host's rights (docs/KITWORK_COMMANDS.md). Function is the manifest's, written in it or in a file it
// imports; reading the manifest keeps it and runs nothing.
type Command struct {
	Function *value.Lambda
}

// EngineCommands are the words of the kitwork command itself (the root's main.go): no command of the manifest takes
// one, so `kitwork check` is always the engine's (docs/KITWORK_COMMANDS.md mục 2). The root's tests hold main.go to
// this list.
var EngineCommands = []string{"run", "dev", "check", "migrate", "database", "version", "help", "new", "create"}

// commandsHow is how app.commands(…) is written.
const commandsHow = `app.commands takes one object of commands, each a function: app.commands({ backup, hello: (context) => context.print("chào") })`

// Commands is app.commands({ backup, hello: (context) => … }): the host's commands, once (docs/KITWORK_COMMANDS.md
// mục 2). A name is lowercase letters, digits and hyphens from a letter, typed after kitwork, and no word of kitwork
// itself; each is a function of the manifest. The manifest declares: no command runs as it is read.
func (a *Application) Commands(arguments ...value.Value) value.Value {
	if a.manifest.Run {
		return a.afterRun("commands(…)")
	}
	if a.manifest.Commands != nil {
		return value.Fail(value.InvalidArgumentCode, "app.commands(…) is declared twice: one manifest, one list of commands")
	}
	declared := argument(arguments, 0)
	if len(arguments) != 1 || !declared.IsObject() || declared.IsArray() {
		return value.Fail(value.InvalidArgumentCode, commandsHow)
	}
	names := declared.Keys()
	if len(names) == 0 {
		return value.Fail(value.InvalidArgumentCode, "app.commands({}) declares no command; "+commandsHow)
	}
	commands := make(map[string]*Command, len(names))
	for _, name := range names {
		switch {
		case !commandName(name):
			return value.Fail(value.InvalidArgumentCode, "app.commands: "+strconv.Quote(name)+" is no command name: lowercase letters, digits and hyphens, from a letter (backup, clean-files)")
		case slices.Contains(EngineCommands, name):
			return value.Fail(value.InvalidArgumentCode, "app.commands: "+name+" is a command of kitwork itself ("+strings.Join(EngineCommands, ", ")+"); name it otherwise")
		}
		function := declared.Get(name).Lambda()
		if function == nil {
			return value.Fail(value.InvalidArgumentCode, "app.commands: "+name+" is no function: a command is (context) => …, written here or imported")
		}
		commands[name] = &Command{Function: function}
	}
	a.manifest.Commands = commands
	return value.Struct(a)
}

// commandName says name is lowercase letters, digits and hyphens, from a letter, ending in none: what a shell passes
// as it is, on every system.
func commandName(name string) bool {
	if name == "" || len(name) > 64 || name[0] < 'a' || name[0] > 'z' || strings.HasSuffix(name, "-") {
		return false
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}
