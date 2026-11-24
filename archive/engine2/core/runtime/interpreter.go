package runtime

import (
	"math"

	"github.com/kitwork/engine2/core/value"
)

// Run executes the root frame of the currently loaded Program.
func (vm *VM) Run() (result value.Value) {
	if vm == nil || vm.program == nil {
		return vm.diagnosticValue(DiagnosticNoProgram, "VM has no verified program", -1)
	}
	if vm.FrameIdx < 0 {
		return vm.diagnosticValue(DiagnosticVMStopped, "VM is stopped", -1)
	}
	vm.entered++
	defer func() {
		if vm.FrameIdx > 0 {
			vm.FrameIdx = 0
		}
		// the outermost run gives the host's pool back what it took (pool.go)
		if vm.entered--; vm.entered == 0 {
			vm.giveBack()
		}
	}()
	return vm.execute(0)
}

// ExecuteLambda binds a lambda to a new frame and runs it through the same
// interpreter used by Run. The caller frame and stack are restored on every
// exit path, including HALT, cancellation, energy exhaustion, and errors.
func (vm *VM) ExecuteLambda(lambda *value.Lambda, args []value.Value) (result value.Value) {
	if lambda == nil {
		return value.Undefined()
	}
	if vm == nil || vm.program == nil {
		return vm.diagnosticValue(DiagnosticNoProgram, "VM has no verified program", -1)
	}

	callerFrame := vm.FrameIdx
	stackBase := len(vm.Stack)
	if fault := vm.pushLambdaFrame(lambda, args); fault != nil {
		return vm.diagnosticValue(fault.code, fault.message, vm.Frames[vm.FrameIdx].LastIP)
	}
	floor := vm.FrameIdx
	return vm.executePreparedLambda(callerFrame, stackBase, floor)
}

func (vm *VM) executePreparedLambda(callerFrame, stackBase, floor int) (result value.Value) {
	vm.entered++
	defer func() {
		if len(vm.Stack) > stackBase {
			vm.Stack = vm.Stack[:stackBase]
		}
		vm.FrameIdx = callerFrame
		// the outermost run gives the host's pool back what it took (pool.go): a host's call of a closure on a VM
		// that is running ends inside that run, which keeps what it took
		if vm.entered--; vm.entered == 0 {
			vm.giveBack()
		}
	}()

	return vm.execute(floor)
}

// executeLambdaFixed runs a lambda with count arguments (at most four) bound straight into its frame,
// with no argument slice: the array methods call it once per item.
func (vm *VM) executeLambdaFixed(
	lambda *value.Lambda,
	count int,
	first value.Value,
	second value.Value,
	third value.Value,
	fourth value.Value,
) value.Value {
	if lambda == nil {
		return value.Undefined()
	}
	if vm == nil || vm.program == nil {
		return vm.diagnosticValue(DiagnosticNoProgram, "VM has no verified program", -1)
	}

	callerFrame := vm.FrameIdx
	stackBase := len(vm.Stack)
	if lambda.Rest {
		if fault := vm.chargeRest(lambda, count); fault != nil {
			return vm.diagnosticValue(fault.code, fault.message, vm.Frames[vm.FrameIdx].LastIP)
		}
	}
	frame, fault := vm.prepareLambdaFrame(lambda)
	if fault != nil {
		return vm.diagnosticValue(fault.code, fault.message, vm.Frames[vm.FrameIdx].LastIP)
	}
	// every parameter is bound, one the call did not pass to undefined (bindParameters)
	arguments := [4]value.Value{first, second, third, fourth}
	slots := frame.Slots
	for index := range lambda.Params {
		if index < count {
			slots[index] = arguments[index]
		} else {
			slots[index] = value.Undefined()
		}
	}
	if lambda.Rest {
		bindRest(frame, lambda, arguments[:count])
	}
	return vm.executePreparedLambda(callerFrame, stackBase, vm.FrameIdx)
}

func (vm *VM) executeLambda3(
	lambda *value.Lambda,
	first value.Value,
	second value.Value,
	third value.Value,
) value.Value {
	return vm.executeLambdaFixed(lambda, 3, first, second, third, value.Undefined())
}

// executeLambda4 is reduce's call: the accumulator, the item, its index and the array.
func (vm *VM) executeLambda4(
	lambda *value.Lambda,
	first value.Value,
	second value.Value,
	third value.Value,
	fourth value.Value,
) value.Value {
	return vm.executeLambdaFixed(lambda, 4, first, second, third, fourth)
}

