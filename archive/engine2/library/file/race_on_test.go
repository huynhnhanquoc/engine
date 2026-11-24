//go:build race

package file

// underRace is true when the tests run with the race detector, which slows each memory access: a test that times
// a walk gives it more time then.
const underRace = true
