package database

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/kitwork/engine2/core/value"
)

// A statement is put together from its lambdas (docs/KITWORK_DATABASE.md mục 2): the first lambda names its tables,
// the first the main one, each next one joined to the one before it by their foreign key, or by what join(…) says;
// a table compared with null anywhere is a LEFT JOIN; where before group is WHERE, after it HAVING; ascending and
// descending order by their keys in the order written, the main table's key last; select says the row's shape.
// assemble checks only what the lambdas say, never a value, so the boot runs it on every statement written whole,
// and a request on the steps it was given (the catalog keeps each plan by its lambdas). render writes the SQL of a
// plan with the values the lambdas take from outside.

// step is one lambda a statement was given: translated, with the lambda itself, whose closure holds the values it
// takes from outside (nil at the boot).
type step struct {
	compiled   *compiled
	lambda     *value.Lambda
	descending bool
}

// plan is a statement put together.
type plan struct {
	entry   *Entry
	action  string
	tables  []*joined // tables[0] the main table; the many side of a select is not among them
	where   []part
	having  []part
	group   []part
	order   []ordering
	output  []output
	column  *part // sum's, avg's, min's, max's
	many    *manyPart
	matches *matchesPart
	data    *part // an insert's, update's, upsert's data (write.go)
	grouped bool  // rows are groups: a group, or a select of aggregates only
}

// part is a node of a step: the step's lambda holds the values it takes from outside.
type part struct {
	node *node
	step int
}

type joined struct {
	name  string
	table *Table
	left  bool
	key   string    // the column a LEFT JOIN's row is there by
	from  [2]string // the foreign key: this table's column or the previous one's, as "table.column"
	to    [2]string
	on    []part
}

type ordering struct {
	part       part
	descending bool
}

// output is what one entry of a row reads.
type output struct {
	name    string
	kind    string // column, value, outside (a value from outside, read off the lambda), one, many, parent (hidden)
	table   string
	column  declared
	part    part
	exact   bool       // an aggregate of a column that reads as the column does
	columns []declared // a one's: its table's
	left    bool       // a one's: its table a LEFT JOIN, null when it has no row
	key     string     // a one's LEFT table's key
}

// matchesPart is the full text of a statement (search.go): the matches of its outermost && chain, ranking (positive)
// and only filtering (negative, !posts.matches(q)), all on its main table; ranked when no ascending or descending
// says the order, the rows then in the order of their score.
type matchesPart struct {
	positive []part
	negative []part
	ranked   bool
}

// manyPart is a select's [orders]: the rows of the many side, read by a second query on the foreign key.
type manyPart struct {
	name    string // the row's entry
	table   *Table
	child   string // the many table's column pointing to the parent
	parent  string // the parent table
	target  string // the parent's column it points to
	columns []declared
}

