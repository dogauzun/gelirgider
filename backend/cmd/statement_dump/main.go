// Command statement_dump prints the text rows a broker statement PDF gives
// pdftext.Rows, one line per row as "<row> | cell|cell|cell". It is how a
// developer inspects a broker's table layout before writing a parser, and the
// only way to run against a real statement: real statements never enter the
// repo because they carry the customer's name, TCKN, address and IBAN.
//
// Run from backend/:
//
//	go run ./cmd/statement_dump -file statement.pdf
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/dogauzun/gelirgider/backend/internal/statement/pdftext"
)

func main() {
	path := flag.String("file", "", "path to the statement PDF")
	flag.Parse()
	if *path == "" {
		flag.Usage()
		os.Exit(2)
	}

	file, err := os.Open(*path)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		log.Fatal(err)
	}

	rows, err := pdftext.Rows(context.Background(), file, info.Size())
	if err != nil {
		log.Fatal(err)
	}
	for i, row := range rows {
		fmt.Printf("%d | %s\n", i+1, strings.Join(row, "|"))
	}
}
