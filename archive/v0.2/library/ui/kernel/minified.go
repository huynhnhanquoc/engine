package kernel

// The files minified at build time (07/11/2026: the first engine minified them as it ran, with tdewolff/minify, a
// library engine2 does not take). tools/minify, a module of its own, runs engine2's own way of building every file
// with the minifier at hand and writes each result in minified/, beside the SHA-256 of the source it was made from
// (minified/sources.txt). In production a page loads the minified file of each source that has not changed since;
// one that has is served readable, and the test of this package says so until tools/minify runs again.
//
// A minified file has a name of its own, by its bytes: a page that loads it, and a component that names a part it
// loads, name the minified one.

import (
	"bufio"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"strings"
	"sync"
)

//go:embed minified
var minifiedFiles embed.FS

// Minifier minifies a source, set by tools/minify only: every file Min builds is then minified on the spot and
// recorded (Made), so the tool writes what engine2 will serve. Nil in engine2, which reads what the tool wrote.
var Minifier func(source string) (string, error)

// made are the files Min built with Minifier, by name: what tools/minify writes.
var made sync.Map

// Made is each file Min built with Minifier: its name, the SHA-256 of its source, its minified bytes.
func Made() map[string][2]string {
	out := map[string][2]string{}
	made.Range(func(name, entry any) bool {
		out[name.(string)] = entry.([2]string)
		return true
	})
	return out
}

// sources is minified/sources.txt read once: the SHA-256 of the source each minified file was made from, by name.
var sources = sync.OnceValue(func() map[string]string {
	out := map[string]string{}
	file, err := minifiedFiles.Open("minified/sources.txt")
	if err != nil {
		return out
	}
	defer file.Close()
	lines := bufio.NewScanner(file)
	for lines.Scan() {
		if name, sum, ok := strings.Cut(strings.TrimSpace(lines.Text()), " "); ok {
			out[name] = sum
		}
	}
	return out
})

// Min is the file of role named name minified from source: what tools/minify made of this very source; nil when it
// made none, or made it from another source (the source changed since, and the readable file is served).
func Min(role, name, source string) *File {
	sum := sha256.Sum256([]byte(source))
	digest := hex.EncodeToString(sum[:])
	if Minifier != nil {
		out, err := Minifier(source)
		if err != nil {
			return nil
		}
		made.Store(name, [2]string{digest, out})
		return New(role, name, out)
	}
	if sources()[name] != digest {
		return nil
	}
	body, err := minifiedFiles.ReadFile("minified/" + name + ".js")
	if err != nil {
		return nil
	}
	return New(role, name, string(body))
}

// KernelMin, DriveMin, CallMin and IntersectMin are Kernel, Drive, Call and Intersect minified at build time, nil when
// their sources changed since.
var (
	KernelMin    = sync.OnceValue(func() *File { return Min("runtime", "kernel", string(Kernel().Body)) })
	DriveMin     = sync.OnceValue(func() *File { return Min("drive", "drive", string(Drive().Body)) })
	CallMin      = sync.OnceValue(func() *File { return Min("bridge", "call", string(Call().Body)) })
	IntersectMin = sync.OnceValue(func() *File { return Min("directive", "directives/intersect", string(Intersect().Body)) })
)
