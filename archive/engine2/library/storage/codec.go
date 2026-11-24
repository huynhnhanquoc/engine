package storage

import (
	"encoding/binary"
	"errors"
	"math"
	"strconv"
	"time"

	"github.com/kitwork/engine2/core/value"
)

// A value is kept as bytes in all three places, written once by Encode and read back by Decode: what memory() gives
// back is what disk() and datastore() give back for the same write (mục 5), and every read is a value of its own, so
// two requests never hold one object. What can be kept is what can be written as JSON, and bytes: null, a boolean, a
// number, a string, bytes, an array, a plain object; a time and a duration keep their kind. A function, a handle of
// the engine (a database, a request, a routine), a failure are refused by name, never dropped. The format is the
// first engine's (storage/codec.go), byte for byte, so what it kept in disk() and datastore() reads here; an object
// is written in its keys' order, which the format leaves free (the first engine sorted them).

// codecVersion is the first byte of every encoded value, so a format of later can be told apart.
const codecVersion byte = 1

// maxDepth is how deep an array or an object may nest; past it the value holds itself or is too deep.
const maxDepth = 64

const (
	tagNull     byte = 'n'
	tagFalse    byte = 'f'
	tagTrue     byte = 't'
	tagNumber   byte = 'd'
	tagTime     byte = 'T'
	tagDuration byte = 'D'
	tagString   byte = 's'
	tagBytes    byte = 'b'
	tagArray    byte = 'a'
	tagObject   byte = 'o'
)

// ValueError is a value that cannot be kept: Path is where in it ("" for the value itself, ".items[2]"), Kind what
// is there.
type ValueError struct {
	Path string
	Kind string
}

func (e *ValueError) Error() string {
	if e.Path == "" {
		return "cannot keep " + e.Kind
	}
	return "cannot keep " + e.Kind + " at " + e.Path
}

// SizeError is a value past the limit.
type SizeError struct{ Limit int }

func (e *SizeError) Error() string {
	return "the value is past " + strconv.Itoa(e.Limit) + " bytes, the ceiling of one value"
}

// Encode writes v as one kept value, refusing it past limit bytes (limit <= 0 is no limit).
func Encode(v value.Value, limit int) ([]byte, error) {
	e := encoder{limit: limit, data: []byte{codecVersion}}
	if err := e.write(v, "", 0); err != nil {
		return nil, err
	}
	return e.data, nil
}

type encoder struct {
	data  []byte
	limit int
}

func (e *encoder) check() error {
	if e.limit > 0 && len(e.data) > e.limit {
		return &SizeError{Limit: e.limit}
	}
	return nil
}

func (e *encoder) tag(tag byte) error {
	e.data = append(e.data, tag)
	return e.check()
}

func (e *encoder) number(tag byte, n float64) error {
	e.data = append(e.data, tag)
	e.data = binary.BigEndian.AppendUint64(e.data, math.Float64bits(n))
	return e.check()
}

func (e *encoder) text(tag byte, text []byte) error {
	if e.limit > 0 && len(e.data)+len(text) > e.limit {
		return &SizeError{Limit: e.limit}
	}
	e.data = append(e.data, tag)
	e.data = binary.AppendUvarint(e.data, uint64(len(text)))
	e.data = append(e.data, text...)
	return e.check()
}

func (e *encoder) count(tag byte, n int) error {
	e.data = append(e.data, tag)
	e.data = binary.AppendUvarint(e.data, uint64(n))
	return e.check()
}

