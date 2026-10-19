// Package currency holds the currency allowlist shared by packages that
// validate a user-supplied currency code (transactions, reporting currency).
// It has no dependencies of its own so both internal/transaction and
// internal/auth can import it without an import cycle.
package currency

import (
	"fmt"
	"slices"
	"strings"
)

// Valid lists the currencies accepted anywhere a user supplies one directly:
// transactions with no instrument to derive a currency from (deposit,
// withdrawal, fee), and a user's reporting currency.
var Valid = []string{"TRY", "USD", "EUR"}

// IsValid reports whether c is one of Valid.
func IsValid(c string) bool {
	return slices.Contains(Valid, c)
}

// Check returns the "currency: must be one of ..." validation error for a c
// outside Valid, or nil.
func Check(c string) error {
	if IsValid(c) {
		return nil
	}
	return fmt.Errorf("currency: must be one of %s", strings.Join(Valid, ", "))
}
