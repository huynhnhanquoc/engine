package compiler

import "sort"

// doorWords are the router's doors whose function answers a request as it chooses: router.get(fn) and the other
// methods. A view's function, router.view(fn), always draws the folder's content.ui, and the host holds its folder
// to one already.
var doorWords = map[string]bool{"get": true, "post": true, "put": true, "patch": true, "delete": true}

// ViewAnswers are the places where a function program gives a door of the router at its top level (router.get(…)
// and the other methods, a rule before it or not; the function written there, or held by a const of the top level)
// calls view(…) on its first parameter, the run's context. context.view(…) draws the folder's content.ui
// (docs/KITWORK_ROUTER.md mục 2), so a folder without one answers every such request with an error: the host reads
// these at boot for a folder that has no content.ui, and the first is the boot's error (15/11/2026, the owner,
// round 3 of the 13/11 review; KITWORK_UI's list of errors, "router.get gọi context.view mà không có content.ui").
// A function the door's function calls, or one it imports, is not read here: its request still says so.
func ViewAnswers(program *Program) []Token {
	if program == nil {
		return nil
	}
	modules, functions, lines := topLevel(program)
	var found []Token
	for _, line := range lines {
		call, ok := line.(*MethodCallExpression)
		if !ok || call.Method == nil || !doorWords[call.Method.Value] || len(call.Arguments) != 1 ||
			modules[rootName(call.Object)] != "router" {
			continue
		}
		if fn := functionOf(call.Arguments[0], functions); fn != nil && len(fn.Parameters) > 0 {
			found = viewCalls(fn, fn.Parameters[0].Value, found)
		}
	}
	sort.Slice(found, func(i, j int) bool { return found[i].Position < found[j].Position })
	return found
}

// viewCalls adds to found each call context.view(…) in fn, context the name of its first parameter: a function in
// it, or a block, that declares the name again has its own, and is passed over. The tree is walked without
// recursion (nodeChildren), as checkNesting walks it.
func viewCalls(fn *FunctionLiteral, context string, found []Token) []Token {
	stack := []Node{fn.Body}
	children := make([]Node, 0, 16)
	for len(stack) > 0 {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		switch n := node.(type) {
		case *FunctionLiteral:
			if declaresParameter(n, context) {
				continue
			}
		case *BlockStatement:
			if n != fn.Body && blockDeclares(n, context) {
				continue
			}
		case *MethodCallExpression:
			if object, ok := n.Object.(*Identifier); ok && object.Value == context && n.Method != nil && n.Method.Value == "view" {
				found = append(found, object.Token)
			}
		}
		children = nodeChildren(node, children[:0])
		stack = append(stack, children...)
	}
	return found
}

// declaresParameter reports a parameter of fn named name.
func declaresParameter(fn *FunctionLiteral, name string) bool {
	for _, parameter := range fn.Parameters {
		if parameter != nil && parameter.Value == name {
			return true
		}
	}
	return false
}

// blockDeclares reports a const or let of block, its own statements, that declares name.
func blockDeclares(block *BlockStatement, name string) bool {
	for _, statement := range block.Statements {
		if declaration, ok := statement.(*VarStatement); ok {
			for _, declared := range declaration.Names {
				if declared != nil && declared.Value == name {
					return true
				}
			}
		}
	}
	return false
}
