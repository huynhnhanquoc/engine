package runtime_test

import (
	"math"
	"math/rand/v2"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/huynhnhanquoc/engine/core/compiler"
	"github.com/huynhnhanquoc/engine/core/runtime"
	"github.com/huynhnhanquoc/engine/core/syntax"
	"github.com/huynhnhanquoc/engine/core/value"
)

// Con trỏ chữ (core/javascript/cursor.go, 23/11/2026): s.length, s[i], slice, substring, startsWith, endsWith, at,
// charAt, charCodeAt, codePointAt đi bằng con trỏ và bộ nhớ chuỗi của lượt chạy. Kết quả phải đúng như đi từng đơn vị
// từ đầu: so với một bản tham chiếu viết riêng ở đây, trên chuỗi ngẫu nhiên đủ loại (ASCII, chữ có dấu hai và ba
// byte, emoji bốn byte, nửa cặp đứng một mình, byte hỏng, ký tự cụt), dài và ngắn, chỉ số tới và lui.

// ---- tham chiếu: đơn vị UTF-16 như unitReader, viết lại không dùng engine ----

// reference là các đơn vị của s, chỗ byte mỗi đơn vị bắt đầu (offsets[len] là cuối), và đơn vị nào là nửa sau của một
// ký tự bốn byte (mid: không byte nào chỉ được).
type reference struct {
	s       string
	units   []uint16
	offsets []int
	mid     []bool
}

func referenceOf(s string) reference {
	r := reference{s: s}
	for at := 0; at < len(s); {
		b := s[at]
		switch {
		case b < 0x80:
			r.add(uint16(b), at, false)
			at++
		case at+3 <= len(s) && b == 0xED && s[at+1] >= 0xA0 && s[at+1] <= 0xBF && s[at+2]&0xC0 == 0x80:
			r.add(0xD000|uint16(s[at+1]&0x3F)<<6|uint16(s[at+2]&0x3F), at, false)
			at += 3
		default:
			c, size := utf8.DecodeRuneInString(s[at:])
			if c >= 0x10000 {
				c -= 0x10000
				r.add(uint16(0xD800+c>>10), at, false)
				r.add(uint16(0xDC00+c&0x3FF), at, true)
			} else {
				r.add(uint16(c), at, false)
			}
			at += size
		}
	}
	r.offsets = append(r.offsets, len(s))
	r.mid = append(r.mid, false)
	return r
}

func (r *reference) add(unit uint16, at int, mid bool) {
	r.units, r.offsets, r.mid = append(r.units, unit), append(r.offsets, at), append(r.mid, mid)
}

// encode viết các đơn vị thành chuỗi: cặp thành ký tự bốn byte, nửa đứng một mình dạng ba byte.
func encode(u []uint16) string {
	var b []byte
	for i := 0; i < len(u); i++ {
		switch c := u[i]; {
		case c >= 0xD800 && c < 0xDC00 && i+1 < len(u) && u[i+1] >= 0xDC00 && u[i+1] < 0xE000:
			b = utf8.AppendRune(b, 0x10000+(rune(c)-0xD800)<<10+(rune(u[i+1])-0xDC00))
			i++
		case c >= 0xD800 && c <= 0xDFFF:
			b = append(b, 0xED, 0x80|byte(c>>6)&0x3F, 0x80|byte(c)&0x3F)
		default:
			b = utf8.AppendRune(b, rune(c))
		}
	}
	return string(b)
}

// slice là các đơn vị [start, end) đã kẹp: byte của chuỗi khi hai đầu đúng ranh giới ký tự, không thì viết lại.
func (r reference) slice(start, end int) string {
	if start >= end {
		return ""
	}
	if !r.mid[start] && !r.mid[end] {
		return r.s[r.offsets[start]:r.offsets[end]]
	}
	return encode(r.units[start:end])
}

func clamp(i, length int) int { return max(0, min(i, length)) }

func relative(i, length int) int {
	if i < 0 {
		return max(0, i+length)
	}
	return min(i, length)
}

