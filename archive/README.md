# The engines written before

Two engines came before the one at the root of this repository. They are kept so that whoever
works on the engine, a person or an AI agent, can see what was already tried, what it cost, and
why it was left, and does not walk the same road again.

Read this file first. Most questions end here; when one does not, this file says which folder
to open.

## Rules for whoever reads this folder

- **Read only.** Nothing under `archive/` is edited, fixed or kept up to date.
- **Do not import it and do not copy code out of it.** Take the idea or the lesson, then write
  it again under the rules of the engine at the root (`blueprint/README.md`). The root engine
  was started precisely because carrying the old code forward carried the old weight with it.
- **Do not search the whole tree.** `v0.1` has about 8,000 files and `v0.2` about 7,000, most of
  them vendored icons. Go to the path this file names.
- **Each folder is its own Go module.** `go build ./...` at the root does not see them. Build
  or test one from inside its folder. `v0.2` needs
  [sqlitewire](https://github.com/huynhnhanquoc/sqlitewire) checked out beside this repository.
- **A number here is a measurement, not a promise.** Each was taken on one laptop on the date
  given. They rank choices against each other; they are not benchmarks to quote.

## At a glance

| | `v0.1` | `v0.2` | the root |
| :--- | :--- | :--- | :--- |
| Go module | `github.com/kitwork/engine` | `github.com/kitwork/engine2` | `github.com/huynhnhanquoc/engine` |
| Written | January to November 2026 | 3 to 24 November 2026 | from late November 2026 |
| History here | all 627 commits | one commit, no history | from its first commit here |
| State | frozen on 11 November 2026 | a copy as of 24 November 2026 | being written |
| What it is | everything, grown in place | the same product, written again to one design | designed again from `application.work` down |

## `v0.1`: the first engine

A Go host that runs a bounded subset of JavaScript on its own bytecode VM, serves many sites
from folders, and grew a page language (`.ui`), a browser kernel, a database and a desktop shell
along the way. Its own README (`v0.1/readme.md`) says what it promised.

**Where things are**

| Path | What is there |
| :--- | :--- |
| `v0.1/compiler/`, `v0.1/runtime/`, `v0.1/value/` | the language: parser, bytecode, VM, the value type |
| `v0.1/work/` | the host: routes, requests, databases, queue, schedules, pages. The largest package and the most tangled |
| `v0.1/ui/`, `v0.1/ui/stylesheet/` | the `.ui` page language and its own CSS |
| `v0.1/jit/hydrate/`, `v0.1/jit/js/` | expressions shared by server and browser, the browser kernel, its components and services |
| `v0.1/jit/icons/`, `v0.1/jit/fonts/` | vendored icons and fonts, most of the file count |
| `v0.1/kitdb/`, `v0.1/search/` | the database work that later left for its own repository (sqlitewire) |
| `v0.1/conformance/`, `v0.1/compatibility/` | fixtures that pin behaviour across versions |
| `v0.1/docs/` | `VM.md`, `BYTECODE.md`, `RUNTIME_ARCHITECTURE.md`, `STABILITY.md` |

**Where it stopped.** Development moved to `v0.2` on 3 November 2026 and the last database
packages left on 11 November. The last commit before the move into this folder holds unfinished
work of 1 November, assignment in browser expressions and an `html` binding
(`v0.1/jit/hydrate/IR.md`): it builds, it is not finished. Two tests of `v0.1/ui` were already
red before that commit (`TestAttributeWrittenTwiceIsAnError`, `TestWrittenOnceEachCompiles`).

**Why it was left.** It had grown as a place to try ideas, and the ideas that stayed sat on the
skeleton of the ones that did not. Measured on 27 October 2026: 107 packages, about 299,000
lines, with `work` alone about 63,000. The value type's three fields were exported and read
directly in about 300 places, one struct of 31 fields was built for every request, and 144 files
reached Go from a program through reflection. Cutting in place was tried first (below) and each
cut was sound, but the skeleton stayed.

**Roads walked and left**

| Road | Why it was left |
| :--- | :--- |
| Variables looked up by name at run time (`v0.1/runtime/interpreter.go`) | about 35% of CPU in hot loops (3 November). The compiler already gave each variable a slot; the VM did not read it |
| Energy charged on every instruction | 11 to 18% of CPU. The grammar already bounds loops, so a charge per block or per backward jump is enough |
| Calling Go from a program through `reflect` | 794 ns and 10 allocations a call, against 38 ns and 1 for a plain dispatch by name |
| A wide value type (56 bytes, 6 fields) | Go keeps a struct in registers only up to 4 words and 4 fields. Cut to `{N, V, K}`, 32 bytes, on 25 October: VM benchmarks about twice as fast. Generics, a handle table, `Result[T]`, 24 bytes and interface-only were all weighed and rejected (`v0.1/value/size_test.go`) |
| Typed number opcodes, an integer kind, object shapes | no measured payoff: arithmetic was about 1% of CPU |
| Template pages (`.kitwork.html`) with a Tailwind-shaped CSS engine and a verb-based browser client | replaced by `.ui` pages with their own CSS and kernel components; cut out from 26 October 2026 on |
| A bundler (esbuild) for imports | removed; the compiler resolves imports itself |
| Turso, then a libSQL wire server, as the database | removed on 4 September and 29 October: the database is pure Go SQLite |
| KROW, a row store written from scratch | removed on 5 November. What stayed is SQLite with one writer and a history, which is sqlitewire |
| An HTTP client that sent the request when its result was first read | replaced on 21 October by an explicit `.send()`: when the request goes out is written in the program |
| A file that switches a feature on in each app | dropped: the folder is the declaration |

**Worth reading before writing the same thing again**

- The parity corpus: `v0.1/jit/hydrate/parity_corpus.json`, 4,093 small programs used to compare
  the engine with JavaScript, and `v0.1/compiler/javascript_parity_test.go`.
- The kernel components the `.ui` tags deliver: `v0.1/jit/js/components/`, and the list of tags
  in `v0.1/ui/catalog.go`.
- `v0.1/docs/VM.md` and `v0.1/docs/BYTECODE.md` for how the VM and its bytecode were laid out.

## `v0.2`: the second engine

The same product written again to the design the first one arrived at: `core/` is the language
and knows no module, `library/<word>` is one importable library each, `host/` runs the apps. What
it does is described module by module in `v0.2/docs/`, written to be changed in the same commit
as the code.

**Where things are**

| Path | What is there |
| :--- | :--- |
| `v0.2/core/value/`, `v0.2/core/compiler/`, `v0.2/core/runtime/`, `v0.2/core/builtins/` | the language |
| `v0.2/host/` | boot, requests, reload, certificates, the limits of a site |
| `v0.2/library/` | `application`, `router`, `database`, `schema`, `queue`, `schedule`, `storage`, `routine`, `http`, `file`, `ui`, `design`, `component`, `module`, `napas` |
| `v0.2/test/` | the tests, outside the packages they test |
| `v0.2/docs/` | one file a module; `language.md` is the language |

**Where it stopped.** This folder is the tree of 24 November 2026 and is not updated. On the
machine it was copied on, one browser test fails
(`TestBrowserStylesheetsHoldInABrowser`: the browser reports `flow-start` where the test expects
`flex-start`), in the original as in the copy.

**Why it was written again.** It was built slice by slice to carry everything the first engine
did, so it carries the things of that time too: the page language, components, views, a set of 18
failure codes, text cursors. The engine at the root starts from one question instead, what
`application.work` returns, and adds a thing when a real need arrives.

**What it got right**

| Idea | Where | What it gave |
| :--- | :--- | :--- |
| Locals in numbered slots, cleared on reset unless a closure holds them | `v0.2/core/runtime/` | VM time 0.55 to 0.96 of before. The clearing is a safety rule too: a slot read before it is written must not show the last run's value, which may be another tenant's |
| One rule for what a program may call: an exported Go method `(...Value) Value` | `v0.2/core/value/` | reflection once at boot, none on the call path: 11 to 16 ns a call, no allocation |
| Builtins built once, frozen, shared by every run | `v0.2/core/builtins/`, `v0.2/test/compiler/shared_builtins_test.go` | building them per run cost 22 µs and 481 allocations. Only what is the same for every app may be shared: the console and the time zone are per site |
| A closed set of failure codes, with a test that reads every call site | `v0.2/core/value/failure.go` | a code cannot be invented in passing. The root engine keeps this idea with a smaller set (`test/value/codes_test.go`) |
| A corpus checked against node, with a ratchet | `v0.2/test/compiler/corpus_test.go`, `v0.2/test/compiler/testdata/ratchet.json` | 2,297 of 4,093 cases matched JavaScript on the first engine, about 3,600 here; a case that gets worse turns the test red |
| A byte meter beside the energy meter | `v0.2/core/runtime/memory.go`, `v0.2/core/value/footprint.go` | energy counts instructions, not bytes: a program held 400 MB for 0.13% of a request's energy |
| A gate on requests per site | `v0.2/host/gate.go` | a site has a ceiling on the requests it runs at once |

**Roads walked and left**

| Road | Why it was left |
| :--- | :--- |
| Compiling in one pass, with no syntax tree | rejected: compiling takes 0.3 to 0.5 ms a file, and forward references, closures over slots and translating a condition to SQL all want the tree |
| Moving array callbacks (`map`, `filter`) into the value package | 48% slower a callback, reverted. With one reused argument slice it was still 14 to 16% slower, so the callbacks stayed in the runtime |
| Counting every byte a run ever makes | stopped normal code: `s += piece` makes about 58 MB to hold 40 KB. Past the ceiling, count what the run still reaches instead |
| Interpreting a page on every request | pages drew 7 to 12 times slower than on the first engine, found only when measured on 11 November. Keeping the parts that cannot change brought it under the first engine's time (`v0.2/library/ui/frozen.go`) |
| A closure that remembers the VM that made it | a function made at load and called during a request ran on the loading VM: a race between requests. A closure runs on the VM that hands it over (`v0.2/test/runtime/caller_test.go`) |
| Values made at load left writable | a request writing a file-level variable was a fatal concurrent map write in Go. What boot makes is frozen |
| The first folder to name a hostname owns it | one tenant could take another's hostname by folder order. A table of domains decides |

## What both engines taught

These are the habits the root engine is written with. They are here because each was learned
by getting it wrong once.

- **A test is not trusted until it has been seen red.** Turn the code it guards off, run it,
  see it fail, turn the code back on. Tests in both engines were green for the wrong reason more
  than once.
- **A rewrite is measured against what it replaces from its first slice**, the two binaries run
  in turn on the same machine. `v0.2` was correct for a week before anyone learned it was slower.
- **No proposal without a number.** The changes that looked most like performance work, an
  integer kind, object shapes, typed opcodes, measured as nothing; the plain ones, slots and the
  energy charge, were most of the time.
- **Take the lesson, leave the code.** Each engine was abandoned for what it carried, not for
  what it lacked.
