package value

// Mỗi Is… hỏi v có phải một loại không; Go hỏi bằng chúng, chương trình JS hỏi bằng Typeof.

func (v Value) IsUndefined() bool { return v.kind == undefined }
func (v Value) IsNull() bool      { return v.kind == null }
func (v Value) IsNumber() bool    { return v.kind == number }
func (v Value) IsBoolean() bool   { return v.kind == boolean }
func (v Value) IsString() bool    { return v.kind == text }
func (v Value) IsBytes() bool     { return v.kind == bytes }
func (v Value) IsTime() bool      { return v.kind == datetime }
func (v Value) IsDuration() bool  { return v.kind == duration }
func (v Value) IsObject() bool    { return v.kind == object }
func (v Value) IsArray() bool     { return v.kind == array }
func (v Value) IsStruct() bool    { return v.kind == structure }
func (v Value) IsFunction() bool  { return v.kind == function }
func (v Value) IsInvalid() bool   { return v.kind == invalid }

// Identical là v và w là cùng một thứ: cùng loại; cùng bit số (0 và -0 khác nhau, NaN cùng bit thì giống); cùng chuỗi;
// cùng chỗ sau con trỏ với object, mảng, struct, hàm, lỗi; cùng dãy byte trong bộ nhớ với bytes. Các luật bằng nhau
// của JavaScript (===, Object.is, includes) dựng trên nó, ở core/javascript. Hàm so bằng ==, nên thứ Function mang
// phải so được (một con trỏ), không phải một func của Go.
func (v Value) Identical(w Value) bool {
	if v.kind != w.kind || v.number != w.number {
		return false
	}
	switch v.kind {
	case text:
		return v.text() == w.text()
	case bytes:
		a, b := v.data.([]byte), w.data.([]byte)
		return len(a) == len(b) && (len(a) == 0 || &a[0] == &b[0])
	case object, array, structure, function, invalid:
		return v.data == w.data
	}
	return true
}

// IsNullish là v là null hay undefined: thứ mà ?? và ?. hỏi.
func (v Value) IsNullish() bool { return v.kind <= null }
