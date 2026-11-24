package javascript

import (
	"encoding/base64"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/huynhnhanquoc/engine/core/value"
)

// JSON của JavaScript: JSON.parse và JSON.stringify, theo JavaScript đúng từng chữ, đối chiếu với node. Loại Kitwork
// có mà JavaScript không có: thời điểm viết như toISOString (toJSON của Date), khoảng thời gian bằng chữ của nó
// ("1h30m0s"), bytes bằng base64; một struct của engine (Math, một Website) không phải dữ liệu, viết nó là lỗi
// (JavaScript viết Math thành "{}"). Chép từ engine2 (core/value/json.go, core/builtins/json.go); bộ nhớ của lượt chạy đếm qua
// Meter (meter.go), năng lượng chưa; chữ viết ra không dài quá maxStringLength.
var jsonNamespace = newNamespace("JSON",
	member{"parse", native("parse", 2, jsonParse)},
	member{"stringify", native("stringify", 3, jsonStringify)},
)

// ---- parse ----

// jsonParse là JSON.parse(text, reviver): text đổi thành chữ trước (JSON.parse(5) là 5); reviver là một hàm thì nó đi
// qua cây vừa đọc (revive).
func jsonParse(run Run, arguments []value.Value) value.Value {
	text, err := toText(run, argument(arguments, 0))
	if err != nil {
		return value.Error(err)
	}
	if failure := spend(run, uint64(len(text))*jsonReadUnits); failure.IsInvalid() {
		return failure
	}
	parsed, err := parseJSON(run, text)
	if err != nil {
		return value.Error(err)
	}
	if reviver := argument(arguments, 1); reviver.IsFunction() {
		defer letGo(run, keep(run, Kept{Items: &[]value.Value{parsed}})) // cây vừa đọc: runtime thấy nó khi reviver chạy
		return revive(run, reviver, "", parsed)
	}
	return parsed
}

// revive là reviver đi qua cây JSON.parse đọc ra, như InternalizeJSONProperty của JavaScript: sâu trước; mỗi phần tử
// của một mảng (tới độ dài mảng có khi tới nó) và mỗi khoá của một object (các khoá nó có khi tới nó) thay bằng
// reviver(khoá, giá trị), khoá mà reviver cho undefined thì gỡ khỏi object; cuối cùng là chính giá trị dưới khoá của
// nó ("" cho cả cây). Một phần tử mảng mà reviver cho undefined thì JavaScript xoá thành một lỗ: mảng giữ undefined ở
// đó và mang dấu lỗ (holes.go). Lỗi của reviver dừng cuộc đi và là kết quả.
func revive(run Run, reviver value.Value, key string, item value.Value) value.Value {
	switch {
	case item.IsArray():
		length := item.Length()
		for i := 0; i < length; i++ {
			revived := revive(run, reviver, strconv.Itoa(i), item.Index(i))
			if revived.IsInvalid() {
				return revived
			}
			if written := item.SetIndex(i, revived); written.IsInvalid() {
				return written // reviver làm mảng ngắn đi: chỗ i giờ là một lỗ, Kitwork không có
			}
			if revived.IsUndefined() {
				item.WithHoles() // JavaScript xoá phần tử ấy: một lỗ
			}
		}
	case item.IsObject():
		for _, k := range item.Keys() {
			child, _ := item.Get(k)
			revived := revive(run, reviver, k, child)
			switch {
			case revived.IsInvalid():
				return revived
			case revived.IsUndefined():
				item.Delete(k)
			default:
				item.Set(k, revived)
			}
		}
	}
	return call(run, reviver, value.String(key), item)
}

// maxJSONDepth là độ lồng sâu nhất JSON.parse đọc: một chuỗi mười nghìn "[" là lỗi, không phải tràn ngăn xếp.
const maxJSONDepth = 512

// parseJSON đọc text là một giá trị JSON, khoảng trắng hai bên được bỏ qua; lỗi nêu byte nó dừng. Cây đọc ra làm từ
// ít ra nhiều ("[0,0,…]" hai byte một phần tử thành một Value): hỏi run chỗ khi cây lớn dần, đếm cả cây khi xong.
func parseJSON(run Run, text string) (value.Value, error) {
	p := jsonParser{text: text, g: grower{run: run}}
	p.skipSpace()
	v, err := p.value(0)
	if err != nil {
		return value.Value{}, err
	}
	p.skipSpace()
	if p.at < len(p.text) {
		return value.Value{}, p.fail("unexpected text after the value")
	}
	if failure := made(run, p.size); failure.IsInvalid() {
		return value.Value{}, failure.Error()
	}
	return v, nil
}