// assemble puts a statement together from its steps.
func assemble(steps []step, action string) (*plan, error) {
	if len(steps) == 0 || steps[0].compiled == nil {
		if action == "delete" {
			return nil, fmt.Errorf("db.delete() cần điều kiện: db.delete((sessions) => sessions.expires < now); mọi dòng là db.delete((sessions) => sessions)")
		}
		return nil, fmt.Errorf("db.%s() cần một lambda nêu bảng: db.%s((orders) => orders)", action, action)
	}
	if writeWords[action] {
		return assembleWrite(steps, action)
	}
	if action == "history" {
		for _, s := range steps {
			if w := s.compiled.word; w != "where" && w != "history" {
				return nil, fmt.Errorf("%s: history nhận where, không nhận %s: db.where((orders) => …).history()", s.compiled.at, w)
			}
		}
		p, err := assemble(steps, "list")
		if err != nil {
			return nil, err
		}
		switch {
		case len(p.tables) != 1:
			return nil, fmt.Errorf("%s: history đọc một bảng", steps[0].compiled.at)
		case p.matches != nil:
			return nil, fmt.Errorf("%s: history cùng matches chưa có", p.matches.positive[0].node.at)
		}
		p.action, p.order, p.output = "history", nil, nil
		return p, nil
	}
	first := steps[0].compiled
	p := &plan{entry: first.entry, action: action}
	index := map[string]int{}
	for i, name := range first.params {
		table := first.entry.table(name)
		p.tables = append(p.tables, &joined{name: name, table: table, key: keyOf(table)})
		index[name] = i
	}
	groupAt, selected := -1, -1
	for i, s := range steps {
		c := s.compiled
		if c == nil {
			return nil, fmt.Errorf("db.%s(): một lambda của câu không được dịch lúc boot", action)
		}
		for _, name := range c.params {
			if _, ok := index[name]; !ok {
				return nil, fmt.Errorf("%s: %s chưa có trong câu: lambda đầu tiên của câu nêu mọi bảng của nó, (%s, %s) => …", c.at, name, strings.Join(first.params, ", "), name)
			}
		}
		if c.word == "group" && groupAt < 0 {
			groupAt = i
		}
		if c.word == "select" {
			if selected >= 0 {
				return nil, fmt.Errorf("%s: select hai lần: một câu có một hình", c.at)
			}
			selected = i
		}
	}
	// what each step says
	var joins []part
	for i, s := range steps {
		c := s.compiled
		switch {
		case c.word == "join":
			if c.body == nil {
				return nil, fmt.Errorf("%s: join nhận điều kiện nối: db.join((orders, leads) => orders.email === leads.email)", c.at)
			}
			if found := matchesIn(c.body); found != nil {
				return nil, fmt.Errorf("%s: matches trong join: tìm toàn văn là một điều kiện lọc, viết trong where", found.at)
			}
			for _, conjunct := range conjuncts(c.body) {
				joins = append(joins, part{node: conjunct, step: i})
			}
		case conditionWords[c.word]:
			if c.body == nil {
				continue
			}
			body := c.body
			if found := matchesIn(body); found != nil {
				if groupAt >= 0 && i > groupAt {
					return nil, fmt.Errorf("%s: matches sau group chưa có trong engine2", found.at)
				}
				rest, positive, negative, err := splitMatches(body)
				if err != nil {
					return nil, err
				}
				if p.matches == nil {
					p.matches = &matchesPart{}
				}
				for _, n := range positive {
					p.matches.positive = append(p.matches.positive, part{node: n, step: i})
				}
				for _, n := range negative {
					p.matches.negative = append(p.matches.negative, part{node: n, step: i})
				}
				if body = rest; body == nil {
					continue
				}
			}
			if groupAt >= 0 && i > groupAt {
				p.having = append(p.having, part{node: body, step: i})
			} else {
				if a := aggregateIn(c.body); a != nil {
					return nil, fmt.Errorf("%s: %s(…) trong where trước group: điều kiện trên giá trị gộp viết sau group (HAVING)", a.at, a.operator)
				}
				p.where = append(p.where, part{node: body, step: i})
			}
		case c.word == "group":
			if c.body.op != "column" {
				return nil, fmt.Errorf("%s: group theo một cột: db.group((orders) => orders.day)", c.at)
			}
			p.group = append(p.group, part{node: c.body, step: i})
		case keyWords[c.word]:
			p.order = append(p.order, ordering{part: part{node: c.body, step: i}, descending: s.descending})
		case columnWords[c.word]:
			p.column = &part{node: c.body, step: i}
		}
	}
	// joins: each table to the one before it
	for _, j := range joins {
		at := 0
		for _, name := range tablesIn(j.node) {
			if index[name] > at {
				at = index[name]
			}
		}
		if at == 0 {
			p.where = append(p.where, j)
		} else {
			p.tables[at].on = append(p.tables[at].on, j)
		}
	}
	for i := 1; i < len(p.tables); i++ {
		this, previous := p.tables[i], p.tables[i-1]
		keys := foreignKeys(this, previous)
		pair := false
		for _, on := range this.on {
			pair = pair || joinsPair(on.node, this.name, index)
		}
		switch {
		case len(keys) == 1:
			this.from, this.to = keys[0][0], keys[0][1]
		case len(keys) == 0 && !pair:
			return nil, fmt.Errorf("%s: %s và %s không có khoá ngoại: viết điều kiện nối trong join, db.join((%s, %s) => %s.cột === %s.cột)", first.at, previous.name, this.name, previous.name, this.name, previous.name, this.name)
		case len(keys) > 1 && !pair:
			var names []string
			for _, key := range keys {
				names = append(names, key[0][0]+"."+key[0][1])
			}
			return nil, fmt.Errorf("%s: %s và %s có %d khoá ngoại (%s): join chọn khoá, db.join((%s, %s) => %s === …)", first.at, previous.name, this.name, len(keys), strings.Join(names, ", "), previous.name, this.name, names[0])
		}
	}
	// a table compared with null is a LEFT JOIN; its columns read only where it is narrowed from null
	for _, s := range steps {
		for name := range s.compiled.nulls {
			if index[name] == 0 {
				return nil, fmt.Errorf("%s: %s là bảng chính của câu: so với null là của một bảng nối (LEFT JOIN)", s.compiled.at, name)
			}
			p.tables[index[name]].left = true
		}
	}
	for _, s := range steps {
		if s.compiled.word == "join" {
			continue
		}
		for name := range s.compiled.reads {
			if t := p.tables[index[name]]; t.left {
				return nil, fmt.Errorf("%s: %s có thể không có dòng (LEFT JOIN vì so với null): đọc cột của nó sau khi thu hẹp, %s === null ? … : %s.cột, hay đặt điều kiện trên %s trong join", s.compiled.at, name, name, name, name)
			}
		}
	}
	// the shape of a row
	var shape *compiled
	if selected >= 0 {
		shape = steps[selected].compiled
	}
	aggregated := false
	if shape != nil {
		for _, f := range shape.shape {
			if f.kind == "value" && aggregateIn(f.value) != nil {
				aggregated = true
			}
		}
	}
	p.grouped = len(p.group) > 0 || aggregated
	if err := p.shape(shape, selected, index); err != nil {
		return nil, err
	}
	if p.grouped {
		if err := p.checkGrouped(shape); err != nil {
			return nil, err
		}
	}
	if len(p.group) > 0 && shape == nil && (action == "list" || action == "first" || action == "detail") {
		return nil, fmt.Errorf("%s: group cần select nói hình của dòng: db.group(…).select((orders) => ({ day: orders.day, total: sum(orders.amount) }))", steps[groupAt].compiled.at)
	}
	if p.column != nil && p.grouped {
		return nil, fmt.Errorf("%s: %s sau group: viết select với %s(…)", steps[p.column.step].compiled.at, action, action)
	}
	if !p.grouped {
		for _, o := range p.order {
			if a := aggregateIn(o.part.node); a != nil {
				return nil, fmt.Errorf("%s: khoá %s(…) cần group: db.group((orders) => orders.day)", a.at, a.operator)
			}
		}
	}
	if m := p.matches; m != nil {
		for _, each := range append(append([]part(nil), m.positive...), m.negative...) {
			if each.node.table != p.tables[0].name {
				return nil, fmt.Errorf("%s: matches trên bảng nối chưa có trong engine2: tìm trên bảng chính của câu, %s", each.node.at, p.tables[0].name)
			}
		}
		if p.grouped {
			return nil, fmt.Errorf("%s: matches cùng group chưa có trong engine2", first.at)
		}
		m.ranked = len(m.positive) > 0 && len(p.order) == 0
	}
	// the order: the main table's key last, or the group's keys; a ranked full text, its score (search.go)
	switch {
	case p.matches != nil && p.matches.ranked:
	case len(p.group) > 0:
		for _, key := range p.group {
			if !p.ordered(key.node) {
				p.order = append(p.order, ordering{part: key})
			}
		}
	case !p.grouped:
		if main := p.tables[0]; main.key != "" {
			column := main.table.field(main.key)
			key := &node{op: "column", table: main.name, column: column, kind: kindOf(column)}
			if !p.ordered(key) {
				p.order = append(p.order, ordering{part: part{node: key}})
			}
		}
	}
	return p, nil
}

