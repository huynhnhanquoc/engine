package schema

import (
	"github.com/kitwork/engine2/core/value"
)

// The words of a table (docs/schema.md mục 5): written on a struct's fields, meaningful once kitwork/database
// places the struct in connection().tables({ … }), which reads them through TableFields. A struct with any of
// them that no tables({ … }) takes is refused when the app boots (Struct.TableWord).

// kindID is a key the engine makes: id() a number counting up, id("uuid") a UUIDv7, id("kitwork") a KitID.
const kindID = "id"

// tableWords are what a field says of the table it is a column of.
type tableWords struct {
	generate   string // id(…): "number", "uuid", "kitwork"
	key        bool   // id(…) or .key(): the table's key
	unique     bool
	group      string // .unique("name"): unique together with the group's other columns
	groupAt    int    // .unique("name", position), 0 for the order written
	index      bool
	indexName  string
	indexAt    int
	searchable bool
	created    bool
	updated    bool
	readonly   bool
	secret     bool
	cascade    bool
	foreign    *reference
}

// reference is a foreign key's target: a field of another struct, known now (foreign(users.id)) or when the
// table is declared (foreign(() => employees.id), a struct pointing at itself or a cycle).
type reference struct {
	target *Struct
	field  string
	lazy   value.Value
}

// Id is id(), id("uuid") or id("kitwork"): the table's key, which the engine makes at insert and parse never
// takes (docs/schema.md mục 4).
func (s *Schema) Id(arguments ...value.Value) value.Value {
	generate := "number"
	if len(arguments) > 1 {
		return value.Fail(value.InvalidArgumentCode, "id nhận không gì, \"uuid\" hay \"kitwork\"")
	}
	if len(arguments) == 1 {
		word := arguments[0]
		switch {
		case word.IsString() && (word.Text() == "uuid" || word.Text() == "kitwork"):
			generate = word.Text()
		case word.IsString() && word.Text() == "serial":
			return value.Fail(value.InvalidArgumentCode, "id(\"serial\") không có: id() đã là số tăng dần")
		default:
			return value.Fail(value.InvalidArgumentCode, "id nhận không gì, \"uuid\" hay \"kitwork\": id() số tăng dần, id(\"uuid\") UUIDv7, id(\"kitwork\") KitID")
		}
	}
	return value.Struct(&Field{kind: kindID, table: tableWords{generate: generate, key: true}})
}

// Foreign is foreign(users.id), or foreign(() => employees.id) for a struct pointing at itself or a cycle: a
// column whose value is a key, or a unique column, of another table, of that column's kind.
func (s *Schema) Foreign(arguments ...value.Value) value.Value {
	written := argument(arguments, 0)
	if len(arguments) != 1 {
		return value.Fail(value.InvalidArgumentCode, "foreign nhận một cột của bảng khác: foreign(users.id)")
	}
	if written.IsFunction() {
		return value.Struct(&Field{table: tableWords{foreign: &reference{lazy: written}}})
	}
	column, ok := written.Host().(*Column)
	if !ok {
		return value.Fail(value.InvalidArgumentCode, "foreign nhận một cột của bảng khác: foreign(users.id), hay foreign(() => employees.id) khi trỏ vào chính nó")
	}
	field := &Field{table: tableWords{foreign: &reference{target: column.owner, field: column.name}}}
	if failure := field.takeKind(column.owner.field(column.name), column.name); failure.IsInvalid() {
		return failure
	}
	return value.Struct(field)
}

// takeKind makes f, a foreign key, of its target's kind: id() an integer, id("uuid") a uuid, id("kitwork") a
// string; a key or unique column its own kind. The target must be a key or a unique column.
func (f *Field) takeKind(target *Field, name string) value.Value {
	if target == nil {
		return value.Fail(value.InvalidArgumentCode, "foreign: struct không có trường "+name)
	}
	if !target.table.key && !(target.table.unique && target.table.group == "") {
		return value.Fail(value.InvalidArgumentCode, "foreign("+name+"): khoá ngoại trỏ tới khoá (id(…), .key()) hay một cột .unique(), không tới cột thường hay một cột chỉ duy nhất trong nhóm")
	}
	switch target.kind {
	case kindID:
		switch target.table.generate {
		case "number":
			f.kind = kindInteger
		case "uuid":
			f.kind = kindUUID
		default:
			f.kind = kindString
		}
	default:
		f.kind, f.scale, f.choices = target.kind, target.scale, target.choices
	}
	return value.Value{}
}

