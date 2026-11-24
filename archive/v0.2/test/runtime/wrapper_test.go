package runtime_test

import "testing"

// A wrapper object is not Kitwork's (19/11/2026, docs/language.md mục 4.3): Object(5), Object("x"), new Object(true) and
// Object.assign of a number, a string or a boolean are the TYPE error, where they gave the primitive back, typeof
// "number" where JavaScript's is "object". Object.assign writing a key on an array, a time or a function is the TYPE
// error too, where the target came back unwritten (Object.assign([1, 2], [3]) was [1, 2], JavaScript's [3, 2]); with
// nothing to write the target comes back, as in JavaScript. Object(null), Object(), Object(o) are JavaScript's.
//
// CONTROL (19/11/2026): with Object giving a primitive back, the first three were "made"; with the target of
// Object.assign given back unwritten, Object.assign([1, 2], [3]) was "made".
func TestAWrapperObjectIsTheTypeError(t *testing.T) {
	program := load(t, `const made = (f) => { const r = safe(f); return r.error === null ? "made" : r.error.code + " " + r.error.message; };
const o = { a: 1 };
const f = (x) => x;
result = [made(() => Object(5)), made(() => Object("x")), made(() => new Object(true)), made(() => Object.assign(5)),
  made(() => Object.assign("x", { a: 1 })), made(() => Object.assign([1, 2], [3])), made(() => Object.assign(f, { a: 1 })),
  Object.assign([1, 2]), Object.assign(f) === f, Object.assign([1], null, {}), Object(null), Object(), Object(o) === o,
  Object.assign({}, { a: 1 }, [9])];`)
	program.run(t)
	want := `array ["TYPE Object(x) bọc một number thành object, Kitwork không có object bọc giá trị nguyên thuỷ: dùng chính giá trị ấy",` +
		`"TYPE Object(x) bọc một string thành object, Kitwork không có object bọc giá trị nguyên thuỷ: dùng chính giá trị ấy",` +
		`"TYPE Object(x) bọc một boolean thành object, Kitwork không có object bọc giá trị nguyên thuỷ: dùng chính giá trị ấy",` +
		`"TYPE Object.assign(x, …) bọc một number thành object, Kitwork không có object bọc giá trị nguyên thuỷ: dùng chính giá trị ấy",` +
		`"TYPE Object.assign(x, …) bọc một string thành object, Kitwork không có object bọc giá trị nguyên thuỷ: dùng chính giá trị ấy",` +
		`"TYPE Object.assign(x, …) viết khoá vào một object; x là array, không giữ khoá: chép một mảng viết [...a], gộp object viết Object.assign({}, …)",` +
		`"TYPE Object.assign(x, …) viết khoá vào một object; x là function, không giữ khoá: chép một mảng viết [...a], gộp object viết Object.assign({}, …)",` +
		`[1,2],true,[1],{},{},true,{"0":9,"a":1}]`
	if got := describe(program.variable("result")); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}
