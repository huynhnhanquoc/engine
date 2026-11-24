package value_test

import (
	"math"
	"math/rand/v2"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/kitwork/engine2/core/value"
)

// The fast paths of core/value keep what a program sees and change what it costs. Each test here
// holds one of them in place: a cost that is an allocation count is exact; a cost that is time is a
// ratio against a reference, measured as the best of several interleaved trials so that a loaded
// machine slows both sides, with a limit well apart from both what the fast path measures and what
// the general path measures.

var (
	sinkValue   value.Value
	sinkBoolean bool
)

// costRatio is the best time of measured over the best time of reference, each running its own loop
// of the same number of rounds, the trials alternating. The rounds grow until the quicker side takes
// a few milliseconds, since a clock (Windows' among them) may tick at half a millisecond, or until
// the slower side takes fifty, when the two are so far apart that the tick no longer matters.
func costRatio(measured, reference func(rounds int)) float64 {
	timed := func(f func(int), rounds int) time.Duration {
		start := time.Now()
		f(rounds)
		return time.Since(start)
	}
	rounds := 1000
	for rounds < 1<<30 {
		first, second := timed(measured, rounds), timed(reference, rounds)
		if min(first, second) >= 4*time.Millisecond || max(first, second) >= 50*time.Millisecond {
			break
		}
		rounds *= 2
	}
	measuredBest, referenceBest := time.Duration(math.MaxInt64), time.Duration(math.MaxInt64)
	for trial := 0; trial < 7; trial++ {
		measuredBest = min(measuredBest, timed(measured, rounds))
		referenceBest = min(referenceBest, timed(reference, rounds))
	}
	return float64(measuredBest) / float64(referenceBest)
}

// requireCost fails when measured costs more than limit times what reference costs in each of five
// measures: a busy machine can slow every trial of one side once (12/11/2026), a cost that is there is
// there every time.
func requireCost(t *testing.T, what string, measured, reference func(rounds int), limit float64) {
	t.Helper()
	if testing.CoverMode() != "" {
		t.Skip("costs are not compared under coverage counters")
	}
	var ratios []string
	for attempt := 0; attempt < 5; attempt++ {
		ratio := costRatio(measured, reference)
		ratios = append(ratios, strconv.FormatFloat(ratio, 'f', 2, 64))
		if ratio <= limit {
			return
		}
	}
	t.Errorf("%s costs %s times its reference, want at most %.2f", what, strings.Join(ratios, ", "), limit)
}

// Two numbers compare, multiply and divide at about the cost of number + number, which has always
// tested for two numbers first: without that test of their own, each ran ToPrimitive and ToNumber on
// both sides (Less twice for <= and >=), three to five times the cost of the add.
func TestTwoNumbersSkipTheConversions(t *testing.T) {
	x, y := value.Number(123), value.Number(10)
	add := func(rounds int) {
		for i := 0; i < rounds; i++ {
			sinkValue = x.Add(y)
		}
	}
	for _, c := range []struct {
		operator string
		run      func(rounds int)
	}{
		{"<", func(rounds int) {
			for i := 0; i < rounds; i++ {
				sinkBoolean = x.Less(y)
			}
		}},
		{"<=", func(rounds int) {
			for i := 0; i < rounds; i++ {
				sinkBoolean = x.LessEqual(y)
			}
		}},
		{">=", func(rounds int) {
			for i := 0; i < rounds; i++ {
				sinkBoolean = x.GreaterEqual(y)
			}
		}},
		{">", func(rounds int) {
			for i := 0; i < rounds; i++ {
				sinkBoolean = x.Greater(y)
			}
		}},
		{"*", func(rounds int) {
			for i := 0; i < rounds; i++ {
				sinkValue = x.Multiply(y)
			}
		}},
		{"/", func(rounds int) {
			for i := 0; i < rounds; i++ {
				sinkValue = x.Divide(y)
			}
		}},
	} {
		requireCost(t, "number "+c.operator+" number", c.run, add, 2.0)
	}
}