// Resolve gives a lazy foreign key its target, when kitwork/database declares the table: the function is
// called once, as the site boots. Go's only.
func (f *Field) Resolve() value.Value {
	ref := f.table.foreign
	if ref == nil || ref.lazy.IsUndefined() || ref.target != nil {
		return value.Value{}
	}
	got := ref.lazy.Call(nil)
	if got.IsInvalid() {
		return got
	}
	column, ok := got.Host().(*Column)
	if !ok {
		return value.Fail(value.InvalidArgumentCode, "foreign(() => …) trả một cột của bảng: foreign(() => employees.id)")
	}
	ref.target, ref.field = column.owner, column.name
	return f.takeKind(column.owner.field(column.name), column.name)
}

// Key is .key(): a natural key, given by whoever writes the row, not made by the engine: a code, a slug, the
// key of a table made elsewhere (code text PRIMARY KEY). A UUID key is id("uuid").
func (f *Field) Key(arguments ...value.Value) value.Value {
	switch {
	case len(arguments) != 0:
		return value.Fail(value.InvalidArgumentCode, ".key() không nhận đối số")
	case f.kind == kindID:
		return value.Fail(value.InvalidArgumentCode, "id(…) đã là khoá: bỏ .key()")
	case f.kind == kindUUID:
		// one way for a key that is a UUID: id("uuid") makes one, and an insert may bring one made elsewhere;
		// uuid() keeps its one meaning, a UUID of another system (06/11/2026)
		return value.Fail(value.InvalidArgumentCode, "khoá là UUID viết id(\"uuid\"): engine sinh UUIDv7, insert mang sẵn id cũng được; uuid() là cột giữ UUID của nơi khác")
	case f.kind != kindString && f.kind != kindInteger && f.kind != kindChoice && f.kind != kindDate:
		return value.Fail(value.InvalidArgumentCode, ".key() đi sau string(), integer(), choice() hay date(), không sau "+f.spelling())
	case f.nullable:
		return value.Fail(value.InvalidArgumentCode, "khoá không nhận null: bỏ .nullable()")
	}
	next := f.step()
	next.table.key = true
	return value.Struct(next)
}

// Unique is .unique(), or .unique("name") on several columns, unique together, .unique("name", position) to
// pin the order of the group.
func (f *Field) Unique(arguments ...value.Value) value.Value {
	next := f.step()
	switch len(arguments) {
	case 0:
		next.table.unique = true
	case 1, 2:
		name, position, failure := groupName(".unique", arguments)
		if failure.IsInvalid() {
			return failure
		}
		next.table.group, next.table.groupAt = name, position
	default:
		return value.Fail(value.InvalidArgumentCode, ".unique nhận không gì, hay tên nhóm và vị trí: .unique(\"linkDay\")")
	}
	return value.Struct(next)
}

// Index is .index(), or .index("name") on several columns, a composite index in the order written,
// .index("name", position) to pin it. A partial index, .index("name", (table) => condition), waits for the
// translation of conditions.
func (f *Field) Index(arguments ...value.Value) value.Value {
	if len(arguments) == 2 && arguments[1].IsFunction() {
		return value.Fail(value.UnsupportedCode, "chỉ mục một phần .index(\"tên\", (bảng) => …) chưa có trong engine2: chờ câu lệnh của kitwork/database")
	}
	next := f.step()
	next.table.index = true
	if len(arguments) > 0 {
		name, position, failure := groupName(".index", arguments)
		if failure.IsInvalid() {
			return failure
		}
		next.table.indexName, next.table.indexAt = name, position
	}
	return value.Struct(next)
}

// groupName reads ("name") or ("name", position) of .unique and .index.
func groupName(word string, arguments []value.Value) (string, int, value.Value) {
	if len(arguments) > 2 || !arguments[0].IsString() || arguments[0].Text() == "" {
		return "", 0, value.Fail(value.InvalidArgumentCode, word+" nhận tên viết bằng chữ, rồi vị trí: "+word+"(\"tên\", 1)")
	}
	position := 0
	if len(arguments) == 2 {
		n := arguments[1].Number()
		if !arguments[1].IsNumber() || n < 1 || n != float64(int(n)) {
			return "", 0, value.Fail(value.InvalidArgumentCode, word+"(\"tên\", vị_trí): vị trí là số nguyên từ 1")
		}
		position = int(n)
	}
	return arguments[0].Text(), position, value.Value{}
}

