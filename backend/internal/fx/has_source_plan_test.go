package fx

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/dogauzun/gelirgider/backend/internal/db"
)

// TestHasSourceQuerySeeks guards GLG-692: the TCMB backfill walk runs HasSource
// once per weekday, so neither EXISTS arm may scan its table. The plan is
// asserted rather than a timing budget because it is stable across
// machines and needs no seeded rows.
func TestHasSourceQuerySeeks(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	rows, err := conn.Query("EXPLAIN QUERY PLAN "+hasSourceQuery, "2026-09-28", "tcmb", "2026-09-28", "tcmb")
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatalf("scan plan: %v", err)
		}
		plan = append(plan, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("plan rows: %v", err)
	}
	joined := strings.Join(plan, "; ")
	for _, want := range []string{"SEARCH fx_rates USING", "SEARCH empty_fetches USING"} {
		if !strings.Contains(joined, want) {
			t.Errorf("plan = %q, want %q", joined, want)
		}
	}
}
