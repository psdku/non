//go:build race

package meta

// raceEnabled reports whether the race detector is active. It makes the
// instrumented code roughly 25× slower, so timing assertions are skipped.
const raceEnabled = true