// assembleWrite puts a write together (write.go): an insert and an upsert stand alone; an update follows where (and
// join), and writes the statement's main table; a delete takes its condition, or follows where.
func assembleWrite(steps []step, action string) (*plan, error) {
	last := steps[len(steps)-1].compiled
	switch action {
	case "insert", "upsert":
		if len(steps) != 1 {
			return nil, fmt.Errorf("%s: %s đứng một mình: db.%s((orders) => ({ … }))", last.at, action, action)
		}
		table := last.entry.table(last.params[0])
		return &plan{entry: last.entry, action: action, tables: []*joined{{name: table.Name, table: table, key: keyOf(table)}}, data: &part{step: 0}}, nil
	}
	conditions := steps
	hasCondition := false
	for _, s := range steps {
		c := s.compiled
		switch {
		case c.word == "where" || c.word == "join" || c.word == "delete":
			hasCondition = hasCondition || c.word != "join"
		case c.word == "update" && s.compiled == last:
		default:
			return nil, fmt.Errorf("%s: %s nhận where và join, không nhận %s", c.at, action, c.word)
		}
	}
	if action == "update" {
		if last.word != "update" {
			return nil, fmt.Errorf("%s: update cần dữ liệu: db.where(…).update((orders) => ({ status: \"huy\" }))", last.at)
		}
		conditions = steps[:len(steps)-1]
	}
	if !hasCondition {
		return nil, fmt.Errorf("%s: %s cần where: viết điều kiện ra, db.where((orders) => orders.id === id).%s(…); mọi dòng là where((orders) => orders)", last.at, action, action)
	}
	p, err := assemble(conditions, "list")
	if err != nil {
		return nil, err
	}
	if p.matches != nil {
		return nil, fmt.Errorf("%s: matches trong %s chưa có: tìm bằng list trước, rồi ghi theo id", p.matches.positive[0].node.at, action)
	}
	p.action, p.order, p.output, p.many = action, nil, nil, nil
	if action == "update" {
		if last.params[0] != p.tables[0].name {
			return nil, fmt.Errorf("%s: update ghi bảng chính của câu, %s", last.at, p.tables[0].name)
		}
		p.data = &part{step: len(steps) - 1}
	}
	return p, nil
}

// field is a declared column of the table.
func (t *Table) field(name string) *declared {
	for _, f := range t.Fields {
		if f.Name == name {
			d := declare(f)
			return &d
		}
	}
	return nil
}

func keyOf(table *Table) string {
	for _, f := range table.Fields {
		if f.Key {
			return f.Name
		}
	}
	return ""
}

func (p *plan) ordered(key *node) bool {
	for _, o := range p.order {
		if same(o.part.node, key) {
			return true
		}
	}
	return false
}

// same reports two nodes of one column.
func same(a, b *node) bool {
	return a != nil && b != nil && a.op == "column" && b.op == "column" && a.table == b.table && a.column.name == b.column.name
}

// conjuncts are a condition's parts joined by &&.
func conjuncts(n *node) []*node {
	if n.op == "and" {
		return append(conjuncts(n.left), conjuncts(n.right)...)
	}
	return []*node{n}
}

// tablesIn are the tables a condition reads, a table compared with null among them.
func tablesIn(n *node) []string {
	var tables []string
	var visit func(*node)
	visit = func(n *node) {
		if n == nil {
			return
		}
		if (n.op == "column" || n.op == "null" || n.op == "truth" || n.op == "in") && !contains(tables, n.table) {
			tables = append(tables, n.table)
		}
		for _, child := range append([]*node{n.left, n.right, n.third}, n.items...) {
			visit(child)
		}
	}
	visit(n)
	return tables
}

// joinsPair reports a condition comparing a column of table with a column of a table before it.
func joinsPair(n *node, table string, index map[string]int) bool {
	if n == nil {
		return false
	}
	if n.op == "compare" {
		left, right := tablesOf(n.left, nil), tablesOf(n.right, nil)
		for _, a := range left {
			for _, b := range right {
				if a != b && (a == table && index[b] < index[table] || b == table && index[a] < index[table]) {
					return true
				}
			}
		}
	}
	return joinsPair(n.left, table, index) || joinsPair(n.right, table, index) || joinsPair(n.third, table, index)
}

// foreignKeys are the foreign keys between a table and the one before it: {this or previous's column, its target}.
func foreignKeys(this, previous *joined) [][2][2]string {
	var keys [][2][2]string
	for _, f := range this.table.Fields {
		if f.ForeignTarget != nil && f.ForeignTarget == previous.table.Struct {
			keys = append(keys, [2][2]string{{this.name, f.Name}, {previous.name, f.ForeignField}})
		}
	}
	for _, f := range previous.table.Fields {
		if f.ForeignTarget != nil && f.ForeignTarget == this.table.Struct {
			keys = append(keys, [2][2]string{{previous.name, f.Name}, {this.name, f.ForeignField}})
		}
	}
	return keys
}

// matchesIn is the first matches a node holds, nil when none.
func matchesIn(n *node) *node {
	if n == nil {
		return nil
	}
	if n.op == "matches" {
		return n
	}
	for _, child := range append([]*node{n.left, n.right, n.third}, n.items...) {
		if found := matchesIn(child); found != nil {
			return found
		}
	}
	return nil
}

