package file

import (
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/kitwork/engine2/core/value"
)

// A value that holds another many times is measured as JSON writes it, each time, and refused as soon as its JSON is
// past ReadLimit: fifty nested a = [a, a] are TOO_LARGE in well under a second, not 2^50 steps of a walk no energy
// bounds (13/11/2026: notJSON bounded only the depth, and read the arrays walked into again at each step). Ten of
// them, 1,024 paths, are written as JSON.stringify writes them.
//
// CONTROL: with notJSON's room never checked, the walk of the fifty did not return before the test's deadline.
func TestAValueThatHoldsAnotherManyTimesIsMeasuredEachTime(t *testing.T) {
	wide := value.Array([]value.Value{value.Number(1)})
	for i := 0; i < 50; i++ {
		wide = value.Array([]value.Value{wide, wide})
	}
	start := time.Now()
	room := ReadLimit
	if where := notJSON(wide, map[value.Value]bool{}, 0, &room); room >= 0 || where != jsonTooLarge() {
		t.Fatalf("room %d: %q", room, where)
	}
	// under the race detector the walk takes some 0.9 s where it takes 0.2, near a second it passed once in five
	deadline := time.Second
	if underRace {
		deadline = 5 * time.Second
	}
	if took := time.Since(start); took > deadline {
		t.Errorf("refused in %v", took)
	}
	ten := value.Array([]value.Value{value.Number(1)})
	for i := 0; i < 10; i++ {
		ten = value.Array([]value.Value{ten, ten})
	}
	room = ReadLimit
	if where := notJSON(ten, map[value.Value]bool{}, 0, &room); where != "" {
		t.Fatalf("ten levels: %q", where)
	}
}

// jsonData is a value JSON writes: numbers, strings (Vietnamese, quotes, backslashes, control characters),
// booleans, null, arrays and objects, some held twice.
func jsonData(random *rand.Rand, depth int, shared *[]value.Value) value.Value {
	switch pick := random.Intn(12); {
	case depth > 4 || pick < 3:
		switch random.Intn(4) {
		case 0:
			return value.Number(float64(random.Intn(2_000_000)-1_000_000) / float64(1+random.Intn(100)))
		case 1:
			return value.Boolean(random.Intn(2) == 0)
		case 2:
			return value.Null()
		}
		return value.String(jsonText(random))
	case pick < 5 && len(*shared) > 0:
		return (*shared)[random.Intn(len(*shared))]
	case pick < 8:
		items := make([]value.Value, random.Intn(6))
		for i := range items {
			items[i] = jsonData(random, depth+1, shared)
		}
		made := value.Array(items)
		*shared = append(*shared, made)
		return made
	}
	made := value.Object()
	for i, n := 0, random.Intn(6); i < n; i++ {
		made.Set(jsonText(random), jsonData(random, depth+1, shared))
	}
	*shared = append(*shared, made)
	return made
}

func jsonText(random *rand.Rand) string {
	parts := []string{"a", "Trà sữa", "\"", "\\", "\n", "\t", "\x01", "<", "/", "đ", "😀", "0", "price", ""}
	var b strings.Builder
	for i, n := 0, random.Intn(5); i < n; i++ {
		b.WriteString(parts[random.Intn(len(parts))])
	}
	return b.String()
}

// What notJSON takes of its room for a value is never more than JSON writes of it: whenever it says a value's JSON is
// past a room, JSON.stringify of that value is longer than the room. Checked on random values, with rooms around their
// length.
//
// CONTROL: with an array's brackets and commas taken twice, notJSON refused values whose JSON fit; this test failed.
func TestNotJSONTakesNoMoreThanJSONWrites(t *testing.T) {
	random := rand.New(rand.NewSource(13112026))
	refused := 0
	for run := 0; run < 20000; run++ {
		var shared []value.Value
		data := jsonData(random, 0, &shared)
		written := value.Stringify(data, "").Text()
		start := int64(len(written) - 8 + random.Intn(16))
		room := start
		if where := notJSON(data, map[value.Value]bool{}, 0, &room); room < 0 {
			refused++
			if int64(len(written)) <= start {
				t.Fatalf("refused for room %d (%s), its JSON is %d bytes: %s", start, where, len(written), written)
			}
		}
	}
	if refused < 1000 {
		t.Fatalf("only %d of the runs were refused: the rooms do not reach the values' length", refused)
	}
	t.Logf("%d runs refused, each longer than its room", refused)
}
