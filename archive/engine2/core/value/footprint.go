package value

import "unsafe"

// The bytes a value holds, as the runtime's meter counts what a run makes (runtime, memory.go, 12/11/2026): Go says
// nothing of when a value is let go, so a run is bounded by what it makes, which is more than it holds at any time.
// The figures are what Go allocates for each part, near enough: a text its bytes, an array a Value an item, an object
// a key's string header, its Value and its place in the index, each made thing its header.
const (
	// ItemBytes is an array's item, one Value.
	ItemBytes = uint64(unsafe.Sizeof(Value{}))
	// KeyBytes is an object's key: its string header, its Value, its share of the index.
	KeyBytes = uint64(unsafe.Sizeof("")) + ItemBytes + 16
	// MadeBytes is an object, an array or a function made: its header and the pointer to it.
	MadeBytes = 64
)

// Count is how many items an array holds, how many keys an object holds; 0 for every other kind. It reads the
// lengths, no copy.
func (v Value) Count() int {
	// one type check, small enough to inline: an object's data is its properties, a function's is its callable
	switch data := v.data.(type) {
	case *list:
		return len(data.items)
	case *properties:
		return len(data.keys) - int(data.dead)
	}
	return 0
}

// Holds reports whether v takes memory of its own: a text, bytes, an array, an object, a function, a struct. A number,
// a boolean, null and the like are held in the Value itself.
func (v Value) Holds() bool {
	switch v.kind {
	case text, bytes, array, object, function, host:
		return true
	}
	return false
}

// Shallow is what v takes itself, not counting what its items and keys hold: a text's or bytes' length, an array's
// header and its items, an object's header and its keys, a function's or a struct's header; 0 for a number, a
// boolean, null and the like, which a Value holds in place.
func (v Value) Shallow() uint64 {
	switch v.kind {
	case text:
		return uint64(len(v.data.(string)))
	case bytes:
		return uint64(len(v.Bytes()))
	case array:
		return MadeBytes + uint64(len(v.Items()))*ItemBytes
	case object:
		if o := v.properties(); o != nil {
			return MadeBytes + uint64(len(o.keys))*KeyBytes
		}
		return MadeBytes
	case function, host:
		return MadeBytes
	}
	return 0
}

// footprintDepth bounds how deep Footprint walks: a value a host made fresh is a tree (a parsed JSON, the rows of a
// query), and a cycle a host made all the same is cut here, and by the limit long before.
const footprintDepth = 64

// Footprint is what v holds with everything inside it, its items, keys and their texts, as a value a host made fresh
// holds it. It stops once past limit, and then returns a figure past limit: the walk costs no more than the bytes it
// counts, whatever v is.
func (v Value) Footprint(limit uint64) uint64 {
	total := uint64(0)
	v.footprint(&total, limit, 0)
	return total
}

func (v Value) footprint(total *uint64, limit uint64, depth int) {
	*total += v.Shallow()
	if *total > limit || depth >= footprintDepth {
		return
	}
	switch v.kind {
	case array:
		for _, item := range v.Items() {
			item.footprint(total, limit, depth+1)
			if *total > limit {
				return
			}
		}
	case object:
		o := v.properties()
		if o == nil {
			return
		}
		for index, key := range o.keys {
			*total += uint64(len(key))
			o.items[index].footprint(total, limit, depth+1)
			if *total > limit {
				return
			}
		}
	}
}

// Holding counts what values still hold, for a run past what it may make (runtime, memory.go): most of what a run
// makes is let go as it goes (s += piece makes every shorter text before the last), so a run that would pass its
// ceiling is counted again by what it can still reach, and goes on when that is little. A large text, array, object or
// function is counted once however many values hold it; a frozen array or object not at all, since it is shared by
// every run (a file's values once its site booted). Counting stops once past Limit: what it costs is no more than what
// it counts.
type Holding struct {
	Total, Limit uint64
	seen         map[unsafe.Pointer]struct{}
}

// counted once from these sizes up: a smaller value is counted at each hold, which costs no map and a bounded excess
const (
	heldTextOnce  = 256
	heldItemsOnce = 16
)

// Past reports whether what was counted is past Limit.
func (h *Holding) Past() bool { return h.Total > h.Limit }

// Add counts v and what it holds.
func (h *Holding) Add(v Value) { h.add(v, 0) }

// AddLambda counts what a function holds: the values it closes over, and those of every function around it.
func (h *Holding) AddLambda(l *Lambda) { h.lambda(l, 0) }

