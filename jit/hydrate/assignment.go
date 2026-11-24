package hydrate

import "math"

// WriteTarget exposes the target of a mutation to compiler checks; it does not evaluate it.
func WriteTarget(ir any) (any, bool) {
	n, ok := ir.([]any)
	if !ok || len(n) != 3 {
		return nil, false
	}
	op, _ := n[0].(string)
	if IsAssignmentOp(op) || op == "++" || op == "--" {
		return n[1], true
	}
	return nil, false
}

// TargetName identifies a bare variable target (members have their own ownership checks).
func TargetName(target any) string {
	n, ok := target.([]any)
	if ok && len(n) == 2 && n[0] == "$" {
		name, _ := n[1].(string)
		return name
	}
	return ""
}

// TargetRoot is the scope name at the start of a member path, if any.
func TargetRoot(target any) string {
	for {
		n, ok := target.([]any)
		if !ok || len(n) < 2 {
			return ""
		}
		if n[0] != "." && n[0] != "[.]" {
			return TargetName(n)
		}
		target = n[1]
	}
}

// A reference resolves a write target once. Its getter is deferred: plain `=` must not read the
// property's old value, and compound assignments must not evaluate an object or computed key twice.
type reference struct {
	get func() (any, error)
	set func(any) error
}

func (m *machine) reference(f *frame, target any) (*reference, error) {
	n, ok := target.([]any)
	if !ok || len(n) < 2 {
		return nil, typeError("Invalid assignment target")
	}
	if n[0] == "$" && len(n) == 2 {
		name, _ := n[1].(string)
		// Preserve the existing no-op behavior of bare blocked names.
		if blockedKey(name) {
			return nil, nil
		}
		if name == "$" {
			return nil, typeError("Invalid assignment target")
		}
		return &reference{
			get: func() (any, error) { return m.readName(f, name), nil },
			set: func(v any) error { f.assign(name, v); return nil },
		}, nil
	}
	if len(n) != 3 || (n[0] != "." && n[0] != "[.]") {
		return nil, typeError("Invalid assignment target")
	}
	holder, parent, err := m.receiver(f, n[1])
	if err != nil {
		return nil, err
	}
	key, _ := n[2].(string)
	if n[0] == "[.]" {
		v, err := m.eval(f, n[2])
		if err != nil {
			return nil, err
		}
		key, err = m.propertyKey(f, v)
		if err != nil {
			return nil, err
		}
	}
	if blockedKey(key) {
		return nil, typeError("Blocked assignment key")
	}
	return &reference{
		get: func() (any, error) {
			if nullish(holder) {
				return nil, typeError("Assignment target is null or undefined")
			}
			return m.get(f, holder, key)
		},
		set: func(v any) error { return m.assignMember(f, holder, key, v, parent) },
	}, nil
}

func (m *machine) assignMember(f *frame, holder any, key string, v any, parent func(any)) error {
	switch t := holder.(type) {
	case *Object:
		t.set(key, v)
		return nil
	case map[string]any:
		if write := m.writeInto(t, key, parent); write != nil {
			write(v)
			return nil
		}
	case []any:
		n := len(t)
		index, indexed := arrayIndex(key)
		if key == "length" {
			length, err := m.toNumber(f, v)
			if err != nil {
				return err
			}
			if math.IsNaN(length) || length < 0 || math.Trunc(length) != length || length > serverListLimit {
				return errTooMany
			}
			n = int(length)
		} else if indexed {
			if index >= serverListLimit {
				return errTooMany
			}
			if int(index) >= n {
				n = int(index) + 1
			}
		} else {
			return typeError("Array assignment needs an index or length")
		}
		if n > serverListLimit {
			return errTooMany
		}
		if parent == nil {
			return typeError("Array assignment needs an owned target")
		}
		copied := newList(n)
		copy(copied, t)
		for i := len(t); i < n; i++ {
			copied[i] = hole
		}
		if indexed {
			copied[index] = v
		}
		parent(copied)
		return nil
	}
	return typeError("Assignment target is not a writable object")
}

func (m *machine) assign(f *frame, op string, target, rhs any) (any, error) {
	ref, err := m.reference(f, target)
	if err != nil {
		return nil, err
	}
	if ref == nil {
		return Undefined, nil
	}
	var old any
	if op != "=" {
		old, err = ref.get()
		if err != nil {
			return nil, err
		}
		if op == "&&=" && !truthy(old) || op == "||=" && truthy(old) || op == "??=" && !nullish(old) {
			return old, nil
		}
	}
	v, err := m.eval(f, rhs)
	if err != nil {
		return nil, err
	}
	if op != "=" && op != "&&=" && op != "||=" && op != "??=" {
		v, err = m.eval(f, []any{op[:1], []any{"#", old}, []any{"#", v}})
		if err != nil {
			return nil, err
		}
	}
	if err := ref.set(v); err != nil {
		return nil, err
	}
	return v, nil
}

func (m *machine) update(f *frame, op string, target any, prefix bool) (any, error) {
	ref, err := m.reference(f, target)
	if err != nil {
		return nil, err
	}
	if ref == nil {
		return Undefined, nil
	}
	v, err := ref.get()
	if err != nil {
		return nil, err
	}
	old, err := m.toNumber(f, present(v))
	if err != nil {
		return nil, err
	}
	next := old + 1
	if op == "--" {
		next = old - 1
	}
	if err := ref.set(next); err != nil {
		return nil, err
	}
	if prefix {
		return next, nil
	}
	return old, nil
}
