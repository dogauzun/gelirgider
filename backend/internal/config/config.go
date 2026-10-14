// Package config loads server configuration from environment variables.
package config

import (
	"encoding/base64"
	"os"
	"strconv"
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
	// decoded from SECRET_KEY (base64 of 32 random bytes). Nil when unset
	// or not valid base64; main checks the length before serving.
	SecretKey []byte
	// APIDocsDir is the directory holding the generated swagger.json. When
	// set, the server serves Swagger UI at /api/docs/ (dev only, ADR 0013);
	// empty, the default, turns it off.
	APIDocsDir string
}

func Load() Config {
	return Config{
		Host:             os.Getenv("HOST"),
		Port:             getEnv("PORT", "8080"),
		DBPath:           getEnv("DB_PATH", "./gelirgider.db"),
		CookieSecure:     getEnv("COOKIE_SECURE", "false") == "true",
		TrustProxy:       getEnv("TRUST_PROXY", "false") == "true",
		TrustedProxyHops: trustedProxyHops(),
		SecretKey:        secretKey(),
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

func secretKey() []byte {
	k, err := base64.StdEncoding.DecodeString(os.Getenv("SECRET_KEY"))
	if err != nil {
		return nil
	}
	return k
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
