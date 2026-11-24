package value

// A failure is a value: Fail makes one, every operation passes it through, safe() reads it as
// { value: undefined, error: { code, message, at } }. Its code is what a program switches on, drawn from the
// closed set below; its text is for a person and never compared. The set is the first engine's,
// kept word for word (upper case, underscores, no vendor prefix): when two codes fit one failure
// the narrower wins, DENIED, TIMEOUT, CONFLICT, NOT_FOUND and TOO_LARGE before DATABASE, NETWORK
// and INVALID_INPUT. A test keeps every Fail in the engine on this set.
const (
	// FailCode is a failure the program made itself, return error("…"); the text is the program's.
	FailCode = "FAIL"
	// TypeCode is an operation on a kind it does not take: "a" * 2, (5).push(1), null.x, a call on
	// what is not a function. JavaScript's TypeError.
	TypeCode = "TYPE"
	// RangeCode is a number outside what an operation takes: toFixed(101), repeat(-1), a string past
	// the longest, a time outside 1678–2262. JavaScript's RangeError.
	RangeCode = "RANGE"
	// InvalidArgumentCode is an engine API called the wrong way: a bank without a BIN, an update
	// without a where. Nothing was spent, so it can be caught and fixed by the program.
	InvalidArgumentCode = "INVALID_ARGUMENT"
	// InvalidInputCode is data from outside that cannot be read: a body that is not JSON or is past its
	// limit, JSON.parse of a bad text, a token.
	InvalidInputCode = "INVALID_INPUT"
	// UnavailableCode is a service the engine has no connection to: not configured, or broken since it
	// was opened (a database not connected, a search index).
	UnavailableCode = "UNAVAILABLE"
	// UnsupportedCode is an operation this engine or this connection does not do.
	UnsupportedCode = "UNSUPPORTED"
	// DatabaseCode is a query or a write that failed in the storage layer, a driver error included.
	DatabaseCode = "DATABASE"
	// ConflictCode is a write that met another: a transaction conflict, a unique constraint.
	ConflictCode = "CONFLICT"
	// PartialCode is a bulk write that stopped midway.
	PartialCode = "PARTIAL"
	// TimeoutCode is an operation past its own time limit: a query, a transaction, an HTTP request.
	TimeoutCode = "TIMEOUT"
	// NetworkCode is an outside service that could not be reached.
	NetworkCode = "NETWORK"
	// DeniedCode is the engine refusing by policy: SSRF, a file outside the app's folder, a right.
	DeniedCode = "DENIED"
	// NotFoundCode is a thing named exactly that is not there, from a function that promised to give
	// it: a file to read, a row by its key. A function that gives a thing only when there is one
	// (file.info, db.first) gives null instead.
	NotFoundCode = "NOT_FOUND"
	// TooLargeCode is past the quota or the size ceiling of a place that stores, or of a read into
	// the VM's memory.
	TooLargeCode = "TOO_LARGE"
	// RoutineCancelledCode is a routine stopped by routine.cancel(handle) that routine.wait reads: the
	// program asked for it, so it can be caught.
	RoutineCancelledCode = "CANCELLED"
	// NotInitializedCode is a let or a const used by a function before the line declaring it ran (18/11/2026,
	// decided 27/10/2026): JavaScript's ReferenceError of its temporal dead zone.
	NotInitializedCode = "NOT_INITIALIZED"

	// CancelledCode is not one of the set a program catches: a host call that failed because the
	// request was over returns it, and the runtime ends the run with it.
	CancelledCode = "EXECUTION_CANCELLED"
)

// Codes is the closed set a failure's code is drawn from, in the order above.
func Codes() []string {
	return []string{
		FailCode, TypeCode, RangeCode, InvalidArgumentCode, InvalidInputCode, UnavailableCode,
		UnsupportedCode, DatabaseCode, ConflictCode, PartialCode, TimeoutCode, NetworkCode, DeniedCode,
		NotFoundCode, TooLargeCode, RoutineCancelledCode, NotInitializedCode, CancelledCode,
	}
}

// failure is what an Invalid value carries.
type failure struct {
	code string
	text string
	// at is where the failure happened, "file:line:column" of the innermost frame at the failing instruction
	// (FailAt): safe() hands it to the program as error.at, apart from the text, which is only for a person
	// (19/11/2026, decided 14/10/2026). "" for a failure no run placed yet: one a function of Go made.
	at string
	// fields are the fields of data from outside that the failure is about, an array of { path, rule, message }
	// (WithFields); the zero Value when it names none
	fields Value
	// progress is how much of its work a failure of PARTIAL did before it stopped, a number (WithProgress): the
	// entries clear() removed (docs/KITWORK_FILE.md mục 7); the zero Value for any other failure
	progress Value
}

