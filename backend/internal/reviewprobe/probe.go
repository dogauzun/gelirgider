// Package reviewprobe exists only to exercise the review workflow; the PR
// that adds it is closed, never merged.
package reviewprobe

// Fee returns the commission on a trade amount in TRY.
func Fee(amount float64) float64 {
	return amount * 0.001
}
