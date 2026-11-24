package schema

import (
	"github.com/kitwork/engine2/core/value"
)

// ToJSONSchema is s.toJSONSchema(): what s.parse() takes, as a JSON Schema (draft 2020-12), for a tool that reads
// one: an API's documentation, the parameters of an agent's tool, a form made elsewhere. The fields in the order
// written, each with its kind and the rules a JSON Schema says (minLength, maximum, format "email", enum of a
// choice(…), null when .nullable(), its default, its .label() as title and .description()); required are the
// fields parse needs (no default, not nullable). Not in it: a column the engine fills (id(…), .created(),
// .updated()), which parse never takes, and a .secret() column, which never leaves the server
// (docs/KITWORK_SCHEMA.md mục 5). A key the data has that the struct has not is left out by parse, not refused, so
// the schema does not close the object; a transform is the server's, a JSON Schema has no word for it.
func (s *Struct) ToJSONSchema(arguments ...value.Value) value.Value {
	if len(arguments) != 0 {
		return value.Fail(value.InvalidArgumentCode, "toJSONSchema() không nhận đối số")
	}
	return s.jsonSchema(value.Object().Set("$schema", value.String("https://json-schema.org/draft/2020-12/schema")))
}

// jsonSchema is out with the object of s's fields: type, properties, required.
func (s *Struct) jsonSchema(out value.Value) value.Value {
	properties := value.Object()
	var required []value.Value
	for _, f := range s.fields {
		if f.field.generated() || f.field.table.secret {
			continue
		}
		properties = properties.Set(f.name, f.field.jsonSchema())
		if !f.field.hasDefault && !f.field.nullable {
			required = append(required, value.String(f.name))
		}
	}
	if _, typed := hasKey(out, "type"); !typed {
		out = out.Set("type", value.String("object"))
	}
	out = out.Set("properties", properties)
	if len(required) > 0 {
		out = out.Set("required", value.Array(required))
	}
	return out
}

// jsonType is the JSON Schema type of the field's values, "" for json(), which takes any value JSON writes.
func (f *Field) jsonType() string {
	switch f.kind {
	case kindString, kindUUID, kindDate, kindChoice:
		return "string"
	case kindInteger, kindTimestamp: // a timestamp() is its milliseconds
		return "integer"
	case kindFloat, kindDecimal:
		return "number"
	case kindBoolean:
		return "boolean"
	case kindArray:
		return "array"
	case kindStruct:
		return "object"
	case kindJSON:
		if f.shape != nil {
			return "object"
		}
	}
	return ""
}

// jsonSchema is one field's schema: its type, title, description, the words of its kind and rules, its default.
func (f *Field) jsonSchema() value.Value {
	out := value.Object()
	switch kind := f.jsonType(); {
	case kind != "" && f.nullable:
		out = out.Set("type", value.Array([]value.Value{value.String(kind), value.String("null")}))
	case kind != "":
		out = out.Set("type", value.String(kind))
	}
	if f.label != "" {
		out = out.Set("title", value.String(f.label))
	}
	if f.description != "" {
		out = out.Set("description", value.String(f.description))
	}
	switch f.kind {
	case kindString:
		out = f.limits("minLength", "maxLength", out)
		if f.email {
			out = out.Set("format", value.String("email"))
		}
	case kindUUID:
		out = out.Set("format", value.String("uuid"))
	case kindDate:
		out = out.Set("format", value.String("date"))
	case kindChoice:
		choices := make([]value.Value, 0, len(f.choices)+1)
		for _, word := range f.choices {
			choices = append(choices, value.String(word))
		}
		if f.nullable {
			choices = append(choices, value.Null()) // an enum holds every value it takes
		}
		out = out.Set("enum", value.Array(choices))
	case kindInteger, kindTimestamp, kindFloat, kindDecimal:
		out = f.limits("minimum", "maximum", out)
	case kindArray:
		out = out.Set("items", f.item.jsonSchema())
		out = f.limits("minItems", "maxItems", out)
	case kindStruct:
		out = f.shape.jsonSchema(out)
	case kindJSON:
		if f.shape != nil {
			out = f.shape.jsonSchema(out)
		}
	}
	if f.hasDefault {
		out = out.Set("default", f.def)
	}
	return out
}

// limits sets the field's min and max under the words of its kind.
func (f *Field) limits(least, most string, out value.Value) value.Value {
	if f.hasMin {
		out = out.Set(least, value.Number(f.min))
	}
	if f.hasMax {
		out = out.Set(most, value.Number(f.max))
	}
	return out
}

// hasKey reports whether the object o has key.
func hasKey(o value.Value, key string) (value.Value, bool) {
	for _, k := range o.Keys() {
		if k == key {
			return o.Get(key), true
		}
	}
	return value.Value{}, false
}