// The fast paths give what the general path gave: IEEE 754 for the arithmetic, and NaN ordering
// with nothing.
func TestTwoNumbersKeepJavaScriptResults(t *testing.T) {
	nan, infinity, negativeZero := math.NaN(), math.Inf(1), math.Copysign(0, -1)
	for _, c := range []struct {
		x, y                float64
		less, lessEqual     bool
		greater, greaterEqu bool
	}{
		{1, 2, true, true, false, false},
		{2, 1, false, false, true, true},
		{1, 1, false, true, false, true},
		{negativeZero, 0, false, true, false, true},
		{0, negativeZero, false, true, false, true},
		{nan, 1, false, false, false, false},
		{1, nan, false, false, false, false},
		{nan, nan, false, false, false, false},
		{infinity, math.MaxFloat64, false, false, true, true},
		{-infinity, -math.MaxFloat64, true, true, false, false},
	} {
		x, y := value.Number(c.x), value.Number(c.y)
		if x.Less(y) != c.less || x.LessEqual(y) != c.lessEqual || x.Greater(y) != c.greater || x.GreaterEqual(y) != c.greaterEqu {
			t.Errorf("%v against %v: < %v <= %v > %v >= %v", c.x, c.y, x.Less(y), x.LessEqual(y), x.Greater(y), x.GreaterEqual(y))
		}
	}
	for _, c := range []struct{ x, y, product, quotient float64 }{
		{6, 3, 18, 2},
		{1, 0, 0, infinity},
		{-1, 0, negativeZero, -infinity},
		{negativeZero, 5, negativeZero, negativeZero},
		{0, 0, 0, nan},
		{infinity, 0, nan, infinity},
	} {
		product, quotient := value.Number(c.x).Multiply(value.Number(c.y)).Number(), value.Number(c.x).Divide(value.Number(c.y)).Number()
		if !sameNumber(product, c.product) || !sameNumber(quotient, c.quotient) {
			t.Errorf("%v and %v: * %v / %v, want %v and %v", c.x, c.y, product, quotient, c.product, c.quotient)
		}
	}
}

// sameNumber is bit for bit, so -0 differs from 0 and NaN equals NaN.
func sameNumber(a, b float64) bool {
	return math.Float64bits(a) == math.Float64bits(b) || a != a && b != b
}

// x % y of two whole numbers, the dividend not negative, is the integer remainder; math.Mod, a
// software loop over the exponents, stays for every other pair. The reference is the same dividend
// and divisor half a unit apart, which math.Mod still computes.
func TestWholeNumberModuloIsTheIntegerRemainder(t *testing.T) {
	whole, fraction, divisor := value.Number(1000), value.Number(1000.5), value.Number(7)
	requireCost(t, "1000 % 7", func(rounds int) {
		for i := 0; i < rounds; i++ {
			sinkValue = whole.Modulo(divisor)
		}
	}, func(rounds int) {
		for i := 0; i < rounds; i++ {
			sinkValue = fraction.Modulo(divisor)
		}
	}, 0.6)
}

// Bit for bit what math.Mod gives, which is what JavaScript's % gives, on every edge of the integer
// path: -0 and 0, negatives on either side, fractions, NaN, the infinities, 2^53 and its neighbours.
func TestModuloAgreesWithMathModBitForBit(t *testing.T) {
	const limit = 1 << 53
	nan, infinity, negativeZero := math.NaN(), math.Inf(1), math.Copysign(0, -1)
	edges := []float64{
		0, negativeZero, 1, -1, 2, 3, 7, 10, -7, 0.5, -0.5, 1.5, 2.5, 1000, 1000.5, 123456789,
		limit - 2, limit - 1, limit, limit + 2, -limit, -(limit - 1), 1 << 62, 1 << 63, -(1 << 63),
		math.MaxInt32, math.MaxUint32, math.MaxFloat64, math.SmallestNonzeroFloat64, nan, infinity, -infinity,
	}
	for _, x := range edges {
		for _, y := range edges {
			got := value.Number(x).Modulo(value.Number(y)).Number()
			if want := math.Mod(x, y); !sameNumber(got, want) {
				t.Errorf("%v %% %v = %v (bits %x), want %v (bits %x)", x, y, got, math.Float64bits(got), want, math.Float64bits(want))
			}
		}
	}
	for x := 0; x < 300; x++ {
		for y := 1; y < 40; y++ {
			got := value.Number(float64(x)).Modulo(value.Number(float64(y))).Number()
			if want := math.Mod(float64(x), float64(y)); !sameNumber(got, want) {
				t.Fatalf("%d %% %d = %v, want %v", x, y, got, want)
			}
		}
	}
	// the dividend comes back as it was when it is zero: -0 % 5 is -0, as JavaScript prints it
	if got := value.Number(negativeZero).Modulo(value.Number(5)).Number(); math.Float64bits(got) != math.Float64bits(negativeZero) {
		t.Errorf("-0 %% 5 = %v, want -0", got)
	}
}