// expected là kết quả tham chiếu của phép op trên s với các đối số (chuỗi chữ của một giá trị JSON-ish để so).
func (r reference) expected(op string, i, j int, x string) value.Value {
	n := len(r.units)
	unitText := func(k int) value.Value { return value.String(encode(r.units[k : k+1])) }
	switch op {
	case "length":
		return value.Number(float64(n))
	case "index":
		if i < 0 || i >= n {
			return value.Undefined()
		}
		return unitText(i)
	case "slice2":
		return value.String(r.slice(relative(i, n), relative(j, n)))
	case "slice1":
		return value.String(r.slice(relative(i, n), n))
	case "substring":
		a, b := clamp(i, n), clamp(j, n)
		return value.String(r.slice(min(a, b), max(a, b)))
	case "startsWith":
		start := clamp(i, n)
		end := start + len(referenceOf(x).units)
		return value.Boolean(end <= n && r.slice(start, end) == x)
	case "endsWith":
		end := clamp(j, n)
		start := end - len(referenceOf(x).units)
		return value.Boolean(start >= 0 && r.slice(start, end) == x)
	case "at":
		if i < 0 {
			i += n
		}
		if i < 0 || i >= n {
			return value.Undefined()
		}
		return unitText(i)
	case "charAt":
		if i < 0 || i >= n {
			return value.String("")
		}
		return unitText(i)
	case "charCodeAt":
		if i < 0 || i >= n {
			return value.Number(math.NaN())
		}
		return value.Number(float64(r.units[i]))
	case "codePointAt":
		if i < 0 || i >= n {
			return value.Undefined()
		}
		c := r.units[i]
		if c >= 0xD800 && c < 0xDC00 && i+1 < n && r.units[i+1] >= 0xDC00 && r.units[i+1] < 0xE000 {
			return value.Number(float64(0x10000 + (rune(c)-0xD800)<<10 + (rune(r.units[i+1]) - 0xDC00)))
		}
		return value.Number(float64(c))
	}
	panic(op)
}

// ---- engine ----

var textOps = []string{"length", "index", "slice2", "slice1", "substring", "startsWith", "endsWith", "at", "charAt",
	"charCodeAt", "codePointAt"}

const textOpsSource = `return {
  length: (s) => s.length, index: (s, i) => s[i], slice2: (s, i, j) => s.slice(i, j), slice1: (s, i) => s.slice(i),
  substring: (s, i, j) => s.substring(i, j), startsWith: (s, i, j, x) => s.startsWith(x, i),
  endsWith: (s, i, j, x) => s.endsWith(x, j), at: (s, i) => s.at(i), charAt: (s, i) => s.charAt(i),
  charCodeAt: (s, i) => s.charCodeAt(i), codePointAt: (s, i) => s.codePointAt(i),
};`

// pieces là các mảnh chuỗi ngẫu nhiên được ghép từ: mỗi loại đơn vị và mỗi cách hỏng.
var pieces = []string{
	"a", "b", "yz", "Phường", "ệ", "ă", "😀", "𝄞",
	string([]byte{0xED, 0xA0, 0xBD}), // nửa cao đứng một mình (D83D)
	string([]byte{0xED, 0xB8, 0x80}), // nửa thấp đứng một mình (DE00)
	string([]byte{0xFF}),             // byte hỏng
	string([]byte{0x80}),             // byte tiếp nối lạc
	string([]byte{0xE1, 0xBB}),       // ký tự ba byte cụt
	string([]byte{0xF0, 0x9F, 0x98}), // ký tự bốn byte cụt
	string([]byte{0xED, 0x9F, 0xBF}), // U+D7FF: ED mà không phải nửa cặp
	string([]byte{0xE0, 0x80, 0x80}), // ba byte viết dài thừa: ba byte hỏng
	string([]byte{0xC0, 0xAF}),       // hai byte viết dài thừa
}

func randomText(rng *rand.Rand, size int) string {
	var b strings.Builder
	for b.Len() < size {
		if rng.IntN(3) == 0 { // đoạn ASCII dài: đường tám byte một bước
			b.WriteString(strings.Repeat("x", rng.IntN(40)))
		}
		b.WriteString(pieces[rng.IntN(len(pieces))])
	}
	return b.String()
}

