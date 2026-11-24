package file

import (
	"bytes"
	"context"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/kitwork/engine2/core/value"
)

// Handle is a path of an app (mục 2): its logical path and the File of the app it belongs to. It never changes,
// opening it touched nothing, and every member is a function: reading handle.delete deletes nothing.
type Handle struct {
	file *File
	path Path
	// stop is closed when the request a call runs for is over (Attend): clear() and copy() of a folder stop there.
	stop <-chan struct{}
}

// Freeze is what value.Freeze calls on a handle a file holds as it boots: a handle never changes.
func (h *Handle) Freeze() {}

// Attend is the handle for one call by a run whose context is ctx: clear() and the copy of a folder stop as the
// request it runs for is over (value.Attender).
func (h *Handle) Attend(ctx context.Context) value.Value {
	attended := *h
	attended.stop = ctx.Done()
	return value.Struct(&attended)
}

// spelling is the handle as a program writes it, for a message.
func (h *Handle) spelling() string { return "file.open(" + strconv.Quote(h.path.String()) + ")" }

// zone is where the handle's path starts: the app's code for "app/…", its data for any other.
func (h *Handle) zone() *Zone { return h.file.zone(h.path.Code) }

// none is the failure of a word given arguments it takes none of.
func (h *Handle) none(word string, arguments []value.Value) value.Value {
	if len(arguments) == 0 {
		return value.Value{}
	}
	if word == "name" {
		return value.Fail(value.InvalidArgumentCode, h.spelling()+".name() không nhận đối số; chọn con bằng segment(…), file.open(\"uploads/\").segment(\"a.png\") (KITWORK_FILE.md mục 17)")
	}
	return value.Fail(value.InvalidArgumentCode, h.spelling()+"."+word+"() không nhận đối số")
}

// changing is the failure of a change while a file boots: a file only declares then (mục 16).
func (h *Handle) changing(word string) value.Value {
	if h.file.Booting != nil && h.file.Booting() {
		return value.Fail(value.InvalidArgumentCode, h.spelling()+"."+word+"(…) ở cấp file: lúc nạp, file chỉ khai; file.open(…) ở cấp file thì được, còn "+word+" gọi trong một handler, một job hay một lịch")
	}
	return value.Value{}
}

// ---- choosing ----

// Segment is segment(…): the handle of a path under this folder, each argument one name (mục 2), read as segment()
// of storage reads them, so a value from outside never leaves the folder.
func (h *Handle) Segment(arguments ...value.Value) value.Value {
	if len(arguments) == 0 {
		return value.Fail(value.InvalidArgumentCode, "segment(…) nhận ít nhất một tên: file.open(\"uploads/\").segment(id, \"avatar.png\")")
	}
	names := make([]string, len(arguments))
	for index, argument := range arguments {
		switch {
		case argument.IsString() && argument.Text() != "":
			names[index] = argument.Text()
		case argument.IsNumber() && argument.Number() == math.Trunc(argument.Number()) && !math.IsInf(argument.Number(), 0):
			names[index] = argument.ToString().Text()
		default:
			kind := argument.Kind()
			switch {
			case argument.IsString():
				kind = "chuỗi rỗng"
			case argument.IsNumber():
				kind = "số " + argument.ToString().Text() + ", không phải số nguyên"
			}
			return value.Fail(value.InvalidArgumentCode, "segment(…) nhận chuỗi không rỗng hay số nguyên; đoạn thứ "+strconv.Itoa(index+1)+" là "+kind+": dựng đường dẫn bằng từng đoạn, file.open(\"uploads/\").segment(id, \"avatar.png\")")
		}
	}
	path, failed := h.path.Segment(names)
	if failed != nil {
		return fault(failed)
	}
	return value.Struct(&Handle{file: h.file, path: path})
}

// Name is name(): the last name of the path, no I/O.
func (h *Handle) Name(arguments ...value.Value) value.Value {
	if failure := h.none("name", arguments); failure.IsInvalid() {
		return failure
	}
	return value.String(h.path.Name())
}

// Path is path(): the path as a program writes it, no I/O.
func (h *Handle) Path(arguments ...value.Value) value.Value {
	if failure := h.none("path", arguments); failure.IsInvalid() {
		return failure
	}
	return value.String(h.path.String())
}

// ---- reading ----