// execute is the VM's only bytecode dispatch loop. floor is the first frame
// owned by this invocation; returning below it hands control back to the caller.
func (vm *VM) execute(floor int) value.Value {
	// stopAtEntry is the stop (vm.stop) already recorded when this invocation began: the defers that
	// run after a stop run under it, so only a stop recorded since ends this invocation.
	stopAtEntry := vm.stop
	for vm.FrameIdx >= floor {
		frame := &vm.Frames[vm.FrameIdx]

		if frame.IP >= len(vm.program.code) {
			result := value.Undefined()
			if len(vm.Stack) > frame.StackBase {
				result = vm.pop()
			}
			finished, result := vm.finishFrame(frame, floor, result)
			if finished {
				return result
			}
			if result.IsInvalid() {
				return vm.exitFailure(floor, stopAtEntry, result)
			}
			continue
		}

		opIP := frame.IP
		op := Opcode(vm.program.code[frame.IP])
		frame.IP++
		frame.LastIP = opIP

		// Program construction verified every opcode, operand width, constant,
		// jump, and stack transition before publication. The immutable dispatch
		// path can therefore read canonical metadata directly.
		spec := &instructionTable[uint8(op)]

		if !vm.consumeEnergy(spec.Energy) {
			return vm.exitFailure(floor, stopAtEntry, vm.diagnosticValue(
				DiagnosticEnergyLimit,
				"Energy Limit Exceeded: Execution halted",
				opIP,
			))
		}

		vm.instructions++
		if vm.instructions&cancellationCheckMask == 0 {
			if failure, failed := vm.checkpoint(opIP); failed {
				return vm.exitFailure(floor, stopAtEntry, failure)
			}
		}
		// executed tells, after the instruction, whether program code ran inside it (a callback, a
		// method kept in an object, a defer, a function a host called): every instruction of it counts.
		executed := vm.instructions

		// An instruction that cannot leave a failure on top of the stack ends its case with continue, past
		// the failure test below: PUSH (a constant is never a failure, program.go, nor is a closure), POP,
		// STORE, STORE_SLOT, STORE_OUTER and DUP (they leave a value an earlier instruction pushed and tested),
		// LOAD_SLOT, LOAD_OUTER and BUILTIN of a value that is not a failure,
		// COMPARE of === and !==, or of two values neither of which is an object or an array, NOT and TYPEOF
		// (a boolean or a word), JUMP, TRUE and FALSE (nothing pushed) and MAKE.
		// Every other instruction may push one and is tested, so no failure stays on the stack for these
		// to meet.
		switch op {
		case PUSH, PUSH_WIDE:
			index := vm.readOperand(frame, spec.operandSize)
			constant := vm.program.constants[index]
			if template := constant.Lambda(); template != nil {
				// a closure is made: its lambda, counted by what the run makes (memory.go)
				if !vm.consumeBytes(closureBytes) {
					return vm.exitFailure(floor, stopAtEntry, vm.memoryLimit(opIP))
				}
				vm.push(vm.makeClosure(frame, template))
			} else {
				vm.push(constant)
			}
			continue

		case POP:
			// the verifier proved the frame holds the value (verify.go), so no frame-base test
			vm.Stack = vm.Stack[:len(vm.Stack)-1]
			continue

		case LOAD, LOAD_WIDE:
			name := vm.program.constants[vm.readOperand(frame, spec.operandSize)].Text()
			if len(name) >= value.LongText {
				// a long name, hashed whole each time it is looked up (value.KeyWork)
				if failure := vm.readKey(name); failure.IsInvalid() {
					vm.push(failure)
					break
				}
			}
			vm.push(vm.load(frame, name))

		case STORE, STORE_WIDE:
			name := vm.program.constants[vm.readOperand(frame, spec.operandSize)].Text()
			if len(name) >= value.LongText {
				// a long name, hashed whole each time it is written (value.KeyWork)
				if failure := vm.readKey(name); failure.IsInvalid() {
					vm.Stack[len(vm.Stack)-1] = failure
					break
				}
			}
			if frame.Fn == nil {
				// the top level's own variable, the common case: the root frame runs no function, so no
				// function around holds the name and none of a frozen VM's (store), and its map is vm.Vars
				vm.Vars[name] = vm.Stack[len(vm.Stack)-1]
				continue
			}
			if vm.store(frame, name, vm.Stack[len(vm.Stack)-1]) {
				// a variable of a frozen VM's code (Freeze): the failure takes the value's place
				vm.Stack[len(vm.Stack)-1] = frozenWrite(name)
				break
			}
			continue

		case LOAD_SLOT, LOAD_SLOT_WIDE:
			// a variable of the function running, by its index: the verifier proved the index inside the
			// frame of the function whose code this is (verify.go), which prepareLambdaFrame made that size.
			// Only a parameter may hold a failure (one a host passed as an argument): such a value goes
			// through the failure test as LOAD's does, any other skips it.
			item := frame.Slots[vm.readOperand(frame, spec.operandSize)]
			vm.push(item)
			if !item.IsInvalid() {
				continue
			}

		case STORE_SLOT, STORE_SLOT_WIDE:
			frame.Slots[vm.readOperand(frame, spec.operandSize)] = vm.Stack[len(vm.Stack)-1]
			continue

		case LOAD_OUTER, LOAD_OUTER_WIDE:
			depth := int(vm.program.code[frame.IP])
			frame.IP++
			index := vm.readOperand(frame, spec.operandSize-1)
			item := outerSlot(frame.Fn, depth, index)
			vm.push(item)
			if !item.IsInvalid() {
				continue
			}

		case STORE_OUTER, STORE_OUTER_WIDE:
			depth := int(vm.program.code[frame.IP])
			frame.IP++
			index := vm.readOperand(frame, spec.operandSize-1)
			if owner := outerLambda(frame.Fn, depth); owner != nil && index < len(owner.Slots) {
				if held := owner.Slots[index]; held.IsInvalid() && held.Code() == value.NotInitializedCode {
					// a let a function writes before its line has run (unset.go): the failure takes the value's place
					vm.Stack[len(vm.Stack)-1] = held
					break
				}
				if madeFrozen(owner) {
					// a variable of a frozen VM's code (Freeze): the failure takes the value's place
					vm.Stack[len(vm.Stack)-1] = frozenOuterWrite(owner, index)
					break
				}
				owner.Slots[index] = vm.Stack[len(vm.Stack)-1]
			}
			continue

		case GET:
			key := vm.pop()
			target := vm.pop()
			switch {
			case target.IsObject():
				// An object's read runs nothing of the host (Kitwork has no getter), so it needs no host
				// boundary; a nested VM's hand-back is taken at the next host call or checkpoint. A number
				// key of an object reads its text, the key o[k] = v writes: o[7] is o["7"], as in
				// JavaScript (15/10/2026; it read null, so o[k]++ and counting by a number lost the count).
				// A key of another kind is made text out of the loop, where that can fail (an object's own
				// toString): the failure is the read's (05/11/2026).
				if key.IsString() {
					name := key.Text()
					if len(name) >= value.LongText {
						// a long key, hashed whole each time it is looked up (value.KeyWork)
						if failure := vm.readKey(name); failure.IsInvalid() {
							vm.push(failure)
							break
						}
					}
					vm.push(target.Get(name))
				} else {
					if key.IsArray() {
						// o[a] reads the key a's text names, made within the run's budget; its failure is the read's
						if key = vm.arrayText(key); key.IsInvalid() {
							vm.push(key)
							break
						}
					}
					vm.push(vm.objectMember(target, key))
				}
			case target.IsString():
				vm.push(vm.textMember(target, key))
			case !key.IsNumber():
				if key.IsString() {
					name := key.Text()
					if len(name) >= value.LongText {
						if failure := vm.readKey(name); failure.IsInvalid() {
							vm.push(failure)
							break
						}
					}
					vm.push(vm.nativeProperty(target, name))
				} else {
					vm.push(vm.keyedProperty(target, key))
				}
			case math.Trunc(key.Number()) != key.Number() || math.IsInf(key.Number(), 0):
				// a[1.5], a[NaN], a[Infinity] name no item: undefined, as JavaScript gives, and as
				// a[1.5] = v is refused (array_item.go). It read a[1], cutting the fraction (15/10/2026).
				vm.push(value.Undefined())
			default:
				vm.push(target.Index(int(key.Number())))
			}

		case DUP:
			vm.push(vm.Stack[len(vm.Stack)-1])
			continue

		case BUILTIN:
			// kitwork (slot 0) or a builtin the compiler bound a name to (builtin.go); only a host's slot
			// can hold a failure, which goes through the failure test as LOAD's does
			index := int(vm.program.code[frame.IP])
			frame.IP++
			item := vm.builtin(index)
			vm.push(item)
			if !item.IsInvalid() {
				continue
			}

		// A binary operator works in place: its two operands are the frame's top two values (verify.go),
		// the result takes the left one's slot, which the stack's high water already counts.
		case ADD:
			top := len(vm.Stack) - 1
			left, right := vm.Stack[top-1], vm.Stack[top]
			if left.IsArray() || right.IsArray() {
				left, right = vm.arrayText(left), vm.arrayText(right)
			}
			// a text joined is built in the run's buffers (value/join.go), what it allocated counted by what the run
			// makes (memory.go); its failure is the result
			sum, made := left.AddJoining(right, vm.joiner)
			if made > 0 {
				if failure := vm.chargeMade(made, opIP); failure.IsInvalid() {
					sum = failure
				}
			}
			vm.Stack[top-1] = sum
			vm.Stack = vm.Stack[:top]
		case SUB:
			top := len(vm.Stack) - 1
			left, right := vm.Stack[top-1], vm.Stack[top]
			if !value.Plain(left, right) {
				left, right = vm.numberOperands(left, right)
			}
			vm.Stack[top-1] = left.Subtract(right)
			vm.Stack = vm.Stack[:top]
		case MUL:
			top := len(vm.Stack) - 1
			left, right := vm.Stack[top-1], vm.Stack[top]
			if !value.Plain(left, right) {
				left, right = vm.numberOperands(left, right)
			}
			vm.Stack[top-1] = left.Multiply(right)
			vm.Stack = vm.Stack[:top]
		case DIV:
			top := len(vm.Stack) - 1
			left, right := vm.Stack[top-1], vm.Stack[top]
			if !value.Plain(left, right) {
				left, right = vm.numberOperands(left, right)
			}
			vm.Stack[top-1] = left.Divide(right)
			vm.Stack = vm.Stack[:top]
		case MOD:
			top := len(vm.Stack) - 1
			left, right := vm.Stack[top-1], vm.Stack[top]
			if !value.Plain(left, right) {
				left, right = vm.numberOperands(left, right)
			}
			vm.Stack[top-1] = left.Modulo(right)
			vm.Stack = vm.Stack[:top]

		case STEP:
			// ++ and --: the value made a number (JavaScript's ToNumber), then stepped (bytecode 4); -x
			// (StepNegate, bytecode 6)
			mode := vm.program.code[frame.IP]
			frame.IP++
			last := len(vm.Stack) - 1
			number := vm.Stack[last]
			if !number.IsNumber() {
				if mode == StepNegate && !number.IsString() && !number.IsArray() {
					// -x of what is not a number is value.Negate: a duration stays one, a failure of
					// ToNumber (a valueOf) is the result; a text or an array is made a number below, what
					// that reads counted, and negated as Negate negates it
					vm.Stack[last] = number.Negate()
					break
				}
				number = vm.toNumber(number)
			}
			if number.IsNumber() {
				switch mode {
				case StepIncrement:
					number = value.Number(number.Number() + 1)
				case StepDecrement:
					number = value.Number(number.Number() - 1)
				case StepNegate:
					number = value.Number(-number.Number())
				}
			}
			vm.Stack[last] = number

		case AND:
			right, left := vm.pop(), vm.pop()
			if left.ToBoolean() {
				vm.push(right)
			} else {
				vm.push(left)
			}
		case OR:
			right, left := vm.pop(), vm.pop()
			if left.ToBoolean() {
				vm.push(left)
			} else {
				vm.push(right)
			}
		case NOT:
			top := len(vm.Stack) - 1
			vm.Stack[top] = value.Boolean(!vm.Stack[top].ToBoolean())
			continue

		case COMPARE:
			mode := vm.program.code[frame.IP]
			frame.IP++
			top := len(vm.Stack) - 1
			if left, right := vm.Stack[top-1], vm.Stack[top]; !value.PlainCompare(left, right) {
				// a text, bytes, an object or an array, or two times: what the comparison reads of a text costs the run's
				// energy, and an object's own valueOf or an array's items may run, and fail (compareRead). Its
				// failure goes through the failure test below; a Boolean skips it, as two plain values do
				result := vm.compareRead(left, right, mode)
				vm.Stack[top-1] = result
				vm.Stack = vm.Stack[:top]
				if result.IsInvalid() {
					break
				}
				continue
			}
			vm.Stack[top-1] = value.Boolean(compare(vm.Stack[top-1], vm.Stack[top], mode))
			vm.Stack = vm.Stack[:top]
			continue

		case JUMP, JUMP_WIDE:
			frame.IP = vm.readOperand(frame, spec.operandSize)
			continue
		case TRUE, TRUE_WIDE:
			address := vm.readOperand(frame, spec.operandSize)
			top := len(vm.Stack) - 1
			if vm.Stack[top].ToBoolean() {
				frame.IP = address
			}
			vm.Stack = vm.Stack[:top]
			continue
		case FALSE, FALSE_WIDE:
			address := vm.readOperand(frame, spec.operandSize)
			top := len(vm.Stack) - 1
			if !vm.Stack[top].ToBoolean() {
				frame.IP = address
			}
			vm.Stack = vm.Stack[:top]
			continue

		case ITER, ITER_WIDE:
			address := vm.readOperand(frame, spec.operandSize)
			index := vm.pop()
			collection := vm.peek()
			if !collection.IsArray() && !collection.IsString() && !collection.IsBytes() {
				// what is not iterable is the loop's TYPE failure, as JavaScript throws (notIterable)
				vm.Stack[len(vm.Stack)-1] = notIterable(collection)
				break
			}
			if collection.IsString() {
				// a string walks its code points, as JavaScript's iterator does (16/11/2026, round 5 of the 13/11
				// review: it walked UTF-16 units, so "😀" was two halves, where a spread of it is one character): the
				// index kept on the stack is the byte the next one starts at
				text := collection.Text()
				if at := int(index.Number()); at < len(text) {
					item, next := value.CodePointAt(text, at)
					vm.push(value.Number(float64(next)))
					vm.push(item)
				} else {
					vm.pop()
					frame.IP = address
				}
				break
			}
			if at := int(index.Number()); at < vm.iterationLength(collection) {
				vm.push(value.Number(index.Number() + 1))
				item := vm.cursor().Index(collection, at)
				vm.push(item)
			} else {
				vm.pop()
				frame.IP = address
			}

		case MAKE:
			kind := vm.program.code[frame.IP]
			frame.IP++
			// an object or an array made, counted by what the run makes (memory.go); its items as SET adds them
			if !vm.consumeBytes(value.MadeBytes) {
				return vm.exitFailure(floor, stopAtEntry, vm.memoryLimit(opIP))
			}
			if kind == 0 {
				vm.push(value.Object())
			} else {
				vm.push(value.Array(nil))
			}
			continue

		case SET:
			item, key, target := vm.pop(), vm.pop(), vm.pop()
			if target.IsObject() {
				// a frozen object (Object.freeze, every builtin) refuses the write: its TYPE failure is
				// placed at the write below, as a write to a frozen object throws in strict JavaScript; so is
				// the failure of a key whose text fails (setObject)
				keys := target.Count()
				if written := vm.setObject(target, key, item, opIP); written.IsInvalid() {
					vm.push(written)
					break
				}
				// a key added, counted by what the run makes (memory.go)
				if added := target.Count() - keys; added > 0 && !vm.consumeBytes(uint64(added)*value.KeyBytes) {
					vm.push(vm.memoryLimit(opIP))
					break
				}
			} else if target.IsArray() {
				// a[i] = v sets item i as JavaScript does: the length appends, past it the gap is holes that
				// read undefined, a key that is no index is a failure (array_item.go, 14/10/2026)
				stored := item
				if lambda := item.Lambda(); lambda != nil && !vm.made(lambda) {
					stored = vm.standIn(item) // as setObject keeps it
				}
				items := target.Count()
				if failure := vm.setArrayItem(target, key, stored, opIP); failure.IsInvalid() {
					vm.push(failure)
					break
				}
				// the items added, a gap's holes with them, counted by what the run makes (memory.go)
				if added := target.Count() - items; added > 0 && !vm.consumeBytes(uint64(added)*value.ItemBytes) {
					vm.push(vm.memoryLimit(opIP))
					break
				}
			} else if cannotHoldProperties(target) {
				// null.x = 1, s[0] = "x", context.x = 1, t.x = 1 of a time: the runtime error TYPE, as JavaScript's
				// TypeError (15/10/2026, property_write.go; a time, a duration, bytes since 18/11/2026), placed at the write
				return vm.exitFailure(floor, stopAtEntry, vm.writeThroughNonHolder(opIP, target))
			}
			// the value written, which is what o.x = v gives as JavaScript says (bytecode 5, 03/11/2026: it
			// left the target, so x = o.a = 2 made x the object); a literal keeps its target by DUP
			vm.push(item)

		case MERGE:
			source, target := vm.pop(), vm.peek()
			if source.HasHoles() {
				vm.Stack[len(vm.Stack)-1] = value.HolesRefused("{ ...a }") // JavaScript writes no key for a hole
				break
			}
			if target.IsObject() && (source.IsArray() || source.IsString() || source.IsBytes()) {
				// { ...[1, 2] } and { ..."ab" } spread their items by index, as JavaScript does (spreadIndexed)
				if failure := vm.spreadIndexed(target, source, opIP); failure.IsInvalid() {
					vm.Stack[len(vm.Stack)-1] = failure
				}
				break
			}
			if library := source.Library(); library != "" {
				vm.Stack[len(vm.Stack)-1] = value.LibraryAsData(library, "{ ..."+library+" }")
				break
			}
			if target.IsObject() && source.IsObject() {
				// what the spread writes past the instruction's price, counted before it writes (value.SpreadWork)
				if failure := vm.read(value.SpreadWork(source)); failure.IsInvalid() {
					vm.Stack[len(vm.Stack)-1] = failure
					break
				}
				keys := target.Count()
				for _, key := range source.Keys() {
					item := source.Get(key)
					if item.IsFunction() {
						item = vm.standIn(item) // as SET keeps it
					}
					target.Set(key, item)
				}
				// the keys a spread added, counted by what the run makes (memory.go); its failure is the result
				if added := target.Count() - keys; added > 0 && !vm.consumeBytes(uint64(added)*value.KeyBytes) {
					vm.Stack[len(vm.Stack)-1] = vm.memoryLimit(opIP)
				}
			}

		case CALL:
			// raised is a failure the call made already placed (a callee that is not a function, a stack
			// overflow): the run ends with it as it is
			if raised := vm.executeCall(frame); raised.IsInvalid() {
				return vm.exitFailure(floor, stopAtEntry, raised)
			}
			// a stop nothing catches (energy, cancellation, a native panic…) recorded inside the call ends
			// the run here, even when a safe wrapped its failure into { ok: false } on the way up
			if vm.stop != stopAtEntry {
				return vm.exitFailure(floor, stopAtEntry, diagnosticResult(vm.stop))
			}

		case INVOKE:
			if raised := vm.executeInvoke(frame); raised.IsInvalid() {
				return vm.exitFailure(floor, stopAtEntry, raised)
			}
			if vm.stop != stopAtEntry {
				return vm.exitFailure(floor, stopAtEntry, diagnosticResult(vm.stop))
			}

		case RETURN:
			result := vm.pop()
			finished, result := vm.finishFrame(frame, floor, result)
			if finished {
				return result
			}

		case HALT:
			result := vm.pop()
			result = vm.unwindFrames(floor, result)
			vm.unwindToCaller(floor)
			return result

		case DEFER:
			deferred := vm.pop()
			if lambda := deferred.Lambda(); lambda != nil {
				frame.Defers = append(frame.Defers, lambda)
			}

		case SPAWN:
			spawned := vm.pop()
			if lambda := spawned.Lambda(); lambda != nil && vm.Spawner != nil {
				if failure := vm.nativeAction("SPAWN", func() {
					vm.Spawner(lambda)
				}); failure.IsInvalid() {
					vm.push(failure)
				}
			}

		case TYPEOF:
			// typeof x (bytecode 6): a word, never a failure, whatever x is; a name nothing declares reads
			// undefined, so it is "undefined", no error
			last := len(vm.Stack) - 1
			vm.Stack[last] = vm.Stack[last].Typeof()
			continue

		case SPREAD:
			// an array that spreads (bytecode 9, spread.go): the items of the value on top, or the value, appended
			// to the array under it; a failure (not iterable, energy, bytes) is the array's place
			mode := vm.program.code[frame.IP]
			frame.IP++
			source := vm.pop()
			if result := vm.spreadInto(vm.peek(), source, mode, opIP); result.IsInvalid() {
				vm.Stack[len(vm.Stack)-1] = result
			}

		case REST:
			// the rest of a pattern (bytecode 9, spread.go): the value destructured stays, its rest above it
			kind, count := vm.program.code[frame.IP], int(vm.program.code[frame.IP+1])
			frame.IP += 2
			var read []value.Value
			if kind == RestObject {
				// the keys read before it, above the value destructured, which the verifier proved are there
				top := len(vm.Stack)
				read = append(read, vm.Stack[top-count:]...)
				clear(vm.Stack[top-count:])
				vm.Stack = vm.Stack[:top-count]
				count = 0
			}
			vm.push(vm.restOf(vm.peek(), kind, count, read, opIP))

		case UNSET_SLOT:
			// a let or a const not yet made (bytecode 10, unset.go): a read of it meets the failure, as a parameter's is
			index := vm.readOperand(frame, 3)
			name := vm.program.constants[vm.readOperand(frame, 3)].Text()
			if !vm.consumeBytes(value.MadeBytes) {
				return vm.exitFailure(floor, stopAtEntry, vm.memoryLimit(opIP))
			}
			frame.Slots[index] = notInitialized(name)
			continue

		case UNSET_NAME:
			name := vm.program.constants[vm.readOperand(frame, spec.operandSize)].Text()
			if !vm.consumeBytes(value.MadeBytes) {
				return vm.exitFailure(floor, stopAtEntry, vm.memoryLimit(opIP))
			}
			vm.Vars[name] = notInitialized(name)
			continue

		case DELETE:
			// delete o.k, delete o[k] (bytecode 10, delete.go): true, or the failure in its place
			key := vm.pop()
			last := len(vm.Stack) - 1
			vm.Stack[last] = vm.deleteKey(vm.Stack[last], key)

		case ITEMS:
			// what an array pattern reads (bytecode 10, spread.go): the value on top made its iteration's items; a
			// failure (not iterable, energy, bytes) is its place
			last := len(vm.Stack) - 1
			vm.Stack[last] = vm.itemsOf(vm.Stack[last], opIP)

		case CALL_SPREAD:
			// f(...args) (bytecode 9): the arguments gathered in an array, laid on the stack, called as CALL calls
			count, failure := vm.spreadArguments(vm.pop())
			if failure.IsInvalid() {
				vm.Stack[len(vm.Stack)-1] = failure
				break
			}
			if raised := vm.callArguments(frame, count); raised.IsInvalid() {
				return vm.exitFailure(floor, stopAtEntry, raised)
			}
			if vm.stop != stopAtEntry {
				return vm.exitFailure(floor, stopAtEntry, diagnosticResult(vm.stop))
			}

		case INVOKE_SPREAD:
			// o.m(...args) (bytecode 9): as CALL_SPREAD, the method's name on top as INVOKE has it
			method := vm.pop()
			count, failure := vm.spreadArguments(vm.pop())
			if failure.IsInvalid() {
				vm.Stack[len(vm.Stack)-1] = failure
				break
			}
			vm.push(method)
			if raised := vm.invokeArguments(frame, count); raised.IsInvalid() {
				return vm.exitFailure(floor, stopAtEntry, raised)
			}
			if vm.stop != stopAtEntry {
				return vm.exitFailure(floor, stopAtEntry, diagnosticResult(vm.stop))
			}

		case TEMPLATE:
			// a template's ${…} (bytecode 8): the last case, so the cases above keep their place in the loop
			top := len(vm.Stack) - 1
			left, right := vm.Stack[top-1], vm.Stack[top]
			if right.IsArray() {
				right = vm.arrayText(right)
			}
			// a text made, as ADD's (value/join.go, memory.go); its failure is the result
			joined, made := left.TemplateJoining(right, vm.joiner)
			if made > 0 {
				if failure := vm.chargeMade(made, opIP); failure.IsInvalid() {
					joined = failure
				}
			}
			vm.Stack[top-1] = joined
			vm.Stack = vm.Stack[:top]
		}

		activeFrame := &vm.Frames[vm.FrameIdx]
		if len(vm.Stack) > activeFrame.StackBase &&
			vm.Stack[len(vm.Stack)-1].IsInvalid() {
			invalid := vm.Stack[len(vm.Stack)-1]
			vm.Stack = vm.Stack[:len(vm.Stack)-1]
			// a stop nothing catches, recorded where it happened (vm.stop): this is its failure
			if vm.stop != stopAtEntry {
				return vm.exitFailure(floor, stopAtEntry, invalid)
			}
			code, message := invalid.Code(), invalid.Message()
			// A host call that failed because the request is over (a query's "context canceled", a VM
			// asked of a closed scope, SQLite's CANCELLED) is the cancellation, which nothing catches.
			if code == value.CancelledCode {
				return vm.exitFailure(floor, stopAtEntry, vm.diagnosticValue(DiagnosticCancelled, cancelledMessage(message), opIP))
			}
			// A failure that came out of program code this instruction ran (a callback, a method kept in an
			// object, a defer, a function a host called, routine.wait's WrapFailure) was placed by the run
			// that met it, where it happened: it ends this run as it is. (A failure value carries no
			// Diagnostic to tell so, 03/11/2026: that code ran is what tells it.)
			if vm.instructions != executed {
				return vm.exitFailure(floor, stopAtEntry, invalid)
			}
			// Any failure of the host while the request is over is the cancellation too; a host that
			// wrapped a failure nothing catches keeps it so; any other failure of the host is a runtime
			// error, caught by safe(() => …).
			if vm.cancelled() {
				return vm.exitFailure(floor, stopAtEntry, vm.diagnosticValue(DiagnosticCancelled, cancelledMessage(message), opIP))
			}
			if never, uncatchable := uncatchableCode(code); uncatchable {
				return vm.exitFailure(floor, stopAtEntry, vm.diagnosticValue(never, message, opIP))
			}
			// A failure a run placed already (Place: another VM's, a frozen file's function a host called, a failure
			// WrapFailure told at this call) happened where it says: placed again here, its text was its headline
			// led by this place too (19/11/2026).
			if invalid.Place() != "" {
				return vm.exitFailure(floor, stopAtEntry, invalid)
			}
			diagnostic := vm.buildDiagnostic(DiagnosticRuntimeError, message, opIP)
			diagnostic.CauseCode, diagnostic.Fields, diagnostic.Progress = code, invalid.Fields(), invalid.Progress()
			return vm.exitFailure(floor, stopAtEntry, diagnosticResult(diagnostic))
		}
	}

	return value.Undefined()
}