type jsonParser struct {
	text string
	at   int
	g    grower
	size uint64 // byte của cây đã đọc: mảng, object, phần tử, khoá, chữ
	// keys, items: khoá và giá trị của các object đang đọc, object trong ở sau object ngoài; một object đọc xong thì
	// được dựng một lần từ phần của nó, đúng kích thước, không lớn dần qua từng khoá
	keys  []string
	items []value.Value
	// shapes: khoá của các object lần đọc này đã dựng, để object sau cùng khoá mượn chúng (jsonShapes)
	shapes map[jsonShapeKey]*jsonShapes
}

// Một lần JSON.parse nhớ khoá của các object nó đã dựng, theo khoá đầu và số khoá (24/11/2026): object sau có đúng các
// khoá ấy, cùng thứ tự, thì mượn chúng thay vì giữ một mảng khoá và một chỉ mục của riêng nó. Một mảng các dòng cùng cột
// (dòng của một database, kết quả của một API) là chỗ hay gặp nhất. Object đầu tiên giữ khoá của riêng nó; khi object
// thứ hai cùng khoá tới, khoá của object đầu thành một Shape (value.Value.Shape) và từ đó mọi object cùng khoá mượn nó:
// một object có khoá không lặp lại không tốn thêm gì. Mỗi loại (khoá đầu, số khoá) nhớ jsonShapesPerKind bộ khoá, để
// hai loại object xen nhau (dòng và chủ của dòng, cùng bắt đầu bằng "id") không đuổi nhau; nhớ quá jsonShapesKinds loại
// thì quên hết và nhớ lại từ đầu, nên một văn bản muôn kiểu object không giữ một bộ nhớ đệm lớn.
const (
	jsonShapesPerKind = 4
	jsonShapesKinds   = 256
)

type jsonShapeKey struct {
	first string
	count int
}

type jsonShapes struct {
	known [jsonShapesPerKind]jsonShape
	next  int // chỗ sẽ thay khi đã đủ
}

type jsonShape struct {
	keys  []string     // khoá của object đầu tiên có chúng
	first value.Value  // object ấy, tới khi khoá của nó thành shape
	shape *value.Shape // nil tới khi object thứ hai cùng khoá tới
}

// borrowed là Shape cho các khoá ấy nếu lần đọc này đã dựng một object có đúng chúng (khoá của object ấy thành Shape lúc
// này nếu chưa); nil khi chưa.
func (p *jsonParser) borrowed(keys []string) *value.Shape {
	kind := p.shapes[jsonShapeKey{keys[0], len(keys)}]
	if kind == nil {
		return nil
	}
	for i := range kind.known {
		known := &kind.known[i]
		if known.keys == nil || !slices.Equal(known.keys, keys) {
			continue
		}
		if known.shape == nil {
			known.shape, known.first = known.first.Shape(), value.Value{}
		}
		return known.shape
	}
	return nil
}

// remember nhớ khoá của object o vừa dựng (khoá của riêng nó, không trùng), cho các object sau.
func (p *jsonParser) remember(keys []string, o value.Value) {
	key := jsonShapeKey{keys[0], len(keys)}
	kind := p.shapes[key]
	if kind == nil {
		if p.shapes == nil || len(p.shapes) >= jsonShapesKinds {
			p.shapes = make(map[jsonShapeKey]*jsonShapes)
		}
		kind = &jsonShapes{}
		p.shapes[key] = kind
	}
	kind.known[kind.next] = jsonShape{keys: keys, first: o}
	kind.next = (kind.next + 1) % jsonShapesPerKind
}

// grew thêm n byte vào cây đã đọc và hỏi chỗ cho nó.
func (p *jsonParser) grew(n uint64) error {
	p.size += n
	if failure := p.g.fits(p.size); failure.IsInvalid() {
		return failure.Error()
	}
	return nil
}

func (p *jsonParser) fail(what string) error {
	return value.Errorf(value.InvalidInputCode, "JSON.parse: %s at byte %d", what, p.at)
}

