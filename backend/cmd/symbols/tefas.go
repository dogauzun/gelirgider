// -market tefas downloads the TEFAS mutual fund list (fonTipi=YAT) from
// tefas.gov.tr's JSON API and writes internal/instrument/tefas_symbols.csv.
// Source, payload, and the robots.txt decision are documented in
// docs/adr/0004-tefas-data-source.md. The endpoint is under /api/, which
// tefas.gov.tr's robots.txt disallows: the server fetches TEFAS only when
// an admin turns the tefas source on (GLG-1114), and running this by hand
// is the developer's own opt-in.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/dogauzun/gelirgider/backend/internal/httpbody"
)

const (
	fundListURL = "https://www.tefas.gov.tr/api/funds/fonGetiriBazliBilgiGetir"
	tefasPath   = "internal/instrument/tefas_symbols.csv"

	// fundListPayload must be sent exactly as is: dropping the donemGetiri*
	// flags makes the endpoint answer 200 with resultList: null (ADR 0004).
	fundListPayload = `{"dil":"TR","fonTipi":"YAT","islem":1,"calismaTipi":2,` +
		`"kurucuKodu":null,"sfonTurKod":null,"fonTurAciklama":null,"fonTurKod":null,"fonGrubu":null,` +
		`"donemGetiri1a":"1","donemGetiri3a":"1","donemGetiri6a":"1","donemGetiri1y":"1",` +
		`"donemGetiriyb":"1","donemGetiri3y":"1","donemGetiri5y":"1",` +
		`"basTarih":null,"bitTarih":null,"getiriOrani":"1"}`
)

func runTEFAS() error {
	funds, err := fetchFunds()
	if err != nil {
		return fmt.Errorf("fonGetiriBazliBilgiGetir: %w", err)
	}

	byNameSymbol := make(map[string]string, len(funds))
	for _, f := range funds {
		if !f.Active {
			continue
		}
		symbol := strings.TrimSpace(f.Code)
		if symbol == "" {
			continue
		}
		byNameSymbol[symbol] = strings.TrimSpace(f.Name)
	}
	return writeCSV(tefasPath, byNameSymbol)
}

// fund is one entry of the fund list response; period returns are ignored.
type fund struct {
	Code   string `json:"fonKodu"`
	Name   string `json:"fonUnvan"`
	Active bool   `json:"tefasDurum"`
}

// fetchFunds POSTs the fund list payload and returns every fund in
// resultList. A non-200 status or a null resultList (the API's error
// shape, returned with 200) is an error naming the HTTP status.
func fetchFunds() ([]fund, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, fundListURL, strings.NewReader(fundListPayload))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("User-Agent", userAgent)

	respBody, err := httpbody.Fetch(nil, req, maxBodyBytes)
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}

	var body struct {
		ResultList []fund `json:"resultList"`
	}
	if err := json.Unmarshal(respBody, &body); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if body.ResultList == nil {
		return nil, fmt.Errorf("status 200 but resultList is null (payload rejected?)")
	}
	return body.ResultList, nil
}