// makeClosure is the function a program's function literal makes when PUSH reaches it: template's
// code, closing over the frame running. A closure is one allocation, its Lambda, whose first Closure
// keeps its function value inside it (value/lambda.go). It shares the template's parameters and slot
// names, which nothing writes (the slices are capped, so an append copies), and the VM's one runner:
// fn.Call(args) from Go or from a builtin (map, sort, safe) runs it without the caller knowing a VM. It
// is a method of its own so that the dispatch loop stays small.
//
// It holds the frame's slots, by reference, only when its code or a function inside it reaches a
// variable of the frame's function (the compiler's CapturesSlots), and the frame's map by name only
// when such code reads or writes a variable by name (CapturesNames); the frame is then marked, so its
// next function makes new ones and leaves these to the closure. A callback that reaches nothing around
// it, (x) => x * 2, costs its frame nothing.
func (vm *VM) makeClosure(frame *Frame, template *value.Lambda) value.Value {
	params, locals := template.Params, template.Locals
	closure := &value.Lambda{
		Address:       template.Address,
		Name:          template.Name,
		SourceFile:    template.SourceFile,
		SourceLine:    template.SourceLine,
		SourceColumn:  template.SourceColumn,
		Params:        params[:len(params):len(params)],
		Locals:        locals[:len(locals):len(locals)],
		Parent:        frame.Fn,
		Program:       vm.currentOrigin(),
		Run:           vm.runLambda,
		CapturesSlots: template.CapturesSlots,
		CapturesNames: template.CapturesNames,
		WritesNames:   template.WritesNames,
		Rest:          template.Rest,
		Defaulted:     template.Defaulted,
	}
	if template.CapturesSlots {
		closure.Slots = frame.Slots
		frame.slotsCaptured = true
	}
	if template.CapturesNames && frame.Vars != nil {
		closure.Scope = frame.Vars
		frame.varsCaptured = true
	}
	return value.Closure(closure)
}

