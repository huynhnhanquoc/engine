package queue

import (
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/kitwork/engine2/core/value"
)

// wideData is fifty arrays, each holding the one before twice: 50 values in memory, 2^50 paths.
func wideData(levels int) value.Value {
	wide := value.Array([]value.Value{value.Number(1)})
	for i := 0; i < levels; i++ {
		wide = value.Array([]value.Value{wide, wide})
	}
	return wide
}

// Data that holds a value many times is measured as JSON writes it, each time, and refused as soon as it is past
// DataLimit: fifty nested a = [a, a] are TOO_LARGE at once, not 2^50 steps of a walk no energy bounds (13/11/2026:
// plain bounded only the depth). Ten of them, 1,024 paths, are data like any other.
//
// CONTROL: with plain's room never checked, encodeData of the fifty did not return before the test's deadline.
func TestDataThatHoldsAValueManyTimesIsMeasuredEachTime(t *testing.T) {
	start := time.Now()
	_, failure := encodeData(value.Object().Set("wide", wideData(50)))
	if failure.Code() != value.TooLargeCode || !strings.Contains(failure.Message(), "dữ liệu viết thành JSON quá trần 64 KiB của một lần chạy (đã quá ở data.wide[0]") {
		t.Fatalf("%s %s", failure.Code(), failure.Message())
	}
	if took := time.Since(start); took > time.Second {
		t.Errorf("refused in %v", took)
	}
	ten := wideData(10)
	data, failure := encodeData(ten)
	if failure.IsInvalid() || data != value.Stringify(ten, "").Text() {
		t.Fatalf("ten levels: %v %d bytes", failure, len(data))
	}
}

// randomData is a value JSON writes as itself, made of what a program sends: numbers, strings (Vietnamese, quotes,
// backslashes, control characters), booleans, null, times, arrays and objects, some held twice.
func randomData(random *rand.Rand, depth int, shared *[]value.Value) value.Value {
	switch pick := random.Intn(12); {
	case depth > 4 || pick < 3:
		switch random.Intn(5) {
		case 0:
			return value.Number(float64(random.Intn(2_000_000)-1_000_000) / float64(1+random.Intn(100)))
		case 1:
			return value.Boolean(random.Intn(2) == 0)
		case 2:
			return value.Null()
		case 3:
			return value.Time(time.Date(2026, 11, 13, random.Intn(24), 0, 0, 0, time.UTC))
		}
		return value.String(dataText(random))
	case pick < 5 && len(*shared) > 0:
		return (*shared)[random.Intn(len(*shared))]
	case pick < 8:
		items := make([]value.Value, random.Intn(6))
		for i := range items {
			items[i] = randomData(random, depth+1, shared)
		}
		made := value.Array(items)
		*shared = append(*shared, made)
		return made
	}
	made := value.Object()
	for i, n := 0, random.Intn(6); i < n; i++ {
		made.Set(dataText(random), randomData(random, depth+1, shared))
	}
	*shared = append(*shared, made)
	return made
}

func dataText(random *rand.Rand) string {
	parts := []string{"a", "Trà sữa", "\"", "\\", "\n", "\t", "\x01", "<", "/", "đ", "😀", "0", "price", ""}
	var b strings.Builder
	for i, n := 0, random.Intn(5); i < n; i++ {
		b.WriteString(parts[random.Intn(len(parts))])
	}
	return b.String()
}

// What plain takes of its room for a value is never more than JSON writes of it: whenever it says data is too
// large for a room, JSON.stringify of that data is longer than the room. Checked on random data, with rooms around
// its length.
//
// CONTROL: with a key taken as twice its length, plain refused data whose JSON fit; this test failed.
func TestPlainTakesNoMoreThanJSONWrites(t *testing.T) {
	random := rand.New(rand.NewSource(13112026))
	refused := 0
	for run := 0; run < 20000; run++ {
		var shared []value.Value
		data := randomData(random, 0, &shared)
		written := value.Stringify(data, "").Text()
		start := len(written) - 8 + random.Intn(16)
		room := start
		if problem := plain(data, "data", 0, &room); room < 0 {
			refused++
			if len(written) <= start {
				t.Fatalf("refused for room %d (%s), its JSON is %d bytes: %s", start, problem, len(written), written)
			}
		}
	}
	if refused < 1000 {
		t.Fatalf("only %d of the runs were refused: the rooms do not reach the data's length", refused)
	}
	t.Logf("%d runs refused, each longer than its room", refused)
}
