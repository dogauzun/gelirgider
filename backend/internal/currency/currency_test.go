package currency

import "testing"

func TestIsValidAndCheck(t *testing.T) {
	if !IsValid("USD") || Check("USD") != nil {
		t.Error("USD must be valid")
	}
	if IsValid("GBP") {
		t.Error("GBP must be invalid")
	}
	if err := Check("GBP"); err == nil || err.Error() != "currency: must be one of TRY, USD, EUR" {
		t.Errorf("Check(GBP) = %v", err)
	}
}