// splitMatches takes the matches out of a condition's outermost && chain: each one ranks, each !matches filters,
// the rest is the condition left (nil when none). A matches anywhere else is not translated yet.
func splitMatches(n *node) (*node, []*node, []*node, error) {
	var rest *node
	var positive, negative []*node
	for _, conjunct := range conjuncts(n) {
		switch {
		case conjunct.op == "matches":
			positive = append(positive, conjunct)
		case conjunct.op == "not" && conjunct.left.op == "matches":
			negative = append(negative, conjunct.left)
		default:
			if found := matchesIn(conjunct); found != nil {
				return nil, nil, nil, fmt.Errorf("%s: matches dưới ||, trong ?: hay sau ! của một điều kiện khác chưa có trong engine2: matches đứng ở chuỗi && ngoài cùng, posts.matches(q) && posts.published", found.at)
			}
			if rest == nil {
				rest = conjunct
			} else {
				rest = &node{op: "and", left: rest, right: conjunct}
			}
		}
	}
	return rest, positive, negative, nil
}

// aggregateIn is the first aggregate a node holds, nil when none.
func aggregateIn(n *node) *node {
	if n == nil {
		return nil
	}
	if n.op == "aggregate" {
		return n
	}
	for _, child := range append([]*node{n.left, n.right, n.third}, n.items...) {
		if a := aggregateIn(child); a != nil {
			return a
		}
	}
	return nil
}

// shape makes the outputs of a row: the select's, or the default one.
func (p *plan) shape(shape *compiled, at int, index map[string]int) error {
	columns := func(name string) []declared {
		var out []declared
		for _, f := range p.tableOf(name).Fields {
			if !f.Secret {
				out = append(out, declare(f))
			}
		}
		return out
	}
	flat := func(name string) {
		for _, column := range columns(name) {
			p.output = append(p.output, output{name: column.name, kind: "column", table: name, column: column})
		}
	}
	one := func(entry, name string) {
		t := p.tables[index[name]]
		p.output = append(p.output, output{name: entry, kind: "one", table: name, columns: columns(name), left: t.left, key: t.key})
	}
	switch {
	case shape == nil && len(p.tables) == 1:
		flat(p.tables[0].name)
		return nil
	case shape == nil:
		for _, t := range p.tables {
			one(t.name, t.name)
		}
		return nil
	case shape.whole != "":
		flat(shape.whole)
		return nil
	}
	for _, f := range shape.shape {
		switch f.kind {
		case "spread":
			flat(f.table)
		case "one":
			i := index[f.table]
			if i > 0 && p.tables[i].from[0] == f.table && !p.tables[i].left {
				// this table holds the foreign key to the one before it: it is the many side
				return fmt.Errorf("%s: %s là phía nhiều của %s: gom các dòng bằng %s: [%s]", f.at, f.table, p.tables[i-1].name, f.name, f.table)
			}
			one(f.name, f.table)
		case "many":
			if err := p.manySide(f, at, index); err != nil {
				return err
			}
		case "value":
			if !mentions(f.value) {
				p.output = append(p.output, output{name: f.name, kind: "outside", part: part{node: f.value, step: at}})
				continue
			}
			o := output{name: f.name, kind: "value", part: part{node: f.value, step: at}}
			if f.value.op == "column" {
				o.kind, o.table, o.column = "column", f.value.table, *f.value.column
			} else if f.value.op == "aggregate" && f.value.left != nil && f.value.left.op == "column" && f.value.operator != "avg" && f.value.operator != "count" {
				o.exact, o.column = true, *f.value.left.column
			}
			p.output = append(p.output, o)
		}
	}
	return nil
}

func (p *plan) tableOf(name string) *Table {
	for _, t := range p.tables {
		if t.name == name {
			return t.table
		}
	}
	if p.many != nil && p.many.table.Name == name {
		return p.many.table
	}
	return p.entry.table(name)
}

// manySide takes [orders] of a select: the last table, the many side of the one before it, read by a second query.
func (p *plan) manySide(f shapeField, at int, index map[string]int) error {
	i := index[f.table]
	this := p.tables[i]
	switch {
	case p.many != nil:
		return fmt.Errorf("%s: một select gom một bảng: [%s] và [%s]", f.at, p.many.table.Name, f.table)
	case i == 0 || i != len(p.tables)-1:
		return fmt.Errorf("%s: [%s] gom bảng cuối của câu, phía nhiều của bảng trước nó: (users, orders) => ({ ...users, orders: [orders] })", f.at, f.table)
	case this.from[0] == "":
		return fmt.Errorf("%s: [%s] gom theo khoá ngoại: %s không có khoá ngoại tới %s", f.at, f.table, f.table, p.tables[i-1].name)
	case this.from[0] != f.table:
		return fmt.Errorf("%s: %s là phía một của %s: viết %s: %s, không ngoặc vuông", f.at, f.table, p.tables[i-1].name, f.name, f.table)
	case this.left || len(this.on) > 0:
		return fmt.Errorf("%s: [%s] gom theo khoá ngoại; điều kiện trên %s chưa có trong engine2", f.at, f.table, f.table)
	}
	for _, list := range [][]part{p.where, p.having, p.group} {
		for _, each := range list {
			if contains(tablesIn(each.node), f.table) {
				return fmt.Errorf("%s: điều kiện trên bảng gom [%s] chưa có trong engine2", f.at, f.table)
			}
		}
	}
	for _, o := range p.order {
		if contains(tablesIn(o.part.node), f.table) {
			return fmt.Errorf("%s: thứ tự theo bảng gom [%s] chưa có trong engine2: các dòng gom xếp theo khoá", f.at, f.table)
		}
	}
	for _, o := range p.output {
		if o.table == f.table || o.kind == "value" && contains(tablesOf(o.part.node, nil), f.table) {
			return fmt.Errorf("%s: %s đã gom bằng [%s]: một select không đọc nó lần nữa", f.at, f.table, f.table)
		}
	}
	m := &manyPart{name: f.name, table: this.table, child: this.from[1], parent: this.to[0], target: this.to[1]}
	for _, field := range this.table.Fields {
		if !field.Secret {
			m.columns = append(m.columns, declare(field))
		}
	}
	p.many = m
	p.tables = p.tables[:i]
	p.output = append(p.output, output{name: f.name, kind: "many"})
	return nil
}

