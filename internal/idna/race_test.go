//go:build race

package idna

// raceEnabled reports a race-detector build, whose instrumentation slows the
// large-input scans far past their uninstrumented time bound.
const raceEnabled = true