// An object of a few keys is three allocations: the properties, its keys and its items, made with
// room for four. The index map waits until the object holds more than eight keys; it was two more
// allocations, and the keys and items grew 1, 2, 4 on the way.
func TestASmallObjectIsThreeAllocations(t *testing.T) {
	id, title, done := value.Number(1), value.String("t"), value.Boolean(false)
	allocations := testing.AllocsPerRun(200, func() {
		o := value.Object()
		o.Set("id", id)
		o.Set("title", title)
		o.Set("done", done)
		sinkValue = o
	})
	if allocations > 3 {
		t.Errorf("an object of three keys makes %v allocations, want 3", allocations)
	}
}

// referenceObject is the order JavaScript lists an object's keys in, kept the plain way: a map of
// the items, the array indexes sorted by number, every other key in the order it was first set.
type referenceObject struct {
	items   map[string]value.Value
	indexes []uint64
	names   []string
}

func referenceIndex(key string) (uint64, bool) {
	if key == "" || len(key) > 10 || key[0] == '0' && len(key) > 1 {
		return 0, false
	}
	n := uint64(0)
	for i := 0; i < len(key); i++ {
		if key[i] < '0' || key[i] > '9' {
			return 0, false
		}
		n = n*10 + uint64(key[i]-'0')
	}
	return n, n < 1<<32-1
}

func (r *referenceObject) set(key string, item value.Value) {
	if _, ok := r.items[key]; !ok {
		if n, isIndex := referenceIndex(key); isIndex {
			at := 0
			for at < len(r.indexes) && r.indexes[at] < n {
				at++
			}
			r.indexes = append(r.indexes[:at], append([]uint64{n}, r.indexes[at:]...)...)
		} else {
			r.names = append(r.names, key)
		}
	}
	r.items[key] = item
}

func (r *referenceObject) delete(key string) bool {
	if _, ok := r.items[key]; !ok {
		return false
	}
	delete(r.items, key)
	if n, isIndex := referenceIndex(key); isIndex {
		for i, m := range r.indexes {
			if m == n {
				r.indexes = append(r.indexes[:i], r.indexes[i+1:]...)
				break
			}
		}
		return true
	}
	for i, name := range r.names {
		if name == key {
			r.names = append(r.names[:i], r.names[i+1:]...)
			break
		}
	}
	return true
}

func (r *referenceObject) keys() []string {
	out := make([]string, 0, len(r.items))
	for _, n := range r.indexes {
		out = append(out, strconv.FormatUint(n, 10))
	}
	return append(out, r.names...)
}

// Random sets, deletes and reads on objects that grow past the eight keys of the linear lookup and
// shrink back below it, against the reference: the same keys in the same order, the same items, the
// same answers to Has and Delete, at every step.
func TestObjectsAgreeWithAMapAtEverySize(t *testing.T) {
	random := rand.New(rand.NewPCG(2026, 11))
	pool := []string{
		"id", "title", "done", "name", "age", "a", "b", "c", "d", "e", "f", "g", "h", "valueOf", "toString",
		"0", "1", "2", "3", "7", "10", "42", "100", "007", "-1", "1.5", "4294967294", "4294967295", "", "length",
	}
	for trial := 0; trial < 3000; trial++ {
		o := value.Object()
		reference := &referenceObject{items: map[string]value.Value{}}
		steps := 1 + random.IntN(60)
		for step := 0; step < steps; step++ {
			key := pool[random.IntN(len(pool))]
			switch operation := random.IntN(10); {
			case operation < 6:
				item := value.Number(float64(random.IntN(1000)))
				o.Set(key, item)
				reference.set(key, item)
			case operation < 8:
				if got, want := o.Delete(key), reference.delete(key); got != want {
					t.Fatalf("trial %d step %d: Delete(%q) = %v, want %v", trial, step, key, got, want)
				}
			default:
				_, want := reference.items[key]
				if got := o.Has(key); got != want {
					t.Fatalf("trial %d step %d: Has(%q) = %v, want %v", trial, step, key, got, want)
				}
			}
			keys, want := o.Keys(), reference.keys()
			if strings.Join(keys, ",") != strings.Join(want, ",") || len(keys) != len(want) {
				t.Fatalf("trial %d step %d: keys %q, want %q", trial, step, keys, want)
			}
			for _, k := range want {
				if !o.Get(k).Equal(reference.items[k]) || !o.Has(k) {
					t.Fatalf("trial %d step %d: %q is %v, want %v", trial, step, k, o.Get(k).ToString().Text(), reference.items[k].ToString().Text())
				}
			}
		}
	}
}

