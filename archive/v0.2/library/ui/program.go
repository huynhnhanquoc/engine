package ui

import (
	"bytes"
	"errors"
	"hash/maphash"
	"html"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/kitwork/engine2/core/compiler"
	"github.com/kitwork/engine2/library/ui/kernel"
)

// A page's program (round 4 of the 13/11 review, slice 2, 15/11/2026; the owner chose "Server sinh JS thật"): every
// source the kernel runs on the page, a value in data-kitwork-bind, -class, -style or -if, the list and the key of a
// data-kitwork-for, what a data-kitwork-event does, written as JavaScript by the compiler (core/compiler
// javascript.go) into one file, /_kitwork/page.<hash>.js, which the page loads after the kernel:
//
//	kitwork.expressions({ "title": function () { return this.title; }, … }, { "title": "title" });
//
// The kernel runs a source the file holds as that function, called with the element's scope as this, so the
// browser's own engine gives it JavaScript's meaning, the one the server's VM gives it. The second object names the
// sources that are a name alone, which a field writes back (value: title). The sources are read from the page as
// drawn, so a part written again from what an earlier page kept holds its own; data-kitwork-scope holds the values
// the engine seeds a component with, written as literals, which are no program. The same attributes find the
// program they made before (by a hash of their values), kept by the site; the file is kept by the process for its
// path (programAt).

// attributeMark begins every attribute of the kernel as the renderer writes one, after a space.
var attributeMark = []byte(" data-kitwork-")

// sourceKind is the kind of an attribute of the kernel that holds sources, by its name after data-kitwork-: 0 for
// any other (component, scope, at…). A switch, as every attribute of the kernel on the page is asked.
func sourceKind(name []byte) byte {
	switch string(name) {
	case "bind":
		return 'b'
	case "class":
		return 'c'
	case "style":
		return 's'
	case "event":
		return 'e'
	case "if":
		return 'i'
	case "for":
		return 'f'
	}
	return 0
}

// attributeValue is the value of an attribute of the kernel that holds sources, as written (escaped), and its kind.
type attributeValue struct {
	kind byte
	raw  string
}

// programKey is what the attributes of the kernel holding sources in a body are, for the site to find the program
// they made before: two sums of a hash of each value once (two seeds), whatever their order and however often a
// value is written (on each row of a for). A body whose values the sums do not tell apart from another's would get
// its program: the two 64-bit sums of independent hashes make that chance 2^-128.
type programKey struct{ a, b uint64 }

// programSeeds are the seeds of the two hashes, the process's own.
var programSeeds = [2]maphash.Seed{maphash.MakeSeed(), maphash.MakeSeed()}

// seenValues lends a set of the values a body has written, by the first hash, cleared after each body.
var seenValues = sync.Pool{New: func() any { return map[uint64]struct{}{} }}

// eachAttribute calls each with the kind and the value, as written (escaped), of every attribute of the kernel that
// holds sources in body, in the order written. It is a pass of bytes.Index over the body, which a page drawn from
// kept parts pays at every request (measured 15/11/2026: a regular expression over the body, the body copied to a
// string, doubled the time of kitwork.io's pages; a map of the values as strings and FNV still cost 7%).
func eachAttribute(body []byte, each func(kind byte, raw []byte)) {
	for at := 0; ; {
		i := bytes.Index(body[at:], attributeMark)
		if i < 0 {
			return
		}
		start := at + i + len(attributeMark)
		equals := bytes.IndexByte(body[start:], '=')
		if equals < 0 || start+equals+1 >= len(body) || body[start+equals+1] != '"' {
			at = start
			continue
		}
		open := start + equals + 2
		end := bytes.IndexByte(body[open:], '"')
		if end < 0 {
			return
		}
		at = open + end + 1
		if kind := sourceKind(body[start : start+equals]); kind != 0 {
			each(kind, body[open:open+end])
		}
	}
}

// valueHash is the two hashes of one value of an attribute holding sources, with its kind.
type valueHash struct{ a, b uint64 }

// hashValue is the two hashes of a value of kind, as written.
func hashValue(hash *maphash.Hash, kind byte, raw []byte) valueHash {
	hash.SetSeed(programSeeds[0])
	hash.WriteByte(kind)
	hash.Write(raw)
	a := hash.Sum64()
	hash.SetSeed(programSeeds[1])
	hash.WriteByte(kind)
	hash.Write(raw)
	return valueHash{a: a, b: hash.Sum64()}
}

// partValues are the hashes of the values of the attributes holding sources that html holds, each once: what a part
// kept for later pages holds, read once as it is kept, so a page that writes it again does not read it again
// (replay, programKeyOf).
func partValues(html string) []valueHash {
	if !strings.Contains(html, string(attributeMark)) {
		return nil
	}
	var values []valueHash
	var hash maphash.Hash
	eachAttribute([]byte(html), func(kind byte, raw []byte) {
		h := hashValue(&hash, kind, raw)
		for _, known := range values {
			if known.a == h.a {
				return
			}
		}
		values = append(values, h)
	})
	return values
}