// skipSpace bỏ qua khoảng trắng của JSON: dấu cách, tab, xuống dòng, về đầu dòng; không gì khác.
func (p *jsonParser) skipSpace() {
	for p.at < len(p.text) {
		switch p.text[p.at] {
		case ' ', '\t', '\n', '\r':
			p.at++
		default:
			return
		}
	}
}

func (p *jsonParser) value(depth int) (value.Value, error) {
	if depth >= maxJSONDepth { // depth đếm từ 0: mảng thứ 513 lồng nhau ở depth 512
		return value.Value{}, p.fail("nesting deeper than 512")
	}
	if p.at >= len(p.text) {
		return value.Value{}, p.fail("unexpected end")
	}
	switch c := p.text[p.at]; {
	case c == '{':
		return p.object(depth)
	case c == '[':
		return p.array(depth)
	case c == '"':
		s, err := p.string()
		if err == nil {
			err = p.grew(uint64(len(s)))
		}
		return value.String(s), err
	case c == '-' || c >= '0' && c <= '9':
		return p.number()
	case p.literal("true"):
		return value.Boolean(true), nil
	case p.literal("false"):
		return value.Boolean(false), nil
	case p.literal("null"):
		return value.Null(), nil
	}
	r, _ := utf8.DecodeRuneInString(p.text[p.at:])
	return value.Value{}, p.fail(fmt.Sprintf("unexpected character %q", r))
}

func (p *jsonParser) literal(word string) bool {
	if strings.HasPrefix(p.text[p.at:], word) {
		p.at += len(word)
		return true
	}
	return false
}

// number đọc một số của JSON: dấu trừ, phần nguyên không có số 0 đứng đầu, phần lẻ, số mũ. Số quá lớn là vô cực, như
// JavaScript.
func (p *jsonParser) number() (value.Value, error) {
	start := p.at
	if p.text[p.at] == '-' {
		p.at++
	}
	digits := func() int {
		from := p.at
		for p.at < len(p.text) && p.text[p.at] >= '0' && p.text[p.at] <= '9' {
			p.at++
		}
		return p.at - from
	}
	wholeStart := p.at
	whole := digits()
	if whole == 0 || whole > 1 && p.text[wholeStart] == '0' {
		return value.Value{}, p.fail("bad number")
	}
	if p.at < len(p.text) && p.text[p.at] == '.' {
		p.at++
		if digits() == 0 {
			return value.Value{}, p.fail("bad number")
		}
	}
	if p.at < len(p.text) && (p.text[p.at] == 'e' || p.text[p.at] == 'E') {
		p.at++
		if p.at < len(p.text) && (p.text[p.at] == '+' || p.text[p.at] == '-') {
			p.at++
		}
		if digits() == 0 {
			return value.Value{}, p.fail("bad number")
		}
	}
	f, _ := strconv.ParseFloat(p.text[start:p.at], 64) // lỗi duy nhất có thể là ngoài khoảng: f đã là vô cực hay 0
	return value.Number(f), nil
}

// string đọc một chuỗi trong nháy kép cùng các thoát của nó. Hai thoát u liền nhau làm thành một cặp thì gộp thành một
// ký tự; một nửa cặp đứng một mình giữ dạng ba byte (WTF-8), như mọi chuỗi của Kitwork.
func (p *jsonParser) string() (string, error) {
	p.at++ // nháy mở
	var b []byte
	start := p.at
	for p.at < len(p.text) {
		c := p.text[p.at]
		switch {
		case c == '"':
			if b == nil {
				s := p.text[start:p.at]
				p.at++
				return s, nil
			}
			b = append(b, p.text[start:p.at]...)
			p.at++
			return mergeSurrogates(string(b)), nil
		case c < 0x20:
			return "", p.fail("control character in a string")
		case c == '\\':
			b = append(b, p.text[start:p.at]...)
			p.at++
			if p.at >= len(p.text) {
				return "", p.fail("unterminated string")
			}
			switch p.text[p.at] {
			case '"', '\\', '/':
				b = append(b, p.text[p.at])
			case 'b':
				b = append(b, '\b')
			case 'f':
				b = append(b, '\f')
			case 'n':
				b = append(b, '\n')
			case 'r':
				b = append(b, '\r')
			case 't':
				b = append(b, '\t')
			case 'u':
				unit, ok := p.hex4(p.at + 1)
				if !ok {
					return "", p.fail("bad unicode escape")
				}
				p.at += 4
				if unit >= 0xD800 && unit < 0xDC00 && p.at+6 < len(p.text) && p.text[p.at+1] == '\\' && p.text[p.at+2] == 'u' {
					if low, ok := p.hex4(p.at + 3); ok && low >= 0xDC00 && low < 0xE000 {
						b = append(b, encodeUnits([]uint16{unit, low})...)
						p.at += 6
						break
					}
				}
				b = appendUnit(b, unit)
			default:
				return "", p.fail("bad escape")
			}
			p.at++
			start = p.at
		default:
			p.at++
		}
	}
	return "", p.fail("unterminated string")
}

