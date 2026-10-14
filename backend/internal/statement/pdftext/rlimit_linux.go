package pdftext

import (
	"fmt"
	"syscall"
)

// limitMemory caps the worker's heap at workerMemory. RLIMIT_DATA, not
// RLIMIT_AS: the Go runtime reserves about a gigabyte of address space at
// start, but reservations only count once they are used. A process cannot
// raise its hard limit, so on a host that is already stricter the worker
// keeps the host's.
func limitMemory() error {
	var limit syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_DATA, &limit); err != nil {
		return fmt.Errorf("getrlimit: %w", err)
	}
	limit.Max = min(limit.Max, workerMemory)
	limit.Cur = limit.Max
	if err := syscall.Setrlimit(syscall.RLIMIT_DATA, &limit); err != nil {
		return fmt.Errorf("setrlimit: %w", err)
	}
	return nil
}
