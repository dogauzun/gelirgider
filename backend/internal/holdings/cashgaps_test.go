package holdings_test

import (
	"fmt"
	"testing"

	"github.com/dogauzun/gelirgider/backend/internal/holdings"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
)

// TestCashGaps walks the cash per currency and emits, on each day it
// reaches a new low below zero, the extra shortfall (GLG-1109).
func TestCashGaps(t *testing.T) {
	var a int64 = 1
	tx := func(id int64, typ, date, amount, fee, currency string) transaction.Transaction {
		r := transaction.Transaction{ID: id, Type: typ, Date: date, Amount: d(amount), Fee: d(fee), Currency: currency}
		if typ == transaction.TypeBuy || typ == transaction.TypeSell {
			r.InstrumentID, r.Quantity, r.Price = &a, d("1"), d(amount)
		}
		return r
	}
	const (
		buy, sell, deposit, withdrawal, fee = transaction.TypeBuy, transaction.TypeSell, transaction.TypeDeposit, transaction.TypeWithdrawal, transaction.TypeFee
	)
	tests := []struct {
		name string
		txs  []transaction.Transaction
		want string
	}{
		{"no gaps", []transaction.Transaction{
			tx(1, deposit, "2024-01-01", "1000", "0", "TRY"),
			tx(2, buy, "2024-01-02", "1000", "0", "TRY"),
		}, "[]"},
		{"one dip", []transaction.Transaction{
			tx(1, deposit, "2024-01-01", "100", "0", "TRY"),
			tx(2, buy, "2024-01-02", "300", "0", "TRY"),
		}, "[2024-01-02 TRY 200]"},
		{"two dips", []transaction.Transaction{
			tx(1, buy, "2024-01-01", "100", "0", "TRY"),
			tx(2, buy, "2024-01-03", "50", "0", "TRY"),
		}, "[2024-01-01 TRY 100 2024-01-03 TRY 50]"},
		{"a dip, a recovery, then a deeper dip", []transaction.Transaction{
			tx(1, buy, "2024-01-01", "100", "0", "TRY"),
			tx(2, sell, "2024-01-02", "100", "0", "TRY"),
			tx(3, buy, "2024-01-03", "150", "0", "TRY"),
		}, "[2024-01-01 TRY 100 2024-01-03 TRY 50]"},
		{"a dip, a recovery, then a shallower dip", []transaction.Transaction{
			tx(1, buy, "2024-01-01", "100", "0", "TRY"),
			tx(2, deposit, "2024-01-02", "100", "0", "TRY"),
			tx(3, withdrawal, "2024-01-03", "60", "0", "TRY"),
		}, "[2024-01-01 TRY 100]"},
		{"two currencies", []transaction.Transaction{
			tx(1, buy, "2024-01-01", "10", "0", "USD"),
			tx(2, buy, "2024-01-01", "300", "0", "TRY"),
			tx(3, deposit, "2024-01-02", "500", "0", "TRY"),
			tx(4, buy, "2024-01-03", "5", "0", "USD"),
		}, "[2024-01-01 TRY 300 2024-01-01 USD 10 2024-01-03 USD 5]"},
		{"a fee and a sell on the dip day count", []transaction.Transaction{
			tx(1, buy, "2024-01-01", "100", "0", "TRY"),
			tx(2, buy, "2024-01-02", "200", "2", "TRY"),
			tx(3, fee, "2024-01-02", "3", "0", "TRY"),
			tx(4, sell, "2024-01-02", "50", "1", "TRY"),
		}, "[2024-01-01 TRY 100 2024-01-02 TRY 156]"},
		// A deposit takes at most 9 fraction digits, a trade amount more
		// (GLG-476): the gap rounds up so the deposit still covers it, and
		// the next gap counts only what that deposit left uncovered.
		{"more than 9 fraction digits round up", []transaction.Transaction{
			tx(1, buy, "2024-01-01", "16.6666666665", "0", "TRY"),
			tx(2, buy, "2024-01-02", "1.0000000005", "0", "TRY"),
		}, "[2024-01-01 TRY 16.666666667 2024-01-02 TRY 1]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := "["
			for i, g := range holdings.CashGaps(tt.txs) {
				if i > 0 {
					got += " "
				}
				got += fmt.Sprintf("%s %s %s", g.Date, g.Currency, g.Amount)
			}
			if got += "]"; got != tt.want {
				t.Errorf("CashGaps = %s, want %s", got, tt.want)
			}
		})
	}
}
