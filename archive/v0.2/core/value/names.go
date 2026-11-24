package value

import (
	"sort"
	"strings"
)

// NearestName is the name of names closest to name, compared in lower case, when it is close enough to be the one
// meant, fewer than three edits; "" otherwise: what a module says of a word it does not have ("router.faviconn": did
// you mean router.favicon?). Ties go to the first in sorted order.
func NearestName(name string, names []string) string {
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	best, bestDistance := "", 3
	lower := strings.ToLower(name)
	for _, candidate := range sorted {
		if distance := editDistance(lower, strings.ToLower(candidate)); distance < bestDistance {
			best, bestDistance = candidate, distance
		}
	}
	return best
}

// editDistance is Levenshtein's distance between a and b, by byte (the names are ASCII).
func editDistance(a, b string) int {
	previous := make([]int, len(b)+1)
	current := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(a); i++ {
		current[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			current[j] = min(previous[j]+1, current[j-1]+1, previous[j-1]+cost)
		}
		previous, current = current, previous
	}
	return previous[len(b)]
}