// Fail is a failure: code is what safe() hands the program as error.code, text is for a person.
func Fail(code, text string) Value {
	return Value{kind: invalid, data: &failure{code: code, text: text}}
}

// FailAt is Fail placed at at, "file:line:column" (Place): the failure of an instruction of a run, which the runtime
// makes.
func FailAt(code, text, at string) Value {
	return Value{kind: invalid, data: &failure{code: code, text: text, at: at}}
}

// Place is where a failure happened, "file:line:column" of the innermost frame at the failing instruction ("line:column"
// for a source with no name); "" for a failure no run placed (one a function of Go made, not yet past the
// instruction that called it) and for every other kind.
func (v Value) Place() string {
	if f, ok := v.data.(*failure); ok {
		return f.at
	}
	return ""
}

// Headline is a failure as a log writes it, its place before its text: "router.work:12:5: message", the text alone
// when no run placed it; "" for every other kind. A text made from a failure for a person (a log, a failure that
// tells another) is its Headline; what the program reads is Message and At apart.
func (v Value) Headline() string {
	f, ok := v.data.(*failure)
	if !ok {
		return ""
	}
	if f.at == "" {
		return f.text
	}
	return f.at + ": " + f.text
}

// Code is a failure's code; "" for every other kind.
func (v Value) Code() string {
	if f, ok := v.data.(*failure); ok {
		return f.code
	}
	return ""
}

// Message is a failure's text for a person, never where it happened (Place); "" for every other kind. safe() hands it
// to the program as error.message, beside Code() as error.code and Place() as error.at.
func (v Value) Message() string {
	if f, ok := v.data.(*failure); ok {
		return f.text
	}
	return ""
}

// WithFields is the failure v naming the fields of data from outside it is about: fields is an array of
// { path, rule, message }, path the field's place (["email"], ["items", 2, "name"]), rule the rule the value broke
// (docs/schema.md mục 6), message for a person. safe() hands it to the program as error.fields, beside code and
// message, so a form shows each under its input. v itself when it is not a failure or fields is not an array.
func (v Value) WithFields(fields Value) Value {
	f, ok := v.data.(*failure)
	if !ok || !fields.IsArray() {
		return v
	}
	return Value{kind: invalid, data: &failure{code: f.code, text: f.text, at: f.at, fields: fields, progress: f.progress}}
}

// WithProgress is the failure v saying how much of its work was done before it stopped: progress is a number, the
// entries a clear() of kitwork/file removed before it stopped (PARTIAL, docs/KITWORK_FILE.md mục 7). safe() hands it
// to the program as error.progress. v itself when it is not a failure or progress is not a number.
func (v Value) WithProgress(progress Value) Value {
	f, ok := v.data.(*failure)
	if !ok || !progress.IsNumber() {
		return v
	}
	return Value{kind: invalid, data: &failure{code: f.code, text: f.text, at: f.at, fields: f.fields, progress: progress}}
}

// Progress is how much of its work a failure did (WithProgress), undefined when it says none and for every other
// kind.
func (v Value) Progress() Value {
	if f, ok := v.data.(*failure); ok {
		return f.progress
	}
	return Value{}
}

// ErrorObject is a failure as the program holds it, safe()'s error and routine.wait's: { code, message, at }, at
// "file:line:column" or null when no run placed it, then fields and progress when it has them (WithFields,
// WithProgress). null for every other kind.
func (v Value) ErrorObject() Value {
	f, ok := v.data.(*failure)
	if !ok {
		return Null()
	}
	at := Null()
	if f.at != "" {
		at = String(f.at)
	}
	object := Object().Set("code", String(f.code)).Set("message", String(f.text)).Set("at", at)
	if f.fields.IsArray() {
		object = object.Set("fields", f.fields)
	}
	if f.progress.IsNumber() {
		object = object.Set("progress", f.progress)
	}
	return object
}

// Fields is the fields a failure names (WithFields), undefined when it names none and for every other kind.
func (v Value) Fields() Value {
	if f, ok := v.data.(*failure); ok {
		return f.fields
	}
	return Value{}
}

// Failure is a failure as one line, "CODE: file:line:column: text" (its Headline led by its code); "" for every
// other kind. (Not Error: a Value is not a Go error, and fmt must never print one as one.)
func (v Value) Failure() string {
	if f, ok := v.data.(*failure); ok {
		return f.code + ": " + v.Headline()
	}
	return ""
}

// RequestBound is a host object that belongs to one request (its context, its request, its response): a routine,
// which runs apart from the request, holds none (runtime Snapshot). Go's only: a method of no result is no word of
// the program.
type RequestBound interface{ RequestBound() }
