//go:build !race

package repository

// raceDetectorEnabled is true when the tests run under -race, which slows
// every statement several times over; timing budgets scale with it.
const raceDetectorEnabled = false
