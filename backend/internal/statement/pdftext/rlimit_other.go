//go:build !linux

package pdftext

// limitMemory does nothing off Linux: macOS answers EINVAL to RLIMIT_DATA
// (GLG-591) and Windows has no rlimit. Those are dev machines, where the
// decoded-size cap, the timeout and the worker slots still hold.
func limitMemory() error { return nil }