// replaySpan is where a part kept for later pages was written again in a page's body, and the values it holds.
type replaySpan struct {
	start, end int
	values     []valueHash
}

// programKeyOf is the key of the attributes holding sources in body; false when it has none. The parts written
// again (spans, in the order written, none inside another) bring the values they hold; the rest of the body is read.
func programKeyOf(body []byte, spans []replaySpan) (programKey, bool) {
	var key programKey
	seen := seenValues.Get().(map[uint64]struct{})
	found := false
	add := func(h valueHash) {
		if _, dup := seen[h.a]; dup {
			return
		}
		seen[h.a] = struct{}{}
		key.a += h.a
		key.b += h.b
		found = true
	}
	var hash maphash.Hash
	read := func(text []byte) {
		eachAttribute(text, func(kind byte, raw []byte) { add(hashValue(&hash, kind, raw)) })
	}
	at := 0
	for _, span := range spans {
		if span.start < at || span.end > len(body) {
			continue // not where a body holds it: read with the rest
		}
		read(body[at:span.start])
		for _, h := range span.values {
			add(h)
		}
		at = span.end
	}
	read(body[at:])
	clear(seen)
	seenValues.Put(seen)
	return key, found
}

// programAttributes are the attributes holding sources in body, each value once, in the order first written: read
// only for a body whose key finds no program.
func programAttributes(body []byte) []attributeValue {
	var values []attributeValue
	seen := map[string]bool{}
	eachAttribute(body, func(kind byte, raw []byte) {
		marked := string(kind) + string(raw)
		if seen[marked] {
			return
		}
		seen[marked] = true
		values = append(values, attributeValue{kind: kind, raw: string(raw)})
	})
	return values
}

// browserSource is one source the kernel runs, as JavaScript.
type browserSource struct {
	javascript string
	// name is the source when it is a name of the scope alone, "" else
	name string
	err  error
}

// pageProgram is the program of a page whose body is body, the parts it wrote again at spans, nil when it runs
// nothing; an error when a source of it cannot be written for the browser.
func (s *Site) pageProgram(body []byte, spans []replaySpan) (*kernel.File, error) {
	key, found := programKeyOf(body, spans)
	if !found {
		return nil, nil
	}
	s.programsLock.RLock()
	kept, ok := s.programs[key]
	s.programsLock.RUnlock()
	if ok {
		return kept, nil
	}
	expressions, handlers := programSources(programAttributes(body))
	if len(expressions)+len(handlers) == 0 {
		return nil, nil
	}
	var functions, names strings.Builder
	functions.WriteString("kitwork.expressions({")
	names.WriteString("}, {")
	first, firstName := true, true
	written := map[string]bool{}
	add := func(source string, handler bool) error {
		if written[source] {
			return nil
		}
		written[source] = true
		compiled := s.browserSource(source, handler)
		if compiled.err != nil {
			return compiled.err
		}
		if !first {
			functions.WriteString(",")
		}
		first = false
		functions.WriteString("\n" + quotedSource(source) + ": function () { return " + compiled.javascript + "; }")
		if compiled.name != "" {
			if !firstName {
				names.WriteString(", ")
			}
			firstName = false
			names.WriteString(quotedSource(source) + ": " + quotedSource(compiled.name))
		}
		return nil
	}
	for _, source := range expressions {
		if err := add(source, false); err != nil {
			return nil, err
		}
	}
	for _, source := range handlers {
		if err := add(source, true); err != nil {
			return nil, err
		}
	}
	names.WriteString("});\n")
	file := kernel.New("page", "page", functions.String()+"\n"+names.String())
	keepProgram(file)
	s.programsLock.Lock()
	if s.programs == nil {
		s.programs = map[programKey]*kernel.File{}
	}
	s.programs[key] = file
	s.programsLock.Unlock()
	return file, nil
}

// browserSource is source written as JavaScript, compiled once by the site: an expression, or what an event runs.
func (s *Site) browserSource(source string, handler bool) browserSource {
	key := source
	if handler {
		key = "\x00" + source
	}
	if kept, ok := s.browser.Load(key); ok {
		return kept.(browserSource)
	}
	var compiled browserSource
	if handler {
		compiled.javascript, compiled.err = compiler.HandlerJavaScript(source)
	} else {
		compiled.javascript, compiled.err = compiler.ExpressionJavaScript(source)
	}
	trimmed := strings.TrimSpace(source)
	if compiled.err != nil {
		compiled.err = errors.New("trình duyệt chạy " + source + ": " + compiled.err.Error())
	} else if compiled.javascript == "this."+trimmed && identifierName(trimmed) {
		compiled.name = trimmed
	}
	kept, _ := s.browser.LoadOrStore(key, compiled)
	return kept.(browserSource)
}

