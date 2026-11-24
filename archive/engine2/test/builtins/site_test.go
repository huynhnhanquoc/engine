package builtins_test

import (
	"strings"
	"testing"
	"time"

	"github.com/kitwork/engine2/core/builtins"
	"github.com/kitwork/engine2/core/value"
)

// A site's console writes to the host's writer, led by the site (04/11/2026): each call one whole line,
// led by the lead and the method's name, frozen as every builtin is.
func TestASitesConsoleWritesItsLinesLedByTheSite(t *testing.T) {
	var out strings.Builder
	console := builtins.ConsoleTo(&out, "[shop.test] ")
	console.Get("log").Call([]value.Value{value.String("xin"), value.Number(2)})
	console.Get("error").Call([]value.Value{value.Boolean(true)})
	if got, want := out.String(), "[shop.test] [console.log] xin 2\n[shop.test] [console.error] true\n"; got != want {
		t.Errorf("console wrote %q, want %q", got, want)
	}
	if !console.IsFrozen() || !console.Get("log").IsFrozen() {
		t.Error("a site's console is frozen, its functions too: one request cannot leave a key for the next")
	}
	if got := console.Set("log", value.Number(1)); got.Code() != value.TypeCode {
		t.Errorf("console.log = 1 is a TYPE failure, got %s", got.Failure())
	}
}

// A site's Date reads the parts of Date(y, m, …) and a text without an offset in its app's zone, not the
// server's; an instant (Date(ms), a text with an offset, now) is the same in every zone. Date() of the
// shared set still reads the server's zone at each call.
func TestASitesDateReadsItsPartsInItsZone(t *testing.T) {
	newYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("no zone database:", err)
	}
	for _, c := range []struct {
		location            *time.Location
		parts, parsed, sure string
	}{
		{time.UTC, "2026-01-01T00:00:00.000Z", "1767225600000", "2026-01-01T00:00:00.000Z"},
		{newYork, "2026-01-01T05:00:00.000Z", "1767243600000", "2026-01-01T00:00:00.000Z"},
		{time.FixedZone("UTC+7", 7*60*60), "2025-12-31T17:00:00.000Z", "1767200400000", "2026-01-01T00:00:00.000Z"},
	} {
		date := builtins.DateIn(c.location)
		parts := date.Call([]value.Value{value.Number(2026), value.Number(0), value.Number(1)}).ToISOString().Text()
		parsed := date.Get("parse").Call([]value.Value{value.String("2026-01-01T00:00:00")}).ToString().Text()
		sure := date.Call([]value.Value{value.String("2026-01-01T00:00:00Z")}).ToISOString().Text()
		if parts != c.parts || parsed != c.parsed || sure != c.sure {
			t.Errorf("%s: Date(2026, 0, 1) %s, Date.parse(local) %s, Date(UTC text) %s; want %s, %s, %s",
				c.location, parts, parsed, sure, c.parts, c.parsed, c.sure)
		}
		if !date.IsFrozen() {
			t.Errorf("%s: a site's Date is frozen", c.location)
		}
	}

	local := time.Local
	t.Cleanup(func() { time.Local = local })
	shared := map[string]value.Value{}
	builtins.Install(shared)
	for _, zone := range []*time.Location{time.UTC, newYork} {
		time.Local = zone
		want := time.Date(2026, 0+1, 1, 0, 0, 0, 0, zone).UTC().Format("2006-01-02T15:04:05.000Z")
		got := shared["Date"].Call([]value.Value{value.Number(2026), value.Number(0), value.Number(1)}).ToISOString().Text()
		if got != want {
			t.Errorf("the shared Date with time.Local = %s: %s, want %s, read at the call", zone, got, want)
		}
	}
}

// Every line a site's console writes is led by the site (05/11/2026): a text of several lines leads each,
// a control character is written as its escape, so a site cannot write a line led by another site's name
// into the log the sites share, and one call writes at most 64 KiB of text, then the count of the rest.
func TestASitesConsoleLeadsEveryLineItWrites(t *testing.T) {
	var out strings.Builder
	console := builtins.ConsoleTo(&out, "[alpha.test] ")
	newline, carriage, escape := string(rune(10)), string(rune(13)), string(rune(27))
	console.Get("log").Call([]value.Value{value.String("hi" + newline + "[beta.test] [console.log] refunded" + carriage + "[beta.test]" + escape + "[2J")})
	written := out.String()
	for _, line := range strings.Split(strings.TrimSuffix(written, newline), newline) {
		if !strings.HasPrefix(line, "[alpha.test] [console.log] ") {
			t.Errorf("a line alpha wrote is not led by alpha: %q", line)
		}
	}
	if strings.ContainsAny(written, carriage+escape) || !strings.Contains(written, `refunded\x0d[beta.test]\x1b[2J`) {
		t.Errorf("control characters are written as their escapes: %q", written)
	}
	out.Reset()
	console.Get("log").Call([]value.Value{value.String(strings.Repeat("é", 40_000))})
	if line := out.String(); len(line) > 64<<10+100 || !strings.HasSuffix(line, " … (14464 bytes more)"+newline) {
		t.Errorf("a call of 80,000 bytes writes %d bytes ending %q", len(line), line[max(len(line)-40, 0):])
	}
}
