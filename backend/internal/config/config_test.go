package config

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func keyOf(c string) string {
	return base64.StdEncoding.EncodeToString([]byte(strings.Repeat(c, 32)))
}

func keyFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "secret_key")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSecretKeyFromFile(t *testing.T) {
	t.Setenv("SECRET_KEY", "")
	t.Setenv("SECRET_KEY_FILE", keyFile(t, keyOf("a")+"\n"))
	cfg := Load()
	if len(cfg.SecretKey) != 32 || cfg.SecretKeyErr != nil {
		t.Fatalf("SecretKey len %d, err %v, want 32 and nil", len(cfg.SecretKey), cfg.SecretKeyErr)
	}
}

func TestSecretKeyEnvWinsOverFile(t *testing.T) {
	t.Setenv("SECRET_KEY", keyOf("a"))
	t.Setenv("SECRET_KEY_FILE", keyFile(t, keyOf("b")))
	cfg := Load()
	if string(cfg.SecretKey) != strings.Repeat("a", 32) {
		t.Fatalf("SecretKey = %q, want the env var's key", cfg.SecretKey)
	}
}

func TestSecretKeyUnreadableFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing")
	t.Setenv("SECRET_KEY", "")
	t.Setenv("SECRET_KEY_FILE", path)
	cfg := Load()
	if cfg.SecretKey != nil {
		t.Fatalf("SecretKey = %q, want nil", cfg.SecretKey)
	}
	if cfg.SecretKeyErr == nil || !strings.Contains(cfg.SecretKeyErr.Error(), "SECRET_KEY_FILE") || !strings.Contains(cfg.SecretKeyErr.Error(), path) {
		t.Fatalf("SecretKeyErr = %v, want it to name SECRET_KEY_FILE and %s", cfg.SecretKeyErr, path)
	}
}
