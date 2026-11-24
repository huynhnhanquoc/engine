//go:build race

package host_test

// underRace is true when the tests run with the race detector, whose sync.Pool drops what it is given at
// random and which counts allocations of its own: a test of allocation counts against a number skips.
const underRace = true