// outerSlots are the slots of the frame of the function depth levels around the closure running
// (fn): 1 is the function fn was made in, whose slots fn holds; each level further is the closure that
// made the last. nil when the chain is shorter (a program the compiler did not make).
func outerSlots(fn *value.Lambda, depth int) []value.Value {
	if owner := outerLambda(fn, depth); owner != nil {
		return owner.Slots
	}
	return nil
}

// outerLambda is the function depth levels around fn whose Slots are that frame's (depth 1 is fn
// itself); nil when there is none.
func outerLambda(fn *value.Lambda, depth int) *value.Lambda {
	for ; depth > 1 && fn != nil; depth-- {
		fn = fn.Parent
	}
	return fn
}

// outerSlot reads slot index of the frame depth levels around fn; undefined when it has no such slot.
func outerSlot(fn *value.Lambda, depth, index int) value.Value {
	if slots := outerSlots(fn, depth); index < len(slots) {
		return slots[index]
	}
	return value.Undefined()
}

// arrayText is an operand of + or of a template made text first when it is an array, within the run's budget
// (value.Budget): the text + and ${…} would make of it, JavaScript's ToPrimitive of an array, asked of the run as
// it grows. An array holding one text a thousand times is it a thousand times once text. Any other operand is
// itself; a failure is the operand, which + gives back.
//
//go:noinline
func (vm *VM) arrayText(v value.Value) value.Value {
	if !v.IsArray() {
		return v
	}
	return v.TextWithin(vm)
}

