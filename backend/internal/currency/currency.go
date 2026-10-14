// Package currency holds the currency allowlist shared by packages that
// validate a user-supplied currency code (transactions, reporting currency).
// It has no dependencies of its own so both internal/transaction and
// internal/auth can import it without an import cycle.
package currency

// Valid lists the currencies accepted anywhere a user supplies one directly:
// transactions with no instrument to derive a currency from (deposit,
// withdrawal, fee), and a user's reporting currency.
var Valid = []string{"TRY", "USD", "EUR"}

// IsValid reports whether c is one of Valid.
func IsValid(c string) bool {
	for _, v := range Valid {
		if v == c {
			return true
		}
	}
	return false
}
