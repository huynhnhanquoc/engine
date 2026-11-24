package host

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/kitwork/engine2/core/runtime"
	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/database"
)

// ErrCommandWords is a command given words it cannot read: an option twice, an option with no name. kitwork says it
// as a mistake of the line typed (exit 2), not a failure of the command.
var ErrCommandWords = errors.New("the words of the command")

// CommandNames are the names of app.commands, sorted: what kitwork help lists after its own words.
func (h *Host) CommandNames() []string {
	names := make([]string, 0, len(h.Manifest.Commands))
	for name := range h.Manifest.Commands {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// RunCommand runs the manifest's command name with the words typed after it: what `kitwork <name>` runs
// (docs/KITWORK_COMMANDS.md mục 3, 4), in the process that reads the manifest, no site booted, no app's code run. Its
// function is called with one context: arguments, the words that are no option, in order; options, each --key value,
// --key=value as a text and --key alone as true, "--" ending the options; print(…), a line to out. Its console writes
// to log. The command has the host's rights: no energy bound, which ctx is instead (Ctrl+C cancels it), and what one of
// its runs makes bounded as a site's (app.limits). A failure the command does not catch is the error.
func (h *Host) RunCommand(ctx context.Context, name string, words []string, out, log io.Writer) error {
	command := h.Manifest.Commands[name]
	if command == nil || command.Function == nil {
		return fmt.Errorf("the manifest declares no command %s", name)
	}
	arguments, options, err := commandWords(words)
	if err != nil {
		return err
	}
	program, ok := runtime.ProgramFromRef(command.Function.Program)
	if !ok {
		return fmt.Errorf("app.commands: %s has no program", name)
	}
	globals, slots := builtinsOf(newLog(log), "["+name+"] ", nil)
	machine := runtime.New(program)
	machine.FastReset(program, globals)
	machine.Builtins = withKitwork(slots, &Kitwork{folder: h.Folder, vm: machine, notice: func(string) {}})
	machine.MaxEnergy = 0
	machine.MaxBytes = h.runBytes()
	machine.Context = ctx
	result := machine.ExecuteLambda(command.Function, []value.Value{value.Struct(&commandContext{arguments: arguments, options: options, out: out, ctx: ctx, host: h})})
	if result.IsInvalid() {
		return errors.New(result.Failure())
	}
	return nil
}

// commandWords reads the words after a command's name: an argument each, but --key value, --key=value and --key (true,
// at the end or before another option), until "--", after which every word is an argument.
func commandWords(words []string) (value.Value, value.Value, error) {
	arguments := []value.Value{}
	options := value.Object()
	seen := map[string]bool{}
	ended := false
	for i := 0; i < len(words); i++ {
		word := words[i]
		switch {
		case ended || !strings.HasPrefix(word, "--"):
			arguments = append(arguments, value.String(word))
			continue
		case word == "--":
			ended = true
			continue
		}
		key, text, valued := strings.Cut(word[2:], "=")
		if key == "" {
			return value.Value{}, value.Value{}, fmt.Errorf("%w: %s names no option; an option is --key value, --key=value or --key", ErrCommandWords, word)
		}
		if seen[key] {
			return value.Value{}, value.Value{}, fmt.Errorf("%w: --%s is given twice", ErrCommandWords, key)
		}
		seen[key] = true
		switch {
		case valued:
			options.Set(key, value.String(text))
		case i+1 < len(words) && !strings.HasPrefix(words[i+1], "--"):
			options.Set(key, value.String(words[i+1]))
			i++
		default:
			options.Set(key, value.Boolean(true))
		}
	}
	return value.Array(arguments), options, nil
}

// commandContext is the context a command is called with (docs/KITWORK_COMMANDS.md mục 4): the words typed, a line
// out, the databases of the host.
type commandContext struct {
	arguments value.Value
	options   value.Value
	out       io.Writer
	ctx       context.Context
	host      *Host
}

// Arguments is context.arguments: the words after the command's name that are no option, texts in order.
func (c *commandContext) Arguments() value.Value { return c.arguments }

// Options is context.options: --to E:/x is "E:/x", --yes is true.
func (c *commandContext) Options() value.Value { return c.options }

// Print is context.print(…): its values as texts, a space between, one line to the command's output, as it is.
func (c *commandContext) Print(arguments ...value.Value) value.Value {
	parts := make([]string, len(arguments))
	for i, argument := range arguments {
		parts[i] = argument.ToString().Text()
	}
	io.WriteString(c.out, strings.Join(parts, " ")+"\n")
	return value.Undefined()
}

// Database is context.database(name): a database the host has, named as its database port names it, "shopping" a
// folder of the host's .database/ (app.database's, or .database/ beside the manifest), "shop.test/application" the
// folder application/ of the .database/ of the app whose site is shop.test; NOT_FOUND when there is none.
func (c *commandContext) Database(arguments ...value.Value) value.Value {
	if len(arguments) != 1 || !arguments[0].IsString() {
		return value.Fail(value.InvalidArgumentCode, `context.database takes a database's name: context.database("shopping"), context.database("shop.test/application")`)
	}
	folder := filepath.Join(c.host.Folder, ".database")
	if c.host.Manifest.Database != nil && c.host.Manifest.Database.Path != "" {
		folder = c.host.Manifest.Database.Path
	}
	target, err := c.host.resolveDatabase(folder, arguments[0].Text())
	if err != nil {
		return value.Fail(value.NotFoundCode, "context.database: "+err.Error())
	}
	return value.Struct(&commandDatabase{context: c, target: target})
}

// commandDatabase is one database a command named.
type commandDatabase struct {
	context *commandContext
	target  portTarget
}

// Name is the database's name, as the command named it, lower case.
func (d *commandDatabase) Name() value.Value { return value.String(d.target.name) }

// backupHow is how backup(…) is written.
const backupHow = `backup takes the folder the copies go to, and { keep } at will: context.database("shopping").backup("E:/kitwork-backups", { keep: 7 })`

// Backup is context.database(name).backup(to, { keep }) (docs/KITWORK_COMMANDS.md mục 6): a copy of each of the
// database's files into <to>/<name>/<moment>/, while the server that holds its writer goes on (library/database,
// BackupFolder); to is read from the manifest's folder. keep keeps that many copies of the database in to, the newest,
// removing the older ones BackupFolder made; none removed without it. With the database's files go the accounts of
// its cluster, .catalog/kitwork.db. It gives { folder, files: [{ name, bytes, revision }], bytes, seconds, removed }.
func (d *commandDatabase) Backup(arguments ...value.Value) value.Value {
	if len(arguments) == 0 || len(arguments) > 2 || !arguments[0].IsString() || strings.TrimSpace(arguments[0].Text()) == "" {
		return value.Fail(value.InvalidArgumentCode, backupHow)
	}
	keep := 0
	if len(arguments) == 2 {
		options := arguments[1]
		if !options.IsObject() || options.IsArray() {
			return value.Fail(value.InvalidArgumentCode, backupHow)
		}
		for _, key := range options.Keys() {
			if key != "keep" {
				return value.Fail(value.InvalidArgumentCode, "backup: "+key+" is no option; "+backupHow)
			}
			number := options.Get("keep")
			if !number.IsNumber() || number.Number() != float64(int(number.Number())) || number.Number() < 1 {
				return value.Fail(value.InvalidArgumentCode, "backup: keep is how many copies are kept, a whole number from 1")
			}
			keep = int(number.Number())
		}
	}
	to := filepath.FromSlash(strings.TrimSpace(arguments[0].Text()))
	if !filepath.IsAbs(to) {
		to = filepath.Join(d.context.host.Folder, to)
	}
	backup, err := database.BackupFolder(d.context.ctx, d.target.folder, d.target.name, to, keep, time.Now())
	if err != nil {
		if d.context.ctx.Err() != nil {
			return value.Fail(value.CancelledCode, "backup of "+d.target.name+": cancelled")
		}
		return value.Fail(value.UnavailableCode, "backup: "+err.Error())
	}
	files := make([]value.Value, len(backup.Files))
	for i, file := range backup.Files {
		item := value.Object()
		item.Set("name", value.String(file.Name))
		item.Set("bytes", value.Number(float64(file.Bytes)))
		if !file.Structure && !file.Accounts {
			item.Set("revision", value.Number(float64(file.Revision)))
		}
		files[i] = item
	}
	removed := make([]value.Value, len(backup.Removed))
	for i, folder := range backup.Removed {
		removed[i] = value.String(filepath.ToSlash(folder))
	}
	made := value.Object()
	made.Set("folder", value.String(filepath.ToSlash(backup.Folder)))
	made.Set("files", value.Array(files))
	made.Set("bytes", value.Number(float64(backup.Bytes)))
	made.Set("seconds", value.Number(float64(int64(backup.Seconds*1000))/1000))
	made.Set("removed", value.Array(removed))
	return made
}
