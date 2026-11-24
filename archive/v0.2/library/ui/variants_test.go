package ui

import (
	"html"
	"math"
	"strings"
	"testing"

	"github.com/kitwork/engine2/core/value"
)

// An opening kept for the values it read (frozen.go) is written again only where each expression it read is again
// what it was: a string, a number bit for bit, a boolean, null, undefined; an object is never one. Writing it counts
// each of its reads as the request's, as reading them did, so the part around it is drawn at each page too.
func TestAVariantIsTheOpeningOfTheSameValues(t *testing.T) {
	site := &Site{}
	r := &renderer{site: site, eval: newEvaluator(&site.machines, nil, 0)}
	defer r.eval.release()
	tone, err := site.compile("item.ready ? 'success' : 'ink-30'")
	if err != nil {
		t.Fatal(err)
	}
	count, err := site.compile("item.count")
	if err != nil {
		t.Fatal(err)
	}
	item := func(ready bool, n float64) func() {
		o := value.Object()
		o.Set("ready", value.Boolean(ready))
		o.Set("count", value.Number(n))
		return r.eval.put("item", o)
	}
	ready := &keptStart{open: start{classes: []string{"success"}}}
	planned := &keptStart{open: start{classes: []string{"planned"}}}
	vs := &variants{}
	vs.keep(site, &variant{reads: []readValue{{expression: tone, result: value.String("success")}, {expression: count, result: value.Number(2)}}, kept: ready})
	vs.keep(site, &variant{reads: []readValue{{expression: tone, result: value.String("ink-30")}}, kept: planned})

	cases := []struct {
		ready   bool
		count   float64
		want    *keptStart
		touched int
	}{
		{true, 2, ready, 2},
		{false, 2, planned, 1},
		{true, 3, nil, 0}, // its second read differs: read again
		{false, 9, planned, 1},
	}
	for _, c := range cases {
		takeBack := item(c.ready, c.count)
		before := r.touched
		got := r.variantOf(vs)
		takeBack()
		if got != c.want {
			t.Errorf("ready %t, count %v: variant %v, want %v", c.ready, c.count, got, c.want)
		}
		if r.touched-before != c.touched {
			t.Errorf("ready %t, count %v: %d reads counted, want %d", c.ready, c.count, r.touched-before, c.touched)
		}
	}

	objects := &variants{}
	objects.keep(site, &variant{reads: []readValue{{expression: tone, result: value.Object()}}, kept: ready})
	if objects.list.Load() != nil || !objects.varies.Load() {
		t.Errorf("an opening that read an object is kept: %v", objects.list.Load())
	}
	for _, v := range []value.Value{value.Undefined(), value.Null(), value.String("a"), value.Number(-0.5), value.Boolean(false)} {
		if !sameValue(v, v) {
			t.Errorf("%v is not the same as itself", v)
		}
	}
	for _, pair := range [][2]value.Value{{value.Undefined(), value.Null()}, {value.String("1"), value.Number(1)},
		{value.Boolean(true), value.Number(1)}, {value.Number(0), value.Number(math.Copysign(0, -1))},
		{value.Object(), value.Object()}, {value.Array(nil), value.Array(nil)}} {
		if sameValue(pair[0], pair[1]) {
			t.Errorf("%s %v and %s %v are the same", pair[0].Kind(), pair[0], pair[1].Kind(), pair[1])
		}
	}
}

// A place keeps at most maxVariants openings: past it the place's values are the page's own, and it is read at each page.
func TestAPlaceKeepsAtMostSoManyVariants(t *testing.T) {
	site := &Site{}
	tone, err := site.compile("item")
	if err != nil {
		t.Fatal(err)
	}
	vs := &variants{}
	for i := range maxVariants + 3 {
		vs.keep(site, &variant{reads: []readValue{{expression: tone, result: value.Number(float64(i))}}, kept: &keptStart{}})
	}
	if list := vs.list.Load(); list == nil || len(*list) != maxVariants || !vs.varies.Load() {
		t.Errorf("a place kept %v variants, varies %t", list, vs.varies.Load())
	}
}

// The page's buffer escapes as html.EscapeString does, byte for byte.
func TestTheOutputEscapesAsHTMLDoes(t *testing.T) {
	for _, s := range []string{"", "plain", "<b>&amp;</b>", `"quoted" 'single'`, "Tiếng Việt <có dấu> & ký tự", "&&&<<>>''\"\"",
		strings.Repeat("a<", 40), "\x00\x7f<\xff>", "emoji 😀 & <tag>"} {
		var o output
		o.WriteString("[")
		o.escape(s)
		o.WriteString("]")
		if got, want := string(o.buf), "["+html.EscapeString(s)+"]"; got != want {
			t.Errorf("escape(%q) = %q, want %q", s, got, want)
		}
	}
}
