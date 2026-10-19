package user_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/user"
)

func TestReportingCurrency(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer conn.Close()
	ctx := context.Background()
	repo := user.NewRepo(conn)
	u, err := repo.Create(ctx, "alice@example.com", "hash")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.UpdateReportingCurrency(ctx, u.ID, "USD"); err != nil {
		t.Fatalf("UpdateReportingCurrency: %v", err)
	}

	got, err := repo.ReportingCurrency(ctx, u.ID)
	if err != nil || got != "USD" {
		t.Fatalf("ReportingCurrency = %q, %v; want USD", got, err)
	}

	_, err = repo.ReportingCurrency(ctx, u.ID+1)
	var missing user.MissingError
	if !errors.As(err, &missing) || missing.ID != u.ID+1 {
		t.Fatalf("unknown id error = %v; want MissingError{%d}", err, u.ID+1)
	}
}
