package value

// An object becomes a primitive the way JavaScript's OrdinaryToPrimitive makes it: through its own
// valueOf and toString, in the order the hint asks (a number or no hint: valueOf first; a string:
// toString first). A method the object does not have as its own key is the inherited one, so valueOf
// gives the object back (no primitive) and toString gives "[object Object]". A method key that holds
// no function is passed over, as JavaScript does; when neither gives a primitive it is a TYPE failure.
// The methods are called with no `this`, since a Kitwork call carries none.

// ownPrimitive is that conversion for a plain object; ok is false when the object has neither an own
// valueOf nor an own toString, which is the common case and costs two key lookups.
func (v Value) ownPrimitive(stringFirst bool) (primitive Value, ok bool) {
	o, isObject := v.data.(*properties)
	if !isObject || v.kind != object {
		return Value{}, false
	}
	if o.find("valueOf") < 0 && o.find("toString") < 0 {
		return Value{}, false
	}
	order := [2]string{"valueOf", "toString"}
	if stringFirst {
		order = [2]string{"toString", "valueOf"}
	}
	for _, name := range order {
		own, isOwn := o.get(name)
		if !isOwn {
			if name == "toString" {
				return String("[object Object]"), true // the inherited toString
			}
			continue // the inherited valueOf gives the object itself
		}
		if own.kind != function {
			continue
		}
		result := own.Call(nil)
		if result.kind == invalid || result.isPrimitive() {
			return result, true
		}
	}
	return Fail(TypeCode, "cannot convert object to primitive value"), true
}

// isPrimitive is what ToPrimitive may give: anything but an object, an array, a function or a struct.
func (v Value) isPrimitive() bool {
	switch v.kind {
	case object, array, function, host:
		return false
	}
	return true
}

// numbers is both sides made numbers for an arithmetic operator, or the first failure on the way.
func numbers(a, b Value) (float64, float64, Value) {
	x := a.ToNumber()
	if x.kind == invalid {
		return 0, 0, x
	}
	y := b.ToNumber()
	if y.kind == invalid {
		return 0, 0, y
	}
	return x.Number(), y.Number(), Value{}
}
