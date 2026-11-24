package ui

import "github.com/kitwork/engine/jit/hydrate"

func isWriteNode(node []any) bool { _, ok := hydrate.WriteTarget(node); return ok }

// aliasWriteBase finds the first member of a write path, including computed/nested paths.
func aliasWriteBase(node []any) any {
	target, ok := hydrate.WriteTarget(node)
	if !ok {
		return nil
	}
	for {
		n, ok := target.([]any)
		if !ok || len(n) != 3 || (n[0] != "." && n[0] != "[.]") {
			return nil
		}
		if hydrate.TargetName(n[1]) != "" {
			return n[1]
		}
		target = n[1]
	}
}