// Searchable is .searchable(): the full-text index matches reads, on a string.
func (f *Field) Searchable(arguments ...value.Value) value.Value {
	if f.kind != kindString {
		return value.Fail(value.InvalidArgumentCode, ".searchable() chỉ đi sau string(), không sau "+f.spelling())
	}
	if len(arguments) != 0 {
		return value.Fail(value.InvalidArgumentCode, ".searchable() không nhận đối số")
	}
	next := f.step()
	next.table.searchable = true
	return value.Struct(next)
}

// Created is timestamp().created(): the moment of the insert, which the engine fills.
func (f *Field) Created(arguments ...value.Value) value.Value {
	return f.stamp(".created()", arguments)
}

// Updated is timestamp().updated(): the moment of the insert and of every update.
func (f *Field) Updated(arguments ...value.Value) value.Value {
	return f.stamp(".updated()", arguments)
}

func (f *Field) stamp(word string, arguments []value.Value) value.Value {
	if f.kind != kindTimestamp {
		return value.Fail(value.InvalidArgumentCode, word+" chỉ đi sau timestamp(), không sau "+f.spelling())
	}
	if len(arguments) != 0 {
		return value.Fail(value.InvalidArgumentCode, word+" không nhận đối số")
	}
	next := f.step()
	if word == ".created()" {
		next.table.created = true
	} else {
		next.table.updated = true
	}
	return value.Struct(next)
}

// Readonly is .readonly(): set once at insert, never changed (docs/schema.md mục 5).
func (f *Field) Readonly(arguments ...value.Value) value.Value {
	if len(arguments) != 0 {
		return value.Fail(value.InvalidArgumentCode, ".readonly() không nhận đối số")
	}
	if f.generated() {
		return value.Fail(value.InvalidArgumentCode, f.spelling()+" do engine điền, đã không sửa được: bỏ .readonly()")
	}
	next := f.step()
	next.table.readonly = true
	return value.Struct(next)
}

// Secret is .secret(): a column that never leaves the server (docs/schema.md mục 5).
func (f *Field) Secret(arguments ...value.Value) value.Value {
	if len(arguments) != 0 {
		return value.Fail(value.InvalidArgumentCode, ".secret() không nhận đối số")
	}
	if f.table.key {
		return value.Fail(value.InvalidArgumentCode, "khoá không là .secret(): khoá là thứ nhận ra dòng")
	}
	next := f.step()
	next.table.secret = true
	return value.Struct(next)
}

// Cascade is foreign(…).cascade(): deleting the row pointed at deletes the rows pointing at it.
func (f *Field) Cascade(arguments ...value.Value) value.Value {
	if f.table.foreign == nil {
		return value.Fail(value.InvalidArgumentCode, ".cascade() chỉ đi sau foreign(…)")
	}
	if len(arguments) != 0 {
		return value.Fail(value.InvalidArgumentCode, ".cascade() không nhận đối số")
	}
	next := f.step()
	next.table.cascade = true
	return value.Struct(next)
}

// generated is a field the engine fills, which parse never takes: a key id(…) makes, .created(), .updated().
func (f *Field) generated() bool {
	return f.table.generate != "" || f.table.created || f.table.updated
}

// tableWord is the first word of a table the field is written with, "" for none.
func (f *Field) tableWord() string {
	t := f.table
	switch {
	case f.kind == kindID:
		return "id()"
	case t.foreign != nil:
		return "foreign(…)"
	case t.key:
		return ".key()"
	case t.unique || t.group != "":
		return ".unique()"
	case t.index:
		return ".index()"
	case t.searchable:
		return ".searchable()"
	case t.created:
		return ".created()"
	case t.updated:
		return ".updated()"
	case t.readonly:
		return ".readonly()"
	case t.secret:
		return ".secret()"
	}
	return ""
}

// Column is a field read off its struct, users.id: what foreign takes. It is no field to write with.
type Column struct {
	owner *Struct
	name  string
}

