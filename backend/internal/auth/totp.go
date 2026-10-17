package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"time"
)

const (
	totpStepSeconds = 30
	totpDigits      = 6
	totpIssuer      = "gelirgider"
)

// totpEncoding is the unpadded base32 alphabet authenticator apps expect.
var totpEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// newTOTPSecret returns a fresh base32 secret of 20 random bytes.
func newTOTPSecret() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("totp secret: %w", err)
	}
	return totpEncoding.EncodeToString(b), nil
}

// totpCode returns the 6-digit code of key at the 30-second step (RFC 4226
// dynamic truncation of HMAC-SHA1 over the big-endian step).
func totpCode(key []byte, step int64) string {
	mac := hmac.New(sha1.New, key)
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	n := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%0*d", totpDigits, n%1_000_000)
}

// verifyTOTP checks code against the base32 secret at the steps around now
// (one step either side) and returns the matched step. A step at or below
// lastStep is a replay and never matches. All three steps are compared with
// no early exit, in constant time.
func verifyTOTP(secret, code string, now time.Time, lastStep int64) (step int64, ok bool) {
	key, err := totpEncoding.DecodeString(secret)
	if err != nil {
		return 0, false
	}
	current := now.Unix() / totpStepSeconds
	for s := current - 1; s <= current+1; s++ {
		match := hmac.Equal([]byte(totpCode(key, s)), []byte(code))
		if match && s > lastStep {
			step, ok = s, true
		}
	}
	return step, ok
}

// totpURI is the otpauth:// URI an authenticator app imports, with the
// account's email as the label.
func totpURI(secret, email string) string {
	u := url.URL{
		Scheme:   "otpauth",
		Host:     "totp",
		Path:     "/" + totpIssuer + ":" + email,
		RawQuery: url.Values{"secret": {secret}, "issuer": {totpIssuer}}.Encode(),
	}
	return u.String()
}

// totpAAD is the additional data a user's TOTP secrets are sealed under.
func totpAAD(userID int64) string {
	return fmt.Sprintf("totp:%d", userID)
}