func (h *Handle) Read(budget value.Budget, arguments ...value.Value) value.Value {
	return h.readingWithin("read", arguments, budget)
}
func (h *Handle) Text(arguments ...value.Value) value.Value  { return h.reading("text", arguments) }
func (h *Handle) Bytes(arguments ...value.Value) value.Value { return h.reading("bytes", arguments) }
func (h *Handle) Json(budget value.Budget, arguments ...value.Value) value.Value {
	return h.readingWithin("json", arguments, budget)
}

func (h *Handle) List(arguments ...value.Value) value.Value {
	if failure := h.none("list", arguments); failure.IsInvalid() {
		return failure
	}
	return h.list()
}

func (h *Handle) Info(arguments ...value.Value) value.Value {
	if failure := h.none("info", arguments); failure.IsInvalid() {
		return failure
	}
	return h.info()
}

func (h *Handle) Exists(arguments ...value.Value) value.Value {
	if failure := h.none("exists", arguments); failure.IsInvalid() {
		return failure
	}
	return h.exists()
}

func (h *Handle) reading(word string, arguments []value.Value) value.Value {
	return h.readingWithin(word, arguments, nil)
}

// readingWithin is reading by a run whose budget is budget: a JSON file's tree is made within it.
func (h *Handle) readingWithin(word string, arguments []value.Value, budget value.Budget) value.Value {
	if failure := h.none(word, arguments); failure.IsInvalid() {
		return failure
	}
	return h.read(word, budget)
}

// read is read(), text(), bytes(), json() (mục 4): the content, decoded by the extension for read(); null for a
// file that is not there (27/10/2026: "không có" is data, not a failure).
//
// A JSON file's tree is made within budget, the run that reads it (value.Budget): a file of 8 MB of "[]," is a tree
// of some 200 MB.
func (h *Handle) read(word string, budget value.Budget) value.Value {
	data, failed := h.zone().Read(h.path, ReadLimit)
	if failed != nil {
		if failed.Code == value.NotFoundCode {
			return value.Null()
		}
		return fault(failed)
	}
	kind := KindOf(h.path.Name())
	if word == "bytes" || word == "read" && kind == Binary {
		return value.Bytes(data)
	}
	if !utf8.Valid(data) {
		return value.Fail(value.InvalidInputCode, h.path.String()+" không phải chữ UTF-8; byte đọc bằng file.bytes("+strconv.Quote(h.path.String())+")")
	}
	if word == "json" || word == "read" && kind == JSON {
		parsed := value.ParseJSONWithin(string(data), budget)
		if parsed.IsInvalid() {
			if parsed.Code() != value.InvalidInputCode {
				return parsed // the run's memory failure as the tree grew
			}
			return value.Fail(value.InvalidInputCode, h.path.String()+" không phải JSON: "+strings.TrimPrefix(parsed.Message(), "JSON: "))
		}
		return parsed
	}
	return value.String(string(data))
}

func (h *Handle) list() value.Value {
	entries, failed := h.zone().List(h.path)
	if failed != nil {
		return fault(failed)
	}
	items := make([]value.Value, len(entries))
	for index := range entries {
		items[index] = infoValue(&entries[index])
	}
	return value.Array(items)
}

func (h *Handle) info() value.Value {
	info, failed := h.zone().Stat(h.path)
	if failed != nil {
		return fault(failed)
	}
	if info == nil {
		return value.Null()
	}
	return infoValue(info)
}

func (h *Handle) exists() value.Value {
	info, failed := h.zone().Stat(h.path)
	if failed != nil {
		return fault(failed)
	}
	return value.Boolean(info != nil)
}

// ---- changing ----

// Create is create() (mục 5): the file or the folder the path says, with the folders above it; what is there of
// the same kind is kept as it is.
func (h *Handle) Create(arguments ...value.Value) value.Value {
	if failure := h.changing("create"); failure.IsInvalid() {
		return failure
	}
	if failure := h.none("create", arguments); failure.IsInvalid() {
		return failure
	}
	info, failed := h.zone().Create(h.path)
	if failed != nil {
		return fault(failed)
	}
	return infoValue(info)
}

