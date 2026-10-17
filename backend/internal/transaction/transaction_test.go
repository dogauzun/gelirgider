package transaction

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/price"
	"github.com/shopspring/decimal"
)

// TestListReadsNullNoteAsEmpty checks that a NULL transactions.note reads
// as an empty note. The column is nullable and rows written by an import or a
// hand-run SQL fix can leave it NULL; the scan used to fail on them, which
// turned summary, history and the transaction list into a 500 for the whole
// portfolio.
func TestListReadsNullNoteAsEmpty(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	for _, q := range []string{
		`INSERT INTO users (id, email, password_hash, created_at)
		 VALUES (1, 'a@example.com', 'x', '2026-01-01T00:00:00Z')`,
		`INSERT INTO portfolios (id, user_id, name, created_at)
		 VALUES (1, 1, 'Main', '2026-01-01T00:00:00Z')`,
		// note omitted on purpose: it lands as NULL.
		`INSERT INTO transactions (user_id, portfolio_id, type, date, amount, fee, currency, created_at)
		 VALUES (1, 1, 'deposit', '2026-01-02', '100', '0', 'TRY', '2026-01-02T00:00:00Z')`,
	} {
		if _, err := conn.Exec(q); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	txs, err := NewRepo(conn).List(context.Background(), 1, 1)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(txs) != 1 {
		t.Fatalf("got %d transactions, want 1", len(txs))
	}
	if txs[0].Note != "" {
		t.Errorf("note = %q, want empty", txs[0].Note)
	}
}

// TestListPageFiltersByType checks that the type filter runs in SQL,
// before the cursor and the limit, so a filtered walk pages over the
// filtered rows and nothing else.
func TestListPageFiltersByType(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	for _, q := range []string{
		`INSERT INTO users (id, email, password_hash, created_at)
		 VALUES (1, 'a@example.com', 'x', '2026-01-01T00:00:00Z')`,
		`INSERT INTO portfolios (id, user_id, name, created_at)
		 VALUES (1, 1, 'Main', '2026-01-01T00:00:00Z')`,
		`INSERT INTO transactions (id, user_id, portfolio_id, type, date, amount, fee, currency, note, created_at)
		 VALUES (1, 1, 1, 'deposit', '2026-01-02', '100', '0', 'TRY', '', '2026-01-02T00:00:00Z')`,
		`INSERT INTO transactions (id, user_id, portfolio_id, type, date, amount, fee, currency, note, created_at)
		 VALUES (2, 1, 1, 'buy', '2026-01-03', '50', '0', 'TRY', '', '2026-01-03T00:00:00Z')`,
		`INSERT INTO transactions (id, user_id, portfolio_id, type, date, amount, fee, currency, note, created_at)
		 VALUES (3, 1, 1, 'sell', '2026-01-04', '24', '0', 'TRY', '', '2026-01-04T00:00:00Z')`,
		`INSERT INTO transactions (id, user_id, portfolio_id, type, date, amount, fee, currency, note, created_at)
		 VALUES (4, 1, 1, 'buy', '2026-01-05', '14', '0', 'TRY', '', '2026-01-05T00:00:00Z')`,
	} {
		if _, err := conn.Exec(q); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	repo := NewRepo(conn)
	ctx := context.Background()

	txs, err := repo.ListPage(ctx, 1, 1, 10, Cursor{}, Filter{Types: []string{TypeSell}})
	if err != nil {
		t.Fatalf("ListPage: %v", err)
	}
	if len(txs) != 1 || txs[0].ID != 3 {
		t.Fatalf("sell filter returned ids %v, want [3]", transactionIDs(txs))
	}

	// First page of the buys, newest first: id 4 alone.
	page1, err := repo.ListPage(ctx, 1, 1, 1, Cursor{}, Filter{Types: []string{TypeBuy}})
	if err != nil {
		t.Fatalf("ListPage page 1: %v", err)
	}
	if len(page1) != 1 || page1[0].ID != 4 {
		t.Fatalf("page 1 = ids %v, want [4]", transactionIDs(page1))
	}

	// Following its cursor must skip the sell in between and land on id 2.
	page2, err := repo.ListPage(ctx, 1, 1, 1, Cursor{Date: page1[0].Date, ID: page1[0].ID}, Filter{Types: []string{TypeBuy}})
	if err != nil {
		t.Fatalf("ListPage page 2: %v", err)
	}
	if len(page2) != 1 || page2[0].ID != 2 {
		t.Fatalf("page 2 = ids %v, want [2]", transactionIDs(page2))
	}
}

// TestListPageFiltersByInstrument checks that the instrument filter runs
// in SQL, before the cursor and the limit, so a filtered walk pages over that
// instrument's rows and nothing else.
func TestListPageFiltersByInstrument(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	for _, q := range []string{
		`INSERT INTO users (id, email, password_hash, created_at)
		 VALUES (1, 'a@example.com', 'x', '2026-01-01T00:00:00Z')`,
		`INSERT INTO portfolios (id, user_id, name, created_at)
		 VALUES (1, 1, 'Main', '2026-01-01T00:00:00Z')`,
		`INSERT INTO instruments (id, symbol, market, name, currency)
		 VALUES (1, 'THYAO', 'BIST', 'Turk Hava Yollari', 'TRY')`,
		`INSERT INTO instruments (id, symbol, market, name, currency)
		 VALUES (2, 'ASELS', 'BIST', 'Aselsan', 'TRY')`,
		`INSERT INTO transactions (id, user_id, portfolio_id, instrument_id, type, date, quantity, price, amount, fee, currency, note, created_at)
		 VALUES (1, 1, 1, 1, 'buy', '2026-01-02', '1', '10', '10', '0', 'TRY', '', '2026-01-02T00:00:00Z')`,
		`INSERT INTO transactions (id, user_id, portfolio_id, instrument_id, type, date, quantity, price, amount, fee, currency, note, created_at)
		 VALUES (2, 1, 1, 2, 'buy', '2026-01-03', '1', '20', '20', '0', 'TRY', '', '2026-01-03T00:00:00Z')`,
		`INSERT INTO transactions (id, user_id, portfolio_id, type, date, amount, fee, currency, note, created_at)
		 VALUES (3, 1, 1, 'deposit', '2026-01-03', '100', '0', 'TRY', '', '2026-01-03T00:00:00Z')`,
		`INSERT INTO transactions (id, user_id, portfolio_id, instrument_id, type, date, amount, fee, currency, note, created_at)
		 VALUES (4, 1, 1, 1, 'dividend', '2026-01-04', '5', '0', 'TRY', '', '2026-01-04T00:00:00Z')`,
	} {
		if _, err := conn.Exec(q); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	repo := NewRepo(conn)
	ctx := context.Background()

	// First page of THYAO, newest first: id 4 alone.
	page1, err := repo.ListPage(ctx, 1, 1, 1, Cursor{}, Filter{InstrumentID: 1})
	if err != nil {
		t.Fatalf("ListPage page 1: %v", err)
	}
	if len(page1) != 1 || page1[0].ID != 4 {
		t.Fatalf("page 1 = ids %v, want [4]", transactionIDs(page1))
	}

	// Following its cursor must skip the ASELS buy and the deposit and land
	// on id 1, the last THYAO row.
	page2, err := repo.ListPage(ctx, 1, 1, 10, Cursor{Date: page1[0].Date, ID: page1[0].ID}, Filter{InstrumentID: 1})
	if err != nil {
		t.Fatalf("ListPage page 2: %v", err)
	}
	if len(page2) != 1 || page2[0].ID != 1 {
		t.Fatalf("page 2 = ids %v, want [1]", transactionIDs(page2))
	}
}

// TestImportIDSurvivesUpdate checks that a transaction created by a
// statement import stays attached to that import when it is edited, and the
// id is read back by List.
func TestImportIDSurvivesUpdate(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	for _, q := range []string{
		`INSERT INTO users (id, email, password_hash, created_at)
		 VALUES (1, 'a@example.com', 'x', '2026-01-01T00:00:00Z')`,
		`INSERT INTO portfolios (id, user_id, name, created_at)
		 VALUES (1, 1, 'Main', '2026-01-01T00:00:00Z')`,
		`INSERT INTO statement_imports (id, user_id, portfolio_id, broker, period_start, period_end, file_sha256, created_at)
		 VALUES (7, 1, 1, 'midas', '2026-01-01', '2026-01-31', 'abc', '2026-02-01T00:00:00Z')`,
	} {
		if _, err := conn.Exec(q); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	repo := NewRepo(conn)
	ctx := context.Background()
	importID := int64(7)
	created, err := repo.Create(ctx, 1, Transaction{
		PortfolioID: 1,
		Type:        TypeDeposit,
		Date:        "2026-01-02",
		Amount:      decimal.NewFromInt(100),
		Currency:    "TRY",
		ImportID:    &importID,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated, err := repo.Update(ctx, 1, created.ID, Transaction{
		Type:     TypeDeposit,
		Date:     "2026-01-03",
		Amount:   decimal.NewFromInt(200),
		Currency: "TRY",
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.ImportID == nil || *updated.ImportID != importID {
		t.Fatalf("import id after update = %v, want %d", updated.ImportID, importID)
	}

	txs, err := repo.List(ctx, 1, 1)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(txs) != 1 || txs[0].ImportID == nil || *txs[0].ImportID != importID {
		t.Fatalf("List returned %+v, want one transaction with import id %d", txs, importID)
	}
}

// TestDeletedIDNotReused checks that a deleted transaction's id is not reused.
// SQLite handed the highest deleted id to the next transaction, so a stale
// row's edit or delete changed the new one. The new id must be above the
// deleted one, and the stale edit and delete must find nothing.
func TestDeletedIDNotReused(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	for _, q := range []string{
		`INSERT INTO users (id, email, password_hash, created_at)
		 VALUES (1, 'a@example.com', 'x', '2026-01-01T00:00:00Z')`,
		`INSERT INTO portfolios (id, user_id, name, created_at)
		 VALUES (1, 1, 'Main', '2026-01-01T00:00:00Z')`,
	} {
		if _, err := conn.Exec(q); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	repo := NewRepo(conn)
	ctx := context.Background()
	deposit := func(date string, amount int64) Transaction {
		return Transaction{PortfolioID: 1, Type: TypeDeposit, Date: date, Amount: decimal.NewFromInt(amount), Currency: "TRY"}
	}
	old, err := repo.Create(ctx, 1, deposit("2026-09-02", 1))
	if err != nil {
		t.Fatalf("Create old: %v", err)
	}
	if err := repo.Delete(ctx, 1, old.ID); err != nil {
		t.Fatalf("Delete old: %v", err)
	}
	next, err := repo.Create(ctx, 1, deposit("2026-09-03", 2))
	if err != nil {
		t.Fatalf("Create next: %v", err)
	}
	if next.ID <= old.ID {
		t.Fatalf("new id = %d, want above deleted id %d", next.ID, old.ID)
	}
	if _, err := repo.Update(ctx, 1, old.ID, deposit("2026-09-04", 3)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stale Update: got %v, want ErrNotFound", err)
	}
	if err := repo.Delete(ctx, 1, old.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stale Delete: got %v, want ErrNotFound", err)
	}
}

// TestCreateAndUpdateReturnInstrument checks that the rows Create and
// Update return carry the instrument's symbol, name and market, the same
// shape List returns, so the POST and PUT responses match the list.
func TestCreateAndUpdateReturnInstrument(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	for _, q := range []string{
		`INSERT INTO users (id, email, password_hash, created_at)
		 VALUES (1, 'a@example.com', 'x', '2026-01-01T00:00:00Z')`,
		`INSERT INTO portfolios (id, user_id, name, created_at)
		 VALUES (1, 1, 'Main', '2026-01-01T00:00:00Z')`,
		`INSERT INTO instruments (id, symbol, market, name, currency)
		 VALUES (1, 'THYAO', 'BIST', 'TURK HAVA YOLLARI', 'TRY')`,
	} {
		if _, err := conn.Exec(q); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	repo := NewRepo(conn)
	ctx := context.Background()
	instrumentID := int64(1)
	buy := Transaction{
		PortfolioID:  1,
		InstrumentID: &instrumentID,
		Type:         TypeBuy,
		Date:         "2026-01-05",
		Quantity:     decimal.NewFromInt(1),
		Price:        decimal.NewFromInt(1),
		Amount:       decimal.NewFromInt(1),
		Currency:     "TRY",
	}
	created, err := repo.Create(ctx, 1, buy)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	updated, err := repo.Update(ctx, 1, created.ID, buy)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	for name, tx := range map[string]Transaction{"Create": created, "Update": updated} {
		if tx.InstrumentSymbol == nil || *tx.InstrumentSymbol != "THYAO" ||
			tx.InstrumentName == nil || *tx.InstrumentName != "TURK HAVA YOLLARI" ||
			tx.InstrumentMarket == nil || *tx.InstrumentMarket != "BIST" {
			t.Errorf("%s returned symbol %v name %v market %v, want THYAO, TURK HAVA YOLLARI, BIST",
				name, tx.InstrumentSymbol, tx.InstrumentName, tx.InstrumentMarket)
		}
	}
}

func transactionIDs(txs []Transaction) []int64 {
	out := make([]int64, len(txs))
	for i, t := range txs {
		out[i] = t.ID
	}
	return out
}

// TestListSkipsFutureSplits checks that a split dated after today is
// not returned by List, so the summary does not apply it to today's
// position; one dated today is.
func TestListSkipsFutureSplits(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	now := time.Now().In(price.Istanbul)
	today := now.Format(time.DateOnly)
	tomorrow := now.AddDate(0, 0, 1).Format(time.DateOnly)

	for _, q := range []string{
		`INSERT INTO users (id, email, password_hash, created_at)
		 VALUES (1, 'a@example.com', 'x', '2026-01-01T00:00:00Z')`,
		`INSERT INTO portfolios (id, user_id, name, created_at)
		 VALUES (1, 1, 'Main', '2026-01-01T00:00:00Z')`,
		`INSERT INTO instruments (id, symbol, market, name, currency)
		 VALUES (1, 'THYAO', 'BIST', 'THY', 'TRY')`,
		`INSERT INTO transactions (user_id, portfolio_id, instrument_id, type, date, quantity, price, amount, fee, currency, note, created_at)
		 VALUES (1, 1, 1, 'buy', '2020-01-02', '50', '10', '500', '0', 'TRY', '', '2020-01-02T00:00:00Z')`,
		`INSERT INTO splits (instrument_id, date, ratio, source) VALUES (1, '` + today + `', '2', 'manual')`,
		`INSERT INTO splits (instrument_id, date, ratio, source) VALUES (1, '` + tomorrow + `', '3', 'manual')`,
	} {
		if _, err := conn.Exec(q); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	txs, err := NewRepo(conn).List(context.Background(), 1, 1)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var splitDates []string
	for _, tx := range txs {
		if tx.Type == TypeSplit {
			splitDates = append(splitDates, tx.Date)
		}
	}
	if len(splitDates) != 1 || splitDates[0] != today {
		t.Fatalf("split dates = %v, want [%s]", splitDates, today)
	}
}

// TestListDropsSplitBookedByZeroPriceBuy checks that a split is left
// out for a portfolio holding a zero-price buy of the instrument within 10
// days of the ex-date, since that buy already books the bonus shares. A
// zero-price buy 30 days out does not count, and another portfolio
// holding the instrument with no such buy still gets the split. List moves
// the booking buy to the ex-date; ListStored keeps its own date.
func TestListDropsSplitBookedByZeroPriceBuy(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	buy := func(portfolioID int, date, price string) string {
		return fmt.Sprintf(`INSERT INTO transactions (user_id, portfolio_id, instrument_id, type, date, quantity, price, amount, fee, currency, note, created_at)
		 VALUES (1, %d, 1, 'buy', '%s', '100', '%s', '0', '0', 'TRY', '', '2024-01-01T00:00:00Z')`, portfolioID, date, price)
	}
	for _, q := range []string{
		`INSERT INTO users (id, email, password_hash, created_at)
		 VALUES (1, 'a@example.com', 'x', '2026-01-01T00:00:00Z')`,
		`INSERT INTO portfolios (id, user_id, name, created_at) VALUES
		 (1, 1, 'Bonus booked', '2026-01-01T00:00:00Z'),
		 (2, 1, 'Plain', '2026-01-01T00:00:00Z'),
		 (3, 1, 'Gift later', '2026-01-01T00:00:00Z')`,
		`INSERT INTO instruments (id, symbol, market, name, currency)
		 VALUES (1, 'THYAO', 'BIST', 'THY', 'TRY')`,
		`INSERT INTO splits (instrument_id, date, ratio, source) VALUES (1, '2024-01-10', '2', 'manual')`,
		buy(1, "2024-01-01", "10"), buy(1, "2024-01-12", "0"),
		buy(2, "2024-01-01", "10"),
		buy(3, "2024-01-01", "10"), buy(3, "2024-02-09", "0"),
	} {
		if _, err := conn.Exec(q); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	repo := NewRepo(conn)
	for _, c := range []struct {
		portfolioID int64
		want        string
	}{
		{1, "200"}, // 100 + bonus 100; with the split as well, 300
		{2, "200"}, // 100 * 2 through the split
		{3, "300"}, // 100 * 2 + a later gift of 100
	} {
		txs, err := repo.List(context.Background(), 1, c.portfolioID)
		if err != nil {
			t.Fatalf("List %d: %v", c.portfolioID, err)
		}
		qty := decimal.Zero
		for _, tx := range txs {
			switch tx.Type {
			case TypeBuy:
				qty = qty.Add(tx.Quantity)
			case TypeSplit:
				qty = qty.Mul(tx.Quantity)
			}
		}
		if qty.String() != c.want {
			t.Errorf("portfolio %d quantity = %s, want %s", c.portfolioID, qty, c.want)
		}
	}

	for _, c := range []struct {
		list func(context.Context, int64, int64) ([]Transaction, error)
		want string
	}{{repo.List, "2024-01-10"}, {func(ctx context.Context, userID, portfolioID int64) ([]Transaction, error) {
		return repo.ListStored(ctx, userID, portfolioID, Filter{})
	}, "2024-01-12"}} {
		txs, err := c.list(context.Background(), 1, 1)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(txs) != 2 || txs[1].Date != c.want {
			t.Errorf("rows = %+v, want the bonus buy last, dated %s", txs, c.want)
		}
	}
}

// TestBookedBonusBuySortsFirstOnExDate checks that the bonus buy
// DropBookedSplits moves to the ex-date applies before a sell of that date
// entered earlier (lower id), as the split it replaces would, so the sell
// realizes against the post-split average cost.
func TestBookedBonusBuySortsFirstOnExDate(t *testing.T) {
	id := int64(1)
	row := func(txID int64, typ, date, qty, price string) Transaction {
		return Transaction{ID: txID, InstrumentID: &id, Type: typ, Date: date,
			Quantity: decimal.RequireFromString(qty), Price: decimal.RequireFromString(price)}
	}
	txs, err := DropBookedSplits([]Transaction{
		row(1, TypeBuy, "2026-07-01", "100", "50"),
		row(0, TypeSplit, "2026-07-10", "2", "0"),
		row(2, TypeSell, "2026-07-10", "80", "30"),
		row(3, TypeBuy, "2026-07-13", "100", "0"),
	})
	if err != nil {
		t.Fatalf("DropBookedSplits: %v", err)
	}
	SortForApply(txs)
	var got []int64
	for _, tx := range txs {
		got = append(got, tx.ID)
	}
	if fmt.Sprint(got) != "[1 3 2]" {
		t.Errorf("apply order = %v, want [1 3 2]: the bonus buy before the ex-date sell", got)
	}
}

// TestDropBookedSplitsMatchesBonusQuantity checks that a zero-price buy books
// a split only when its quantity is what the split would add, held x (ratio
// - 1) give or take a share for the fraction the broker pays in cash: a gift
// of 10 shares next to a 2-for-1 split on 1,000 held keeps the split.
func TestDropBookedSplitsMatchesBonusQuantity(t *testing.T) {
	id := int64(1)
	row := func(typ, date, qty, price string) Transaction {
		return Transaction{InstrumentID: &id, Type: typ, Date: date,
			Quantity: decimal.RequireFromString(qty), Price: decimal.RequireFromString(price)}
	}
	for _, c := range []struct {
		name      string
		buy       Transaction
		ratio     string
		wantSplit bool
	}{
		{"gift of 10 on 1,000 held", row(TypeBuy, "2026-07-06", "10", "0"), "2", true},
		{"bonus before the ex-date", row(TypeBuy, "2026-07-06", "1000", "0"), "2", false},
		{"bonus after the ex-date", row(TypeBuy, "2026-07-13", "1000", "0"), "2", false},
		{"fraction paid in cash", row(TypeBuy, "2026-07-13", "500", "0"), "1.5005", false},
		{"reverse split is never a bonus", row(TypeBuy, "2026-07-13", "10", "0"), "0.1", true},
	} {
		txs, err := DropBookedSplits([]Transaction{
			row(TypeBuy, "2026-07-01", "1000", "50"),
			row(TypeSplit, "2026-07-10", c.ratio, "0"),
			c.buy,
		})
		if err != nil {
			t.Fatalf("%s: DropBookedSplits: %v", c.name, err)
		}
		hasSplit := slices.ContainsFunc(txs, func(tx Transaction) bool { return tx.Type == TypeSplit })
		if hasSplit != c.wantSplit {
			t.Errorf("%s: split kept = %v, want %v", c.name, hasSplit, c.wantSplit)
		}
	}
}

// TestDropBookedSplitsMatchesEachBuyOnItsOwn checks that a genuine bonus books
// the split even when an unrelated zero-price buy sits in the same window: a
// gift of 10 shares 4 days before a 2-for-1 split on 1,000 held, then a bonus
// of 1,010 (the gift is held too) 3 days after. The gift alone keeps the split.
func TestDropBookedSplitsMatchesEachBuyOnItsOwn(t *testing.T) {
	id := int64(1)
	row := func(typ, date, qty, price string) Transaction {
		return Transaction{InstrumentID: &id, Type: typ, Date: date,
			Quantity: decimal.RequireFromString(qty), Price: decimal.RequireFromString(price)}
	}
	gift := row(TypeBuy, "2026-07-06", "10", "0")
	bonus := row(TypeBuy, "2026-07-13", "1010", "0")
	for _, c := range []struct {
		name      string
		buys      []Transaction
		wantSplit bool
	}{
		{"gift and bonus", []Transaction{gift, bonus}, false},
		{"gift alone", []Transaction{gift}, true},
	} {
		txs, err := DropBookedSplits(append([]Transaction{
			row(TypeBuy, "2026-07-01", "1000", "50"),
			row(TypeSplit, "2026-07-10", "2", "0"),
		}, c.buys...))
		if err != nil {
			t.Fatalf("%s: DropBookedSplits: %v", c.name, err)
		}
		hasSplit := slices.ContainsFunc(txs, func(tx Transaction) bool { return tx.Type == TypeSplit })
		if hasSplit != c.wantSplit {
			t.Errorf("%s: split kept = %v, want %v", c.name, hasSplit, c.wantSplit)
		}
	}
}