// propertyKey is the text a key names a property by: a string itself, any other value its text, as
// JavaScript's ToPropertyKey (o[7] is o["7"]), or the failure of making that text, as JavaScript throws
// there: an object's own toString that fails, or that a request cannot run (one a frozen file made). It
// was read as "", so table[p] read the key "" and o[p] = 1 wrote it (05/11/2026).
func propertyKey(key value.Value) (string, value.Value) {
	if key.IsString() {
		return key.Text(), value.Value{}
	}
	return key.ToText()
}

// keyedProperty is target[key] of a value that is neither an object nor a string, for a key that is
// neither a number nor a string, through the host boundary: a key whose text fails is the read's failure.
func (vm *VM) keyedProperty(target, key value.Value) value.Value {
	if target.IsNullish() && key.ConvertsWithCode() {
		// null[k] fails before k is converted, as JavaScript reads nothing of a null or undefined base:
		// k's own toString neither runs nor gives the failure, and V8 names no key it cannot show
		// (05/11/2026: a failing toString of k was the failure of null[k])
		nullish := "undefined"
		if target.IsNull() {
			nullish = "null"
		}
		return value.Fail(value.TypeCode, "cannot read properties of "+nullish)
	}
	if key.IsArray() {
		// the key's text, made within the run's budget; its failure is the read's
		if key = vm.arrayText(key); key.IsInvalid() {
			return key
		}
	}
	name, failure := propertyKey(key)
	if failure.IsInvalid() {
		return failure
	}
	if failure := vm.read(value.KeyWork(name)); failure.IsInvalid() {
		return failure
	}
	return vm.nativeProperty(target, name)
}

// objectMember is target[key] of an object for a key that is not a string: o[7] reads o["7"], and a key
// whose text fails is the read's failure; a long key costs the run's energy (value.KeyWork).
func (vm *VM) objectMember(target, key value.Value) value.Value {
	name := key.ToString()
	if name.IsInvalid() {
		return name
	}
	if failure := vm.read(value.KeyWork(name.Text())); failure.IsInvalid() {
		return failure
	}
	return target.Get(name.Text())
}

// setObject is target[key] = item (SET at ip) of an object, a failure when the write is refused: o[7] writes
// o["7"], a key whose text fails (an object's own toString) is a failure, as is a write to a frozen object
// (frozenIterationWrite names a loop's variable then). One call of the loop, as propertyKey and Set were.
func (vm *VM) setObject(target, key, item value.Value, ip int) value.Value {
	name := key.Text()
	if !key.IsString() {
		if key.IsArray() {
			// the key's text, made within the run's budget; its failure is the write's
			if key = vm.arrayText(key); key.IsInvalid() {
				return key
			}
		}
		text := key.ToString()
		if text.IsInvalid() {
			return text
		}
		name = text.Text()
	}
	// a function another VM made (one of a frozen file's top level) is kept as a stand-in that runs on
	// this VM (standIn), so that a conversion of what holds it (its valueOf, its toString) runs it here:
	// { toString: label } of the handler converts as JavaScript does (05/11/2026)
	if lambda := item.Lambda(); lambda != nil && !vm.made(lambda) {
		item = vm.standIn(item)
	}
	// a long key, hashed whole each time it is written (value.KeyWork)
	if len(name) >= value.LongText {
		if failure := vm.readKey(name); failure.IsInvalid() {
			return failure
		}
	}
	if written := target.Set(name, item); written.IsInvalid() {
		return vm.frozenIterationWrite(written, target, name, ip)
	}
	return value.Value{}
}

