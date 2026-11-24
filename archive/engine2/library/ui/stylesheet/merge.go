package stylesheet

// Merge adds to r every rule, class, weight and custom property other recorded, as if what other recorded had
// been recorded in r (10/11/2026): a page keeps the rules of a part it draws once, and gives them to the page each
// time it shows that part again. Every recording of a Rules adds (a rule by its key, a class, a weight), so a merge is
// a union, and merging the parts of a page in the order they were drawn makes the rules drawing the page whole made;
// a backdrop recorded in other is r's, as the last <page> drawn decides.
//
// The union waits: r keeps other, its trace mixed into r's at once, and adds other's maps only when something reads
// them (materialize). A page whose stylesheet is kept by its trace never needs them. other is read, never changed: it
// may be merged into many pages at once, and must not be recorded in after.
func (r *Rules) Merge(other *Rules) {
	if other == nil || r == other || other.trace == ([2]uint64{}) {
		return // other recorded nothing: merging it adds nothing
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	// the merge mixes other's trace into r's by two rounds of splitmix64, one a lane: no allocation, as a page merges a
	// part's rules at every element it writes again
	r.trace[0] = splitmix(r.trace[0] ^ splitmix(other.trace[0]^mergeLanes[0]))
	r.trace[1] = splitmix(r.trace[1] ^ splitmix(other.trace[1]^mergeLanes[1]))
	r.materials = r.materials || other.anyMaterials()
	r.gradientText = r.gradientText || other.anyGradientText()
	if backdropped, focus, ground := other.lastBackdrop(); backdropped {
		r.backdropped, r.focus = true, focus
		if ground != "" {
			r.ground = ground
		}
	}
	r.pending = append(r.pending, other)
}

// anyMaterials, anyGradientText and lastBackdrop read what r holds as one value, its merges waiting included.
func (r *Rules) anyMaterials() bool {
	if r.materials {
		return true
	}
	for _, p := range r.pending {
		if p.anyMaterials() {
			return true
		}
	}
	return false
}

func (r *Rules) anyGradientText() bool {
	if r.gradientText {
		return true
	}
	for _, p := range r.pending {
		if p.anyGradientText() {
			return true
		}
	}
	return false
}

// lastBackdrop is the backdrop r ends with: its own, set as its merges were made, so r's own.
func (r *Rules) lastBackdrop() (bool, [2]string, string) {
	return r.backdropped, r.focus, r.ground
}

// anyScrolled says a class of r, or of a merge waiting, holds while the page is scrolled.
func (r *Rules) anyScrolled() bool {
	if len(r.scrolled) > 0 {
		return true
	}
	for _, p := range r.pending {
		if p.anyScrolled() {
			return true
		}
	}
	return false
}

// materialize adds the maps of every merge waiting, in the order merged; r.mu is held. What is merged is only read.
func (r *Rules) materialize() {
	if len(r.pending) == 0 {
		return
	}
	pending := r.pending
	r.pending = nil
	for _, other := range pending {
		r.add(other)
	}
}

// add adds other's maps and those of the merges it waits on to r's; r.mu is held, other is read.
func (r *Rules) add(other *Rules) {
	if other.rules != nil && r.rules == nil {
		r.rules = map[string]rule{}
		r.classes = map[string]bool{}
		r.weights = map[int]bool{}
		r.moves = map[string][]string{}
		r.scrolled = map[string]bool{}
		r.gradients = map[string]bool{}
	}
	for key, x := range other.rules {
		r.rules[key] = x
	}
	for class := range other.classes {
		r.classes[class] = true
	}
	for weight := range other.weights {
		r.weights[weight] = true
	}
	for class, moves := range other.moves {
		r.moves[class] = moves
	}
	for class := range other.scrolled {
		r.scrolled[class] = true
	}
	for class := range other.gradients {
		r.gradients[class] = true
	}
	if len(other.pairs) > 0 {
		if r.pairs == nil {
			r.pairs = map[string]pairValue{}
		}
		for name, v := range other.pairs {
			r.pairs[name] = v
		}
	}
	if len(other.scopes) > 0 {
		if r.scopes == nil {
			r.scopes = map[string]scopeRules{}
		}
		for class, scope := range other.scopes {
			r.scopes[class] = scope
		}
	}
	for _, p := range other.pending {
		r.add(p)
	}
}

// mergeLanes set a merge apart from a recording in each lane of a trace.
var mergeLanes = [2]uint64{0x6a09e667f3bcc909, 0xbb67ae8584caa73b}

// splitmix is the finalizer of splitmix64: every bit of x moves every bit of the result.
func splitmix(x uint64) uint64 {
	x += 0x9e3779b97f4a7c15
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return x ^ (x >> 31)
}

// pendingScope says a merge waiting records the scope of class.
func (r *Rules) pendingScope(class string) bool {
	for _, p := range r.pending {
		if _, ok := p.scopes[class]; ok || p.pendingScope(class) {
			return true
		}
	}
	return false
}
