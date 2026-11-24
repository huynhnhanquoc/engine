package value_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/kitwork/engine2/core/value"
)

// What an object costs as a program uses it: a few keys (an options bag), a row, many keys, array indexes in order
// and against it, deletes; and JSON.parse of a text a request sends. The engine must never be slower than it was
// (13/11/2026: array indexes set against their order cost O(n²), JSON.parse of 20,000 in descending order 6.1 s).

var keyNames = func() []string {
	names := make([]string, 100)
	for i := range names {
		names[i] = "key" + strconv.Itoa(i)
	}
	return names
}()

func BenchmarkObjectFourKeys(b *testing.B) {
	for i := 0; i < b.N; i++ {
		o := value.Object()
		o.Set("id", value.Number(1)).Set("name", value.String("a")).Set("price", value.Number(2)).Set("open", value.Boolean(true))
		_ = o.Get("price")
	}
}

func BenchmarkObjectRow(b *testing.B) {
	for i := 0; i < b.N; i++ {
		o := value.Object()
		for _, name := range keyNames[:8] {
			o.Set(name, value.Number(1))
		}
		for _, name := range keyNames[:8] {
			_ = o.Get(name)
		}
		_ = o.Keys()
	}
}

func BenchmarkObjectHundredKeys(b *testing.B) {
	for i := 0; i < b.N; i++ {
		o := value.Object()
		for _, name := range keyNames {
			o.Set(name, value.Number(1))
		}
		for _, name := range keyNames {
			_ = o.Get(name)
		}
		_ = o.Keys()
	}
}

func BenchmarkObjectIndexesInOrder(b *testing.B) {
	for i := 0; i < b.N; i++ {
		o := value.Object()
		for k := 0; k < 100; k++ {
			o.Set(strconv.Itoa(k), value.Number(1))
		}
		_ = o.Keys()
	}
}

func BenchmarkObjectIndexesAgainstOrder(b *testing.B) {
	for i := 0; i < b.N; i++ {
		o := value.Object()
		for k := 99; k >= 0; k-- {
			o.Set(strconv.Itoa(k), value.Number(1))
		}
		_ = o.Keys()
	}
}

func BenchmarkObjectSetAndDelete(b *testing.B) {
	for i := 0; i < b.N; i++ {
		o := value.Object()
		for _, name := range keyNames {
			o.Set(name, value.Number(1))
		}
		for _, name := range keyNames {
			o.Delete(name)
		}
	}
}

func BenchmarkJSONParseRows(b *testing.B) {
	text := `[` + strings.Repeat(`{"id":1,"name":"Trà","price":25000,"open":true,"tags":["a","b"]},`, 99) + `{"id":1}]`
	b.SetBytes(int64(len(text)))
	for i := 0; i < b.N; i++ {
		_ = value.ParseJSON(text)
	}
}

// descendingIndexes is the JSON of an object whose n keys are array indexes, from n-1 down to 0.
func descendingIndexes(n int) string {
	var b strings.Builder
	b.WriteByte('{')
	for k := n - 1; k >= 0; k-- {
		if k < n-1 {
			b.WriteByte(',')
		}
		b.WriteString(`"` + strconv.Itoa(k) + `":0`)
	}
	b.WriteByte('}')
	return b.String()
}

func BenchmarkJSONParseIndexesAgainstOrder(b *testing.B) {
	text := descendingIndexes(5000)
	b.SetBytes(int64(len(text)))
	for i := 0; i < b.N; i++ {
		_ = value.ParseJSON(text).Keys()
	}
}
