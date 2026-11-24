package value_test

import (
	"math/rand"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kitwork/engine2/core/value"
)

// keyModel is how JavaScript lists an object's keys, kept the plain way: each key with the moment it was set; the
// array indexes first in numeric order, then every other key by that moment. A key deleted and set again is set
// anew, at the end of the others.
type keyModel struct {
	at    map[string]int
	items map[string]float64
	clock int
}

func newKeyModel() *keyModel {
	return &keyModel{at: map[string]int{}, items: map[string]float64{}}
}

// modelIndex is a key that is an array index: the decimal text of an integer below 2^32 - 1, no leading zero.
func modelIndex(key string) (uint64, bool) {
	if key == "" || key != strings.TrimLeft(key, "+-") || len(key) > 1 && key[0] == '0' {
		return 0, false
	}
	n, err := strconv.ParseUint(key, 10, 64)
	return n, err == nil && n < 1<<32-1
}

func (m *keyModel) set(key string, item float64) {
	if _, ok := m.at[key]; !ok {
		m.clock++
		m.at[key] = m.clock
	}
	m.items[key] = item
}

func (m *keyModel) delete(key string) {
	delete(m.at, key)
	delete(m.items, key)
}

func (m *keyModel) keys() []string {
	keys := make([]string, 0, len(m.at))
	for key := range m.at {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(a, b string) int {
		an, aIndex := modelIndex(a)
		bn, bIndex := modelIndex(b)
		switch {
		case aIndex && bIndex:
			return int(int64(an) - int64(bn))
		case aIndex:
			return -1
		case bIndex:
			return 1
		}
		return m.at[a] - m.at[b]
	})
	return keys
}

// An object lists its keys as JavaScript does through any run of sets, deletes and reads, small or past its index
// map: array indexes first in numeric order, then every other key in the order it was set; a deleted key gone, one
// set again at the end; Count, Has, Get, JSON.stringify and Object.freeze agreeing. Each run is set against a model
// that sorts the plain way (13/11/2026: keys are put in order as they are read, no longer as they are written).
//
// CONTROL: with order() returning at once, the first read after an index set out of its place listed it last; with
// Count not leaving out dead slots, the count after a delete was one too many; this test failed on each.
func TestAnObjectListsItsKeysAsJavaScriptDoes(t *testing.T) {
	pool := []string{"0", "1", "2", "3", "7", "10", "11", "42", "99", "100", "1000", "65536", "4294967294",
		"4294967295", "007", "-1", "1.5", "1e3", "", "a", "b", "c", "id", "name", "price", "x", "y", "z", "length",
		"constructor", "__proto__x", "toString"}
	random := rand.New(rand.NewSource(13112026))
	reads := 0
	for run := 0; run < 3000; run++ {
		object, model := value.Object(), newKeyModel()
		keys := pool[:4+random.Intn(len(pool)-3)]
		var trail []string
		for step, steps := 0, 1+random.Intn(120); step < steps; step++ {
			key := keys[random.Intn(len(keys))]
			if random.Intn(8) == 0 { // a key of its own, past any small object
				key = strconv.Itoa(random.Intn(400))
				if random.Intn(2) == 0 {
					key = "k" + key
				}
			}
			switch r := random.Intn(10); {
			case r < 6:
				item := float64(step)
				object.Set(key, value.Number(item))
				model.set(key, item)
				trail = append(trail, "set "+strconv.Quote(key))
			case r < 9:
				_, had := model.at[key]
				if object.Delete(key) != had {
					t.Fatalf("run %d: delete %q said %v, the key was there: %v\n%s", run, key, !had, had, strings.Join(trail, "; "))
				}
				model.delete(key)
				trail = append(trail, "delete "+strconv.Quote(key))
			default:
				reads++
				if got, want := object.Keys(), model.keys(); !slices.Equal(got, want) {
					t.Fatalf("run %d: keys\n %q\nwant\n %q\nafter %s", run, got, want, strings.Join(trail, "; "))
				}
			}
			if object.Count() != len(model.at) {
				t.Fatalf("run %d: count %d, want %d after %s", run, object.Count(), len(model.at), strings.Join(trail, "; "))
			}
		}
		for key, item := range model.items {
			if !object.Has(key) || object.Get(key).Number() != item {
				t.Fatalf("run %d: %q is %v (has %v), want %v after %s", run, key, object.Get(key), object.Has(key), item, strings.Join(trail, "; "))
			}
		}
		if object.Has("never") {
			t.Fatalf("run %d: has a key never set", run)
		}
		// JSON.stringify writes the keys in the same order, and Object.freeze keeps it
		want := model.keys()
		var b strings.Builder
		b.WriteByte('{')
		for i, key := range want {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(strconv.Quote(key) + ":" + strconv.FormatFloat(model.items[key], 'f', -1, 64))
		}
		b.WriteByte('}')
		if got := value.Stringify(object, "").Text(); got != b.String() {
			t.Fatalf("run %d: JSON\n %s\nwant\n %s\nafter %s", run, got, b.String(), strings.Join(trail, "; "))
		}
		if got := object.Freeze().Keys(); !slices.Equal(got, want) {
			t.Fatalf("run %d: frozen keys %q, want %q", run, got, want)
		}
	}
	if reads < 10000 {
		t.Fatalf("only %d reads were checked", reads)
	}
}

