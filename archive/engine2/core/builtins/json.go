package builtins

import (
	"strconv"
	"strings"

	"github.com/kitwork/engine2/core/value"
)

// JSON is JSON.parse and JSON.stringify over value.ParseJSON and value.Stringify. parse calls a reviver
// as JavaScript does (revive). stringify takes the indent as JavaScript does (a number of spaces up to
// 10, or the first 10 units of a string, anything else none) and a replacer as JavaScript does: a
// function is called with each key and value, an array is the keys an object keeps, anything else is
// no replacer (JSON.stringify(2024, 0, 2) is "2024").
func JSON() value.Value {
	json := value.Object()
	// what parse gives is all new, made from the text: the runtime counts the whole of it (value.Fresh)
	json.Set("parse", value.Fresh(nativeWithin("parse", 2, func(budget value.Budget, arguments ...value.Value) value.Value {
		arguments, failure := value.ConvertArguments("tk", arguments)
		if failure.IsInvalid() {
			return failure
		}
		// the tree, asked of the run as it grows: a text of "[]," made some 80 bytes for each 3 of its own
		parsed := value.ParseJSONWithin(argument(arguments, 0).ToString().Text(), budget)
		if reviver := argument(arguments, 1); reviver.IsFunction() && !parsed.IsInvalid() {
			// the tree the text gave, held here while the reviver runs, is counted by its run (value.Kept)
			defer reviver.Keep(value.Kept{Value: parsed}).Release()
			return revive(parsed, "", reviver)
		}
		return parsed
	})))
	json.Set("stringify", nativeWithin("stringify", 3, func(budget value.Budget, arguments ...value.Value) value.Value {
		item, replacer := argument(arguments, 0), argument(arguments, 1)
		switch {
		case replacer.IsFunction():
			item = replaceWith(replacer, nil, "", item, nil)
		case replacer.IsArray():
			item = replaceWith(value.Undefined(), allowedKeys(replacer), "", item, nil)
		}
		if item.IsInvalid() {
			return item
		}
		if replacer.IsFunction() || replacer.IsArray() {
			// the walk of the replacer called each toJSON (replaceWith)
			return value.StringifyWithin(item, indent(argument(arguments, 2)), budget)
		}
		// the text, asked of the run as it grows: an array holding one text a thousand times is it a thousand times;
		// each object's toJSON called as it is written (18/11/2026)
		return value.StringifyCalling(item, indent(argument(arguments, 2)), budget)
	}))
	return json
}

// revive is JSON.parse's reviver walking what the text gave, as JavaScript's InternalizeJSONProperty:
// depth first, each array item (up to the length the array had when the walk reached it) and each key
// of an object (the keys it had then) replaced by reviver(key, value), a key whose reviver gave
// undefined deleted, and last the value itself under its own key ("" for the whole). An array keeps
// undefined where JavaScript leaves a hole. A failure of the reviver stops the walk and comes back out.
func revive(item value.Value, key string, reviver value.Value) value.Value {
	switch {
	case item.IsArray():
		length := len(item.Items())
		for i := 0; i < length; i++ {
			revived := revive(item.Index(i), strconv.Itoa(i), reviver)
			if revived.IsInvalid() {
				return revived
			}
			item.SetIndex(i, revived)
		}
	case item.IsObject():
		for _, k := range append([]string(nil), item.Keys()...) {
			revived := revive(item.Get(k), k, reviver)
			switch {
			case revived.IsInvalid():
				return revived
			case revived.IsUndefined():
				item.Delete(k)
			default:
				item.Set(k, revived)
			}
		}
	}
	return reviver.Call([]value.Value{value.String(key), item})
}

// indent is stringify's third argument as JavaScript reads it: a number is that many spaces (its
// integer part, at most 10, none below 1), a string its first 10 units, anything else no indent.
func indent(v value.Value) string {
	switch {
	case v.IsNumber():
		n := int(min(10, max(0, v.Integer())))
		return strings.Repeat(" ", n)
	case v.IsString():
		return v.Slice(value.Number(0), value.Number(10)).Text()
	}
	return ""
}

// replaceWith is what stringify writes of item under key once a replacer has seen it: a function
// replacer's result for (key, item), then the same for every item of an array and every key of an
// object, whose keys are allowed's when an array replacer gave a list. It builds a new value and leaves
// item as it was; undefined stays where the replacer put it, for Stringify to leave out of an object
// and write as null in an array. A value met again on its own path is a TYPE failure, as JavaScript's
// cycle check is, before the walk would never end.
func replaceWith(replacer value.Value, allowed []string, key string, item value.Value, active []value.Value) value.Value {
	// an object's toJSON first, then the replacer, as JavaScript's SerializeJSONProperty (18/11/2026)
	if item.IsObject() {
		if method := item.Get("toJSON"); method.IsFunction() {
			if item = method.Call([]value.Value{value.String(key)}); item.IsInvalid() {
				return item
			}
		}
	}
	if replacer.IsFunction() {
		if item = replacer.Call([]value.Value{value.String(key), item}); item.IsInvalid() {
			return item
		}
	}
	// what is written is no library (value.LibraryAsData): a replacer may give Math back, or take it away
	if library := item.Library(); library != "" {
		return value.LibraryAsData(library, "JSON.stringify("+library+")")
	}
	if !item.IsArray() && !item.IsObject() {
		return item
	}
	for _, seen := range active {
		if seen.Equal(item) {
			return value.Fail(value.TypeCode, "converting circular structure to JSON")
		}
	}
	active = append(active, item)
	if item.IsArray() {
		items := item.Items()
		out := make([]value.Value, len(items))
		// what the walk made so far, held here while the replacer runs, is counted by its run (value.Kept)
		defer replacer.Keep(value.Kept{Items: &out}).Release()
		for i, each := range items {
			if out[i] = replaceWith(replacer, allowed, strconv.Itoa(i), each, active); out[i].IsInvalid() {
				return out[i]
			}
		}
		return value.Array(out)
	}
	// a copy, because the replacer runs inside the walk and may write to the object: a written index key
	// moves the keys after it inside the object's own list, and the walk would repeat one and lose one
	keys := append([]string(nil), item.Keys()...)
	if allowed != nil {
		keys = allowed
	}
	out := value.Object()
	defer replacer.Keep(value.Kept{Value: out}).Release()
	for _, k := range keys {
		if allowed != nil && !item.Has(k) {
			continue
		}
		replaced := replaceWith(replacer, allowed, k, item.Get(k), active)
		if replaced.IsInvalid() {
			return replaced
		}
		out.Set(k, replaced)
	}
	return out
}

// allowedKeys is an array replacer read as JavaScript reads it: its strings and numbers as keys, in
// order, each once; anything else in it is passed over.
func allowedKeys(replacer value.Value) []string {
	keys := []string{}
	seen := map[string]bool{}
	for _, item := range replacer.Items() {
		if !item.IsString() && !item.IsNumber() {
			continue
		}
		k := item.ToString().Text()
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	return keys
}
