package auth

import (
	"net/url"
	"testing"
	"time"
)

// rfcKey is the RFC 6238 Appendix B SHA1 secret, "12345678901234567890".
var rfcKey = []byte("12345678901234567890")

// TestTOTPCodeRFC6238 checks the Appendix B SHA1 vectors. The RFC lists 8
// digits; the 6-digit code is their last 6.
func TestTOTPCodeRFC6238(t *testing.T) {
	for _, tc := range []struct {
		unix int64
		want string
	}{
		{59, "287082"},
		{1111111109, "081804"},
		{1111111111, "050471"},
		{1234567890, "005924"},
		{2000000000, "279037"},
		{20000000000, "353130"},
	} {
		if got := totpCode(rfcKey, tc.unix/totpStepSeconds); got != tc.want {
			t.Errorf("code at %d = %s, want %s", tc.unix, got, tc.want)
		}
	}
}

func TestVerifyTOTPWindowAndReplay(t *testing.T) {
	secret := totpEncoding.EncodeToString(rfcKey)
	now := time.Unix(1111111109, 0)
	cur := now.Unix() / totpStepSeconds

	for _, off := range []int64{-1, 0, 1} {
		step, ok := verifyTOTP(secret, totpCode(rfcKey, cur+off), now, 0)
		if !ok || step != cur+off {
			t.Errorf("step %+d: got (%d, %v), want (%d, true)", off, step, ok, cur+off)
		}
	}
	for _, off := range []int64{-2, 2} {
		if _, ok := verifyTOTP(secret, totpCode(rfcKey, cur+off), now, 0); ok {
			t.Errorf("step %+d accepted, want rejected", off)
		}
	}
	// A step at or below the last accepted one is a replay.
	if _, ok := verifyTOTP(secret, totpCode(rfcKey, cur), now, cur); ok {
		t.Error("replayed step accepted")
	}
	if _, ok := verifyTOTP(secret, "12345", now, 0); ok {
		t.Error("short code accepted")
	}
}

func TestTOTPURI(t *testing.T) {
	u, err := url.Parse(totpURI("ABCDEFGH", "alice@example.com"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if u.Scheme != "otpauth" || u.Host != "totp" || u.Path != "/gelirgider:alice@example.com" {
		t.Errorf("uri = %s", u)
	}
	if q := u.Query(); q.Get("secret") != "ABCDEFGH" || q.Get("issuer") != "gelirgider" {
		t.Errorf("query = %v", q)
	}
}