// An own toString is found whether the object still compares its keys in order or has its index
// map: "" + o calls it at either size.
func TestAnOwnToStringIsFoundAtEitherSize(t *testing.T) {
	for _, size := range []int{0, 3, 8, 9, 20} {
		o := value.Object()
		for i := 0; i < size; i++ {
			o.Set("key"+strconv.Itoa(i), value.Number(float64(i)))
		}
		o.Set("toString", value.Function(func(...value.Value) value.Value { return value.String("own") }))
		if got := value.String("").Add(o).Text(); got != "own" {
			t.Errorf("with %d other keys: \"\" + o = %q, want \"own\"", size, got)
		}
		o.Delete("toString")
		if got := value.String("").Add(o).Text(); got != "[object Object]" {
			t.Errorf("with %d other keys and toString deleted: \"\" + o = %q", size, got)
		}
	}
}

// s[i] on ASCII text reads the bytes up to i, not the whole string: near the start of 64 KiB it
// costs what it costs on sixteen bytes, where checking the whole string cost a hundred times more.
func TestAnASCIIUnitReadsOnlyTheBytesBeforeIt(t *testing.T) {
	long, short := value.String(strings.Repeat("abcdefgh", 8192)), value.String("abcdefghabcdefgh")
	requireCost(t, "s[3] on 64 KiB", func(rounds int) {
		for i := 0; i < rounds; i++ {
			sinkValue = long.Index(3)
		}
	}, func(rounds int) {
		for i := 0; i < rounds; i++ {
			sinkValue = short.Index(3)
		}
	}, 3)
}

// byteScanLength is s.length the way it was counted for ASCII text, one byte a step: the reference
// for the eight-byte scan.
//
//go:noinline
func byteScanLength(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return -1
		}
	}
	return len(s)
}

var sinkLength int

// s.length checks the whole text for ASCII on every read, eight bytes a step: a loop to s.length
// pays that once per turn.
func TestASCIILengthScansEightBytesAStep(t *testing.T) {
	text := strings.Repeat("abcdefgh", 8192)
	s := value.String(text)
	requireCost(t, "s.length on 64 KiB", func(rounds int) {
		for i := 0; i < rounds; i++ {
			sinkValue = s.Get("length")
		}
	}, func(rounds int) {
		for i := 0; i < rounds; i++ {
			sinkLength = byteScanLength(text)
		}
	}, 0.65)
}

// Length and s[i] stay JavaScript's UTF-16 units when one character that is not ASCII sits at any
// byte of a string, before, inside or after an eight-byte step.
func TestUnitsAroundOneCharacterThatIsNotASCII(t *testing.T) {
	for _, other := range []string{"é", "Đ", "😀", "한"} {
		for size := 0; size <= 20; size++ {
			for at := 0; at <= size; at++ {
				text := strings.Repeat("a", at) + other + strings.Repeat("b", size-at)
				units := utf16.Encode([]rune(text))
				s := value.String(text)
				if got := s.Get("length").Number(); got != float64(len(units)) {
					t.Fatalf("%q.length = %v, want %d", text, got, len(units))
				}
				for i := -1; i <= len(text)+1; i++ {
					got := s.Index(i)
					if i < 0 || i >= len(units) {
						if !got.IsUndefined() {
							t.Fatalf("%q[%d] = %q, want undefined", text, i, got.Text())
						}
						continue
					}
					if want := value.StringFromUnits(units[i : i+1]); !got.Equal(want) {
						t.Fatalf("%q[%d] = %q, want %q", text, i, got.Text(), want.Text())
					}
				}
			}
		}
		if got := value.String(strings.Repeat("a", 16)).Get("length").Number(); got != 16 {
			t.Fatalf("16 ASCII bytes have length %v", got)
		}
	}
}

