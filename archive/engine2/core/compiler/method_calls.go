package compiler

// MethodCallArguments are the arguments of each call object.method(…) in source, an expression or an event's handler
// of a .ui file, in the order the calls are written ($dialog.open({ title: 'Delete?', confirm: () => remove(id) })
// gives that object literal); nil when there is none or source does not read. library/ui reads what a component's
// call is given where it is written: a dialog's confirm is an arrow with no parameter (docs/components/dialog.md,
// "Compiler kiểm"). An arrow with a parameter named object has it as its own, and its calls are not object's.
func MethodCallArguments(source, object, method string) (found [][]Expression) {
	defer func() {
		if recover() != nil {
			found = nil
		}
	}()
	text := "const " + wrapper + " = (" + source + ");"
	program, err := parseProgramSource(text, "<method-calls>")
	if err != nil {
		return nil
	}
	statement, ok := oneValue(program)
	if !ok {
		return nil
	}
	stack := []Node{statement.Value}
	children := make([]Node, 0, 16)
	for len(stack) > 0 {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		switch n := node.(type) {
		case *FunctionLiteral:
			if declaresParameter(n, object) {
				continue
			}
		case *MethodCallExpression:
			if owner, ok := n.Object.(*Identifier); ok && owner.Value == object && n.Method != nil && n.Method.Value == method {
				found = append(found, n.Arguments)
			}
		}
		children = nodeChildren(node, children[:0])
		for i := len(children) - 1; i >= 0; i-- {
			stack = append(stack, children[i])
		}
	}
	return found
}