// cursor is the VM's text cursor, made at the first string it reads by index or length.
func (vm *VM) cursor() *value.TextCursor {
	if vm.textCursor == nil {
		vm.textCursor = new(value.TextCursor)
	}
	return vm.textCursor
}

// textMember is s[key] of a string s: s[i] and s.length read on from where the VM's text cursor last
// stood in the same string (value.TextCursor, 04/11/2026), since a loop over a string that is not ASCII
// walked it from the start twice a turn; any other key through the host boundary, as GET reads any
// value but an object. A method of its own, so that the dispatch loop stays small.
func (vm *VM) textMember(text, key value.Value) value.Value {
	switch {
	case key.IsNumber():
		if number := key.Number(); math.Trunc(number) == number && !math.IsInf(number, 0) {
			return vm.walked(vm.cursor().Index(text, int(number)))
		}
		// s[1.5], s[NaN] name no unit
		return value.Undefined()
	case key.IsString() && key.Text() == "length":
		return vm.walked(value.Number(float64(vm.cursor().Length(text))))
	}
	return vm.keyedProperty(text, key)
}

// iterationLength is how many items for…of walks (ITER): an array's items, a string's UTF-16 units
// (counted once by the text cursor, not once a turn), the bytes of bytes; 0 for every other kind, which ITER
// refuses before (notIterable).
func (vm *VM) iterationLength(collection value.Value) int {
	switch {
	case collection.IsArray():
		return len(collection.Items())
	case collection.IsString(), collection.IsBytes():
		// a string's length counted once by the cursor; what that read is counted with the unit ITER reads next
		return vm.cursor().Length(collection)
	}
	return 0
}

// notIterable is the TYPE failure of for…of over what is not an array, a string or bytes (15/11/2026, round 3 of
// the 13/11 review), as JavaScript throws "is not iterable": an object walked as many undefined as it had keys, and
// null, undefined or a number did not walk at all, both in silence.
func notIterable(collection value.Value) value.Value {
	if collection.IsObject() {
		return value.Fail(value.TypeCode, "for…of: một object không lặp được; lặp các khoá, giá trị hay cặp của nó: for (const key of Object.keys(o)), Object.values(o), for (const [key, value] of Object.entries(o))")
	}
	return value.Fail(value.TypeCode, "for…of: "+collection.Kind()+" không lặp được: chỉ lặp mảng, chuỗi hay bytes")
}

// readOperand reads a constant index or a jump address: two bytes, or three in a wide form (size is
// the instruction's operand size, which the verifier proved).
func (vm *VM) readOperand(frame *Frame, size uint8) int {
	code, ip := vm.program.code, frame.IP
	if size == 3 {
		frame.IP = ip + 3
		return int(code[ip])<<16 | int(code[ip+1])<<8 | int(code[ip+2])
	}
	frame.IP = ip + 2
	return int(code[ip])<<8 | int(code[ip+1])
}

func (vm *VM) load(frame *Frame, name string) value.Value {
	if item, ok := frame.Vars[name]; ok {
		return item
	}
	if item, ok := lookupScopeChain(frame.Fn, name); ok {
		return item
	}
	if item, ok := vm.Vars[name]; ok {
		return item
	}
	if item, ok := vm.Globals[name]; ok {
		return item
	}
	return value.Undefined()
}

// store writes name by name: in the nearest map of the functions around that holds it, else in the
// top level's or the frame's own. refused is a variable of a frozen VM's code, not written (Freeze).
func (vm *VM) store(frame *Frame, name string, item value.Value) (refused bool) {
	stored, refused := storeScopeChain(frame.Fn, name, item)
	if stored || refused {
		return refused
	}
	if vm.FrameIdx == 0 {
		vm.Vars[name] = item
		return false
	}
	if frame.Vars == nil {
		// the compiler marks a function that writes a name nothing declares (WritesNames), whose frame
		// has its map from the start; one it did not mark gets it at its first such write
		frame.Vars = make(map[string]value.Value)
	}
	frame.Vars[name] = item
	return false
}

func (vm *VM) pushLambdaFrame(lambda *value.Lambda, args []value.Value) *runtimeFault {
	if lambda.Rest {
		if fault := vm.chargeRest(lambda, len(args)); fault != nil {
			return fault
		}
	}
	frame, fault := vm.prepareLambdaFrame(lambda)
	if fault != nil {
		return fault
	}
	bindParameters(frame, len(lambda.Params), args)
	if lambda.Rest {
		bindRest(frame, lambda, args)
	}
	return nil
}

// chargeRest counts the array the rest of a function called with count arguments makes, (a, ...rest) => …, by what
// the run makes (memory.go), before its frame is pushed: the fault of the run's memory past its ceiling.
func (vm *VM) chargeRest(lambda *value.Lambda, count int) *runtimeFault {
	items := max(count-(len(lambda.Params)-1), 0)
	if !vm.consumeBytes(value.MadeBytes + uint64(items)*value.ItemBytes) {
		return &runtimeFault{code: DiagnosticMemoryLimit, message: memoryMessage}
	}
	return nil
}

// bindRest writes the last parameter of a function with a rest: the arguments past the others, an array.
func bindRest(frame *Frame, lambda *value.Lambda, args []value.Value) {
	last := len(lambda.Params) - 1
	var rest []value.Value
	if len(args) > last {
		rest = append(rest, args[last:]...)
	}
	frame.Slots[last] = value.Array(rest)
}

// bindParameters writes every parameter into its slot (the first ones of the frame), one the call did
// not pass as undefined, as JavaScript binds it (04/11/2026): a parameter left unbound was looked up in
// the functions around, so `var x = 5; const f = (x) => x; f()` read the outer 5. An argument past the
// parameters is not bound. The arguments are copied, so the caller's slice (a window on the stack) is
// not kept. They are copied one by one: copy of values that hold pointers is the runtime's
// typedslicecopy, a call and a bulk write barrier for what is mostly one to three values, which measured
// 9% of an array method's callback (04/11/2026).
func bindParameters(frame *Frame, params int, args []value.Value) {
	slots := frame.Slots[:params]
	for index := range slots {
		if index < len(args) {
			slots[index] = args[index]
		} else {
			slots[index] = value.Value{}
		}
	}
}

