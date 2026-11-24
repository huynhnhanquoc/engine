package value_test

import (
	"testing"

	"github.com/kitwork/engine2/core/value"
)

// A struct may answer the names its type does not have, by the Go method MissingMember(name) (04/11/2026):
// the manifest's app names an unknown app.x() and lets the chain go on, a .env answers env.KEY for any key.
// Its answer is what a read gives, and a call calls it as a property's value is called; a type without it
// reads undefined and fails a call, as before.

// Settings answers every name with its text, "lang" with a function, "broken" with a failure.
type Settings struct{ asked []string }

func (s *Settings) Size() value.Value { return value.Number(3) }

func (s *Settings) MissingMember(name string) value.Value {
	s.asked = append(s.asked, name)
	switch name {
	case "lang":
		return value.NamedFunction("lang", 0, func(arguments ...value.Value) value.Value {
			return value.String("vi:" + value.Array(arguments).Join().Text())
		})
	case "broken":
		return value.Fail(value.InvalidArgumentCode, "broken on purpose")
	}
	return value.String("the value of " + name)
}

// Bare has no MissingMember.
type Bare struct{}

func (b *Bare) Size() value.Value { return value.Number(1) }

func TestAStructAnswersTheNamesItsTypeDoesNotHave(t *testing.T) {
	settings := &Settings{}
	v := value.Struct(settings)
	if got := v.Get("PORT").Text(); got != "the value of PORT" {
		t.Errorf("settings.PORT = %q, want MissingMember's answer", got)
	}
	if got := v.Get("size").Number(); got != 3 {
		t.Errorf("settings.size = %v: a property the type has is read first", got)
	}
	if got := v.Invoke("lang", []value.Value{value.String("a"), value.String("b")}).Text(); got != "vi:a,b" {
		t.Errorf("settings.lang(\"a\", \"b\") = %q, want the function it answers, called", got)
	}
	if got := v.Invoke("PORT", nil); got.Code() != value.TypeCode || got.Message() != "PORT is not a function" {
		t.Errorf("settings.PORT() = %s, want TYPE \"PORT is not a function\"", got.Failure())
	}
	if got := v.Invoke("broken", nil); got.Message() != "broken on purpose" {
		t.Errorf("settings.broken() = %s, want its failure as it is", got.Failure())
	}
	if len(settings.asked) != 4 {
		t.Errorf("MissingMember was asked %v, want PORT, lang, PORT, broken: never for size", settings.asked)
	}

	bare := value.Struct(&Bare{})
	if got := bare.Get("PORT"); !got.IsUndefined() {
		t.Errorf("bare.PORT = %s, want undefined: a type without MissingMember reads nothing", got.Kind())
	}
	if got := bare.Invoke("PORT", nil); got.Code() != value.TypeCode || got.Message() != "bare has no method PORT" {
		t.Errorf("bare.PORT() = %s, want TYPE \"bare has no method PORT\"", got.Failure())
	}
	if names := value.Expose[Settings]().Names(); len(names) != 1 || names[0] != "size" {
		t.Errorf("Settings exposes %v: MissingMember is Go's, never a name of the program", names)
	}
}
