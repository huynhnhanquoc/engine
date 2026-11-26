package value_test

import (
	"math"
	"testing"

	"github.com/huynhnhanquoc/engine/core/value"
)

// Scan vào *int và *int64 (core/value/helper.go, 26/11/2026): một số nguyên ngoài khoảng nói khoảng nó phải nằm
// trong, viết đủ chữ số; trước đó 3000000000 vào *int báo "3e+09 is not an integer", sai về một số nguyên. *int64 nhận
// tới Number.MAX_SAFE_INTEGER, 2^53 − 1: từ 2^53 hai số nguyên liền nhau là cùng một float64.
func TestScanIntegersOutOfRange(t *testing.T) {
	const narrow = " is not an integer from -2147483648 to 2147483647"
	const wide = " is not an integer from -9007199254740991 to 9007199254740991"
	for _, c := range []struct {
		n    float64
		want string // "" là đọc được
	}{
		{math.MaxInt32, ""},
		{math.MinInt32, ""},
		{3_000_000_000, "3000000000" + narrow},
		{-3_000_000_000, "-3000000000" + narrow},
		{math.MaxInt32 + 1, "2147483648" + narrow},
		{1e300, "1e+300" + narrow},
		{math.Inf(1), "Infinity" + narrow},
		{math.Inf(-1), "-Infinity" + narrow},
		{1.5, "1.5 is not an integer"},
		{math.NaN(), "NaN is not an integer"},
	} {
		var got int
		err := value.Number(c.n).Scan(&got)
		switch {
		case c.want == "" && (err != nil || float64(got) != c.n):
			t.Errorf("%v into *int: %d, %v", c.n, got, err)
		case c.want != "" && (err == nil || err.Error() != c.want):
			t.Errorf("%v into *int: %v, want %q", c.n, err, c.want)
		}
	}
	for _, c := range []struct {
		n    float64
		want string
	}{
		{1<<53 - 1, ""},
		{-(1<<53 - 1), ""},
		{3_000_000_000, ""},
		{1 << 53, "9007199254740992" + wide},
		{-(1 << 53), "-9007199254740992" + wide},
		{1e300, "1e+300" + wide},
		{math.Inf(1), "Infinity" + wide},
		{1.5, "1.5 is not an integer"},
	} {
		var got int64
		err := value.Number(c.n).Scan(&got)
		switch {
		case c.want == "" && (err != nil || float64(got) != c.n):
			t.Errorf("%v into *int64: %d, %v", c.n, got, err)
		case c.want != "" && (err == nil || err.Error() != c.want):
			t.Errorf("%v into *int64: %v, want %q", c.n, err, c.want)
		}
	}
}
