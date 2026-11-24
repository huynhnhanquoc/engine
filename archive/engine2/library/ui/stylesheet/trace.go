package stylesheet

import (
	"encoding/binary"
	"hash/maphash"
	"strconv"
)

// A Rules keeps a trace of what was recorded in it (10/11/2026): each recording mixes what it records into a pair of
// numbers, in the order recorded. Two Rules of one palette with the same trace hold the same rules, so a page drawn
// again as it was drawn before writes the stylesheet it wrote then (ui, Document) without ordering its rules again.
// Two seeds of 64 bits each: two different traces meet once in 2^128.

var traceSeeds = [2]maphash.Seed{maphash.MakeSeed(), maphash.MakeSeed()}

// tracer mixes one recording into a trace.
type tracer struct {
	hashes [2]maphash.Hash
}

func (r *Rules) tracer(kind string) *tracer {
	t := &tracer{}
	for i := range t.hashes {
		t.hashes[i].SetSeed(traceSeeds[i])
		var b [8]byte
		binary.LittleEndian.PutUint64(b[:], r.trace[i])
		t.hashes[i].Write(b[:])
		t.hashes[i].WriteString(kind)
	}
	return t
}

func (t *tracer) text(parts ...string) *tracer {
	for i := range t.hashes {
		for _, part := range parts {
			t.hashes[i].WriteByte(0)
			t.hashes[i].WriteString(part)
		}
	}
	return t
}

func (t *tracer) number(n int) *tracer { return t.text(strconv.Itoa(n)) }

func (t *tracer) pairs(pairs []pairValue) *tracer {
	for _, v := range pairs {
		t.text(v.name, v.light, v.dark)
	}
	return t
}

// into ends the recording: r's trace is the mix.
func (t *tracer) into(r *Rules) {
	for i := range t.hashes {
		r.trace[i] = t.hashes[i].Sum64()
	}
}

// Trace is what r recorded, as the pair of numbers its recordings mixed into, in the order made.
func (r *Rules) Trace() [2]uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.trace
}

// traceRecord mixes a class recorded into r's trace: every part of it that reaches the stylesheet; r.mu is held.
func (r *Rules) traceRecord(a attribute, rank int, c condition, value, class string, read reading) {
	t := r.tracer("record").text(a.name, class, value, c.label).number(rank).text(read.declarations, read.handover)
	for _, scoped := range read.scoped {
		t.text(scoped.selector, scoped.declarations, scoped.wrap, scoped.width)
	}
	t.text(read.transition...).number(read.weight).pairs(read.pairs)
	if read.gradient {
		t.text("gradient")
	}
	if a.show {
		t.text("show")
	}
	t.into(r)
}