func (vm *VM) prepareLambdaFrame(lambda *value.Lambda) (*Frame, *runtimeFault) {
	// Only a closure of the VM's own program runs: its frame is as many slots as its template names,
	// which the verifier proved its code stays inside. A lambda no VM made (a template, one Go built)
	// has no program and is refused, as one of another program is.
	owner, ok := ProgramFromRef(lambda.Program)
	if !ok || owner != vm.program {
		return nil, &runtimeFault{
			code:    DiagnosticProgramMismatch,
			message: "lambda belongs to a different program",
		}
	}
	// callFloor is the frames held by the VMs this one runs inside (nesting.go): one ceiling for all.
	if vm.callFloor+vm.FrameIdx+1 >= defaultMaxCallDepth || vm.FrameIdx+1 >= len(vm.Frames) {
		return nil, &runtimeFault{
			code:    DiagnosticStackOverflow,
			message: "Stack overflow: Call stack limit exceeded",
		}
	}

	// A frame of many slots pays for them as it starts (limits.go, freeSlots): it is made or cleared
	// whole, whatever the function then runs.
	slots := len(lambda.Locals)
	if slots > freeSlots && !vm.consumeEnergy(Cost(slots-freeSlots)) {
		return nil, &runtimeFault{
			code:    DiagnosticEnergyLimit,
			message: "Energy Limit Exceeded: Execution halted",
		}
	}

	vm.FrameIdx++
	if vm.FrameIdx > vm.frameHighWater {
		vm.frameHighWater = vm.FrameIdx
	}
	frame := &vm.Frames[vm.FrameIdx]
	frame.IP = lambda.Address
	frame.LastIP = -1
	frame.Fn = lambda
	frame.StackBase = len(vm.Stack)

	// The function's slots: new ones when a closure holds the frame's last (or they are too few), else
	// the frame's own, cleared past the parameters, which the caller binds next. A slot is never read
	// before the function writes it but as undefined.
	if frame.slotsCaptured || cap(frame.Slots) < slots {
		frame.Slots = make([]value.Value, slots)
		frame.slotsCaptured = false
	} else {
		if len(frame.Slots) > slots {
			// what a function of more slots left past this one's: a closure that holds these slots
			// later would keep it alive
			clear(frame.Slots[slots:])
		}
		frame.Slots = frame.Slots[:slots]
		clear(frame.Slots[len(lambda.Params):])
	}
	// Its map by name, which only a function writing a name nothing declares needs (WritesNames): a new
	// one when a closure holds the last, else the last emptied.
	if frame.varsCaptured {
		frame.Vars = nil
		frame.varsCaptured = false
	} else if len(frame.Vars) > 0 {
		clear(frame.Vars)
	}
	if lambda.WritesNames && frame.Vars == nil {
		frame.Vars = make(map[string]value.Value)
	}
	clear(frame.Defers)
	frame.Defers = frame.Defers[:0]

	return frame, nil
}

func (vm *VM) finishFrame(
	frame *Frame,
	floor int,
	result value.Value,
) (bool, value.Value) {
	result = vm.runFrameDefers(frame, result)

	if len(vm.Stack) > frame.StackBase {
		vm.Stack = vm.Stack[:frame.StackBase]
	}

	if vm.FrameIdx == floor {
		if floor > 0 {
			vm.FrameIdx--
		}
		return true, result
	}

	vm.FrameIdx--
	vm.push(result)
	return false, result
}

func (vm *VM) runFrameDefers(frame *Frame, result value.Value) value.Value {
	for index := len(frame.Defers) - 1; index >= 0; index-- {
		deferredResult := vm.ExecuteLambda(frame.Defers[index], nil)
		result = mergeFailures(result, deferredResult)
	}
	clear(frame.Defers)
	frame.Defers = frame.Defers[:0]
	return result
}

func (vm *VM) unwindFrames(floor int, result value.Value) value.Value {
	for vm.FrameIdx >= floor {
		frame := &vm.Frames[vm.FrameIdx]
		result = vm.runFrameDefers(frame, result)
		if len(vm.Stack) > frame.StackBase {
			vm.Stack = vm.Stack[:frame.StackBase]
		}
		if vm.FrameIdx == floor {
			break
		}
		vm.FrameIdx--
	}
	return result
}

// exitFailure unwinds to floor with failure. After the energy limit the defers still run, on a small
// reserve above what was spent: when the run ends with that stop, recorded on vm.stop since this
// invocation began (stopAtEntry), as the failure the energy limit made once told it. A defer running
// under an earlier energy stop that fails otherwise unwinds on what it has. After the memory limit the same, on a
// reserve of bytes (memory.go).
func (vm *VM) exitFailure(floor int, stopAtEntry *Diagnostic, failure value.Value) value.Value {
	if vm.stop != nil && vm.stop != stopAtEntry && (vm.stop.Code == DiagnosticMemoryLimit || vm.stop.Code == DiagnosticHostMemory) && vm.MaxBytes > 0 {
		originalBytes := vm.MaxBytes
		vm.MaxBytes = vm.Bytes + cleanupBytesReserve
		if vm.MaxBytes < vm.Bytes {
			vm.MaxBytes = ^uint64(0)
		}
		result := vm.unwindFrames(floor, failure)
		vm.MaxBytes = originalBytes
		return result
	}
	if vm.stop == nil || vm.stop == stopAtEntry || vm.stop.Code != DiagnosticEnergyLimit || vm.MaxEnergy == 0 {
		return vm.unwindFrames(floor, failure)
	}

	originalLimit := vm.MaxEnergy
	cleanupLimit := vm.Energy + cleanupEnergyReserve
	if cleanupLimit < vm.Energy {
		cleanupLimit = ^uint64(0)
	}
	vm.MaxEnergy = cleanupLimit
	result := vm.unwindFrames(floor, failure)
	vm.MaxEnergy = originalLimit
	return result
}

func (vm *VM) unwindToCaller(floor int) {
	if floor > 0 {
		vm.FrameIdx = floor - 1
		return
	}
	vm.FrameIdx = 0
}

// executeCall runs CALL. It pushes the call's result, or returns raised: a failure the call made
// already placed (a stack overflow, a callee that is not a function), which ends the run as it is.
func (vm *VM) executeCall(frame *Frame) (raised value.Value) {
	count := int(vm.program.code[frame.IP])
	frame.IP++
	return vm.callArguments(frame, count)
}

// callArguments runs a call of the callee under count arguments on the stack: CALL's, and CALL_SPREAD's once it laid
// its arguments there.
func (vm *VM) callArguments(frame *Frame, count int) (raised value.Value) {

	// Script lambdas consume arguments immediately into their frame. Bind from
	// the verified VM stack directly so an internal call does not allocate an
	// argument slice that no caller can retain. The verifier proved the callee
	// and its arguments are in the frame.
	callableIndex := len(vm.Stack) - count - 1
	if function := vm.Stack[callableIndex].Lambda(); function != nil {
		args := vm.Stack[callableIndex+1:]
		vm.Stack = vm.Stack[:callableIndex]
		if fault := vm.pushLambdaFrame(function, args); fault != nil {
			return vm.diagnosticValue(fault.code, fault.message, frame.LastIP)
		}
		return value.Undefined()
	}

	// A function of the host reads its arguments from a window on the stack and lets go of them (the
	// contract value.Invoke states): the window is capped, so a host that appends to it writes a copy,
	// and a lambda it runs on this VM pushes above it. The slots are cleared once it returns.
	top := len(vm.Stack)
	args := vm.Stack[callableIndex+1 : top : top]
	callable := vm.Stack[callableIndex]
	// safe is a plain builtin (core/builtins), called as any function of the host: what it never
	// catches has already stopped the run (vm.stop), which the instruction loop reads after this call.
	// The host may run a lambda among args in a VM nested in this one (nesting.go: LambdaCaller).
	if mark := vm.passLambdas(args); len(vm.passing) > mark {
		defer vm.withdrawLambdas(mark)
	}
	var before counts
	if vm.MaxBytes > 0 {
		vm.countBefore(&before, value.Undefined(), args)
	}
	result, isFunction := vm.callHost(callable, args)
	if isFunction {
		// what a function of the host gives is new, counted whole (memory.go), read before the arguments' slots are
		// cleared; its failure past the ceiling is the result, which ends the run (vm.stop)
		counting := resultWhole
		if callable.GivesFresh() {
			counting = resultFresh
		}
		result = vm.chargeCall(&before, value.Undefined(), args, result, counting, frame.LastIP)
	}
	clear(vm.Stack[callableIndex:top])
	vm.Stack = vm.Stack[:callableIndex]
	if isFunction {
		vm.push(result)
		return value.Undefined()
	}

	// Anything else is not a function: calling it is a runtime error (14/10/2026), and it never prints.
	// Until then such a call gave null, and printed its arguments to standard output when the value was
	// the string "log" or "PRINT", so data a request carried could write a secret into the server's log.
	return vm.calledNotAFunction(frame.LastIP, callable)
}