// Write is write(dữ_liệu) (mục 5): makes or replaces the file with data that fits its extension, a value of JSON
// for .json, a string for an extension of text, bytes or an upload for any other. Nothing is turned into another
// kind on the way: a "data:…" string written to .png is refused, not decoded.
func (h *Handle) Write(arguments ...value.Value) value.Value {
	if failure := h.changing("write"); failure.IsInvalid() {
		return failure
	}
	if len(arguments) != 1 {
		return value.Fail(value.InvalidArgumentCode, h.spelling()+".write(dữ_liệu) nhận một giá trị: chuỗi, một giá trị JSON, bytes hay một upload theo đuôi của file")
	}
	if h.path.Code {
		return fault(readOnly(h.path, "write()"))
	}
	if h.path.Directory || h.path.Root() {
		return value.Fail(value.ConflictCode, strconv.Quote(h.path.String())+` là thư mục; write() ghi một file, đường dẫn không có "/" cuối: file.open("notes.txt").write(…)`)
	}
	source, done, failure := h.source(arguments[0])
	if failure.IsInvalid() {
		return failure
	}
	defer done()
	info, failed := h.zone().Write(h.path, source)
	if failed != nil {
		return fault(failed)
	}
	return infoValue(info)
}

// source is what write() writes, read from data as the extension says.
func (h *Handle) source(data value.Value) (io.Reader, func(), value.Value) {
	none := func() {}
	written := strconv.Quote(h.path.String())
	switch KindOf(h.path.Name()) {
	case JSON:
		room := ReadLimit
		if where := notJSON(data, map[value.Value]bool{}, 0, &room); where != "" {
			if room < 0 {
				return nil, none, value.Fail(value.TooLargeCode, written+": "+where)
			}
			return nil, none, value.Fail(value.InvalidArgumentCode, written+" nhận một giá trị viết được thành JSON; "+where)
		}
		encoded := value.Stringify(data, "")
		if encoded.IsInvalid() {
			return nil, none, value.Fail(value.InvalidArgumentCode, written+": không viết được thành JSON ("+encoded.Message()+")")
		}
		return strings.NewReader(encoded.Text()), none, value.Value{}
	case Text:
		if !data.IsString() {
			hint := ""
			if data.IsObject() || data.IsArray() {
				hint = "; object và mảng ghi vào .json, hay JSON.stringify(…) trước"
			}
			return nil, none, value.Fail(value.InvalidArgumentCode, written+" nhận chuỗi (đuôi chữ), không nhận "+kindName(data)+hint)
		}
		if !utf8.ValidString(data.Text()) {
			// what text() would refuse to read back: bytes from outside that are not UTF-8
			return nil, none, value.Fail(value.InvalidArgumentCode, written+" nhận chữ UTF-8; chuỗi này có byte không phải UTF-8 (dữ liệu từ ngoài kiểm trước khi ghi, hay ghi bytes vào đuôi nhị phân)")
		}
		return strings.NewReader(data.Text()), none, value.Value{}
	}
	switch {
	case data.IsBytes():
		return bytes.NewReader(data.Bytes()), none, value.Value{}
	case data.IsStruct():
		if upload, ok := data.Host().(*Upload); ok {
			opened, err := upload.header.Open()
			if err != nil {
				return nil, none, value.Fail(value.InvalidInputCode, "không đọc được upload "+strconv.Quote(upload.field)+" của request")
			}
			return opened, func() { opened.Close() }, value.Value{}
		}
	case data.IsString() && strings.HasPrefix(data.Text(), "data:"):
		return nil, none, value.Fail(value.InvalidArgumentCode, written+` nhận bytes hay một upload (đuôi nhị phân hay không đuôi); một chuỗi "data:…" không được giải mã ngầm, giải mã là việc tường minh của người viết (KITWORK_FILE.md mục 5)`)
	}
	return nil, none, value.Fail(value.InvalidArgumentCode, written+" nhận bytes hay một upload (đuôi nhị phân hay không đuôi), không nhận "+kindName(data)+"; chữ ghi vào đuôi chữ (.txt, .md, .csv…), giá trị JSON vào .json")
}

// kindName names the kind of a value for a message.
func kindName(data value.Value) string {
	switch data.Host().(type) {
	case *Upload:
		return "một upload"
	case *Handle:
		return "một handle của file"
	}
	return data.Kind()
}

// maxJSONDepth is how deep a value written as JSON may nest: deeper is a failure, never Go's stack.
const maxJSONDepth = 10_000

