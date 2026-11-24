package javascript

import "github.com/huynhnhanquoc/engine/core/value"

// Lỗ của mảng, như engine2 (19/11/2026, engine2/docs/language.md mục 4.3). Array(n) dựng n lỗ của JavaScript; mảng
// Kitwork giữ undefined ở đó và mang dấu lỗ (value.WithHoles). Đọc một lỗ ra undefined như JavaScript, nên
// Array(n).fill(0), Array(n).join("-"), Array.from(Array(n), f), JSON của nó là của JavaScript. Thứ JavaScript bỏ qua
// lỗ thì cho kết quả khác trên undefined (Array(3).every(f) là true ở JavaScript, f chưa từng được gọi), nên là lỗi
// trên mảng còn dấu: các phương thức holeSensitive, các khoá của Object.keys, values, entries, hasOwn, nguồn của
// Object.assign, và flat, flatMap khi mảng chúng trải ra có dấu. slice, concat, splice giữ dấu như JavaScript giữ lỗ;
// fill cả mảng gỡ nó, fill một khúc thì không.

// holeSensitive là các phương thức bỏ qua lỗ: map giữ lỗ, forEach, filter, some, every, reduce, reduceRight không gọi
// hàm ở lỗ, indexOf, lastIndexOf không thấy lỗ, flat, flatMap bỏ lỗ, hasOwnProperty nói lỗ không phải khoá.
var holeSensitive = map[string]bool{
	"every": true, "filter": true, "flat": true, "flatMap": true, "forEach": true, "hasOwnProperty": true,
	"indexOf": true, "lastIndexOf": true, "map": true, "reduce": true, "reduceRight": true, "some": true,
}

// holesRefused là lỗi của written trên một mảng còn dấu lỗ.
func holesRefused(written string) error {
	return value.Errorf(value.TypeCode, "%s: the array Array(n) made still has holes, which JavaScript passes over here and a Kitwork "+
		"array does not have: fill it first, Array(n).fill(x), or write Array.from({ length: n }, (_, i) => …)", written)
}