// callHost calls a function of the host (not a script lambda) with args, as CALL calls it; callable is
// false when the value is not a function. A function of the host that needs the VM captured it when it
// was made, so every function is called the same way (fn.Call); a program's closure called this way
// runs through its Run, on the VM that made it.
func (vm *VM) callHost(callable value.Value, args []value.Value) (result value.Value, ok bool) {
	if !callable.IsFunction() {
		return value.Undefined(), false
	}
	return vm.nativeValue("function call", func() value.Value {
		return callable.CallWithin(vm, args) // a builtin that makes much asks vm as it grows (value.Budget)
	}), true
}

// executeInvoke runs INVOKE. It pushes the call's result, or returns raised: a failure the call made
// already placed (an array method's callback that is not a function), which ends the run as it is.
func (vm *VM) executeInvoke(frame *Frame) (raised value.Value) {
	count := int(vm.program.code[frame.IP])
	frame.IP++
	return vm.invokeArguments(frame, count)
}

// invokeArguments runs a call of the method named on top of the stack, of the target under count arguments: INVOKE's,
// and INVOKE_SPREAD's once it laid its arguments there.
func (vm *VM) invokeArguments(frame *Frame, count int) (raised value.Value) {
	method := vm.pop().Text()
	if len(method) >= value.LongText {
		// a long name, hashed whole as the method is looked up (value.KeyWork): past the run's energy, the call
		// is not made and its target and arguments leave the stack
		if failure := vm.readKey(method); failure.IsInvalid() {
			targetIndex := len(vm.Stack) - count - 1
			clear(vm.Stack[targetIndex:])
			vm.Stack = vm.Stack[:targetIndex]
			return failure
		}
	}

	// The verifier proved the target and its arguments are in the frame (INVOKE takes count + 2).
	targetIndex := len(vm.Stack) - count - 1
	target := vm.Stack[targetIndex]

	// a method that passes over holes, on an array Array(n) made (value.HoleSensitive): its result would not be
	// JavaScript's, so it is the call's TYPE error, placed at it (19/11/2026)
	if target.IsArray() && value.HoleSensitive(method) && target.HasHoles() {
		clear(vm.Stack[targetIndex:])
		vm.Stack = vm.Stack[:targetIndex]
		vm.push(value.HolesRefused("a." + method + "(…)"))
		return value.Value{}
	}

	if target.IsArray() && callsBack(method) {
		argument := value.Undefined()
		if count > 0 {
			argument = vm.Stack[targetIndex+1]
		}
		// The callback is called for each element when it is a function: a script lambda, or a function
		// of the engine (console.log, Boolean), as JavaScript calls it. sort is core/value's, below.
		if callback, ok := newArrayCallback(argument); ok && method != "sort" {
			initial := value.Undefined()
			hasInitial := count > 1
			if hasInitial {
				initial = vm.Stack[targetIndex+2]
			}
			// the array stays on the stack while its callbacks run, as map's array above it (arrayCallbackMethod):
			// a run counted again by what it holds counts it, a variable holding it or not (memory.go)
			vm.Stack = vm.Stack[:targetIndex+1]
			var before counts
			if vm.MaxBytes > 0 {
				vm.countBefore(&before, target, nil)
			}
			result, _ := vm.arrayCallbackMethod(
				target,
				method,
				callback,
				initial,
				hasInitial,
			)
			vm.Stack = vm.Stack[:targetIndex]
			// map's and filter's array made, counted as it takes itself (memory.go); find's item is held already
			counting := resultAsItself
			if returnsItem(method) {
				counting = resultHeld
			}
			vm.push(vm.chargeCall(&before, target, nil, result, counting, frame.LastIP))
			return value.Undefined()
		}
		// Anything else is not a function, so the call is the runtime error (14/10/2026), naming the
		// argument and never showing its value; sort() without one keeps its own order.
		if !argument.IsFunction() && (method != "sort" || !argument.IsNullish()) {
			clear(vm.Stack[targetIndex:])
			vm.Stack = vm.Stack[:targetIndex]
			return vm.callbackNotAFunction(frame.LastIP, method, count, argument)
		}
	}

	// An own key of an object, or of a function (Number.isInteger), is looked up once: Has, then Get
	// only when the key is there, since a Get that misses falls back to the builtins of the kind and
	// builds a bound function of the method (value/javascript.go) for nothing.
	var own value.Value
	if (target.IsObject() || target.IsFunction()) && target.Has(method) {
		own = target.Get(method)
	}
	switch {
	case own.Lambda() != nil:
		// a script lambda kept in an object runs on this VM as CALL runs it: a frame bound from the
		// arguments on the stack, with no argument slice and no nested run
		args := vm.Stack[targetIndex+1:]
		vm.Stack = vm.Stack[:targetIndex]
		if fault := vm.pushLambdaFrame(own.Lambda(), args); fault != nil {
			return vm.diagnosticValue(fault.code, fault.message, frame.LastIP)
		}
		return value.Undefined()

	case own.IsFunction():
		// a function of the host kept in an object ({ all: routine.wait }.all(fn), Math.max) is called
		// as CALL calls one (it captured the VM it needs when it was made), below

	case standardMethodUsesStackArgs(target):
		// Any other call of such a value is its builtin, through value.Invoke, which answers an own key
		// holding data as JavaScript does (not a function). The builtin reads its arguments from a
		// window on the stack (value.Invoke takes one), so the call allocates no argument slice. The
		// slots above the top are not cleared: the next push overwrites them, and resetStack clears up
		// to the high water mark.
		top := len(vm.Stack)
		args := vm.Stack[targetIndex+1 : top : top]
		var before counts
		if vm.MaxBytes > 0 {
			vm.countBefore(&before, target, args)
			// a builtin that would make past what is left stops before it allocates (memory.go, expectedMade)
			left := uint64(0)
			if vm.Bytes < vm.MaxBytes {
				left = vm.MaxBytes - vm.Bytes
			}
			if need := expectedMade(target, method, args, left); need > 0 && !vm.room(need) {
				vm.Stack = vm.Stack[:targetIndex]
				vm.push(vm.memoryLimit(frame.LastIP))
				return value.Undefined()
			}
		}
		result := vm.nativeMethod(target, value.Undefined(), method, args)
		vm.Stack = vm.Stack[:targetIndex]
		// what the builtin made, as it takes itself, and what it added to its target (memory.go)
		counting := resultAsItself
		if target.IsArray() && returnsItem(method) {
			counting = resultHeld
		}
		vm.push(vm.chargeCall(&before, target, args, result, counting, frame.LastIP))
		return value.Undefined()
	}

	// A function of the host an own key holds, a struct's method, a function's builtin: the host reads
	// its arguments from a capped window on the stack as CALL hands them (executeCall), and the slots
	// are cleared once it returns.
	top := len(vm.Stack)
	args := vm.Stack[targetIndex+1 : top : top]
	var before counts
	if vm.MaxBytes > 0 {
		vm.countBefore(&before, target, args)
	}
	result := vm.nativeMethod(target, own, method, args)
	// what a module's method gives is new, counted whole, as what JSON.parse gives (value.Fresh); a builtin's as it
	// takes itself (memory.go); read before the arguments' slots are cleared
	counting := resultAsItself
	switch {
	case own.GivesFresh():
		counting = resultFresh
	case target.IsStruct():
		counting = resultWhole
	}
	result = vm.chargeCall(&before, target, args, result, counting, frame.LastIP)
	clear(vm.Stack[targetIndex:top])
	vm.Stack = vm.Stack[:targetIndex]
	vm.push(result)
	return value.Undefined()
}

// standardMethodUsesStackArgs reports the values whose methods are the builtins of value itself (a
// number, a boolean, a time, a duration, a string, bytes, an object, an array), which read their
// arguments and let go of them.
func standardMethodUsesStackArgs(target value.Value) bool {
	return target.IsNumber() ||
		target.IsBoolean() ||
		target.IsTime() ||
		target.IsDuration() ||
		target.IsString() ||
		target.IsBytes() ||
		target.IsObject() ||
		target.IsArray()
}
