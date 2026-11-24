package runtime

import (
	"strconv"

	"github.com/kitwork/engine2/core/value"
)

// Energy by work (15/11/2026, value/work.go has the figures). Energy counted instructions, a call 150 units whatever it
// did, so one instruction over a large value ran for seconds on a price meant for a few nanoseconds: a loop of a ===
// of two texts of 20 MB, or of s.includes, s.slice or s.toUpperCase on one, ran to the request's time (30 s) having
// spent 1% of its energy; a sort of 100,000 numbers took 364 ms for 150 units. A unit is now what a plain instruction
// takes (4.4 ns where measured), and what the engine's own code does for the run is charged in such units, counted
// from sizes:
//   - what the run makes, a unit for value.MadeBytesPerUnit bytes, as the meter counts it again (spendMade): so a
//     loop that makes a large text and lets it go, which the meter lets on as the run holds little, spends as it makes;
//   - what a method of a text or an array reads, a function of the engine or a module reads of the texts it is
//     handed, a builder writes, through the run's value.Budget (Spend), which the engine's code asks as it works.
//
// What an instruction reads past its price is counted too (value/work.go, its second part): a text compared or made a
// number, a key or a name hashed, a large object spread, a text's length and units the cursor walked for (read,
// readKey, walked, numberOperands, compareRead).
//
// A request's energy (10,000,000 units) is then about 44 ms of the engine's work on the machine measured, whatever the
// program calls; a run past it stops with ENERGY_LIMIT, which nothing catches. What a run spends no longer depends on
// the instructions alone, but it still depends on nothing but the program and its data: never on a clock.

// spendMade counts the energy of what the run made since it was last counted by what it holds (memory.go), a unit for
// value.MadeBytesPerUnit bytes, as it is counted again: a loop that makes a large text and lets it go is let on by
// the meter, the run holding little, and spent but its instructions' energy (a + of a text of 10 MB, 1 ms, for a few
// units). Charged as the meter counts again, not as each byte is made, the meter's own path pays nothing more for it:
// a run that never passes its ceiling made at most its ceiling (64 MiB, some 10 ms of making), at no energy. The
// instruction that follows stops a run past its energy (consumeEnergy).
func (vm *VM) spendMade() {
	if vm.Bytes > vm.counted {
		vm.spend((vm.Bytes - vm.counted) / value.MadeBytesPerUnit)
	}
}

// energyMessage is the failure of a run past its energy.
const energyMessage = "Energy Limit Exceeded: Execution halted"

// spend counts units of energy the engine's code took for the run: false once the run is past its energy, which is
// then its ceiling, as consumeEnergy leaves it. An instruction that follows stops there (consumeEnergy); a builder
// asked through Spend stops at once.
func (vm *VM) spend(units uint64) bool {
	if ^uint64(0)-vm.Energy < units {
		vm.Energy = ^uint64(0)
	} else {
		vm.Energy += units
	}
	if vm.MaxEnergy > 0 && vm.Energy > vm.MaxEnergy {
		vm.Energy = vm.MaxEnergy
		return false
	}
	return true
}

// Spend is value.Budget's: units of the run's energy for what the engine's code did for it (value/work.go); the
// run's energy failure, recorded as its stop, once it is past its energy.
func (vm *VM) Spend(units uint64) value.Value {
	if units == 0 || vm.spend(units) {
		return value.Value{}
	}
	ip := -1
	if vm.FrameIdx >= 0 && vm.FrameIdx < len(vm.Frames) {
		ip = vm.Frames[vm.FrameIdx].LastIP
	}
	return vm.diagnosticValue(DiagnosticEnergyLimit, energyMessage, ip)
}

// read counts what an instruction read on the run's energy (value/work.go, what an instruction reads): every
// unit, an instruction's work being counted from value.LongText bytes on, not from the 64 units a call's price
// covers; Value{} while the run has energy, its energy failure once it has none.
func (vm *VM) read(units uint64) value.Value {
	if units == 0 {
		return value.Value{}
	}
	return vm.Spend(units)
}

// readKey counts a long key or name looked up, its bytes hashed (value.KeyWork); its caller tests the length
// first, so a short key pays a comparison.
//
//go:noinline
func (vm *VM) readKey(name string) value.Value {
	return vm.read(value.KeyWork(name))
}

// walked is result, read by the text cursor, once what the cursor walked for it is counted (TextCursor.Owed): the
// run's energy failure instead when that is past the run's energy. The cursor gives its walk once it reaches 64
// units, so a loop over one text pays a unit a character it walks, and a turn reading two texts each again.
func (vm *VM) walked(result value.Value) value.Value {
	if owed := vm.textCursor.Owed(); owed > 0 {
		if failure := vm.Spend(owed); failure.IsInvalid() {
			return failure
		}
	}
	return result
}

// numberOperands are the operands of - * / % of which one is a text, bytes, an object or an array (value.Plain),
// as the operator reads them (15/11/2026): an array made text within the run's budget, where ToNumber made it
// text past what the run may hold, the left side converted before the right as JavaScript converts; a text the
// operator makes a number counted (value.NumberWork). A failure is given as the left operand, or as the right,
// which every operator gives back once the left is not one.
//
//go:noinline
func (vm *VM) numberOperands(left, right value.Value) (value.Value, value.Value) {
	if left.IsArray() {
		if left = vm.arrayText(left); left.IsInvalid() {
			return left, right
		}
	}
	if right.IsArray() {
		if left.ConvertsWithCode() {
			// an object's own valueOf runs before the array's items are made text, as JavaScript converts the
			// left side first: the operator then reads the number it gave, as it would have
			if left = left.ToNumber(); left.IsInvalid() {
				return left, right
			}
		}
		if right = vm.arrayText(right); right.IsInvalid() {
			return left, right
		}
	}
	if failure := vm.read(value.NumberWork(left) + value.NumberWork(right)); failure.IsInvalid() {
		return left, failure
	}
	return left, right
}

// spreadIndexed is { ...source } of an array, a string or bytes into target (MERGE, 15/11/2026, round 3 of the 13/11
// review): each item, each UTF-16 unit of a string, each byte, under its index ("0", "1", …), as JavaScript spreads
// them, where Kitwork wrote nothing and gave {}. What it writes costs the run's energy before it writes, as an
// object's spread (value.IndexedSpreadWork), and the keys it adds what the run makes, once written, as an object's
// spread counts them (memory.go); the failure of either is the spread's.
//
//go:noinline
func (vm *VM) spreadIndexed(target, source value.Value, ip int) value.Value {
	var cursor value.TextCursor
	n := cursor.Length(source)
	if failure := vm.read(value.IndexedSpreadWork(n)); failure.IsInvalid() {
		return failure
	}
	keys := target.Count()
	for i := 0; i < n; i++ {
		item := cursor.Index(source, i)
		if item.IsFunction() {
			item = vm.standIn(item) // as SET keeps it
		}
		target.Set(strconv.Itoa(i), item)
	}
	if added := target.Count() - keys; added > 0 && !vm.consumeBytes(uint64(added)*value.KeyBytes) {
		return vm.memoryLimit(ip)
	}
	return value.Value{}
}