func (p *jsonParser) hex4(at int) (uint16, bool) {
	if at+4 > len(p.text) {
		return 0, false
	}
	var unit uint16
	for i := range 4 {
		d := digitValue(p.text[at+i])
		if d >= 16 {
			return 0, false
		}
		unit = unit<<4 | uint16(d)
	}
	return unit, true
}

// array đọc một mảng: các phần tử gom ở p.items sau phần của cái đang đọc bên ngoài, đọc xong thì dựng mảng một lần,
// đúng kích thước (24/11/2026; trước đó Push từng phần tử: ["a","b"] ba lần cấp phát, một mảng lớn lớn dần qua từng lần
// chép), rồi trả chỗ.
func (p *jsonParser) array(depth int) (value.Value, error) {
	p.at++ // [
	if err := p.grew(value.MadeBytes); err != nil {
		return value.Value{}, err
	}
	p.skipSpace()
	if p.at < len(p.text) && p.text[p.at] == ']' {
		p.at++
		return value.Array(), nil
	}
	start := len(p.items)
	for {
		p.skipSpace()
		item, err := p.value(depth + 1)
		if err != nil {
			return value.Value{}, err
		}
		p.items = append(p.items, item)
		if err := p.grew(value.ItemBytes); err != nil {
			return value.Value{}, err
		}
		p.skipSpace()
		if p.at >= len(p.text) {
			return value.Value{}, p.fail("unexpected end")
		}
		switch p.text[p.at] {
		case ',':
			p.at++
		case ']':
			p.at++
			a := value.Array(p.items[start:]...) // bản chép đúng kích thước
			p.items = p.items[:start]
			return a, nil
		default:
			return value.Value{}, p.fail("expected , or ]")
		}
	}
}

// object đọc một object; một khoá viết hai lần giữ giá trị sau ở chỗ của lần đầu, như JavaScript. Khoá và giá trị gom
// ở p.keys, p.items; đọc xong thì dựng (build). Hai ngăn có chỗ bắt đầu riêng: một mảng bên ngoài gom giá trị mà không
// gom khoá, nên trong nó hai ngăn dài khác nhau.
func (p *jsonParser) object(depth int) (value.Value, error) {
	p.at++ // {
	if err := p.grew(value.MadeBytes); err != nil {
		return value.Value{}, err
	}
	p.skipSpace()
	if p.at < len(p.text) && p.text[p.at] == '}' {
		p.at++
		return value.Object(), nil
	}
	keyStart, itemStart := len(p.keys), len(p.items)
	for {
		p.skipSpace()
		if p.at >= len(p.text) || p.text[p.at] != '"' {
			return value.Value{}, p.fail("expected a key")
		}
		key, err := p.string()
		if err != nil {
			return value.Value{}, err
		}
		p.skipSpace()
		if p.at >= len(p.text) || p.text[p.at] != ':' {
			return value.Value{}, p.fail("expected :")
		}
		p.at++
		p.skipSpace()
		item, err := p.value(depth + 1) // một object bên trong gom sau phần của object này rồi trả chỗ
		if err != nil {
			return value.Value{}, err
		}
		p.keys, p.items = append(p.keys, key), append(p.items, item)
		if err := p.grew(value.ItemBytes); err != nil {
			return value.Value{}, err
		}
		p.skipSpace()
		if p.at >= len(p.text) {
			return value.Value{}, p.fail("unexpected end")
		}
		switch p.text[p.at] {
		case ',':
			p.at++
		case '}':
			p.at++
			return p.build(keyStart, itemStart)
		default:
			return value.Value{}, p.fail("expected , or }")
		}
	}
}

