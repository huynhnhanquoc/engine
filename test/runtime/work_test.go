package runtime_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/huynhnhanquoc/engine/core/compiler"
	"github.com/huynhnhanquoc/engine/core/runtime"
	"github.com/huynhnhanquoc/engine/core/syntax"
	"github.com/huynhnhanquoc/engine/core/value"
)

// Năng lượng cho việc của engine (core/javascript/work.go, 23/11/2026). Mỗi ca là một vòng lặp một phép nặng, chạy tới
// khi hết workEnergy; thời gian của nó so với một vòng cộng tốn cùng năng lượng. Tỉ số gần 1 là phép ấy tốn năng lượng
// đúng việc nó làm. Trên maxRatio là năng lượng không chặn được nó: một lượt chạy làm việc lâu hơn năng lượng của nó
// hứa (trước lát này: hàng trăm lần). Dưới minRatio là nó tốn năng lượng quá việc nó làm: một chương trình bình thường
// dừng sớm.

const (
	workEnergy = 3_000_000
	maxRatio   = 4.0
	minRatio   = 0.2
)

// workInputs là các giá trị Go đưa vào: không phải lượt chạy làm ra, nên chỉ vòng lặp tốn năng lượng.
func workInputs() []value.Value {
	const n = 100_000
	numbers := make([]value.Value, n)
	texts := make([]value.Value, n)
	for i := range numbers {
		numbers[i] = value.Number(float64((i * 7919) % n))
		texts[i] = value.String(fmt.Sprintf("item-%06d", (i*7919)%n))
	}
	o := value.Object()
	for i := range 2000 {
		o.Set(fmt.Sprintf("key-%05d", i), value.Number(float64(i)))
	}
	json := strings.Repeat("12345,", n/2)
	var rows strings.Builder // 2000 dòng 12 cột, như kết quả của một API
	rows.WriteString("[")
	for i := range 2000 {
		fmt.Fprintf(&rows, `{"id":%d,"sku":"s%d","name":"item","price":10,"stock":3,"brand":"b","category":"c",`+
			`"color":"red","size":"m","weight":1,"rating":4,"sold":%d},`, i, i, i)
	}
	rows.WriteString("{}]")
	return []value.Value{
		value.String(strings.Repeat("y", 1<<20)),   // a: 1 MB ASCII
		value.String(strings.Repeat("y", 1<<20)),   // b: bằng a, chỗ khác
		value.String(strings.Repeat("ệ", 1<<20/3)), // v: 1 MB tiếng Việt
		value.String(strings.Repeat("ệ", 1<<20/3)), // w: bằng v, chỗ khác
		value.Array(numbers...),                    // n: 100 000 số
		value.Array(texts...),                      // t: 100 000 chuỗi ngắn
		o,                                          // o: 2000 khoá
		value.String("[" + json + "0]"),            // text: JSON 300 KB
		value.String(strings.Repeat("7", 300)),     // digits
		value.String(rows.String()),                // rows: JSON 300 KB, 2000 object cùng khoá
	}
}

// spendAll chạy body trong một vòng lặp tới khi hết workEnergy và trả thời gian, cùng lỗi (ENERGY_LIMIT là đúng).
func spendAll(t *testing.T, body string) (time.Duration, value.Value) {
	t.Helper()
	source := "return (a, b, v, w, n, t, o, text, digits, rows) => { for (let i = 0; i < 1000000000; i++) { " + body + "; } return 0; };"
	program, err := syntax.Parse("work.work", source)
	if err != nil {
		t.Fatal(body, err)
	}
	main, err := compiler.Compile(program, compiler.Options{})
	if err != nil {
		t.Fatal(body, err)
	}
	inputs := workInputs()
	m := runtime.New(runtime.Options{Energy: workEnergy})
	f := m.Run(main)
	start := time.Now()
	r := m.Call(f, inputs...)
	return time.Since(start), r
}