func sameValue(a, b value.Value) bool {
	switch {
	case a.IsNumber() && b.IsNumber():
		x, y := a.Number(), b.Number()
		return x == y || (x != x && y != y)
	case a.IsString() && b.IsString():
		return a.String() == b.String()
	case a.IsBoolean() && b.IsBoolean():
		return a.Boolean() == b.Boolean()
	}
	return a.IsUndefined() && b.IsUndefined()
}

func TestTextCursorMatchesUnits(t *testing.T) {
	program, err := syntax.Parse("text.work", textOpsSource)
	if err != nil {
		t.Fatal(err)
	}
	main, err := compiler.Compile(program, compiler.Options{})
	if err != nil {
		t.Fatal(err)
	}
	rng := rand.New(rand.NewPCG(23, 11))
	checked := 0
	for round := range 300 {
		size := []int{3, 40, 300, 3000}[round%4] // ngắn: không nhớ; từ 256 byte: lượt chạy nhớ chuỗi
		s := randomText(rng, size)
		r := referenceOf(s)
		n := len(r.units)
		m := runtime.New(runtime.Options{})
		ops := m.Run(main)
		// cùng một machine cho mọi phép trên s: bộ nhớ chuỗi và con trỏ đi tới, đi lui, theo thứ tự ngẫu nhiên
		for range 60 {
			op := textOps[rng.IntN(len(textOps))]
			i, j := rng.IntN(n+8)-4, rng.IntN(n+8)-4
			if rng.IntN(4) == 0 {
				i = -rng.IntN(n + 4)
			}
			x := r.slice(clamp(i, n), clamp(i+rng.IntN(5), n))
			if rng.IntN(3) == 0 {
				x = pieces[rng.IntN(len(pieces))]
			}
			if op == "endsWith" {
				x = r.slice(clamp(j-rng.IntN(5), n), clamp(j, n))
			}
			f, _ := ops.Get(op)
			got := m.Call(f, value.String(s), value.Number(float64(i)), value.Number(float64(j)), value.String(x))
			want := r.expected(op, i, j, x)
			if !sameValue(got, want) {
				t.Fatalf("%s(%q, i=%d, j=%d, x=%q) on a text of %d bytes: got %v (%v), want %v", op, shorten(s), i, j, x, len(s), got, got.Error(), want)
			}
			checked++
		}
	}
	// một lượt chạy đọc xen kẽ sáu chuỗi dài: nhiều hơn chỗ nó nhớ, nên chỗ nhớ được thay và nhớ lại
	m := runtime.New(runtime.Options{})
	ops := m.Run(main)
	var texts []string
	var refs []reference
	for range 6 {
		s := randomText(rng, 400+rng.IntN(2000))
		texts, refs = append(texts, s), append(refs, referenceOf(s))
	}
	// cùng số byte, khác nội dung: chỗ nhớ phân biệt chuỗi bằng chỗ của byte, không chỉ bằng độ dài
	twin := "Z" + strings.Repeat("ệ", 200) + texts[0][601:]
	if len(texts[0]) < 700 {
		twin = "Z" + texts[0][1:]
	}
	texts, refs = append(texts, twin), append(refs, referenceOf(twin))
	for range 3000 {
		k := rng.IntN(len(texts))
		r, n := refs[k], len(refs[k].units)
		op := textOps[rng.IntN(len(textOps))]
		i, j := rng.IntN(n+8)-4, rng.IntN(n+8)-4
		x := r.slice(clamp(i, n), clamp(i+rng.IntN(5), n))
		if op == "endsWith" {
			x = r.slice(clamp(j-rng.IntN(5), n), clamp(j, n))
		}
		f, _ := ops.Get(op)
		got := m.Call(f, value.String(texts[k]), value.Number(float64(i)), value.Number(float64(j)), value.String(x))
		if want := r.expected(op, i, j, x); !sameValue(got, want) {
			t.Fatalf("%s on text %d (i=%d, j=%d, x=%q): got %v (%v), want %v", op, k, i, j, x, got, got.Error(), want)
		}
		checked++
	}
	t.Logf("%d calls checked", checked)
}

