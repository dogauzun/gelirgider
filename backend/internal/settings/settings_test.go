package settings_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/settings"
)

// TestRoundTrip covers plain and secret round trips, that the stored secret
// is not the plaintext, delete by empty secret, and a wrong key failing.
func TestRoundTrip(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()
	ctx := context.Background()
	repo := settings.NewRepo(conn, bytes.Repeat([]byte{1}, 32))

	if _, ok, err := repo.Get(ctx, "registration"); err != nil || ok {
		t.Fatalf("Get missing = %v, %v; want false, nil", ok, err)
	}
	if err := repo.Set(ctx, "registration", "open"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := repo.Set(ctx, "registration", "closed"); err != nil {
		t.Fatalf("Set overwrite: %v", err)
	}
	if v, ok, err := repo.Get(ctx, "registration"); err != nil || !ok || v != "closed" {
		t.Fatalf("Get = %q, %v, %v; want closed", v, ok, err)
	}

	const secret = "finnhub-key-1234"
	if err := repo.SetSecret(ctx, "finnhub_api_key", secret); err != nil {
		t.Fatalf("SetSecret: %v", err)
	}
	if v, ok, err := repo.GetSecret(ctx, "finnhub_api_key"); err != nil || !ok || v != secret {
		t.Fatalf("GetSecret = %q, %v, %v; want %q", v, ok, err, secret)
	}
	stored, _, err := repo.Get(ctx, "finnhub_api_key")
	if err != nil || stored == secret || strings.Contains(stored, secret) {
		t.Fatalf("stored value %q (err %v) leaks the plaintext", stored, err)
	}

	other := settings.NewRepo(conn, bytes.Repeat([]byte{2}, 32))
	if _, _, err := other.GetSecret(ctx, "finnhub_api_key"); err == nil || !strings.Contains(err.Error(), "finnhub_api_key") {
		t.Fatalf("GetSecret with wrong key err = %v, want error naming the key", err)
	}

	if err := repo.SetSecret(ctx, "finnhub_api_key", ""); err != nil {
		t.Fatalf("SetSecret empty: %v", err)
	}
	if _, ok, err := repo.GetSecret(ctx, "finnhub_api_key"); err != nil || ok {
		t.Fatalf("GetSecret after delete = %v, %v; want false, nil", ok, err)
	}
}

// TestSealOpen checks that a sealed value opens only under the AAD it was
// sealed with.
func TestSealOpen(t *testing.T) {
	repo := settings.NewRepo(nil, bytes.Repeat([]byte{1}, 32))
	sealed, err := repo.Seal("totp:1", "JBSWY3DPEHPK3PXP")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if v, err := repo.Open("totp:1", sealed); err != nil || v != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("Open = %q, %v; want the plaintext", v, err)
	}
	if _, err := repo.Open("totp:2", sealed); !errors.Is(err, settings.ErrUndecryptable) {
		t.Fatalf("Open with other aad err = %v, want ErrUndecryptable", err)
	}
}

// TestSetConcurrentNewKey checks that concurrent Sets of a key that does
// not exist yet all succeed. Before the fix two Sets could both miss on
// UPDATE and both INSERT, and the second failed on the primary key.
func TestSetConcurrentNewKey(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()
	ctx := context.Background()
	repo := settings.NewRepo(conn, bytes.Repeat([]byte{1}, 32))

	for i := range 28 {
		key := fmt.Sprintf("race.%d", i)
		var wg sync.WaitGroup
		errs := make(chan error, 8)
		start := make(chan struct{})
		for range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				errs <- repo.Set(ctx, key, "true")
			}()
		}
		close(start)
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatalf("Set %s: %v", key, err)
			}
		}
	}
}
