// Package file is "kitwork/file": file.env, the one way a manifest and an app read their environment
// (docs/KITWORK_ENVIRONMENT.md mục 7), and an app's files by docs/KITWORK_FILE.md (module.go, handle.go):
//
//	import * as file from "kitwork/file";
//	const env = file.env("app/.env", "./.env");   // the app's .env, then this folder's over it, key by key
//	const settings = file.json("settings.json");  // read: call directly; null when it is not there
//	file.open("notes.txt").write("Xin chào");     // change: open() first
package file

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kitwork/engine2/core/value"
)

// File is "kitwork/file" as one file sees it: where its paths are read from and what lies on top.
type File struct {
	// Root is the folder file.env never leaves: the manifest's folder, or the app's root. "" outside a
	// file's run at boot (a request), where file.env is refused: the environment is read once.
	Root string
	// Here is the folder of the file writing the call, relative to Root, "/"-separated ("" is Root
	// itself): what "./…" is read from.
	Here func() string
	// Manifest is the host's manifest: "./…" only, and the operating system's variables lie on top. An
	// app never sees them.
	Manifest bool
	// Require records a key env.require(…) does not find at boot, for the one boot error that lists
	// them all; it says false once the boot is over.
	Require func(key string) bool
	// Notice prints one line at boot.
	Notice func(text string)
	// Booting says the file is running at boot: a handler that kept file and calls file.env at a request is
	// refused, as the environment is read once; a change of a file at boot is refused too (handle.go).
	Booting func() bool
	// zones is the app's folder the words of its files reach the disk through (module.go).
	zones zones
}

// Env is file.env(path, …): the files read in order, a later one's key over an earlier one's, and the
// result read by name (env.PORT), coerced by canonical form. A missing file holds nothing; a path out of
// Root, through a link out of it, or to a folder, is a failure that names the path as written.
func (f *File) Env(arguments ...value.Value) value.Value {
	if f.Root == "" || f.Booting == nil || !f.Booting() {
		return value.Fail(value.UnsupportedCode, "file.env(…) reads the environment once, at the top of a file: const env = file.env(\"app/.env\")")
	}
	if len(arguments) == 0 {
		return value.Fail(value.InvalidArgumentCode, "file.env(…) takes the path of each file it reads: file.env(\"app/.env\", \"./.env\")")
	}
	root, err := os.OpenRoot(f.Root)
	if err != nil {
		return value.Fail(value.UnavailableCode, "file.env(…): the folder cannot be opened")
	}
	defer root.Close()
	values := map[string]entry{}
	for _, argument := range arguments {
		if !argument.IsString() {
			return value.Fail(value.InvalidArgumentCode, "file.env(…) takes paths written as text, got "+argument.Kind())
		}
		written := argument.Text()
		relative, problem := f.resolve(written)
		if problem != "" {
			return value.Fail(value.DeniedCode, "file.env("+strconv.Quote(written)+"): "+problem)
		}
		read, err := root.ReadFile(filepath.FromSlash(relative))
		switch {
		case errors.Is(err, fs.ErrNotExist):
			continue // a file that is not there holds nothing (mục 1)
		case err != nil:
			if info, statError := root.Stat(filepath.FromSlash(relative)); statError == nil && info.IsDir() {
				return value.Fail(value.InvalidArgumentCode, "file.env("+strconv.Quote(written)+"): a folder, not a file")
			}
			return value.Fail(value.DeniedCode, "file.env("+strconv.Quote(written)+"): the file is outside "+f.place()+" or cannot be read")
		}
		for key, item := range parse(string(read)) {
			if f.Manifest && f.Notice != nil {
				if system, ok := os.LookupEnv(key); ok && system != "" {
					f.Notice(written + " sets " + key + ", which the operating system sets too: the system's value wins")
				}
			}
			values[key] = item
		}
	}
	return value.Struct(&Environment{values: values, system: f.Manifest, require: f.Require})
}

// resolve is a written path from Root, or what is wrong with it. "./x" is from the writing file's
// folder, "app/x" from the app's root; nothing leaves Root, whatever the dots.
func (f *File) resolve(written string) (string, string) {
	if strings.Contains(written, "\\") {
		return "", "a path is written with \"/\""
	}
	var from, rest string
	switch {
	case strings.HasPrefix(written, "./"):
		from, rest = f.here(), strings.TrimPrefix(written, "./")
	case strings.HasPrefix(written, "app/") && !f.Manifest:
		from, rest = "", strings.TrimPrefix(written, "app/")
	case f.Manifest:
		return "", "a path of the manifest starts with ./, its own folder: file.env(\"./.env\")"
	default:
		return "", "a path starts with ./ (this file's folder) or app/ (the app's root): file.env(\"app/.env\")"
	}
	if rest == "" || path.IsAbs(rest) || strings.Contains(rest, ":") {
		return "", "the path names no file in " + f.place()
	}
	joined := path.Clean(path.Join(from, rest))
	if joined == ".." || strings.HasPrefix(joined, "../") || joined == "." {
		return "", "the path is outside " + f.place()
	}
	return joined, ""
}

func (f *File) here() string {
	if f.Here == nil {
		return ""
	}
	return f.Here()
}

func (f *File) place() string {
	if f.Manifest {
		return "the manifest's folder"
	}
	return "the app's root"
}