// build dựng object từ khoá gom từ keyStart và giá trị gom từ itemStart rồi trả chỗ ấy: mượn khoá của một object trước cùng khoá
// (borrowed), hay giữ khoá của riêng nó và nhớ chúng cho các object sau (remember). Khoá của riêng nó mới tính vào cây
// (giá trị đã tính khi đọc); khoá trùng thì dựng bằng Set như JavaScript, không nhớ.
func (p *jsonParser) build(keyStart, itemStart int) (value.Value, error) {
	keys, items := p.keys[keyStart:], slices.Clone(p.items[itemStart:])
	p.keys, p.items = p.keys[:keyStart], p.items[:itemStart] // keys còn đọc được tới lần gom sau: build không gom gì
	if shape := p.borrowed(keys); shape != nil {
		return value.ObjectFrom(shape, items), nil
	}
	keyBytes := uint64(len(keys)) * (value.KeyBytes - value.ItemBytes)
	for _, key := range keys {
		keyBytes += uint64(len(key))
	}
	if err := p.grew(keyBytes); err != nil {
		return value.Value{}, err
	}
	own := slices.Clone(keys)
	if o, ok := value.ObjectOf(own, items); ok {
		p.remember(own, o)
		return o, nil
	}
	o := value.Object()
	for i, key := range keys {
		o.Set(key, items[i])
	}
	return o, nil
}

// ---- stringify ----

// jsonStringify là JSON.stringify(item, replacer, space). replacer là một hàm thì được gọi với từng khoá và giá trị;
// là một mảng thì chuỗi và số trong nó là các khoá object được viết, theo thứ tự ấy; khác thì không có. space là một số
// thì là chừng ấy dấu cách (tối đa 10), một chuỗi thì 10 đơn vị đầu của nó, khác thì không thụt lề. Kết quả là
// undefined khi item không có JSON (undefined, một hàm).
func jsonStringify(run Run, arguments []value.Value) value.Value {
	item, replacer := argument(arguments, 0), argument(arguments, 1)
	w := jsonWriter{run: run, indent: jsonIndent(argument(arguments, 2)), g: grower{run: run}}
	switch {
	case replacer.IsFunction():
		w.replacer = replacer
	case replacer.IsArray():
		w.keys, w.listed = listedKeys(replacer), true
	}
	written, err := w.property(jsonKey{index: -1}, item)
	if err == nil {
		err = w.spendWritten() // phần cuối, dưới jsonSpendStep
	}
	switch {
	case err != nil:
		return value.Error(err)
	case !written:
		return value.Undefined()
	}
	return value.String(string(w.b))
}

// jsonIndent là space của stringify như JavaScript đọc: số thì phần nguyên của nó, tối đa 10, dưới 1 là không; chuỗi
// thì 10 đơn vị đầu; khác là không.
func jsonIndent(space value.Value) string {
	switch {
	case space.IsNumber():
		n := toIntegerOrInfinity(space.Number())
		if n < 1 {
			return ""
		}
		return strings.Repeat(" ", int(min(10, n)))
	case space.IsString():
		s := space.String()
		return unitSlice(s, 0, min(10, unitLength(s)))
	}
	return ""
}

// listedKeys là một mảng thay như JavaScript đọc: chuỗi và số trong nó (số viết thành chữ) là khoá, theo thứ tự, mỗi
// khoá một lần; thứ khác bị bỏ qua.
func listedKeys(replacer value.Value) []string {
	keys := []string{}
	seen := map[string]bool{}
	for i := range replacer.Length() {
		var key string
		switch item := replacer.Index(i); {
		case item.IsString():
			key = item.String()
		case item.IsNumber():
			key = numberText(item.Number())
		default:
			continue
		}
		if !seen[key] {
			seen[key] = true
			keys = append(keys, key)
		}
	}
	return keys
}

// jsonKey là khoá một giá trị nằm dưới: tên của nó trong một object, hay chỉ số của nó trong một mảng (index >= 0),
// viết thành chữ chỉ khi toJSON hay hàm thay cần.
type jsonKey struct {
	name  string
	index int
}

func (k jsonKey) text() value.Value {
	if k.index >= 0 {
		return value.String(strconv.Itoa(k.index))
	}
	return value.String(k.name)
}

