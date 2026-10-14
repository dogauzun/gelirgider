//go:build !race

package pdftext

// raceEnabled is true in a binary built with -race. See race.go.
const raceEnabled = false
