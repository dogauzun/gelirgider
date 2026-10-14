//go:build race

package pdftext

// raceEnabled is true in a binary built with -race. The race detector's
// runtime maps its own heap and shadow memory, which RLIMIT_DATA counts, so
// under workerMemory the worker dies before it reads a byte.
const raceEnabled = true
