package price

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dogauzun/gelirgider/backend/internal/db"
)

// TestLatestQueriesSeekPerKey checks that each Latest lookup is
// primary-key seeks per requested id, not a correlated subquery run once per
// stored row, so its cost does not grow with the length of price history.
// One statement serves every id: the plan walks the ids, seeks
// each one's newest date in the subquery, then seeks the row at that date.
// The old per-row shape searched the price table by instrument_id alone, so
// the outer price row's search must carry date=?.
// The plan is asserted rather than a timing budget because it is stable
// across machines; with no ANALYZE stats it does not depend on row counts,
// so no seed is needed.
func TestLatestQueriesSeekPerKey(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	for name, q := range map[string]struct {
		query string
		args  []any
	}{
		"prices":        {fmt.Sprintf(latestQuery, "?,?"), []any{1, 2, "2026-09-28"}},
		"manual_prices": {fmt.Sprintf(latestManualQuery, "?,?"), []any{1, 1, 2, "2026-09-28"}},
	} {
		rows, err := conn.Query("EXPLAIN QUERY PLAN "+q.query, q.args...)
		if err != nil {
			t.Fatalf("%s: explain: %v", name, err)
		}
		var plan []string
		for rows.Next() {
			var id, parent, notused int
			var detail string
			if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
				t.Fatalf("%s: scan plan: %v", name, err)
			}
			plan = append(plan, detail)
		}
		rows.Close()
		joined := strings.Join(plan, "; ")
		if len(plan) != 4 || strings.Contains(joined, "SCAN ") || strings.Contains(joined, "TEMP B-TREE") ||
			!strings.HasPrefix(plan[0], "SEARCH i ") || !strings.Contains(plan[1], "date=?)") {
			t.Errorf("%s: plan = %q, want a seek per id: instruments, the row at its newest date, the date subquery", name, joined)
		}
	}
}
