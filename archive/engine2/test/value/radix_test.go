package value_test

import (
	"bytes"
	"encoding/json"
	"math"
	"math/rand"
	"os/exec"
	"strconv"
	"testing"

	"github.com/kitwork/engine2/core/value"
)

// (n).toString(radix) writes what node writes, digit for digit (18/11/2026, docs/language.md mục 4.3): V8's
// DoubleToRadixCString, the fraction to the precision the double holds, rounded half to even with its carry taken
// back, the integer part's digits below a double's precision 0. Each case is a number of the edges (2^53, 2^63, 1e21,
// the largest and the smallest double, fractions of the radix and not) or one of 3,000 drawn from a fixed seed, every
// bit pattern of a finite double among them, in each radix from 2 to 36.
//
// CONTROL (18/11/2026): with the old radixText (the integer part past 2^63 in decimal, the fraction to 52 digits),
// 89,905 of 105,000 cases differed from node.
func TestRadixTextIsNodes(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed")
	}
	numbers := []float64{0, 1, -1, 0.5, 0.1, 1.0 / 3, 255.5, -255, 123.456, 4.35, 1e-7, 1e21, 1e300, -1e300,
		math.MaxFloat64, math.SmallestNonzeroFloat64, 1 << 53, 1<<53 + 2, 1 << 63, 1 << 64, 9007199254740993,
		0.0022002200220022, 2.5, 3.5, 0.999999999999999, 1 - 1e-16}
	random := rand.New(rand.NewSource(18112026))
	for len(numbers) < 3000 {
		var f float64
		switch random.Intn(3) {
		case 0:
			f = math.Float64frombits(random.Uint64()) // any bit pattern
		case 1:
			f = random.Float64() * math.Pow(10, float64(random.Intn(40)-20))
		default:
			f = float64(random.Int63n(1<<60)) / float64(int64(1)<<random.Intn(60))
		}
		if math.IsNaN(f) || math.IsInf(f, 0) {
			continue
		}
		if random.Intn(2) == 0 {
			f = -f
		}
		numbers = append(numbers, f)
	}
	texts := make([]string, len(numbers))
	for i, f := range numbers {
		texts[i] = strconv.FormatFloat(f, 'g', -1, 64) // read back by JSON.parse as the same double
	}
	encoded, _ := json.Marshal(texts)
	command := exec.Command("node", "-e", `const ns = JSON.parse(require("fs").readFileSync(0, "utf8")).map(Number);
const out = [];
for (const n of ns) { const row = []; for (let r = 2; r <= 36; r++) row.push(n.toString(r)); out.push(row); }
process.stdout.write(JSON.stringify(out));`)
	command.Stdin = bytes.NewReader(encoded)
	raw, err := command.Output()
	if err != nil {
		t.Fatalf("node: %v", err)
	}
	var expected [][]string
	if err := json.Unmarshal(raw, &expected); err != nil || len(expected) != len(numbers) {
		t.Fatalf("node output: %v", err)
	}
	differ := 0
	for i, f := range numbers {
		for radix := 2; radix <= 36; radix++ {
			got := value.Number(f).ToString(value.Number(float64(radix))).Text()
			if want := expected[i][radix-2]; got != want {
				if differ++; differ <= 10 {
					t.Errorf("(%s).toString(%d): %q, node %q", texts[i], radix, got, want)
				}
			}
		}
	}
	if differ > 0 {
		t.Errorf("%d of %d cases differ from node", differ, len(numbers)*35)
	}
	if got := value.Number(0.1).ToString(value.Number(10)).Text(); got != "0.1" {
		t.Errorf("(0.1).toString(10): %q, want 0.1 as String(0.1)", got)
	}
}
