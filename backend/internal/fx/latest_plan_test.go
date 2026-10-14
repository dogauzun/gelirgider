package fx

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dogauzun/gelirgider/backend/internal/db"
)

// TestLatestQuerySeeksPerCurrency checks that the Latest lookup is
// primary-key seeks per requested currency, not a correlated subquery run
// once per stored row, so its cost does not grow with the length of rate
// history. One statement serves every currency: the plan walks
// the VALUES list, seeks the row at each currency's newest date, and seeks
// that date in the subquery. Same check as price.TestLatestQueriesSeekPerKey.
func TestLatestQuerySeeksPerCurrency(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	rows, err := conn.Query("EXPLAIN QUERY PLAN "+fmt.Sprintf(latestQuery, "(?),(?)"), "USD", "EUR", "2026-09-28")
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
	joined := strings.Join(plan, "; ")
	if strings.Contains(joined, "SCAN f") || strings.Contains(joined, "SCAN q") || strings.Contains(joined, "TEMP B-TREE") ||
		!strings.Contains(joined, "SEARCH f USING INDEX sqlite_autoindex_fx_rates_1 (currency=? AND date=?)") ||
		!strings.Contains(joined, "SEARCH q USING COVERING INDEX sqlite_autoindex_fx_rates_1 (currency=? AND date<?)") {
		t.Errorf("plan = %q, want a seek per currency: the row at its newest date, the date subquery", joined)
	}
}