// A list of keys read before (Keys) keeps what it held as the object is written and read again.
func TestKeysReadBeforeKeepWhatTheyHeld(t *testing.T) {
	object := value.Object()
	for k := 20; k > 0; k-- {
		object.Set(strconv.Itoa(k), value.Number(1))
	}
	before := slices.Clone(object.Keys())
	held := object.Keys()
	object.Set("0", value.Number(1))
	object.Delete("5")
	_ = object.Keys()
	if !slices.Equal(held, before) {
		t.Errorf("the list read before holds %q, it held %q", held, before)
	}
}

// An object frozen to be read by every run at once is put in order as it is frozen: reads from many goroutines
// write nothing (go test -race). CONTROL: with Freeze not ordering it first, the race detector caught the reads
// writing the object as they put it in order.
func TestAFrozenObjectIsReadByEveryRunAtOnce(t *testing.T) {
	object := value.Object()
	for k := 40; k >= 0; k-- {
		object.Set(strconv.Itoa(k), value.Number(float64(k)))
	}
	object.Set("name", value.String("a"))
	object.Delete("7")
	object.Freeze()
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				if keys := object.Keys(); len(keys) != 41 || keys[0] != "0" || keys[40] != "name" {
					t.Errorf("keys %q", keys)
					return
				}
				_ = value.Stringify(object, "")
			}
		}()
	}
	wg.Wait()
}

// elapsed is how long fn takes, the best of three.
func elapsed(fn func()) time.Duration {
	best := time.Duration(1<<63 - 1)
	for range 3 {
		start := time.Now()
		fn()
		best = min(best, time.Since(start))
	}
	return best
}

// Array indexes set against their order, as a request's JSON can send them, and keys deleted from the front cost
// O(n log n) over all: eight times the keys take far less than the 64 times of a square, and 160,000 keys well under
// a second (13/11/2026: JSON.parse of 20,000 descending took 6.1 s; a request of 1 MiB holds 100,000).
//
// CONTROL: with the engine before (each array index put in its place as it was set, each delete closing up the keys
// after it), 5,000 and 20,000 keys took 0.21-0.37 s and 3.6-6.6 s, 17.5-18.8x for four times the keys; this test's
// own sizes take minutes there. With the engine now, under a whole suite running at once, 4x took 3.6-8.5x and 8x up
// to 26x (the bound was 24x until 14/11/2026: it failed so once; a square is 64x, and minutes).
func TestKeysAgainstTheirOrderCostNoSquare(t *testing.T) {
	if testing.Short() {
		t.Skip("timing in short mode")
	}
	for _, c := range []struct {
		name string
		run  func(n int)
	}{
		{"JSON.parse of array indexes in descending order", func(n int) {
			if keys := value.ParseJSON(descendingIndexes(n)).Keys(); len(keys) != n || keys[0] != "0" {
				t.Fatalf("%d keys, the first %q", len(keys), keys[0])
			}
		}},
		{"array indexes set in descending order, counted as they are set", func(n int) {
			object := value.Object()
			for k := n - 1; k >= 0; k-- {
				object.Set(strconv.Itoa(k), value.Number(1))
				if k%1000 == 0 {
					_ = object.Count()
				}
			}
			if keys := object.Keys(); len(keys) != n || keys[0] != "0" {
				t.Fatalf("%d keys", len(keys))
			}
		}},
		{"keys deleted from the front", func(n int) {
			object := value.Object()
			for k := 0; k < n; k++ {
				object.Set("k"+strconv.Itoa(k), value.Number(1))
			}
			for k := 0; k < n; k++ {
				object.Delete("k" + strconv.Itoa(k))
			}
			if object.Count() != 0 || len(object.Keys()) != 0 {
				t.Fatalf("%d keys left", object.Count())
			}
		}},
	} {
		const few, many = 20000, 160000
		small := elapsed(func() { c.run(few) })
		large := elapsed(func() { c.run(many) })
		ratio := float64(large) / float64(max(small, time.Microsecond))
		t.Logf("%s: %d in %v, %d in %v (%.1fx)", c.name, few, small, many, large, ratio)
		if ratio > 40 || large > time.Second {
			t.Errorf("%s: %d in %v, %d in %v (%.1fx): want well under the 64x of a square and under a second", c.name, few, small, many, large, ratio)
		}
	}
}
