package engine2

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestCommandSaysWhatItRan: kitwork <name> is Command (docs/KITWORK_COMMANDS.md mục 3): exit 0 with the command's
// lines on the output and nothing else there, 1 with its failure on the error output, 2 for a word no command answers
// (the words there are said, no server started), for words it cannot read, and for a manifest that does not read.
// CommandNames is what kitwork help lists.
//
// CONTROL (13/11/2026): with the read's notices on the error output, `hello wrote "chào Quốc\n" and "[app]
// app.desktop() is not served by engine2 yet — ignored\n"`; with a word no command answers 1, "chekc: exit 1"; with
// the words' mistake a failure of the command, "hello --to a --to b: exit 1, want 2"; with a manifest that does not read
// 1, "a manifest that does not read: exit 1".
func TestCommandSaysWhatItRan(t *testing.T) {
	folder := t.TempDir()
	manifest := filepath.Join(folder, "application.work")
	if err := os.WriteFile(manifest, []byte(`import * as app from "kitwork/application";
app.desktop({ name: "Shop" });
app.commands({
  hello: (context) => context.print("chào", context.arguments.join(" ")),
  broken: (context) => context.nothing.here,
});
`), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(name string, words ...string) (int, string, string) {
		var out, errOut bytes.Buffer
		code := Command(manifest, name, words, &out, &errOut)
		return code, out.String(), errOut.String()
	}
	if code, out, errOut := run("hello", "Quốc"); code != 0 || out != "chào Quốc\n" || errOut != "" {
		t.Errorf("hello wrote %q and %q, exit %d", out, errOut, code)
	}
	if code, out, errOut := run("broken"); code != 1 || out != "" || !strings.HasPrefix(errOut, "kitwork broken: TYPE: ") {
		t.Errorf("broken: exit %d, %q, %q", code, out, errOut)
	}
	if code, _, errOut := run("chekc"); code != 2 || errOut != "kitwork: chekc is no command: kitwork has run, dev, check, migrate, database, version, help, new, create; "+manifest+" declares broken, hello\n" {
		t.Errorf("chekc: exit %d, %q", code, errOut)
	}
	if code, _, errOut := run("hello", "--to", "a", "--to", "b"); code != 2 || !strings.Contains(errOut, "--to is given twice") {
		t.Errorf("hello --to a --to b: exit %d, want 2: %q", code, errOut)
	}
	missing := filepath.Join(folder, "missing.work")
	var errOut bytes.Buffer
	if code := Command(missing, "backup", nil, &bytes.Buffer{}, &errOut); code != 2 || !strings.Contains(errOut.String(), "kitwork: backup is no word of kitwork (run, dev, check, migrate, database, version, help, new, create), and "+missing+", which declares the others, does not read") {
		t.Errorf("a manifest that does not read: exit %d, %q", code, errOut.String())
	}
	if names, err := CommandNames(manifest); err != nil || !slices.Equal(names, []string{"broken", "hello"}) {
		t.Errorf("CommandNames: %v, %v", names, err)
	}
	if _, err := CommandNames(missing); err == nil {
		t.Error("CommandNames of a manifest that does not read: no error")
	}

	empty := filepath.Join(folder, "empty.work")
	if err := os.WriteFile(empty, []byte("import * as app from \"kitwork/application\";\napp.desktop({ name: \"Shop\" });\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, errOut := func() (int, string, string) {
		var out, errOut bytes.Buffer
		return Command(empty, "backup", nil, &out, &errOut), out.String(), errOut.String()
	}(); code != 2 || !strings.HasSuffix(errOut, empty+" declares none (app.commands({ … }))\n") {
		t.Errorf("a manifest without commands: exit %d, %q", code, errOut)
	}
}
