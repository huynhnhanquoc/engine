package host_test

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kitwork/engine2/host"
	"github.com/kitwork/engine2/library/application"
)

// TestACommandRunsWithItsContext: `kitwork <name>` runs the manifest's command (docs/KITWORK_COMMANDS.md mục 3, 4,
// step 3 of mục 9) with one context: context.arguments the words that are no option, context.options --key value,
// --key=value and --key alone as true, "--" ending them; context.print a line to the command's output, its console to
// the log beside it. It reads what the manifest declared around it (env, a const), runs past what a request's energy
// allows (the host's rights), stops when its context is cancelled (Ctrl+C), and is bounded by what one run of a site
// makes. No site boots. A failure it does not catch is the error, with its place; words it cannot read are
// ErrCommandWords.
//
// CONTROL (13/11/2026), each turned off alone (twelve): the arguments kept, `hello printed "chào  {…}"`; the options
// read, `hello printed "chào Quốc,nhé,--to,E:/x,--yes,--,--raw,--keep=7 {}"`; --key=value read, `the imported backup:
// "backup 1 true"`; "--" ending them, `… {"to":"E:/x","yes":true,"raw":true,"keep":"7"}`; print to the output, `hello
// printed ""`; the console to the log, `hello printed "… [hello] [console.log] a line of the console"`; no energy bound,
// "count: \"\", FAIL: application.work:17:23: Energy Limit Exceeded"; the context given, "the cancelled loop ran to its
// end, in 8.3s"; what it makes bounded, "big: <nil>, want the memory's limit"; an option twice and one with no name
// refused, "hello --to a --to b: <nil>", "hello --=x: <nil>"; a command not declared said, a nil pointer's panic.
func TestACommandRunsWithItsContext(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work": `import * as app from "kitwork/application";
import * as file from "kitwork/file";
import { backup } from "./commands/backup.work";

const env = file.env("./.env");
const greeting = "chào";
app.root("apps");
app.commands({
  backup,
  hello: (context) => {
    context.print(greeting, context.arguments, JSON.stringify(context.options));
    console.log("a line of the console");
  },
  where: (context) => context.print(env.SHOP_NAME),
  count: (context) => {
    let total = 0;
    for (let i = 0; i < 3000000; i++) { total = total + 1; }
    context.print(total);
  },
  forever: (context) => {
    let total = 0;
    for (let i = 0; i < 100000000; i++) { total = total + 1; }
    context.print("ran to its end");
  },
  big: (context) => context.print("x".repeat(3000000).length),
  broken: (context) => context.nothing.here,
});
app.website({ hostname: "shop.test", port: 8080 });
app.run();
`,
		"commands/backup.work": `export const backup = (context) => {
  context.print("backup", context.arguments.length, context.options.to);
};
`,
		".env": "SHOP_NAME=Tiệm Quốc\n",
		"apps/shop/shop.test/router.work": routerImport + `console.log("booted");
router.get((context) => context.text("ok"));
`,
	})
	read, err := host.ReadWithLog(filepath.Join(folder, "application.work"), &lines{})
	if err != nil {
		t.Fatal(err)
	}
	run := func(name string, words ...string) (string, string, error) {
		t.Helper()
		var out, log bytes.Buffer
		err := read.RunCommand(context.Background(), name, words, &out, &log)
		return out.String(), log.String(), err
	}
	out, log, err := run("hello", "Quốc", "nhé", "--to", "E:/x", "--yes", "--", "--raw", "--keep=7")
	if err != nil {
		t.Fatalf("hello: %v", err)
	}
	if want := "chào Quốc,nhé,--raw,--keep=7 {\"to\":\"E:/x\",\"yes\":true}\n"; out != want {
		t.Errorf("hello printed %q, want %q", out, want)
	}
	if !strings.Contains(log, "[hello] [console.log] a line of the console") || strings.Contains(out, "console") {
		t.Errorf("hello's console wrote %q to the log and %q to the output", log, out)
	}
	if out, _, err = run("backup", "shopping", "--to=E:/b", "--keep", "7"); err != nil || out != "backup 1 E:/b\n" {
		t.Errorf("the imported backup: %q, %v", out, err)
	}
	if out, _, err = run("where"); err != nil || out != "Tiệm Quốc\n" {
		t.Errorf("a command reading the manifest's env: %q, %v", out, err)
	}
	if out, _, err = run("count"); err != nil || out != "3000000\n" {
		t.Errorf("count: %q, %v", out, err)
	}

	cancelled, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	var output bytes.Buffer
	started := time.Now()
	err = read.RunCommand(cancelled, "forever", nil, &output, &bytes.Buffer{})
	if strings.Contains(output.String(), "ran to its end") {
		t.Errorf("the cancelled loop ran to its end, in %v", time.Since(started))
	} else if err == nil || !strings.Contains(err.Error(), "EXECUTION_CANCELLED") || time.Since(started) > 5*time.Second {
		t.Errorf("a cancelled command: %v after %v, want EXECUTION_CANCELLED at once", err, time.Since(started))
	}

	read.RunBytes = 2 << 20
	if _, _, err = run("big"); err == nil || !strings.Contains(err.Error(), "Memory Limit Exceeded") {
		t.Errorf("big: %v, want the memory's limit", err)
	}
	read.RunBytes = 0
	if _, _, err = run("broken"); err == nil || !strings.Contains(err.Error(), "application.work:") || !strings.Contains(err.Error(), "cannot read properties of undefined (reading 'here')") {
		t.Errorf("broken: %v, want its failure at its place", err)
	}
	if _, _, err = run("missing"); err == nil || !strings.Contains(err.Error(), "the manifest declares no command missing") {
		t.Errorf("a command not declared: %v", err)
	}
	for words, want := range map[string]string{
		"--to a --to b": "--to is given twice",
		"--=x":          "--=x names no option",
	} {
		if _, _, err = run("hello", strings.Fields(words)...); !errors.Is(err, host.ErrCommandWords) || !strings.Contains(err.Error(), want) {
			t.Errorf("hello %s: %v, want ErrCommandWords saying %q", words, err, want)
		}
	}
	if _, log, _ = run("hello"); strings.Contains(log, "booted") {
		t.Errorf("a command booted a site: %q", log)
	}
}

// app.commands({ … }) declares the host's commands (docs/KITWORK_COMMANDS.md mục 2, step 2 of mục 9): each name a
// function of the manifest, written in it or imported, kept in Manifest.Commands; none runs as the manifest is read.
// Declared twice, written as no object of functions, a name a shell would not pass as it is, a word of kitwork itself:
// each stops the manifest at its line saying how it is written.
//
// CONTROL (13/11/2026), each turned off alone: the second call refused, "app.commands({ a: () => 1 });
// app.commands({ b: () => 1 });: <nil>"; one object, `app.commands("backup");: … app.commands({}) declares no
// command …`; the empty object, "app.commands({});: <nil>"; the names, `app.commands({ "Backup": () => 1 });: <nil>`;
// the words of kitwork, `app.commands({ "run": () => 1 });: <nil>`; a value that is no function, `app.commands({
// backup: "backup.work" });: <nil>`; keeping them, "Manifest.Commands names [], want [backup clean-files hello]"; with
// each function run as it is kept, "reading the manifest ran a command"; the guard after app.run(), "app.commands({
// hello: (context) => 1 }) after app.run(): <nil>" (manifest_test.go).
func TestAppCommandsDeclaresTheHostsCommands(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work": `import * as app from "kitwork/application";
import { backup } from "./commands/backup.work";

app.root("apps");
app.commands({
  backup,
  hello: (context) => console.log("ran hello " + context.arguments.join(" ")),
  "clean-files": (context) => console.log("ran clean-files"),
});
app.website({ hostname: "shop.test", port: 8080 });
app.run();
`,
		"commands/backup.work": `export const backup = (context) => {
  console.log("ran backup");
};
`,
		"apps/.keep": "",
	})
	log := &lines{}
	read, err := host.ReadWithLog(filepath.Join(folder, "application.work"), log)
	if err != nil {
		t.Fatal(err)
	}
	commands := read.Manifest.Commands
	var names []string
	for name, command := range commands {
		names = append(names, name)
		if command == nil || command.Function == nil {
			t.Errorf("command %s keeps no function", name)
		}
	}
	slices.Sort(names)
	if want := []string{"backup", "clean-files", "hello"}; !slices.Equal(names, want) {
		t.Fatalf("Manifest.Commands names %v, want %v", names, want)
	}
	if file := commands["backup"].Function.SourceFile; !strings.Contains(filepath.ToSlash(file), "commands/backup.work") {
		t.Errorf("the imported command's function is written in %q, want commands/backup.work", file)
	}
	if strings.Contains(log.String(), "ran ") {
		t.Errorf("reading the manifest ran a command:\n%s", log.String())
	}
	if err := read.Check(); err != nil {
		t.Errorf("check: %v", err)
	}

	stops := func(source, want string) {
		t.Helper()
		folder := write(t, map[string]string{
			"application.work": "import * as app from \"kitwork/application\";\n" + source + "\napp.website(\"shop.test\");\n",
			"apps/.keep":       "",
		})
		if _, err := host.ReadWithLog(filepath.Join(folder, "application.work"), &lines{}); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want an error saying %q", source, err, want)
		}
	}
	stops("app.commands({ a: () => 1 });\napp.commands({ b: () => 1 });", "application.work:3:4: app.commands(…) is declared twice: one manifest, one list of commands")
	for _, source := range []string{
		`app.commands("backup");`,
		`app.commands([() => 1]);`,
		`app.commands();`,
		`app.commands({ a: () => 1 }, { b: () => 1 });`,
	} {
		stops(source, "application.work:2:4: app.commands takes one object of commands, each a function: app.commands({ backup, hello: (context) => context.print(\"chào\") })")
	}
	stops(`app.commands({});`, "application.work:2:4: app.commands({}) declares no command")
	for _, name := range []string{"Backup", "clean_files", "1st", "clean-", "-x", "clean files", "báo-cáo", ""} {
		stops(`app.commands({ "`+name+`": () => 1 });`, "app.commands: \""+name+"\" is no command name: lowercase letters, digits and hyphens, from a letter (backup, clean-files)")
	}
	for _, name := range application.EngineCommands {
		stops(`app.commands({ "`+name+`": () => 1 });`, "app.commands: "+name+" is a command of kitwork itself (run, dev, check, migrate, database, version, help, new, create); name it otherwise")
	}
	for _, value := range []string{`"backup.work"`, `null`, `42`, `{ run: () => 1 }`, `console.log`} {
		stops(`app.commands({ backup: `+value+` });`, "app.commands: backup is no function: a command is (context) => …, written here or imported")
	}
}
