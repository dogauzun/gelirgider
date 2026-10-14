package pdftext

import (
	"os"
	"os/exec"
	"syscall"
	"testing"
)

func TestRows_KeepsAStricterHostDataLimit(t *testing.T) {
	// A process can lower its hard limit but never raise it, and the worker
	// inherits its parent's. The test binary runs itself again with a hard
	// limit below workerMemory, as a stricter host or container would set.
	if raceEnabled {
		t.Skip("the race detector does not run under a low RLIMIT_DATA")
	}
	const env = "GELIRGIDER_PDFTEXT_TEST_LOW_LIMIT"
	if os.Getenv(env) == "" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestRows_KeepsAStricterHostDataLimit$")
		cmd.Env = append(os.Environ(), env+"=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("under a lower hard limit: %v\n%s", err, out)
		}
		return
	}
	var limit syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_DATA, &limit); err != nil {
		t.Fatal(err)
	}
	limit.Max = min(limit.Max, workerMemory/2)
	limit.Cur = limit.Max
	if err := syscall.Setrlimit(syscall.RLIMIT_DATA, &limit); err != nil {
		t.Fatal(err)
	}
	TestRows(t)
}