// checkGrouped refuses what a group's row cannot read: a column neither a key nor in an aggregate.
func (p *plan) checkGrouped(shape *compiled) error {
	var keys []*node
	for _, key := range p.group {
		keys = append(keys, key.node)
	}
	hint := func(column *node) string {
		if len(keys) > 0 {
			return "không phải khoá của group, không bọc trong sum, count, avg, min, max"
		}
		return fmt.Sprintf("không gộp cạnh hàm gộp: thêm group((%s) => %s.%s)", column.table, column.table, column.column.name)
	}
	check := func(n *node) error {
		if column := loose(n, keys); column != nil {
			return fmt.Errorf("%s: %s.%s %s", column.at, column.table, column.column.name, hint(column))
		}
		return nil
	}
	if shape != nil {
		for _, f := range shape.shape {
			switch f.kind {
			case "spread", "one", "many":
				return fmt.Errorf("%s: sau group, select chỉ có khoá và giá trị gộp", f.at)
			case "value":
				if err := check(f.value); err != nil {
					return err
				}
			}
		}
		if shape.whole != "" {
			return fmt.Errorf("%s: sau group, select chỉ có khoá và giá trị gộp", shape.at)
		}
	}
	for _, h := range p.having {
		if err := check(h.node); err != nil {
			return err
		}
	}
	for _, o := range p.order {
		if err := check(o.part.node); err != nil {
			return err
		}
	}
	return nil
}

// loose is a column a node reads outside an aggregate that is not one of keys.
func loose(n *node, keys []*node) *node {
	if n == nil || n.op == "aggregate" {
		return nil
	}
	if n.op == "column" {
		for _, key := range keys {
			if same(n, key) {
				return nil
			}
		}
		return n
	}
	for _, child := range append([]*node{n.left, n.right, n.third}, n.items...) {
		if column := loose(child, keys); column != nil {
			return column
		}
	}
	return nil
}

// renderer writes a plan's SQL, its values appended to args in the order the SQL reads them. searched says the main
// table's rows are those of rowids, a full text's (none of them when exclude), rowid that a row reads its rowid last
// (search.go).
type renderer struct {
	plan     *plan
	steps    []step
	args     []any
	searched bool
	rowids   []int64
	exclude  bool
	rowid    bool
	// inserting renders the columns of an upsert's data in the branch that inserts (write.go)
	inserting bool
	// relations are what a table is read from instead of itself: at()'s past (history.go)
	relations map[string]string
}

func (r *renderer) lambda(p part) *value.Lambda {
	if p.step < 0 || p.step >= len(r.steps) {
		return nil
	}
	return r.steps[p.step].lambda
}

func quote(name string) string { return `"` + strings.ReplaceAll(name, `"`, `""`) + `"` }

func qualified(table, column string) string { return quote(table) + "." + quote(column) }

// statement is the plan's main SQL: the action's, a page of rows at offset, at most limit.
func (r *renderer) statement(limit, offset int) (string, value.Value) {
	p := r.plan
	var selectList string
	switch {
	case p.action == "count" && p.grouped && len(p.group) > 0:
		selectList = "1"
	case p.action == "count":
		selectList = "count(*)"
	case p.action == "exists":
		selectList = "1"
	case p.column != nil:
		text, failure := r.aggregateAction()
		if failure.IsInvalid() {
			return "", failure
		}
		selectList = text
	default:
		text, failure := r.selectList()
		if failure.IsInvalid() {
			return "", failure
		}
		selectList = text
	}
	from, failure := r.from()
	if failure.IsInvalid() {
		return "", failure
	}
	tail, failure := r.filters()
	if failure.IsInvalid() {
		return "", failure
	}
	text := "SELECT " + selectList + from + tail
	switch {
	case p.action == "count" && p.grouped && len(p.group) > 0:
		return "SELECT count(*) FROM (" + text + ")", value.Value{}
	case p.action == "count" || p.column != nil:
		return text, value.Value{}
	case p.action == "exists":
		return text + " LIMIT 1", value.Value{}
	}
	order, failure := r.orderBy()
	if failure.IsInvalid() {
		return "", failure
	}
	text += order + " LIMIT " + strconv.Itoa(limit)
	if offset > 0 {
		text += " OFFSET " + strconv.Itoa(offset)
	}
	return text, value.Value{}
}

func (r *renderer) aggregateAction() (string, value.Value) {
	p := r.plan
	column := p.column.node
	exact := column.op == "column" && p.action != "avg"
	inner, failure := r.value(column, r.lambda(*p.column), !exact, "")
	if failure.IsInvalid() {
		return "", failure
	}
	if p.action == "sum" {
		return "coalesce(sum(" + inner + "), 0)", value.Value{}
	}
	return p.action + "(" + inner + ")", value.Value{}
}

func (r *renderer) selectList() (string, value.Value) {
	var items []string
	for _, o := range r.plan.output {
		switch o.kind {
		case "column":
			items = append(items, qualified(o.table, o.column.name))
		case "value":
			text, failure := r.output(o)
			if failure.IsInvalid() {
				return "", failure
			}
			items = append(items, text)
		case "one":
			if o.left {
				items = append(items, qualified(o.table, o.key)+" IS NOT NULL")
			}
			for _, column := range o.columns {
				items = append(items, qualified(o.table, column.name))
			}
		}
	}
	if m := r.plan.many; m != nil {
		items = append(items, qualified(m.parent, m.target))
	}
	if r.rowid {
		items = append(items, qualified(r.plan.tables[0].name, "rowid"))
	}
	if len(items) == 0 {
		items = append(items, "1")
	}
	return strings.Join(items, ", "), value.Value{}
}

