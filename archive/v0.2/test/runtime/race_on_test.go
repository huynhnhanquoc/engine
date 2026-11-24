//go:build race

package runtime_test

// underRace is true when the tests run with the race detector, which slows each memory access by its own
// amount, so one way of doing a thing is not a fixed multiple of another: costs are not compared.
const underRace = true