// Freeze is what value.Freeze calls on a column a site's file read as it booted: nothing writes into one.
func (c *Column) Freeze() {}

// MissingMember is any word after users.id: a column of another struct is pointed at with foreign(…), and a
// modifier written after foreign(…) is the new column's.
func (c *Column) MissingMember(name string) value.Value {
	return value.Fail(value.TypeCode, "…"+c.name+"."+name+": "+c.name+" là cột của một struct khác; khoá ngoại viết foreign(bảng."+c.name+"), bổ nghĩa đặt sau foreign(…): foreign(bảng."+c.name+")."+name+"()")
}

// TableField is what kitwork/database reads of one field of a struct it makes a table. Go's only.
type TableField struct {
	Name     string
	Kind     string // "id" for a key the engine makes, else the builder's kind
	Generate string // id(…)'s: "number", "uuid", "kitwork"
	Scale    int
	Choices  []string
	// Nested is a struct or an array written as a field, which a table holds only through json(…)
	Nested                                                     bool
	Nullable, HasDefault                                       bool
	Key, Unique, Index, Searchable, Created, Updated, Readonly bool
	Secret, Cascade                                            bool
	Group, IndexName                                           string
	GroupAt, IndexAt                                           int
	Transforms                                                 int
	Default                                                    value.Value // .default(…)'s, when HasDefault
	ForeignTarget                                              *Struct
	ForeignField                                               string
	field                                                      *Field
}

// Resolve is the field's lazy foreign key resolved (Field.Resolve), its kind then known.
func (t *TableField) Resolve() value.Value {
	if failure := t.field.Resolve(); failure.IsInvalid() {
		return failure
	}
	if ref := t.field.table.foreign; ref != nil {
		t.ForeignTarget, t.ForeignField, t.Kind = ref.target, ref.field, t.field.kind
		t.Scale, t.Choices = t.field.scale, t.field.choices
	}
	return value.Value{}
}

// Check is a value written into the column held to the field's rules, as .parse() holds it (read.go): the value
// kept (a UUID in lower case), or what is wrong with it, never the value itself. null is the caller's to judge.
// Go's only (kitwork/database's writes).
func (t TableField) Check(v value.Value) (kept value.Value, rule, problem string) {
	if t.field == nil {
		return v, "", ""
	}
	return t.field.check(v)
}

// TransformFunctions are the field's transforms, in the order written: kitwork/database reads a table's as the
// steps they take (library/database, transform.go). Go's only.
func (t TableField) TransformFunctions() []value.Value {
	if t.field == nil {
		return nil
	}
	return t.field.transforms
}

// TableFields are the struct's fields as kitwork/database reads them, in the order written.
func (s *Struct) TableFields() []TableField {
	out := make([]TableField, len(s.fields))
	for i, named := range s.fields {
		f := named.field
		t := f.table
		out[i] = TableField{
			Name: named.name, Kind: f.kind, Generate: t.generate, Scale: f.scale, Choices: f.choices,
			Nested:   f.kind == kindStruct || f.kind == kindArray,
			Nullable: f.nullable, HasDefault: f.hasDefault,
			Key: t.key, Unique: t.unique, Index: t.index, Searchable: t.searchable, Created: t.created,
			Updated: t.updated, Readonly: t.readonly, Secret: t.secret, Cascade: t.cascade,
			Group: t.group, IndexName: t.indexName, GroupAt: t.groupAt, IndexAt: t.indexAt,
			Transforms: len(f.transforms), Default: f.def, field: f,
		}
		if t.foreign != nil {
			out[i].ForeignTarget, out[i].ForeignField = t.foreign.target, t.foreign.field
		}
	}
	return out
}

// TableWord is the first word of a table the struct is written with ("customers.email: .unique()"), "" when it
// has none: a struct with one is a table, or an error. Go's only.
func (s *Struct) TableWord() string {
	for _, named := range s.fields {
		if word := named.field.tableWord(); word != "" {
			return named.name + ": " + word
		}
	}
	return ""
}

// keys counts the struct's keys, id(…) and .key(): at most one, and a table exactly one.
func (s *Struct) keys() []string {
	var names []string
	for _, named := range s.fields {
		if named.field.table.key {
			names = append(names, named.name)
		}
	}
	return names
}