// output is a value of a row as SQL: an aggregate of a column as the column's file keeps it, else its value.
func (r *renderer) output(o output) (string, value.Value) {
	n := o.part.node
	if o.exact {
		inner := qualified(n.left.table, n.left.column.name)
		if n.operator == "sum" {
			return "coalesce(sum(" + inner + "), 0)", value.Value{}
		}
		return n.operator + "(" + inner + ")", value.Value{}
	}
	return r.value(n, r.lambda(o.part), n.kind == "number", n.kind)
}

func (r *renderer) from() (string, value.Value) {
	p := r.plan
	text := " FROM " + r.relation(p.tables[0].name)
	for _, t := range p.tables[1:] {
		var on []string
		if t.from[0] != "" {
			on = append(on, qualified(t.from[0], t.from[1])+" = "+qualified(t.to[0], t.to[1]))
		}
		for _, each := range t.on {
			cond, failure := r.condition(each.node, r.lambda(each))
			if failure.IsInvalid() {
				return "", failure
			}
			on = append(on, cond)
		}
		join := " JOIN "
		if t.left {
			join = " LEFT JOIN "
		}
		text += join + quote(t.name) + " ON " + strings.Join(on, " AND ")
	}
	return text, value.Value{}
}

// relation is a table as the FROM reads it: itself, or what at() reads it from, named as the table.
func (r *renderer) relation(table string) string {
	if relation := r.relations[table]; relation != "" && relation != table {
		return quote(relation) + " AS " + quote(table)
	}
	return quote(table)
}

// filters are WHERE, GROUP BY and HAVING.
func (r *renderer) filters() (string, value.Value) {
	p := r.plan
	text := ""
	clause := func(word string, parts []part) value.Value {
		var conditions []string
		for _, each := range parts {
			cond, failure := r.condition(each.node, r.lambda(each))
			if failure.IsInvalid() {
				return failure
			}
			conditions = append(conditions, cond)
		}
		if len(conditions) > 0 {
			text += " " + word + " " + strings.Join(conditions, " AND ")
		}
		return value.Value{}
	}
	where := p.where
	if r.searched {
		// the rows the full text kept, bound as one JSON array
		ids, err := json.Marshal(r.rowids)
		if err != nil {
			return "", value.Fail(value.DatabaseCode, err.Error())
		}
		in := " IN "
		if r.exclude {
			in = " NOT IN "
		}
		where = append([]part{{node: &node{op: "rows", operator: in, table: p.tables[0].name, literal: value.String(string(ids))}, step: -1}}, where...)
	}
	if failure := clause("WHERE", where); failure.IsInvalid() {
		return "", failure
	}
	if len(p.group) > 0 {
		var keys []string
		for _, key := range p.group {
			keys = append(keys, qualified(key.node.table, key.node.column.name))
		}
		text += " GROUP BY " + strings.Join(keys, ", ")
	}
	if failure := clause("HAVING", p.having); failure.IsInvalid() {
		return "", failure
	}
	return text, value.Value{}
}

func (r *renderer) orderBy() (string, value.Value) {
	var items []string
	for _, o := range r.plan.order {
		n := o.part.node
		var text string
		if n.op == "column" {
			text = qualified(n.table, n.column.name)
		} else {
			var failure value.Value
			if text, failure = r.value(n, r.lambda(o.part), true, ""); failure.IsInvalid() {
				return "", failure
			}
		}
		direction := " ASC"
		if o.descending {
			direction = " DESC"
		}
		if r.mayBeNull(n) {
			direction += " NULLS LAST" // null last both ways, on every database (docs/KITWORK_DATABASE.md mục 2)
		}
		items = append(items, text+direction)
	}
	if len(items) == 0 {
		return "", value.Value{}
	}
	return " ORDER BY " + strings.Join(items, ", "), value.Value{}
}

func (r *renderer) mayBeNull(n *node) bool {
	switch n.op {
	case "column":
		for _, t := range r.plan.tables {
			if t.name == n.table && t.left {
				return true
			}
		}
		return n.column.field.Nullable
	case "aggregate":
		return n.operator != "count" && n.operator != "sum"
	}
	return true
}

// condition is a condition as SQL: a value from outside read off the lambda's closure, as the lambda would read it.
func (r *renderer) condition(n *node, lambda *value.Lambda) (string, value.Value) {
	switch n.op {
	case "and", "or":
		left, failure := r.condition(n.left, lambda)
		if failure.IsInvalid() {
			return "", failure
		}
		right, failure := r.condition(n.right, lambda)
		if failure.IsInvalid() {
			return "", failure
		}
		return "(" + left + " " + strings.ToUpper(n.op) + " " + right + ")", value.Value{}
	case "not":
		inner, failure := r.condition(n.left, lambda)
		if failure.IsInvalid() {
			return "", failure
		}
		return "NOT (" + inner + ")", value.Value{}
	case "case":
		parts := make([]string, 3)
		for i, child := range []*node{n.left, n.right, n.third} {
			text, failure := r.condition(child, lambda)
			if failure.IsInvalid() {
				return "", failure
			}
			parts[i] = text
		}
		return "(CASE WHEN " + parts[0] + " THEN " + parts[1] + " ELSE " + parts[2] + " END)", value.Value{}
	case "constant":
		if n.literal.Boolean() {
			return "1", value.Value{}
		}
		return "0", value.Value{}
	case "outside":
		item, failure := holeValue(n, lambda)
		if failure.IsInvalid() {
			return "", failure
		}
		if item.ToBoolean() {
			return "1", value.Value{}
		}
		return "0", value.Value{}
	case "truth":
		// IS, never NULL: !orders.paid holds a row whose paid is null, as !null is true
		return qualified(n.table, n.column.name) + " IS 1", value.Value{}
	case "null":
		t := r.joinedOf(n.table)
		present := "rowid"
		if t != nil && t.key != "" {
			present = t.key
		}
		if n.operator == "===" {
			return qualified(n.table, present) + " IS NULL", value.Value{}
		}
		return qualified(n.table, present) + " IS NOT NULL", value.Value{}
	case "rows":
		r.args = append(r.args, n.literal.Text())
		return qualified(n.table, "rowid") + n.operator + "(SELECT value FROM json_each(?))", value.Value{}
	case "in":
		return r.in(n, lambda)
	case "text":
		return r.text(n, lambda)
	case "compare":
		return r.compare(n, lambda)
	}
	return "", value.Fail(value.InvalidArgumentCode, n.at+": điều kiện chưa có trong engine2")
}