// notJSON is where in data a value lies that JSON cannot write, "" when there is none: undefined, a function,
// bytes, a value of the engine, a number JSON has not, a string of bytes that are not UTF-8, a value that holds
// itself (active are the arrays and objects walked into, a set: a list read again at each step cost O(depth) a
// value), or one nested past maxJSONDepth. room is what is left of ReadLimit as data is walked, taken by each value at
// least as much as JSON writes of it: a byte for any value, a string's text and quotes, an array's or an object's
// brackets and commas, a key's text, quotes and colon. Past it the JSON is more than json() reads back, said at once:
// a value an array holds twice is walked twice, so fifty nested a = [a, a] would be 2^50 paths, and until 13/11/2026
// only the depth was bounded.
func notJSON(data value.Value, active map[value.Value]bool, depth int, room *int64) string {
	at := func() string {
		if depth == 0 {
			return "giá trị"
		}
		return "một giá trị bên trong"
	}
	if *room--; data.IsString() {
		*room -= int64(len(data.Text())) + 1
	}
	if *room < 0 {
		return jsonTooLarge()
	}
	switch {
	case data.IsNull() || data.IsBoolean():
		return ""
	case data.IsString():
		if !utf8.ValidString(data.Text()) {
			return at() + " là chuỗi có byte không phải UTF-8"
		}
		return ""
	case data.IsNumber():
		if n := data.Number(); math.IsNaN(n) || math.IsInf(n, 0) {
			return at() + " là " + data.ToString().Text() + ", JSON không có số đó"
		}
		return ""
	case data.IsArray() || data.IsObject():
		if depth >= maxJSONDepth {
			return "giá trị lồng quá " + strconv.Itoa(maxJSONDepth) + " tầng"
		}
		if active[data] {
			return "giá trị chứa chính nó, JSON không viết được"
		}
		if *room -= int64(data.Count()); *room < 0 { // with the byte above, the brackets and the commas
			return jsonTooLarge()
		}
		active[data] = true
		if data.IsArray() {
			for _, child := range data.Items() {
				if where := notJSON(child, active, depth+1, room); where != "" {
					return where
				}
			}
		} else {
			for _, key := range data.Keys() {
				if *room -= int64(len(key)) + 3; *room < 0 {
					return jsonTooLarge()
				}
				if where := notJSON(data.Get(key), active, depth+1, room); where != "" {
					return where
				}
			}
		}
		delete(active, data)
		return ""
	}
	if depth == 0 && (data.IsBytes() || data.IsStruct()) {
		return "giá trị là " + kindName(data) + ", không phải object, mảng, chuỗi, số, boolean hay null; bytes và upload ghi vào đuôi nhị phân hay không đuôi"
	}
	return at() + " là " + kindName(data)
}

// jsonTooLarge says a value's JSON is past ReadLimit.
func jsonTooLarge() string {
	return "giá trị viết thành JSON quá " + strconv.FormatInt(ReadLimit>>20, 10) + " MiB, json() không đọc lại được file này: chia thành nhiều file, hay ghi vào database"
}

// target is the destination of copy() and move(): a path of the data zone, or a handle.
func (h *Handle) target(word string, arguments []value.Value) (Path, value.Value) {
	if len(arguments) == 1 {
		switch {
		case arguments[0].IsString():
			path, failed := Parse(arguments[0].Text())
			if failed != nil {
				return Path{}, fault(failed)
			}
			return path, value.Value{}
		case arguments[0].IsStruct():
			if other, ok := arguments[0].Host().(*Handle); ok {
				return other.path, value.Value{}
			}
		}
	}
	return Path{}, value.Fail(value.InvalidArgumentCode, h.spelling()+"."+word+"(đích) nhận một đường dẫn trong vùng dữ liệu hay một handle: "+word+"("+strconv.Quote("backup/"+h.path.Name())+")")
}

// Copy is copy(đích) (mục 8): the file or the folder copied, never over what is there; from the code zone into the
// data zone too.
func (h *Handle) Copy(arguments ...value.Value) value.Value {
	if failure := h.changing("copy"); failure.IsInvalid() {
		return failure
	}
	target, failure := h.target("copy", arguments)
	if failure.IsInvalid() {
		return failure
	}
	if target.Code {
		return fault(readOnly(target, "copy() tới"))
	}
	info, failed := h.file.zone(false).Copy(h.zone(), h.path, target, TreeLimit, h.stop)
	if failed != nil {
		return fault(failed)
	}
	return infoValue(info)
}

// Move is move(đích) (mục 8): the file or the folder moved inside the data zone; this handle keeps its path.
func (h *Handle) Move(arguments ...value.Value) value.Value {
	if failure := h.changing("move"); failure.IsInvalid() {
		return failure
	}
	target, failure := h.target("move", arguments)
	if failure.IsInvalid() {
		return failure
	}
	if target.Code {
		return fault(readOnly(target, "move() tới"))
	}
	info, failed := h.file.zone(false).Move(h.path, target)
	if failed != nil {
		return fault(failed)
	}
	return infoValue(info)
}

