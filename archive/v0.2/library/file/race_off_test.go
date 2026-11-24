//go:build !race

package file

// underRace is true when the tests run with the race detector (race_on_test.go).
const underRace = false
