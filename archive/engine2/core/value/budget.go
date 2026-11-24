package value

import (
	"math"
	"strconv"
)

// Budget is what the engine's builders may make for the run they work for (runtime, memory.go; 14/11/2026): Room(n)
// reports whether n bytes more than the run has counted fit within its ceiling, the run counted again by what it
// holds when it must; Exceeded is the run's memory failure, which ends it. A builder asks as what it makes grows,
// before it grows: join, a text of an array, JSON.stringify, JSON.parse, flat, which make much from little (an array
// holding one text a thousand times is a thousand texts once joined), stop where the run would pass its ceiling,
// where counting what they made after the call let one call make gigabytes first. What the builder made is counted
// once the call returns, as before: Room counts nothing.
//
// Spend counts n units of the run's energy for what the engine's code did for it (work.go, 15/11/2026): Value{} while
// the run has energy left, its energy failure once it has none, which ends the run as an instruction past its energy
// does.
type Budget interface {
	Room(n uint64) bool
	Exceeded() Value
	Spend(n uint64) Value
}

// UnbudgetedLength is the most a builder makes where no run counts it (a module writing a value as JSON, a key
// made of an array): 64 MiB, a run's ceiling by default, where JavaScript's longest string is eight times that.
const UnbudgetedLength = 64 << 20

// unbudgeted is the failure of a builder past UnbudgetedLength.
func unbudgeted() Value {
	return Fail(RangeCode, "invalid string length: more than "+strconv.Itoa(UnbudgetedLength>>20)+
		" MiB made where no run counts it")
}

// limit is what a builder may make: what budget gave so far (granted), asked again as the builder passes it, at
// least twice as much each time so the asks stay few. A text never passes maxStringLength, and with no budget
// UnbudgetedLength; items (flat's, JSON.parse's tree) with no budget are bounded as they were, by what each
// builder bounds itself.
type limit struct {
	budget  Budget
	granted int
	items   bool
	// refused is the least the budget refused when asked ahead: the limit then asks halfway between what is
	// needed and it, so a run near its ceiling is asked a few times, not at each item, and counted again only when
	// an ask does not fit; once what is needed passes it, it is forgotten
	refused int
	// work is the energy what the builder writes costs, in units for 32 bytes of what the limit counts (work.go),
	// spent with each ask, as the builder grows, and at its end (finish): charged is the size spent so far, owed the
	// units the builder owes beside its size (join's items)
	work    int
	charged int
	owed    uint64
}

// firstGrant is the least a limit asks its budget for: a small text never asks twice.
const firstGrant = 64 << 10

// allow is Value{} when the builder may have made size bytes, and the failure otherwise: tooLong past the
// longest string, the run's memory failure past its budget, unbudgeted past UnbudgetedLength. It is small enough
// to inline, for a builder that asks at each item.
func (l *limit) allow(size int) Value {
	if size <= l.granted {
		return Value{}
	}
	return l.grow(size)
}

func (l *limit) grow(size int) Value {
	most := maxStringLength
	switch {
	case l.items && l.budget == nil:
		l.granted = math.MaxInt
		return Value{}
	case l.items:
		most = math.MaxInt / 2
	case size > maxStringLength:
		return tooLong()
	case l.budget == nil:
		if size > UnbudgetedLength {
			return unbudgeted()
		}
		l.granted = UnbudgetedLength
		return Value{}
	}
	if l.work > 0 || l.owed > 0 {
		if spent := l.charge(size); spent.kind == invalid {
			return spent
		}
	}
	ask := min(max(size, 2*l.granted, firstGrant), most)
	switch {
	case l.refused > size && ask >= l.refused:
		ask = size + (l.refused-size)/2
	case l.refused != 0 && l.refused <= size:
		l.refused = 0
	}
	if !l.budget.Room(uint64(ask)) {
		// what was asked ahead did not fit: what is needed may
		if ask == size || !l.budget.Room(uint64(size)) {
			return l.budget.Exceeded()
		}
		l.refused, ask = ask, size
	}
	l.granted = ask
	return Value{}
}

// charge spends the energy of what the builder wrote up to size and what it owes beside; its failure once the run is
// past its energy.
func (l *limit) charge(size int) Value {
	units := l.owed
	if size > l.charged {
		units += uint64(size-l.charged) * uint64(l.work) / 32
		l.charged = size
	}
	l.owed = 0
	return Spend(l.budget, units)
}

// finish spends what the builder wrote and owes since it last grew, once it wrote size: Value{}, or the run's energy
// failure. With no budget it spends nothing.
func (l *limit) finish(size int) Value {
	if l.budget == nil {
		return Value{}
	}
	return l.charge(size)
}

// Afford is Value{} when budget has room for n bytes more, and its failure otherwise; a builder that knows what it
// will make asks before it makes it (Array.from, Object.keys). With no budget it is always Value{}.
func Afford(budget Budget, n uint64) Value {
	if budget == nil || budget.Room(n) {
		return Value{}
	}
	return budget.Exceeded()
}