// jsonWriter viết một giá trị thành JSON.
type jsonWriter struct {
	run      Run
	replacer value.Value   // hàm thay, hay undefined
	keys     []string      // khoá của mảng thay, khi listed
	listed   bool          // có mảng thay: object chỉ viết các khoá trong keys
	indent   string        // một bậc thụt lề
	prefix   string        // thụt lề của bậc đang viết
	active   []value.Value // mảng và object đang viết ở trên: gặp lại một cái là vòng lặp
	b        []byte
	g        grower // hỏi lượt chạy chỗ khi b lớn dần: một mảng nhắc lại một object lớn viết ra nhiều hơn nó giữ
	spent    int    // số byte của b đã tính năng lượng (jsonWriteUnits)
}

// property viết item nằm dưới key như SerializeJSONProperty của JavaScript: toJSON riêng của một object trước (thời
// điểm, khoảng thời gian, bytes thành chữ của chúng ở bước này, như toJSON của Date), rồi hàm thay, rồi viết. false khi
// không có gì để viết (undefined, một hàm): object bỏ khoá ấy, mảng viết null.
func (w *jsonWriter) property(key jsonKey, item value.Value) (bool, error) {
	switch {
	case item.IsObject():
		if toJSON, found := item.Get("toJSON"); found && toJSON.IsFunction() {
			if item = call(w.run, toJSON, key.text()); item.IsInvalid() {
				return false, item.Error()
			}
		}
	case item.IsTime(), item.IsDuration(), item.IsBytes():
		item = value.String(jsonText(item))
	}
	if w.replacer.IsFunction() {
		if item = call(w.run, w.replacer, key.text(), item); item.IsInvalid() {
			return false, item.Error()
		}
	}
	switch {
	case item.IsNull():
		w.b = append(w.b, "null"...)
	case item.IsBoolean():
		w.b = strconv.AppendBool(w.b, item.Boolean())
	case item.IsNumber():
		if n := item.Number(); n != n || math.IsInf(n, 0) {
			w.b = append(w.b, "null"...)
		} else {
			w.b = appendNumber(w.b, n)
		}
	case item.IsString():
		return true, w.quote(item.String())
	case item.IsTime(), item.IsDuration(), item.IsBytes(): // hàm thay trả một thời điểm: viết như toJSON đã viết
		return true, w.quote(jsonText(item))
	case item.IsArray():
		return true, w.array(item)
	case item.IsObject():
		return true, w.object(item)
	case item.IsInvalid():
		return false, item.Error()
	case item.IsStruct():
		return false, fmt.Errorf("JSON.stringify: %w", notData(item))
	default: // undefined, một hàm
		return false, nil
	}
	return true, nil
}

// jsonText là chữ JSON viết cho một loại của Kitwork: thời điểm như toISOString, khoảng thời gian như chữ của nó,
// bytes bằng base64.
func jsonText(item value.Value) string {
	switch {
	case item.IsTime():
		return isoText(item.Time())
	case item.IsDuration():
		return item.Duration().String()
	}
	return base64.StdEncoding.EncodeToString(item.Bytes())
}

// enter ghi v là đang viết; v đã đang viết ở trên là vòng lặp, lỗi như JavaScript.
func (w *jsonWriter) enter(v value.Value) error {
	for _, outer := range w.active {
		if outer.Identical(v) {
			return value.Errorf(value.TypeCode, "converting circular structure to JSON")
		}
	}
	w.active = append(w.active, v)
	return nil
}

func (w *jsonWriter) leave() { w.active = w.active[:len(w.active)-1] }

func (w *jsonWriter) newline() {
	if w.indent != "" {
		w.b = append(w.b, '\n')
		w.b = append(w.b, w.prefix...)
	}
}

// grown là lỗi khi chữ đã viết dài quá maxStringLength.
func (w *jsonWriter) grown() error {
	if len(w.b) > maxStringLength {
		return tooLong().Error()
	}
	if failure := w.g.fits(uint64(len(w.b))); failure.IsInvalid() {
		return failure.Error()
	}
	if len(w.b)-w.spent >= jsonSpendStep {
		return w.spendWritten()
	}
	return nil
}

// jsonSpendStep là số byte JSON.stringify viết giữa hai lần tính năng lượng: tính mỗi phần tử tốn hơn chính việc viết
// một phần tử nhỏ.
const jsonSpendStep = 4 << 10

