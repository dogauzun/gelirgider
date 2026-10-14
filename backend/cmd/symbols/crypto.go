// -market crypto downloads BtcTurk's pair list (api/v2/server/exchangeinfo)
// and writes two CSVs: internal/instrument/crypto_try_symbols.csv for TRY
// pairs (symbol BTC-TRY) and crypto_usd_symbols.csv for USDT pairs (symbol
// BTC-USD, 1 USDT treated as 1 USD). Only TRADING pairs are kept; other
// quote currencies are skipped. Source and decisions are in
// docs/adr/0007-crypto.md.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/dogauzun/gelirgider/backend/internal/httpbody"
)

const exchangeInfoURL = "https://api.btcturk.com/api/v2/server/exchangeinfo"

// outputs maps a BtcTurk denominator to the CSV it goes to and the quote
// suffix used in our symbol.
var outputs = map[string]struct{ path, suffix string }{
	"TRY":  {"internal/instrument/crypto_try_symbols.csv", "-TRY"},
	"USDT": {"internal/instrument/crypto_usd_symbols.csv", "-USD"},
}

type exchangeInfo struct {
	Success bool `json:"success"`
	Data    struct {
		Symbols []struct {
			Numerator   string `json:"numerator"`
			Denominator string `json:"denominator"`
			Status      string `json:"status"`
		} `json:"symbols"`
		Currencies []struct {
			Symbol string `json:"symbol"`
			Name   string `json:"name"`
		} `json:"currencies"`
	} `json:"data"`
}

func runCrypto() error {
	info, err := fetch()
	if err != nil {
		return fmt.Errorf("exchangeinfo: %w", err)
	}

	names := make(map[string]string, len(info.Data.Currencies))
	for _, c := range info.Data.Currencies {
		names[c.Symbol] = strings.TrimSpace(c.Name)
	}

	rows := map[string]map[string]string{} // denominator -> symbol -> name
	for _, s := range info.Data.Symbols {
		out, ok := outputs[s.Denominator]
		if !ok || s.Status != "TRADING" || s.Numerator == "" {
			continue
		}
		name := names[s.Numerator]
		if name == "" {
			name = s.Numerator
		}
		if rows[s.Denominator] == nil {
			rows[s.Denominator] = map[string]string{}
		}
		rows[s.Denominator][s.Numerator+out.suffix] = name
	}

	for denom, out := range outputs {
		if err := writeCSV(out.path, rows[denom]); err != nil {
			return err
		}
	}
	return nil
}

func fetch() (exchangeInfo, error) {
	var info exchangeInfo
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, exchangeInfoURL, nil)
	if err != nil {
		return info, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)

	body, err := httpbody.Fetch(nil, req, maxBodyBytes)
	if err != nil {
		return info, fmt.Errorf("download: %w", err)
	}
	if err := json.Unmarshal(body, &info); err != nil {
		return info, fmt.Errorf("decode: %w", err)
	}
	if !info.Success || len(info.Data.Symbols) == 0 {
		return info, fmt.Errorf("success=%v with %d symbols", info.Success, len(info.Data.Symbols))
	}
	return info, nil
}
