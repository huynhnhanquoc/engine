package value_test

import (
	"strings"
	"testing"

	"github.com/kitwork/engine2/core/value"
)

// The signature of a struct's method says what a program reads (04/11/2026): a method of no parameter,
// func (*T) X() value.Value, is a property, read by counter.count without (); a method of the
// variadic signature, func (*T) X(...value.Value) value.Value, is a method, called by counter.add(n) and,
// read without a call, a function bound to the struct. Until then a read called every exposed method
// with no argument, so `const add = counter.add` ran add().

// Counter has one of each, a property that gives a function, and a method of Go's own.
type Counter struct{ count, reads int }

// Count is a property: every read calls it.
func (c *Counter) Count() value.Value {
	c.reads++
	return value.Number(float64(c.count))
}

// Add is a method: counter.add(n, …) adds each n.
func (c *Counter) Add(arguments ...value.Value) value.Value {
	for _, argument := range arguments {
		c.count += int(argument.Number())
	}
	return value.Number(float64(c.count))
}

// Handler is a property whose value is a function: counter.handler(…) calls what it gives, as
// JavaScript calls what a getter gives.
func (c *Counter) Handler() value.Value {
	return value.Function(func(arguments ...value.Value) value.Value {
		return value.Number(float64(len(arguments)))
	})
}

// Reset is Go's: another signature, so no program sees it.
func (c *Counter) Reset(to int) { c.count = to }

// Adder is another type with a method of the same name: its add is not Counter's.
type Adder struct{}

func (*Adder) Add(...value.Value) value.Value { return value.Undefined() }

func TestAStructMethodReadWithoutACallIsNotCalled(t *testing.T) {
	counter := &Counter{}
	s := value.Struct(counter)
	add := s.Get("add")
	if counter.count != 0 {
		t.Fatalf("reading counter.add called it: count is %d", counter.count)
	}
	if !add.IsFunction() || add.Typeof().Text() != "function" {
		t.Fatalf("counter.add read without a call is a function, got %s", add.Kind())
	}
	if got := add.Call([]value.Value{value.Number(2), value.Number(3)}); got.Number() != 5 || counter.count != 5 {
		t.Errorf("the function read is the method bound to the counter: got %s, count %d", got.ToString().Text(), counter.count)
	}
	if got := s.Invoke("add", []value.Value{value.Number(1)}); got.Number() != 6 {
		t.Errorf("counter.add(1) = %s", got.ToString().Text())
	}
	if !s.Get("add").Equal(s.Get("add")) {
		t.Error("two reads of one method of one struct are ===")
	}
	if !s.Get("add").Equal(value.Struct(&Counter{}).Get("add")) {
		t.Error("a method of one type read off two structs is ===, as a class's method read off two instances")
	}
	if s.Get("add").Equal(value.Struct(&Adder{}).Get("add")) {
		t.Error("the add of two types are two functions")
	}
	if s.Get("add").Equal(value.String("x").Get("concat")) || s.Get("add").Equal(s.Get("handler")) {
		t.Error("a struct's method is === to nothing but a read of itself")
	}
	if name := add.Get("name").Text(); name != "add" {
		t.Errorf("counter.add.name = %q", name)
	}
}

func TestAStructPropertyIsReadWithoutACall(t *testing.T) {
	counter := &Counter{count: 4}
	s := value.Struct(counter)
	if got := s.Get("count"); got.Number() != 4 || counter.reads != 1 {
		t.Errorf("counter.count = %s after %d reads", got.ToString().Text(), counter.reads)
	}
	if got := s.Invoke("count", nil); got.Code() != value.TypeCode || got.Message() != "count is not a function" {
		t.Errorf("counter.count() is not a function: %s", got.Failure())
	}
	if got := s.Invoke("handler", []value.Value{value.Number(1), value.Number(2)}); got.Number() != 2 {
		t.Errorf("counter.handler(1, 2) calls the function the property gives: %s", got.Failure())
	}
	if !s.Get("nope").IsUndefined() || !s.Get("reset").IsUndefined() {
		t.Error("a name the struct has no property or method for reads as undefined, Go's own method too")
	}
	if got := s.Invoke("nope", nil); got.Failure() != "TYPE: counter has no method nope" {
		t.Errorf("counter.nope(): %s", got.Failure())
	}
}

func TestExposeTellsPropertiesFromMethods(t *testing.T) {
	table := value.Expose[Counter]()
	if got := strings.Join(table.Names(), " "); got != "add count handler" {
		t.Errorf("Names = %q, want every name a program reaches", got)
	}
	if got := strings.Join(table.Properties(), " "); got != "count handler" {
		t.Errorf("Properties = %q, want the methods of no parameter", got)
	}
}