// spendWritten tính năng lượng cho các byte viết từ lần tính trước (jsonWriteUnits).
func (w *jsonWriter) spendWritten() error {
	written := len(w.b) - w.spent
	w.spent = len(w.b)
	if failure := spend(w.run, uint64(written)*jsonWriteUnits); failure.IsInvalid() {
		return failure.Error()
	}
	return nil
}

// array viết một mảng: độ dài đọc một lần lúc bắt đầu, như JavaScript; phần tử không có JSON viết null.
func (w *jsonWriter) array(a value.Value) error {
	if err := w.enter(a); err != nil {
		return err
	}
	defer w.leave()
	length := a.Length()
	if length == 0 {
		w.b = append(w.b, "[]"...)
		return nil
	}
	outer := w.prefix
	w.prefix += w.indent
	w.b = append(w.b, '[')
	for i := range length {
		if i > 0 {
			w.b = append(w.b, ',')
		}
		w.newline()
		written, err := w.property(jsonKey{index: i}, a.Index(i))
		if err != nil {
			return err
		}
		if !written {
			w.b = append(w.b, "null"...)
		}
		if err := w.grown(); err != nil {
			return err
		}
	}
	w.prefix = outer
	w.newline()
	w.b = append(w.b, ']')
	return nil
}

// object viết một object: các khoá theo thứ tự của Keys (hay của mảng thay), mỗi giá trị đọc khi tới lượt nó (một
// toJSON trước có thể đã sửa object); khoá mà giá trị không có JSON thì bỏ.
func (w *jsonWriter) object(o value.Value) error {
	if err := w.enter(o); err != nil {
		return err
	}
	defer w.leave()
	keys := w.keys
	if !w.listed {
		keys = o.ReadKeys() // chỉ đọc: khoá mượn của một object literal không chép (một toJSON sửa o thì o chép trước)
	}
	outer := w.prefix
	w.prefix += w.indent
	w.b = append(w.b, '{')
	written := 0
	for _, key := range keys {
		child, _ := o.Get(key)
		mark := len(w.b)
		if written > 0 {
			w.b = append(w.b, ',')
		}
		w.newline()
		if err := w.quote(key); err != nil {
			return err
		}
		w.b = append(w.b, ':')
		if w.indent != "" {
			w.b = append(w.b, ' ')
		}
		ok, err := w.property(jsonKey{name: key, index: -1}, child)
		if err != nil {
			return err
		}
		if !ok {
			w.b = w.b[:mark]
			continue
		}
		written++
		if err := w.grown(); err != nil {
			return err
		}
	}
	w.prefix = outer
	if written > 0 {
		w.newline()
	}
	w.b = append(w.b, '}')
	return nil
}

const hexDigits = "0123456789abcdef"

// quote viết s trong nháy kép như JSON.stringify: các thoát ngắn (nháy, gạch chéo ngược, b f n r t), ký tự điều khiển
// khác bằng thoát u00xx, một nửa cặp đứng một mình bằng thoát u của nó (chữ hex thường), để JSON viết ra luôn đúng
// UTF-8; một byte hỏng là U+FFFD, như Kitwork đọc nó.
func (w *jsonWriter) quote(s string) error {
	if len(w.b)+len(s)+2 > maxStringLength {
		return tooLong().Error()
	}
	b := append(w.b, '"')
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '"' || c == '\\':
			b = append(b, '\\', c)
		case c == '\n':
			b = append(b, '\\', 'n')
		case c == '\r':
			b = append(b, '\\', 'r')
		case c == '\t':
			b = append(b, '\\', 't')
		case c == '\b':
			b = append(b, '\\', 'b')
		case c == '\f':
			b = append(b, '\\', 'f')
		case c < 0x20:
			b = append(b, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0xF])
		case c < utf8.RuneSelf:
			b = append(b, c)
		default:
			if unit, ok := loneSurrogateAt(s, i); ok {
				b = append(b, '\\', 'u', hexDigits[unit>>12], hexDigits[unit>>8&0xF], hexDigits[unit>>4&0xF], hexDigits[unit&0xF])
				i += 3
				continue
			}
			r, size := utf8.DecodeRuneInString(s[i:])
			if r == utf8.RuneError && size == 1 {
				b = utf8.AppendRune(b, utf8.RuneError)
			} else {
				b = append(b, s[i:i+size]...)
			}
			i += size
			continue
		}
		i++
	}
	w.b = append(b, '"')
	return w.grown()
}