func (e *encoder) write(v value.Value, path string, depth int) error {
	switch {
	case v.IsNullish():
		return e.tag(tagNull) // undefined is kept as null, as the first engine kept it
	case v.IsBoolean():
		if v.Boolean() {
			return e.tag(tagTrue)
		}
		return e.tag(tagFalse)
	case v.IsNumber():
		return e.number(tagNumber, v.Number())
	case v.IsTime():
		return e.number(tagTime, float64(v.Time().UnixNano()))
	case v.IsDuration():
		return e.number(tagDuration, float64(v.Duration()))
	case v.IsString():
		return e.text(tagString, []byte(v.Text()))
	case v.IsBytes():
		return e.text(tagBytes, v.Bytes())
	case v.IsArray():
		if depth >= maxDepth {
			return &ValueError{Path: path, Kind: "an array that holds itself or nests past " + strconv.Itoa(maxDepth) + " levels"}
		}
		items := v.Items()
		if err := e.count(tagArray, len(items)); err != nil {
			return err
		}
		for index, item := range items {
			if err := e.write(item, path+"["+strconv.Itoa(index)+"]", depth+1); err != nil {
				return err
			}
		}
		return nil
	case v.IsObject():
		if depth >= maxDepth {
			return &ValueError{Path: path, Kind: "an object that holds itself or nests past " + strconv.Itoa(maxDepth) + " levels"}
		}
		keys := v.Keys()
		if err := e.count(tagObject, len(keys)); err != nil {
			return err
		}
		for _, key := range keys {
			if err := e.text(tagString, []byte(key)); err != nil {
				return err
			}
			if err := e.write(v.Get(key), path+"."+key, depth+1); err != nil {
				return err
			}
		}
		return nil
	case v.IsFunction():
		return &ValueError{Path: path, Kind: "a function"}
	case v.IsStruct():
		return &ValueError{Path: path, Kind: "a handle of the engine (" + v.Kind() + ")"}
	case v.IsInvalid():
		return &ValueError{Path: path, Kind: "a failure"}
	}
	return &ValueError{Path: path, Kind: "a value of kind " + v.Kind()}
}

// ErrCorrupt is bytes Decode cannot read: written by another format, or damaged.
var ErrCorrupt = errors.New("a kept value cannot be read")

// Decode reads a value Encode wrote (or the first engine did), as a value of its own.
func Decode(data []byte) (value.Value, error) {
	if len(data) == 0 || data[0] != codecVersion {
		return value.Value{}, ErrCorrupt
	}
	d := decoder{data: data[1:]}
	result, err := d.read(0)
	if err != nil {
		return value.Value{}, err
	}
	if len(d.data) != 0 {
		return value.Value{}, ErrCorrupt
	}
	return result, nil
}

type decoder struct{ data []byte }

func (d *decoder) read(depth int) (value.Value, error) {
	if len(d.data) == 0 || depth > maxDepth {
		return value.Value{}, ErrCorrupt
	}
	tag := d.data[0]
	d.data = d.data[1:]
	switch tag {
	case tagNull:
		return value.Null(), nil
	case tagFalse:
		return value.Boolean(false), nil
	case tagTrue:
		return value.Boolean(true), nil
	case tagNumber, tagTime, tagDuration:
		if len(d.data) < 8 {
			return value.Value{}, ErrCorrupt
		}
		n := math.Float64frombits(binary.BigEndian.Uint64(d.data))
		d.data = d.data[8:]
		switch tag {
		case tagTime:
			read := value.Time(time.Unix(0, int64(n)))
			if read.IsInvalid() {
				return value.Value{}, ErrCorrupt
			}
			return read, nil
		case tagDuration:
			return value.Duration(time.Duration(n)), nil
		}
		return value.Number(n), nil
	case tagString, tagBytes:
		text, err := d.text()
		if err != nil {
			return value.Value{}, err
		}
		if tag == tagString {
			return value.String(string(text)), nil
		}
		return value.Bytes(append([]byte(nil), text...)), nil
	case tagArray:
		count, err := d.count()
		if err != nil {
			return value.Value{}, err
		}
		items := make([]value.Value, count)
		for index := range items {
			if items[index], err = d.read(depth + 1); err != nil {
				return value.Value{}, err
			}
		}
		return value.Array(items), nil
	case tagObject:
		count, err := d.count()
		if err != nil {
			return value.Value{}, err
		}
		object := value.Object()
		for range count {
			if len(d.data) == 0 || d.data[0] != tagString {
				return value.Value{}, ErrCorrupt
			}
			d.data = d.data[1:]
			key, err := d.text()
			if err != nil {
				return value.Value{}, err
			}
			item, err := d.read(depth + 1)
			if err != nil {
				return value.Value{}, err
			}
			object.Set(string(key), item)
		}
		return object, nil
	}
	return value.Value{}, ErrCorrupt
}

func (d *decoder) count() (int, error) {
	n, size := binary.Uvarint(d.data)
	// each item takes a byte at least: a count past what is left is damage, not a value
	if size <= 0 || n > uint64(len(d.data)) {
		return 0, ErrCorrupt
	}
	d.data = d.data[size:]
	return int(n), nil
}

func (d *decoder) text() ([]byte, error) {
	n, size := binary.Uvarint(d.data)
	if size <= 0 || n > uint64(len(d.data)-size) {
		return nil, ErrCorrupt
	}
	text := d.data[size : size+int(n)]
	d.data = d.data[size+int(n):]
	return text, nil
}
