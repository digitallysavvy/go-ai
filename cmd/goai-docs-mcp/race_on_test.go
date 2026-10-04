//go:build race

package main

// raceEnabled reports whether tests run under the race detector, which slows
// code several times over and makes wall-clock thresholds meaningless.
const raceEnabled = true
