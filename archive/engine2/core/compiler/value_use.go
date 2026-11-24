package compiler

import (
	"reflect"
	"strings"
)

// ReadsAsValue says source, an expression or an event's handler of a .ui file, uses name other than as what a call
// calls: open() is a call; open, !open, { open }, open = false are not. An arrow with a parameter of that name has it
// as its own, and a property's name ({ open: 1 }, $menu.open for the name open) is no use of it. A name with a dot,
// $menu.open, is that property of that name: $menu.open() is a call, $menu.open a value. A page reading one of a
// component's functions as a value never meant the function (16/11/2026: {{ open }} where the state is opened, the
// spelling of before 26/10/2026), so library/ui says so where it is written. Source the compiler cannot read is false: its own
// error is said where the expression is compiled.
func ReadsAsValue(source, name string) (uses bool) {
	defer func() {
		if recover() != nil {
			uses = false
		}
	}()
	text := "const " + wrapper + " = (" + source + ");"
	program, err := parseProgramSource(text, "<value-use>")
	if err != nil {
		return false
	}
	statement, ok := oneValue(program)
	if !ok {
		return false
	}
	object, property, member := strings.Cut(name, ".")
	if !member {
		object, property = "", name
	}
	return valueUse(reflect.ValueOf(statement.Value), object, property)
}

// valueUse walks a node of the tree for a use of property other than as a callee: of the name property when object is
// "", else of object.property.
func valueUse(v reflect.Value, object, property string) bool {
	switch v.Kind() {
	case reflect.Interface, reflect.Pointer:
		if v.IsNil() {
			return false
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			if valueUse(v.Index(i), object, property) {
				return true
			}
		}
		return false
	}
	if v.CanInterface() {
		switch node := v.Interface().(type) {
		case *Identifier:
			return object == "" && node.Value == property
		case *CallExpression:
			if callee, ok := node.Function.(*Identifier); ok && object == "" && callee.Value == property {
				return valueUse(reflect.ValueOf(node.Arguments), object, property)
			}
		case *MemberExpression:
			if owner, ok := node.Object.(*Identifier); ok && object != "" && owner.Value == object && node.Property != nil && node.Property.Value == property {
				return true
			}
			return valueUse(reflect.ValueOf(node.Object), object, property)
		case *MethodCallExpression:
			return valueUse(reflect.ValueOf(node.Object), object, property) || valueUse(reflect.ValueOf(node.Arguments), object, property)
		case *FunctionLiteral:
			for _, parameter := range node.Parameters {
				if parameter != nil && parameter.Value == property && object == "" || parameter != nil && parameter.Value == object {
					return false
				}
			}
			return valueUse(reflect.ValueOf(node.Body), object, property)
		case ObjectEntry:
			if _, key := node.Key.(*Identifier); !key && valueUse(reflect.ValueOf(node.Key), object, property) {
				return true
			}
			return valueUse(reflect.ValueOf(node.Value), object, property)
		case Token:
			return false
		}
	}
	switch v.Kind() {
	case reflect.Interface, reflect.Pointer:
		return valueUse(v.Elem(), object, property)
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).IsExported() && valueUse(v.Field(i), object, property) {
				return true
			}
		}
	}
	return false
}