func (r *renderer) joinedOf(name string) *joined {
	for _, t := range r.plan.tables {
		if t.name == name {
			return t
		}
	}
	return nil
}

func (r *renderer) in(n *node, lambda *value.Lambda) (string, value.Value) {
	var items []value.Value
	if n.left != nil {
		list, failure := holeValue(n.left, lambda)
		if failure.IsInvalid() {
			return "", failure
		}
		if !list.IsArray() {
			return "", value.Fail(value.InvalidArgumentCode, n.at+": includes đọc một mảng: "+strings.Join(n.left.hole, ".")+" không phải mảng")
		}
		items = list.Items()
	} else {
		for _, item := range n.items {
			items = append(items, item.literal)
		}
	}
	// includes is ===: a null in the list holds a row whose column is null, an undefined none; a column that is
	// null is in no list without a null, false and never SQL's unknown, so ! of it is true
	var marks []string
	holdsNull := false
	for _, item := range items {
		if item.IsNullish() {
			holdsNull = holdsNull || item.IsNull()
			continue
		}
		bound, failure := bind(n.column, r.plan.entry.transformed(n.table, n.column.name, item), n.at)
		if failure.IsInvalid() {
			return "", failure
		}
		r.args = append(r.args, bound)
		marks = append(marks, "?")
	}
	name := qualified(n.table, n.column.name)
	switch {
	case len(marks) == 0 && holdsNull:
		return name + " IS NULL", value.Value{}
	case len(marks) == 0:
		return "0", value.Value{} // as [].includes(x)
	case holdsNull:
		return "(" + name + " IN (" + strings.Join(marks, ", ") + ") OR " + name + " IS NULL)", value.Value{}
	case n.column.field.Nullable:
		return "(" + name + " IS NOT NULL AND " + name + " IN (" + strings.Join(marks, ", ") + "))", value.Value{}
	}
	return name + " IN (" + strings.Join(marks, ", ") + ")", value.Value{}
}

// text is column.includes(x), startsWith(x), endsWith(x): a substring as JavaScript finds it, letter for letter,
// case kept, no wildcard (instr, a range on the index, the last characters).
func (r *renderer) text(n *node, lambda *value.Lambda) (string, value.Value) {
	item := n.right.literal
	if n.right.op == "hole" {
		var failure value.Value
		if item, failure = holeValue(n.right, lambda); failure.IsInvalid() {
			return "", failure
		}
	}
	if !item.IsString() {
		return "", value.Fail(value.InvalidArgumentCode, n.at+": "+n.operator+"(…) nhận một chữ")
	}
	text := item.Text()
	receiver, failure := r.value(n.left, lambda, false, "string")
	if failure.IsInvalid() {
		return "", failure
	}
	switch n.operator {
	case "includes":
		r.args = append(r.args, text)
		return "instr(" + receiver + ", ?) > 0", value.Value{}
	case "startsWith":
		// every string starting with text lies between text and text followed by a byte UTF-8 never holds
		r.args = append(r.args, text, text+"\xff")
		return "(" + receiver + " >= ? AND " + receiver + " < ?)", value.Value{}
	}
	if text == "" {
		return "1", value.Value{}
	}
	r.args = append(r.args, int64(utf8.RuneCountInString(text)), text)
	return "substr(" + receiver + ", -?) = ?", value.Value{}
}

func (r *renderer) compare(n *node, lambda *value.Lambda) (string, value.Value) {
	anchor, other, operator := n.left, n.right, n.operator
	if !mentions(anchor) {
		anchor, other, operator = other, anchor, flipped(operator)
	}
	if mentions(other) {
		// two values of the tables: as the file keeps them when they are kept alike, else as numbers
		numbers := anchor.kind == "number" && !(anchor.op == "column" && other.op == "column" && anchor.column.kind == other.column.kind && anchor.column.scale == other.column.scale)
		left, failure := r.value(anchor, lambda, numbers, anchor.kind)
		if failure.IsInvalid() {
			return "", failure
		}
		right, failure := r.value(other, lambda, numbers, other.kind)
		if failure.IsInvalid() {
			return "", failure
		}
		return left + " " + sqlOperator(operator) + " " + right, value.Value{}
	}
	item := other.literal
	if other.op == "hole" {
		var failure value.Value
		if item, failure = holeValue(other, lambda); failure.IsInvalid() {
			return "", failure
		}
	}
	numbers := anchor.op != "column" && anchor.kind == "number"
	text, failure := r.value(anchor, lambda, numbers, anchor.kind)
	if failure.IsInvalid() {
		return "", failure
	}
	if item.IsNullish() {
		// === as JavaScript: a value is null, never undefined
		switch {
		case operator == "===" && item.IsNull():
			return text + " IS NULL", value.Value{}
		case operator == "!==" && item.IsNull():
			return text + " IS NOT NULL", value.Value{}
		case operator == "===":
			return "0", value.Value{}
		case operator == "!==":
			return "1", value.Value{}
		}
		return "", value.Fail(value.InvalidArgumentCode, n.at+": null so thứ tự với "+describe(anchor)+": chỉ === hay !== so với null")
	}
	var bound any
	if anchor.op == "column" {
		// compared as the column keeps its values: through its transform
		item = r.plan.entry.transformed(anchor.table, anchor.column.name, item)
		bound, failure = bind(anchor.column, item, n.at)
	} else {
		bound, failure = bindKind(anchor, item, n.at)
	}
	if failure.IsInvalid() {
		return "", failure
	}
	r.args = append(r.args, bound)
	return text + " " + sqlOperator(operator) + " ?", value.Value{}
}

