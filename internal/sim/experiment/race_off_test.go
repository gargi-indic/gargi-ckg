//go:build !race

package experiment

// raceEnabled reports whether the race detector is on; heavy tests shrink.
const raceEnabled = false
