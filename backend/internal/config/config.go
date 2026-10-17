// Package config loads server configuration from environment variables.
package config

import (
	"encoding/base64"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	// Host is the address the server binds to, from HOST. Empty, the
	// default, binds every interface (Docker needs that); make dev sets
	// 127.0.0.1 so the plain-http dev backend stays off the LAN.
	Host         string
	Port         string
	DBPath       string
	CookieSecure bool
	// TrustProxy indicates the server sits behind a reverse proxy, so the
	// client IP should be read from X-Forwarded-For instead of the raw
	// connection address.
	TrustProxy bool
	// TrustedProxyHops is how many proxies in front of the server append to
	// X-Forwarded-For, from TRUSTED_PROXY_HOPS (default 1). Used only when
	// TrustProxy is set; 0 when the value is not an integer, which main
	// rejects along with any value below 1.
	TrustedProxyHops int
	// SecretKey is the AES-256 key for secrets in the settings table,
	// decoded from SECRET_KEY, or from the file named by SECRET_KEY_FILE
	// (base64 of 32 random bytes). Nil when unset, unreadable or not valid
	// base64; main checks the length before serving.
	SecretKey []byte
	// SecretKeyErr is set when SECRET_KEY_FILE names a file that cannot be
	// read, so main can say so instead of reporting a missing key.
	SecretKeyErr error
	// APIDocsDir is the directory holding the generated swagger.json. When
	// set, the server serves Swagger UI at /api/docs/ (dev only, ADR 0013);
	// empty, the default, turns it off.
	APIDocsDir string
}

func Load() Config {
	key, keyErr := secretKey()
	return Config{
		Host:             os.Getenv("HOST"),
		Port:             getEnv("PORT", "8080"),
		DBPath:           getEnv("DB_PATH", "./gelirgider.db"),
		CookieSecure:     getEnv("COOKIE_SECURE", "false") == "true",
		TrustProxy:       getEnv("TRUST_PROXY", "false") == "true",
		TrustedProxyHops: trustedProxyHops(),
		SecretKey:        key,
		SecretKeyErr:     keyErr,
		APIDocsDir:       os.Getenv("API_DOCS_DIR"),
	}
}

func trustedProxyHops() int {
	n, err := strconv.Atoi(getEnv("TRUSTED_PROXY_HOPS", "1"))
	if err != nil {
		return 0
	}
	return n
}

func secretKey() ([]byte, error) {
	v, err := secretEnv("SECRET_KEY")
	if err != nil {
		return nil, err
	}
	k, err := base64.StdEncoding.DecodeString(v)
	if err != nil {
		return nil, nil
	}
	return k, nil
}

// secretEnv returns the env var key, else the trimmed contents of the file
// named by key+"_FILE" (Docker secrets mount a file), else "". An
// unreadable file is an error that names the variable and the path, never
// the contents.
func secretEnv(key string) (string, error) {
	if v := os.Getenv(key); v != "" {
		return v, nil
	}
	path := os.Getenv(key + "_FILE")
	if path == "" {
		return "", nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("%s_FILE: cannot read %s: %w", key, path, err)
	}
	return strings.TrimSpace(string(b)), nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