// value is a value as SQL. numbers: a number as JavaScript reads it (a decimal column's stored integer divided by
// 10^scale), else as the file keeps it. want is the kind a value from outside is read as.
func (r *renderer) value(n *node, lambda *value.Lambda, numbers bool, want string) (string, value.Value) {
	switch n.op {
	case "column":
		if r.inserting {
			// an upsert's branch that inserts: the row is not there, its number column null, which + and - read as 0
			return "0", value.Value{}
		}
		text := qualified(n.table, n.column.name)
		if numbers && n.column.kind == "decimal" && n.column.scale > 0 {
			return "(" + text + " / " + strconv.FormatFloat(math.Pow10(n.column.scale), 'f', -1, 64) + ".0)", value.Value{}
		}
		return text, value.Value{}
	case "literal", "hole":
		item := n.literal
		if n.op == "hole" {
			var failure value.Value
			if item, failure = holeValue(n, lambda); failure.IsInvalid() {
				return "", failure
			}
		}
		if item.IsNullish() {
			return "NULL", value.Value{}
		}
		bound, failure := bindKind(&node{kind: want, at: n.at}, item, n.at)
		if failure.IsInvalid() {
			return "", failure
		}
		r.args = append(r.args, bound)
		return "?", value.Value{}
	case "math":
		left, failure := r.value(n.left, lambda, true, "number")
		if failure.IsInvalid() {
			return "", failure
		}
		right, failure := r.value(n.right, lambda, true, "number")
		if failure.IsInvalid() {
			return "", failure
		}
		if n.operator == "/" {
			return "(CAST(" + left + " AS REAL) / " + right + ")", value.Value{}
		}
		return "(" + left + " " + n.operator + " " + right + ")", value.Value{}
	case "negate":
		inner, failure := r.value(n.left, lambda, true, "number")
		if failure.IsInvalid() {
			return "", failure
		}
		return "(-" + inner + ")", value.Value{}
	case "lower":
		inner, failure := r.value(n.left, lambda, false, "string")
		if failure.IsInvalid() {
			return "", failure
		}
		return "kitwork_lower(" + inner + ")", value.Value{}
	case "aggregate":
		if n.left == nil {
			return "count(*)", value.Value{}
		}
		inner, failure := r.value(n.left, lambda, n.left.kind == "number", n.left.kind)
		if failure.IsInvalid() {
			return "", failure
		}
		if n.operator == "sum" {
			return "coalesce(sum(" + inner + "), 0)", value.Value{}
		}
		return n.operator + "(" + inner + ")", value.Value{}
	case "choose":
		cond, failure := r.condition(n.left, lambda)
		if failure.IsInvalid() {
			return "", failure
		}
		consequence, failure := r.value(n.right, lambda, n.kind == "number", n.kind)
		if failure.IsInvalid() {
			return "", failure
		}
		alternative, failure := r.value(n.third, lambda, n.kind == "number", n.kind)
		if failure.IsInvalid() {
			return "", failure
		}
		return "(CASE WHEN " + cond + " THEN " + consequence + " ELSE " + alternative + " END)", value.Value{}
	}
	return "", value.Fail(value.InvalidArgumentCode, n.at+": "+describe(n)+" không phải một giá trị của câu")
}

// sqlOperator is a comparison as SQL writes it: === and !== null-safe as JavaScript compares (IS, IS NOT).
func sqlOperator(operator string) string {
	switch operator {
	case "===":
		return "IS"
	case "!==":
		return "IS NOT"
	}
	return operator
}

// flipped is a comparison with its sides swapped: 5 < orders.amount is orders.amount > 5.
func flipped(operator string) string {
	switch operator {
	case "<":
		return ">"
	case "<=":
		return ">="
	case ">":
		return "<"
	case ">=":
		return "<="
	}
	return operator
}

// bindKind is a value from outside compared with a computed value, by the computed value's kind.
func bindKind(n *node, item value.Value, at string) (any, value.Value) {
	wrong := func(want string) (any, value.Value) {
		return nil, value.Fail(value.InvalidArgumentCode, fmt.Sprintf("giá trị ở %s không phải %s", at, want))
	}
	switch n.kind {
	case "number":
		if !item.IsNumber() {
			return wrong("số")
		}
		return item.Number(), value.Value{}
	case "string":
		if !item.IsString() {
			return wrong("chữ")
		}
		return item.Text(), value.Value{}
	case "boolean":
		if !item.IsBoolean() {
			return wrong("true hay false")
		}
		if item.Boolean() {
			return int64(1), value.Value{}
		}
		return int64(0), value.Value{}
	case "time":
		return bind(&declared{name: describe(n), kind: "datetime", spelling: "timestamp()"}, item, at)
	}
	switch {
	case item.IsNumber():
		return item.Number(), value.Value{}
	case item.IsString():
		return item.Text(), value.Value{}
	case item.IsBoolean():
		if item.Boolean() {
			return int64(1), value.Value{}
		}
		return int64(0), value.Value{}
	case item.IsTime():
		return item.Time().UTC().Format(timeLayout), value.Value{}
	}
	return wrong("số, chữ hay true, false")
}