// The text of a whole number from 0 to 1023 is made once: o[3] and "a" + i read it without a new
// string, where each made one.
func TestSmallIntegerTextIsMadeOnce(t *testing.T) {
	three := value.Number(3)
	if allocations := testing.AllocsPerRun(200, func() { sinkValue = three.ToString() }); allocations != 0 {
		t.Errorf("(3).toString() makes %v allocations, want 0", allocations)
	}
	negativeZero := math.Copysign(0, -1)
	for _, c := range []struct {
		n    float64
		text string
	}{
		{0, "0"}, {negativeZero, "0"}, {1, "1"}, {42, "42"}, {1023, "1023"}, {1024, "1024"}, {-1, "-1"},
		{3.5, "3.5"}, {1023.5, "1023.5"}, {0.000001, "0.000001"}, {math.NaN(), "NaN"}, {math.Inf(1), "Infinity"},
	} {
		if got := value.Number(c.n).ToString().Text(); got != c.text {
			t.Errorf("String(%v) = %q, want %q", c.n, got, c.text)
		}
	}
	for i := 0; i < 1100; i++ {
		if got, want := value.Number(float64(i)).ToString().Text(), strconv.Itoa(i); got != want {
			t.Fatalf("String(%d) = %q", i, got)
		}
	}
	// a radix still makes its own text
	if got := value.Number(10).ToString(value.Number(2)).Text(); got != "1010" {
		t.Errorf("(10).toString(2) = %q, want \"1010\"", got)
	}
}

// A closure is the one allocation of its Lambda: Closure keeps the Function's callable inside the
// lambda instead of allocating one beside it. The runtime makes a Lambda each time a program reaches
// a function literal; with its parameters shared and its runner made once, that Lambda is all a
// closure costs.
func TestAClosureAllocatesNothingBesideItsLambda(t *testing.T) {
	lambdas := make([]value.Lambda, 512)
	next := 0
	allocations := testing.AllocsPerRun(200, func() {
		sinkValue = value.Closure(&lambdas[next])
		next++
	})
	if allocations != 0 {
		t.Errorf("Closure makes %v allocations beside its Lambda, want 0", allocations)
	}
}

// Closure stays what it was to every caller: the lambda back from Lambda(), a call through Run, a
// property of one function not seen on another, and two Closures of one lambda two functions.
func TestAClosureKeepsItsBehaviour(t *testing.T) {
	lambda := &value.Lambda{Name: "add", Params: []string{"a", "b"}}
	lambda.Run = func(l *value.Lambda, arguments []value.Value) value.Value {
		if l != lambda {
			return value.String("another lambda")
		}
		return arguments[0].Add(arguments[1])
	}
	first := value.Closure(lambda)
	if first.Lambda() != lambda || !first.IsFunction() {
		t.Fatal("Closure must hold the lambda it was given")
	}
	if got := first.Call([]value.Value{value.Number(2), value.Number(3)}); got.Number() != 5 {
		t.Fatalf("a call runs the lambda: got %v", got.ToString().Text())
	}
	if first.Get("name").Text() != "add" || first.Get("length").Number() != 2 {
		t.Fatal("name and length come from the lambda")
	}
	first.Set("tag", value.Number(1))

	second := value.Closure(lambda)
	if second.Lambda() != lambda || second.Equal(first) || !first.Equal(first) {
		t.Fatal("two Closures of one lambda are two functions, each === itself")
	}
	if !second.Get("tag").IsUndefined() || first.Get("tag").Number() != 1 {
		t.Fatal("a property of one function is not another's")
	}

	copied := *lambda // a Lambda copied from one already closed starts as one never closed
	third := value.Closure(&copied)
	if third.Lambda() != &copied || third.Equal(first) || !third.Get("tag").IsUndefined() {
		t.Fatal("the closure of a copied lambda holds the copy and nothing of the original's function")
	}
	if first.Lambda() != lambda || first.Get("tag").Number() != 1 {
		t.Fatal("closing a copy leaves the original's function as it was")
	}
	if !value.Closure(nil).IsInvalid() {
		t.Fatal("a closure of no lambda is a failure")
	}
}

func TestTypeofMakesNothing(t *testing.T) {
	values := []value.Value{
		value.Undefined(), value.Null(), value.Number(1), value.Boolean(true), value.String("s"),
		value.Object(), value.Array(nil), value.Function(func(...value.Value) value.Value { return value.Undefined() }),
		value.Time(time.Unix(0, 0)), value.Duration(time.Second), value.Bytes([]byte("b")),
	}
	words := []string{"undefined", "object", "number", "boolean", "string", "object", "object", "function", "object", "object", "object"}
	for i, v := range values {
		if allocations := testing.AllocsPerRun(100, func() { sinkValue = v.Typeof() }); allocations != 0 {
			t.Errorf("typeof of %s makes %v allocations, want 0", words[i], allocations)
		}
		if got := v.Typeof().Text(); got != words[i] {
			t.Errorf("typeof value %d = %q, want %q", i, got, words[i])
		}
	}
	if failure := value.Fail(value.TypeCode, "x"); !failure.Typeof().IsInvalid() {
		t.Errorf("typeof of a failure is the failure")
	}
}