// Delete is delete() (mục 7): true when the file or the empty folder was removed, false when nothing was there;
// never the content of a folder.
func (h *Handle) Delete(arguments ...value.Value) value.Value {
	if failure := h.changing("delete"); failure.IsInvalid() {
		return failure
	}
	if failure := h.none("delete", arguments); failure.IsInvalid() {
		return failure
	}
	removed, failed := h.zone().Delete(h.path)
	if failed != nil {
		return fault(failed)
	}
	return value.Boolean(removed)
}

// Clear is clear() (mục 7): everything inside the folder removed, the folder kept; the number of entries removed.
// Stopped midway, past TreeLimit or as the request ends, it is PARTIAL with error.progress.
func (h *Handle) Clear(arguments ...value.Value) value.Value {
	if failure := h.changing("clear"); failure.IsInvalid() {
		return failure
	}
	if failure := h.none("clear", arguments); failure.IsInvalid() {
		return failure
	}
	removed, failed := h.zone().Clear(h.path, TreeLimit, h.stop)
	if failed != nil {
		return fault(failed)
	}
	return value.Number(float64(removed))
}

// MissingMember is a word a handle has not.
func (h *Handle) MissingMember(name string) value.Value {
	if name == "remove" {
		return value.Fail(value.TypeCode, h.spelling()+".remove() không có: làm rỗng rồi xoá, clear() rồi delete() (KITWORK_FILE.md mục 7)")
	}
	return value.Fail(value.TypeCode, h.spelling()+"."+name+" không có: handle có segment, name, path, read, text, bytes, json, list, info, exists, create, write, copy, move, delete, clear (KITWORK_FILE.md mục 3)")
}

// infoValue is a FileInfo as a program reads it (mục 6).
func infoValue(info *Info) value.Value {
	name, kind, size, extension := value.String(info.Path.Name()), value.String("file"), value.Number(float64(info.Size)), value.Null()
	if info.Directory {
		kind, size = value.String("directory"), value.Null()
	} else if written := Extension(info.Path.Name()); written != "" {
		extension = value.String(written)
	}
	created := value.Null()
	if !info.Created.IsZero() {
		created = value.String(info.Created.UTC().Format(time.RFC3339))
	}
	return value.Object().
		Set("name", name).
		Set("path", value.String(info.Path.String())).
		Set("type", kind).
		Set("size", size).
		Set("extension", extension).
		Set("createdAt", created).
		Set("modifiedAt", value.String(info.Modified.UTC().Format(time.RFC3339)))
}

// ---- serving (context.file, mục 9) ----

// Serve answers with the file of the handle from the disk: Range, ETag and Last-Modified are the system's, the
// bytes never pass through the VM. mediaType is what .type(…) set, "" to take it by the extension. A path out of
// the zone, one the code zone never reaches, a folder, a file that is not there: 404, which never tells which.
func (h *Handle) Serve(w http.ResponseWriter, r *http.Request, mediaType string) {
	file, stat, failed := h.zone().Open(h.path)
	if failed != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	if mediaType == "" {
		mediaType = mediaTypeOf(Extension(h.path.Name()))
	}
	header := w.Header()
	header.Set("Content-Type", mediaType)
	header.Set("X-Content-Type-Options", "nosniff")
	if !h.path.Code && RunsAsPage(mediaType) {
		// what the app wrote, an upload under the name its sender chose: shown, its scripts not run, its origin not
		// the site's (09/11/2026; an upload "a.html" or "a.svg" ran as a page of the site)
		header.Set("Content-Security-Policy", "sandbox")
	}
	header.Set("ETag", `"`+strconv.FormatInt(stat.ModTime().UnixNano(), 16)+"-"+strconv.FormatInt(stat.Size(), 16)+`"`)
	http.ServeContent(w, r, stat.Name(), stat.ModTime(), file)
}

// RunsAsPage reports a media type a browser opens as a document that may run scripts: HTML, XHTML, SVG, XML and its
// +xml kinds, XSLT.
func RunsAsPage(mediaType string) bool {
	base, _, _ := strings.Cut(mediaType, ";")
	base = strings.ToLower(strings.TrimSpace(base))
	switch base {
	case "text/html", "application/xhtml+xml", "image/svg+xml", "text/xml", "application/xml", "text/xsl":
		return true
	}
	return strings.HasSuffix(base, "+xml")
}