func TestEnergyEndsTheEngineWork(t *testing.T) {
	baseline := time.Duration(1 << 62)
	for range 3 {
		d, _ := spendAll(t, "i = i + 0")
		baseline = min(baseline, d)
	}
	for _, body := range []string{
		`a.indexOf("zz")`, `v.indexOf("zz")`, `a.includes("zz")`, `a.lastIndexOf("zz")`, `v.lastIndexOf("zz")`,
		`a.toUpperCase()`, `v.toUpperCase()`, `a.split("z")`, `a.trim()`, `a.slice(0, 50000).split("")`, `a.replace("y", "z")`,
		// năm chuỗi dài xoay vòng: nhiều hơn chỗ một lượt chạy nhớ, nên mỗi lần con trỏ đi thật (text_test.go thử
		// lần đọc lại, giờ tốn như một chuỗi ngắn)
		`[a, b, v, w, text][i % 5].length`, `[a, b, v, w, text][i % 5].slice(-3)`, `[a, b, v, w, text][i % 5][200000]`,
		`a.replaceAll("y", "z")`, `v.replaceAll("ệ", "e")`, `a.repeat(2)`, `a + "x"`, "`${a}${v}`", `a.padEnd(2000000)`,
		`a === b`, `v === w`, `a < b`, `v < w`, `digits - 0`, `encodeURIComponent(a)`,
		`n.indexOf(-1)`, `t.includes("zz")`, `n.reverse()`, `n.fill(0)`, `n.join(",")`, `t.join(",")`,
		`n.slice(0, 1000).toSorted()`, `t.slice(0, 1000).toSorted()`, `n.slice(0, 1000).toSorted((x, y) => x - y)`,
		`[n, n].flat()`, `n.slice()`, `n.concat(n)`,
		`n.map((x) => x)`, `n.map(String)`, `n.filter(Boolean)`, `n.some((x) => x < 0)`,
		`JSON.stringify(n)`, `JSON.stringify(t)`, `JSON.stringify(o)`, `JSON.parse(text)`, `JSON.parse(rows)`,
		`Object.keys(o)`, `Object.values(o)`, `Object.entries(o)`, `Object.assign({}, o)`, `({ ...o })`,
		`Object.assign({ x: 1 }, o)`, `({ x: 1, ...o })`, `({ ...o, x: 1 })`,
		`Array.from({ length: 100000 })`, `Array.from(n)`,
	} {
		// nhanh nhất trong ba lần, như vòng cộng làm mốc: một phép làm việc lớn (một triệu chỗ khớp) kéo GC theo, và máy
		// bận làm một lần chậm gấp đôi
		elapsed, r := spendAll(t, body)
		for range 2 {
			again, _ := spendAll(t, body)
			elapsed = min(elapsed, again)
		}
		ratio := float64(elapsed) / float64(baseline)
		t.Logf("%-32s %6.2f× (%v)", body, ratio, elapsed.Round(time.Millisecond))
		if r.Code() != value.EnergyCode {
			t.Errorf("%s: want ENERGY_LIMIT, got %v", body, r.Error())
		}
		if ratio > maxRatio {
			t.Errorf("%s: %.1f× the time of its energy: the engine's work goes past it", body, ratio)
		}
		if ratio < minRatio {
			t.Errorf("%s: %.2f× the time of its energy: charged far more than it works", body, ratio)
		}
	}
}

// spent là năng lượng một lần gọi body tốn, với các giá trị của workInputs.
func spent(t *testing.T, body string) int64 {
	t.Helper()
	program, err := syntax.Parse("work.work", "return (a, b, v, w, n, t, o, text, digits, rows) => "+body+";")
	if err != nil {
		t.Fatal(body, err)
	}
	main, err := compiler.Compile(program, compiler.Options{})
	if err != nil {
		t.Fatal(body, err)
	}
	m := runtime.New(runtime.Options{Energy: 1 << 40, Memory: 1 << 30})
	f := m.Run(main)
	before := m.Energy()
	if r := m.Call(f, workInputs()...); r.IsInvalid() {
		t.Fatal(body, r.Error())
	}
	return before - m.Energy()
}

// Tất định, không đồng hồ: mỗi phép trên dữ liệu lớn tốn ít nhất 1000 đơn vị (không có chỗ tính, nó tốn vài lệnh), và
// một phép trên chuỗi ngắn tốn vài lệnh (dưới smallText không tính thêm). Tắt một chỗ tính thì ca của nó đỏ.
func TestEnergyChargedForWork(t *testing.T) {
	for _, body := range []string{
		`a.indexOf("zz")`, `v.indexOf("zz")`, `a.toUpperCase()`, `a.lastIndexOf("zz")`, `a.padEnd(2000000)`,
		`a.slice(0, 50000).split("")`, `a.replaceAll("y", "z")`, `a + "x"`, "`${a}${v}`",
		`a.length`, `v[300000]`, `a === b`, `a < b`, `digits.repeat(10) - 0`, `encodeURIComponent(a)`,
		`n.indexOf(-1)`, `n.reverse()`, `n.fill(0)`, `n.join(",")`, `n.toSorted()`, `n.slice(0, 1000).map((x) => x)`,
		`[n, n].flat()`, `n.slice()`, `n.concat(n)`, `JSON.stringify(n)`, `JSON.parse(text)`, `JSON.parse(rows)`,
		`Object.keys(o)`, `Object.assign({}, o)`, `({ ...o })`, `Array.from({ length: 100000 })`,
	} {
		if used := spent(t, body); used < 1000 {
			t.Errorf("%s: %d units, want at least 1000", body, used)
		}
	}
	// assign lên một object đã có khoá đọc rồi ghi từng khoá: tốn hơn Object.keys, chỉ đọc
	if keys, assign := spent(t, "Object.keys(o)"), spent(t, "Object.assign({ x: 1 }, o)"); assign < keys+2000*10 {
		t.Errorf("Object.assign %d units, Object.keys %d: writing 2000 keys costs nothing more", assign, keys)
	}
	// lên một object chưa có khoá ({ ...o }, Object.assign({}, o)) thì mượn khoá của o, chỉ chép giá trị: rẻ hơn ghi
	// từng khoá, mà vẫn theo số khoá (24/11/2026)
	for _, body := range []string{"({ ...o })", "Object.assign({}, o)"} {
		if copied, written := spent(t, body), spent(t, "({ x: 1, ...o })"); copied >= written/2 || copied < 2000 {
			t.Errorf("%s %d units, writing each key %d", body, copied, written)
		}
	}
	// Math.max, o.f(): chương trình gọi, như một lệnh CALL, không phải lời gọi lại của engine (callbackUnits)
	for _, body := range []string{`"abcdef".indexOf("c")`, `"abc".length`, `"ab" + "c"`, `"x" === "x"`, `[1, 2].indexOf(2)`,
		`Math.max(1, 2)`, `({ f: () => 1 }).f()`} {
		if used := spent(t, body); used > 20 {
			t.Errorf("%s: %d units, want a few instructions", body, used)
		}
	}
}
