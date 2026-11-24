# KitJS expression IR

The Go parser/evaluator and browser parser/interpreter use the same nested-array tree. These are
expression instructions, separate from the server `.work` stack VM. Source expressions keep their
JavaScript-like spelling; opcode names are internal. Grouped HTML carries source; a prepared page
program may contain compiled expression trees.

## Nodes

`E` denotes an expression, `O` a receiver, `K` a computed key, and `args` an array of expressions.

| Opcode | Shape | Meaning |
| --- | --- | --- |
| `#` | `["#", value]` | Number, string, boolean or null literal; not a `const` declaration |
| `$` | `["$", name]` | Read a scope name; the name `$` addresses the page scope |
| `.` | `[".", O, name]` | Read a fixed member |
| `[.]` | `["[.]", O, K]` | Read a computed member/index |
| `[]` | `["[]", [E, ...]]` | Construct an array |
| `{}` | `["{}", [[key, E], ...]]` | Construct an object |
| `()` | `["()", E, args]` | Call a function |
| `(.)` | `["(.)", O, nameOrKey, args]` | Call a member with its receiver |
| `=>` | `["=>", params, body, source]` | Construct an IR lambda, retaining its original source |
| `u!`, `u+`, `u-` | `[op, E]` | Logical not, numeric conversion, numeric negation |
| `+`, `-`, `*`, `/`, `%` | `[op, E, E]` | Arithmetic; `+` also concatenates |
| `==`, `!=`, `===`, `!==` | `[op, E, E]` | Equality |
| `<`, `>`, `<=`, `>=` | `[op, E, E]` | Ordering |
| `&&`, `\|\|`, `??` | `[op, E, E]` | Short-circuit operators; return an operand |
| `?` | `["?", condition, yes, no]` | Evaluate only the selected branch |
| `=` | `["=", target, E]` | Assignment |
| `+=`, `-=`, `*=`, `/=`, `%=` | `[op, target, E]` | Read-modify-write using the corresponding binary operator |
| `&&=`, `\|\|=`, `??=` | `[op, target, E]` | Conditional assignment, skipping RHS and setter when unnecessary |
| `++`, `--` | `[op, target, prefix]` | Numeric update; `true` returns the new value, `false` the old |
| `;` | `[";", E, ...]` | Sequence; return the final expression's value |

`#`, `$`, and the unary instructions are unchanged. The renamed tags are `idx`/`.[]` -> `[.]`,
bare `call` -> `()`, and member `()` -> `(.)`. The assignment target is now an expression node,
replacing the old string target and the specialized `=$`, `++$`, `--$` instructions.

## Write targets

All assignments and updates use the same reference resolver:

```js
count = 1
// ["=", ["$", "count"], ["#", 1]]

user.name = 'An'
// ["=", [".", ["$", "user"], "name"], ["#", "An"]]

items[index] += 2
// ["+=", ["[.]", ["$", "items"], ["$", "index"]], ["#", 2]]

$.count++
// ["++", [".", ["$", "$"], "count"], false]
```

Resolve the receiver and key once, before evaluating the RHS. Plain assignment does not read the
old property value. Compound assignment reads it once. Logical assignment may skip the RHS and
write entirely. No instruction may implement these operations by evaluating the target twice.

An optional chain is not a write target (`a?.b = 1`, `a?.[k]++` are compile errors). Optional reads
still lower to the existing null-safe read/call nodes; parse-local provenance enforces the target
restriction without adding runtime opcodes. An optional read inside a computed key remains legal.

Only a scope name or member is a syntactically valid target. `$` itself, literals, arithmetic and
call results themselves cannot be assigned to. At runtime, null/primitives and read-only properties
cannot be written. Existing blocked member keys remain blocked. Compiler mutation checks include
every new write form: bindings stay read-only, and catalogue-specific state restrictions remain.

Array writes accept canonical indices and `length`, with a maximum resulting length of 2^20,
matching the server's list materialization ceiling. Extending an array creates holes; truncating
its length drops elements. Arbitrary named array properties are outside this data model.

The Go evaluator preserves its existing ownership policy: an expression-created `Object` is
mutable; host maps/lists are copied along the owning path, and the page-scope map is updated.
This does not turn host collections into full JavaScript reference objects: aliases of a copied
host map/list may retain their previous value, and an array without an owning write-back path
cannot be assigned through. The browser uses ordinary object identity. Do not claim unrestricted
JavaScript alias equivalence for these host collection cases.

## View updates

The interpreter explicitly schedules a coalesced kernel paint after a write. It does not need a
deep Proxy to observe a write that it executes itself. Prepared page-program event evaluation also
receives a write notification: it marks dependent roots and affected lists/rows, including writes
that retain object identity. Key caches are invalidated for local mutation, so a changed key is
computed again. Unknown roots (for example lambda parameters) conservatively invalidate the view.

This notification covers expressions executed by the interpreter. Deep writes inside arbitrary
trusted component JavaScript still follow that runtime's existing state/update contract.

## Compatibility

IR is an internal contract. Both parsers, both evaluators, generated page programs and compiler
checks must be updated together. Legacy opcode arrays are not accepted as a second dialect.
Restart the development host and reload existing browser documents when changing the kernel/IR
contract; hot-swapping a new page program into an old in-memory kernel is not a supported migration.

The grammar remains a deliberate expression subset: this change does not add template literals,
bitwise operators, declarations, loops, destructuring or a complete JavaScript AST.