// identifierPattern is a name a scope holds: letters, digits, _ and $, not starting with a digit.
var identifierPattern = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*$`)

func identifierName(name string) bool { return identifierPattern.MatchString(name) }

// quotedSource is a source as a key of the program: a JavaScript string that a <script>'s text holds whole.
func quotedSource(source string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range source {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '<':
			b.WriteString(`<`)
		case 0x2028:
			b.WriteString(` `)
		case 0x2029:
			b.WriteString(` `)
		default:
			if r < 0x20 {
				b.WriteString(`\u00`)
				b.WriteByte("0123456789abcdef"[r>>4])
				b.WriteByte("0123456789abcdef"[r&15])
				continue
			}
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// programSources are the sources the kernel runs in the attributes' values, read as the kernel reads them
// (kernel.js split, cut and loop): the expressions, and what the events run, each once, sorted.
func programSources(values []attributeValue) (expressions, handlers []string) {
	seen := map[string]bool{}
	add := func(list *[]string, source string, handler bool) {
		key := source
		if handler {
			key = "\x00" + source
		}
		if source == "" || seen[key] {
			return
		}
		seen[key] = true
		*list = append(*list, source)
	}
	for _, value := range values {
		raw := html.UnescapeString(value.raw)
		switch value.kind {
		case 'i':
			add(&expressions, strings.TrimSpace(raw), false)
		case 'f':
			parts, ok := splitPairs(raw)
			if !ok || len(parts) == 0 {
				continue
			}
			if head := loopHead.FindStringSubmatch(parts[0]); head != nil {
				add(&expressions, strings.TrimSpace(head[3]), false)
			}
			for _, part := range parts[1:] {
				if name, source, ok := cutPair(part); ok && name == "key" {
					add(&expressions, source, false)
				}
			}
		default:
			parts, ok := splitPairs(raw)
			if !ok {
				continue
			}
			for _, part := range parts {
				if _, source, ok := cutPair(part); ok {
					if value.kind == 'e' {
						add(&handlers, source, true)
					} else {
						add(&expressions, source, false)
					}
				}
			}
		}
	}
	sort.Strings(expressions)
	sort.Strings(handlers)
	return expressions, handlers
}

// loopHead is the head of a data-kitwork-for, item[, index] of list, as the kernel's FOR_HEAD reads it.
var loopHead = regexp.MustCompile(`^\s*([$A-Za-z_][\w$]*)\s*(?:,\s*([$A-Za-z_][\w$]*)\s*)?\s+of\s+([\s\S]+)$`)

// splitPairs cuts raw at every ; outside a string ('…' "…" `…`, backslash escapes) and outside ( ) [ ] { }, as the
// kernel's split does: the parts trimmed, the empty ones dropped; false for what is not balanced or closed.
func splitPairs(raw string) ([]string, bool) {
	var out []string
	var stack []byte
	var quote byte
	start := 0
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		switch {
		case quote != 0:
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"' || c == '`':
			quote = c
		case c == '(':
			stack = append(stack, ')')
		case c == '[':
			stack = append(stack, ']')
		case c == '{':
			stack = append(stack, '}')
		case c == ')' || c == ']' || c == '}':
			if len(stack) == 0 || stack[len(stack)-1] != c {
				return nil, false
			}
			stack = stack[:len(stack)-1]
		case c == ';' && len(stack) == 0:
			out = append(out, raw[start:i])
			start = i + 1
		}
	}
	if quote != 0 || len(stack) > 0 {
		return nil, false
	}
	out = append(out, raw[start:])
	parts := out[:0]
	for _, part := range out {
		if part = strings.TrimSpace(part); part != "" {
			parts = append(parts, part)
		}
	}
	return parts, true
}

// pairName is the name of a pair as the kernel's cut takes it: letters, digits, - . _ + / and a modifier's flat (…).
var pairName = regexp.MustCompile(`^([\w.+/-]|\([\w.+/-]*\))+$`)

// cutPair takes a part apart at its first :, the name and the value, as the kernel's cut does.
func cutPair(part string) (name, value string, ok bool) {
	at := strings.IndexByte(part, ':')
	if at < 0 {
		return "", "", false
	}
	name, value = strings.TrimSpace(part[:at]), strings.TrimSpace(part[at+1:])
	if !pairName.MatchString(name) || value == "" {
		return "", "", false
	}
	return name, value, true
}

// maxPrograms bounds the programs kept to be served: past it the oldest are let go. A site's pages hold a few, each
// the same at every request, so the bound is met only by a host of very many pages.
const maxPrograms = 16384

// programFiles are the pages' programs, by path, for FileAt: the same sources are the same name.
var programFiles struct {
	sync.Mutex
	byPath map[string]*kernel.File
	order  []string
}

// keepProgram keeps a page's program to be served at its path.
func keepProgram(file *kernel.File) {
	programFiles.Lock()
	defer programFiles.Unlock()
	if _, kept := programFiles.byPath[file.Path]; kept {
		return
	}
	if programFiles.byPath == nil {
		programFiles.byPath = map[string]*kernel.File{}
	}
	if len(programFiles.order) >= maxPrograms {
		delete(programFiles.byPath, programFiles.order[0])
		programFiles.order = programFiles.order[1:]
	}
	programFiles.byPath[file.Path] = file
	programFiles.order = append(programFiles.order, file.Path)
}

// programAt is the page's program kept at path, nil when there is none.
func programAt(path string) *kernel.File {
	programFiles.Lock()
	defer programFiles.Unlock()
	return programFiles.byPath[path]
}