func shorten(s string) string {
	if len(s) > 60 {
		return s[:60] + "…"
	}
	return s
}

// Trên chuỗi 1 MB, các phép đi bằng con trỏ tốn như trên một chuỗi ngắn: s.length lần thứ hai nhớ, slice(1) và
// startsWith không đếm độ dài, s[i] lần lượt đi tiếp. Trước lát này mỗi lời gọi đi cả chuỗi: 0,1–3 ms một lần.
func TestTextCursorCostsLikeAShortText(t *testing.T) {
	long := map[string]value.Value{
		"a": value.String(strings.Repeat("y", 1<<20)),
		"v": value.String(strings.Repeat("ệ", 1<<20/3)),
	}
	// perCall là thời gian một vòng: gọi tới khi đủ 30 ms (đồng hồ Windows nhích 0,5 ms), trên một lượt chạy (nó nhớ
	// chuỗi qua các lời gọi); một phép đi cả chuỗi mỗi lần thì một lời gọi đã quá 30 ms, nên ca hỏng dừng nhanh.
	perCall := func(body string, text value.Value) time.Duration {
		program, err := syntax.Parse("text.work", "return (s) => { for (let i = 0; i < 2000; i++) { "+body+"; } return 0; };")
		if err != nil {
			t.Fatal(err)
		}
		main, _ := compiler.Compile(program, compiler.Options{})
		m := runtime.New(runtime.Options{Energy: 1 << 40})
		f := m.Run(main)
		calls := 0
		start := time.Now()
		for calls == 0 || time.Since(start) < 30*time.Millisecond {
			if r := m.Call(f, text); r.IsInvalid() {
				t.Fatal(body, r.Error())
			}
			calls++
		}
		return time.Since(start) / time.Duration(calls*2000)
	}
	for _, body := range []string{
		"s.length", "s[i]", "s.slice(1)", "s.slice(2, 9)", "s.slice(-3)", "s.substring(1, 4)", `s.startsWith("yy")`,
		`s.startsWith("ệ")`, `s.endsWith("y")`, "s.at(-1)", "s.charAt(i)", "s.charCodeAt(i)", "s.codePointAt(i)",
	} {
		short := perCall(body, value.String("yệyệyệyệ"))
		for name, text := range long {
			got := perCall(body, text)
			if got > 3*short+200*time.Nanosecond {
				t.Fatalf("%s on 1 MB %s: %v a call, on a short text %v", body, name, got, short) // một ca đủ: ca sau còn chậm hơn
			}
		}
	}
}

// Một lần đếm lại byte quên các chỗ nhớ chuỗi (core/runtime/memory.go). valueOf của một đối số chạy chương trình, có
// thể làm ra đủ để đếm lại, nên một hàm đổi đối số trước rồi mới lấy con trỏ: kết quả vẫn đúng.
func TestTextCursorSurvivesARecount(t *testing.T) {
	for _, c := range []struct{ body, want string }{
		{"s.slice(index)", "end"},
		{"s.substring(index, 1000000)", "end"},
		{"s.charAt(index)", "e"},
		{"s.at(index)", "e"},
		{`s.startsWith("end", index) ? "yes" : "no"`, "yes"},
		{`s.endsWith("yyy", index) ? "yes" : "no"`, "yes"},
	} {
		program, err := syntax.Parse("text.work", `const s = "y".repeat(500000) + "end";
const remembered = s.length + s.slice(-1).length;
const index = { valueOf: () => { let g = ""; for (let i = 0; i < 6; i++) { g = "z".repeat(1000000); } return 500000; } };
return `+c.body+";")
		if err != nil {
			t.Fatal(err)
		}
		main, _ := compiler.Compile(program, compiler.Options{})
		r := runtime.New(runtime.Options{Memory: 4 << 20}).Run(main)
		if r.String() != c.want {
			t.Errorf("%s: got %q %v, want %q", c.body, r.String(), r.Error(), c.want)
		}
	}
}