// first reports whether p is counted for the first time.
func (h *Holding) first(p unsafe.Pointer) bool {
	if h.seen == nil {
		h.seen = map[unsafe.Pointer]struct{}{}
	}
	if _, ok := h.seen[p]; ok {
		return false
	}
	h.seen[p] = struct{}{}
	return true
}

func (h *Holding) add(v Value, depth int) {
	if h.Total > h.Limit || depth > footprintDepth {
		return
	}
	switch v.kind {
	case text:
		s := v.data.(string)
		if len(s) >= heldTextOnce && !h.first(unsafe.Pointer(unsafe.StringData(s))) {
			return
		}
		h.Total += uint64(len(s))
	case bytes:
		b := v.Bytes()
		if len(b) >= heldTextOnce && !h.first(unsafe.Pointer(unsafe.SliceData(b))) {
			return
		}
		h.Total += uint64(len(b))
	case array:
		l, ok := v.data.(*list)
		if !ok || l.frozen || (len(l.items) >= heldItemsOnce && !h.first(unsafe.Pointer(l))) {
			return
		}
		h.Total += MadeBytes + uint64(len(l.items))*ItemBytes
		for _, item := range l.items {
			h.add(item, depth+1)
			if h.Total > h.Limit {
				return
			}
		}
	case object:
		o := v.properties()
		if o == nil || o.frozen || (len(o.keys) >= heldItemsOnce && !h.first(unsafe.Pointer(o))) {
			return
		}
		h.Total += MadeBytes + uint64(len(o.keys))*KeyBytes
		for index, key := range o.keys {
			h.Total += uint64(len(key))
			h.add(o.items[index], depth+1)
			if h.Total > h.Limit {
				return
			}
		}
	case function:
		c, ok := v.data.(*callable)
		if !ok || !h.first(unsafe.Pointer(c)) {
			return
		}
		if c.lambda != nil {
			h.Total += uint64(unsafe.Sizeof(Lambda{})) // a closure: its lambda, the callable inside it
			h.lambda(c.lambda, depth+1)
		} else {
			h.Total += MadeBytes
		}
	case host:
		h.Total += MadeBytes
	}
}

func (h *Holding) lambda(l *Lambda, depth int) {
	for ; l != nil && depth <= footprintDepth && h.Total <= h.Limit; depth++ {
		if !h.first(unsafe.Pointer(l)) {
			return
		}
		for _, item := range l.Slots {
			h.add(item, depth+1)
		}
		for _, item := range l.Scope {
			h.add(item, depth+1)
		}
		l = l.Parent
	}
}

// Shares reports whether v holds no memory of its own beside other: the same array, object or function, or a text or
// bytes lying within other's (a slice of it, or it whole). A result that shares made nothing.
func (v Value) Shares(other Value) bool {
	switch v.kind {
	case text:
		if other.kind != text {
			return false
		}
		return within(unsafe.StringData(v.data.(string)), len(v.data.(string)), unsafe.StringData(other.data.(string)), len(other.data.(string)))
	case bytes:
		if other.kind != bytes {
			return false
		}
		mine, theirs := v.Bytes(), other.Bytes()
		return within(unsafe.SliceData(mine), len(mine), unsafe.SliceData(theirs), len(theirs))
	case array, object, function, host:
		return v.kind == other.kind && v.data == other.data
	}
	return false
}

// within reports whether the n bytes at p lie within the m bytes at q. An empty text holds nothing.
func within(p *byte, n int, q *byte, m int) bool {
	if n == 0 {
		return true
	}
	if p == nil || q == nil {
		return false
	}
	start, end := uintptr(unsafe.Pointer(q)), uintptr(unsafe.Pointer(q))+uintptr(m)
	at := uintptr(unsafe.Pointer(p))
	return at >= start && at+uintptr(n) <= end
}

// Fresh marks fn, a Go function, as one whose result is all new, made by the call from nothing the program holds
// (JSON.parse): the runtime counts the whole of it, where for any other function of the engine it counts what the
// result takes itself (a function that hands back what it was given, Object.values, would count it again each time).
// fn is returned. A function of a module's struct needs no mark: what a module gives is counted whole.
func Fresh(fn Value) Value {
	if c, ok := fn.data.(*callable); ok && c.native != nil {
		c.fresh = true
	}
	return fn
}

// GivesFresh reports whether v is a function Fresh marked.
func (v Value) GivesFresh() bool {
	c, ok := v.data.(*callable)
	return ok && c.fresh
}
