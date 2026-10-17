package statement

import (
	"bytes"
	"encoding/csv"
	"io"
	"unicode/utf8"
)

// cp1254High is Windows-1254's 0x80–0x9F, U+FFFD where it defines none.
// From 0xA0 on it is Latin-1 but for six letters, see fromWindows1254.
var cp1254High = []rune("€\uFFFD‚ƒ„…†‡ˆ‰Š‹Œ\uFFFD\uFFFD\uFFFD\uFFFD‘’“”•–—˜™š›œ\uFFFD\uFFFDŸ")

// fromWindows1254 decodes data, which Excel saves in a Turkish locale as
// plain "CSV", from Windows-1254 to UTF-8.
func fromWindows1254(data []byte) []byte {
	out := make([]byte, 0, len(data)+len(data)/4)
	for _, b := range data {
		r := rune(b)
		switch {
		case b >= 0x80 && b < 0xa0:
			r = cp1254High[b-0x80]
		case b == 0xd0:
			r = 'Ğ'
		case b == 0xdd:
			r = 'İ'
		case b == 0xde:
			r = 'Ş'
		case b == 0xf0:
			r = 'ğ'
		case b == 0xfd:
			r = 'ı'
		case b == 0xfe:
			r = 'ş'
		}
		out = utf8.AppendRune(out, r)
	}
	return out
}

// csvRecords splits an uploaded CSV into records. A spreadsheet saved in a
// Turkish locale separates with ";" because "," is its decimal point, so the
// separator is ";" when the first line has one and no ",". Rows may differ in
// length and a stray quote is read as text: the parser reports a bad row, the
// reader does not refuse the file over it. It reads record by record and stops
// past the header and maxStatementRows records with errTooManyRows. A file that
// is not UTF-8 is read as Windows-1254.
func csvRecords(data []byte) ([][]string, error) {
	data = bytes.TrimPrefix(data, utf8BOM)
	if !utf8.Valid(data) {
		data = fromWindows1254(data)
	}
	r := csv.NewReader(bytes.NewReader(data))
	first, _, _ := bytes.Cut(data, []byte("\n"))
	if bytes.ContainsRune(first, ';') && !bytes.ContainsRune(first, ',') {
		r.Comma = ';'
	}
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	var records [][]string
	for {
		record, err := r.Read()
		if err == io.EOF {
			return records, nil
		}
		if err != nil {
			return nil, err
		}
		if len(records) > maxStatementRows {
			return nil, errTooManyRows
		}
		records = append(records, record)
	}
}
