//go:build race

package jsrt

// raceEnabled: the race detector slows the engine ~15x, so tests that
// race a memory limit against a time budget skip.
const raceEnabled = true
