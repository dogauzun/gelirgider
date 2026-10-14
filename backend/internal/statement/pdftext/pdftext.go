// Package pdftext extracts a PDF's text as rows of cells. It is the only
// place that knows about the PDF format; broker parsers work on the rows.
package pdftext

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/ledongthuc/pdf"
)

// maxInflated bounds what a file's compressed streams may decode to. Deflate
// packs about 1000:1, and the library inflates with no limit of its own, so
// without this a sub-megabyte upload decodes to gigabytes and the whole server
// dies of OOM (GLG-579). A one-page statement is a 21-24 KB file, so a long
// statement stays far below 8 MB.
const maxInflated = 8 << 20

var errTooLarge = fmt.Errorf("pdftext: compressed data decodes to more than %d MB", maxInflated>>20)

// workerEnv marks the child process Rows runs the extraction in.
const workerEnv = "GELIRGIDER_PDFTEXT_WORKER"

// workerMemory caps the worker's heap. The library sizes some allocations
// from a plain number in the file (an xref stream's /Size, a predictor's
// /Columns, a CMap's entry count), so a few-hundred-byte upload can ask for
// more than the machine has, and a failed allocation is a fatal error no
// recover catches (GLG-583). A statement's extraction needs a few MB.
const workerMemory = 256 << 20

// workerTimeout bounds the worker's run: a huge /Count loops over pages for
// good, and row grouping is quadratic in the rows of a page. A statement
// takes milliseconds. A var so the test can wait less.
var workerTimeout = 10 * time.Second

// workers holds one slot per extraction worker that may run at once. Each
// worker can take a core and workerMemory for workerTimeout, so without a
// bound a burst of crafted uploads still starves the host (GLG-584). An import
// is rare and interactive; two at once is plenty. A var so the test can fill
// it.
var workers = make(chan struct{}, 2)

// Every binary that links this package, the test binaries included, can be
// the worker: Rows starts its own executable with workerEnv set, and this
// runs before main.
func init() {
	if os.Getenv(workerEnv) != "" {
		os.Exit(work())
	}
}

// work extracts the PDF on stdin and writes its rows as JSON to stdout, or
// the error to stderr.
func work() int {
	// A -race build, a test run only, skips the cap: the race detector needs
	// more than it allows.
	if !raceEnabled {
		if err := limitMemory(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	rows, err := extract(data)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := json.NewEncoder(os.Stdout).Encode(rows); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

// Rows returns one []string per text row of the document, top to bottom, page
// after page. Each entry is one piece of text the PDF draws, trimmed; in the
// table-shaped statements this exists for, that is one table cell. Empty cells
// and empty rows are dropped.
//
// The extraction runs in a child process with a memory cap and a timeout, so
// a crafted file that loops or runs out of memory kills only that process.
// At most cap(workers) run at once; Rows waits for a free slot until ctx is
// done, and a ctx done while the worker runs kills it.
func Rows(ctx context.Context, r io.ReaderAt, size int64) ([][]string, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	select {
	case workers <- struct{}{}:
		defer func() { <-workers }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	parent := ctx
	ctx, cancel := context.WithTimeout(ctx, workerTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe)
	cmd.Env = []string{workerEnv + "=1"}
	if raceEnabled {
		// A race build sleeps a second on a clean exit by default, which
		// every import under -race would wait for.
		cmd.Env = append(cmd.Env, "GORACE="+os.Getenv("GORACE")+" atexit_sleep_ms=0")
	}
	cmd.Stdin = io.NewSectionReader(r, 0, size)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if err := parent.Err(); err != nil {
			return nil, err
		}
		if ctx.Err() != nil {
			return nil, fmt.Errorf("pdftext: extraction took longer than %v", workerTimeout)
		}
		msg, _, _ := strings.Cut(strings.TrimSpace(stderr.String()), "\n")
		return nil, fmt.Errorf("pdftext: %s (%v)", msg, err)
	}
	var rows [][]string
	if err := json.Unmarshal(stdout.Bytes(), &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// extract is Rows in the current process.
func extract(data []byte) (rows [][]string, err error) {
	// The library panics on malformed input outside the calls that recover
	// it themselves, chargePage's stream reads included.
	defer func() {
		if x := recover(); x != nil {
			rows, err = nil, fmt.Errorf("pdftext: malformed PDF: %v", x)
		}
	}()

	if err := checkInflated(data); err != nil {
		return nil, err
	}
	reader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	left := int64(maxInflated)
	for i := 1; i <= reader.NumPage(); i++ {
		page := reader.Page(i)
		if err := chargePage(page, &left); err != nil {
			return nil, err
		}
		pageRows, err := page.GetTextByRow()
		if err != nil {
			return nil, fmt.Errorf("page %d: %w", i, err)
		}
		for _, pageRow := range pageRows {
			var cells []string
			for _, text := range pageRow.Content {
				if cell := strings.TrimSpace(text.S); cell != "" {
					cells = append(cells, cell)
				}
			}
			if len(cells) > 0 {
				rows = append(rows, cells)
			}
		}
	}
	return rows, nil
}

// checkInflated fails once the zlib streams in data decode to more than
// maxInflated bytes. It covers the object and cross-reference streams the
// library inflates while it resolves objects, before chargePage can look.
//
// It tries every offset instead of parsing the file: the library reads a
// stream wherever an object, or an object inside an object stream, says it
// starts, and for as long as /Length says, so a parse here could be lied to.
// A stream that decodes to another zlib stream (/Filter [/FlateDecode
// /FlateDecode]) is followed down. Bytes read are charged along with bytes
// written, so a stream of empty blocks costs what it reads.
//
// Raw bytes only: an object stream behind ASCII85, a PNG predictor or
// encryption is not counted here. The worker's memory cap bounds those.
func checkInflated(data []byte) error {
	left := int64(maxInflated)
	src := new(bytes.Reader)
	var zr io.ReadCloser
	for i := range data {
		for layer := data[i:]; isZlib(layer); {
			src.Reset(layer)
			var err error
			if zr == nil {
				zr, err = zlib.NewReader(src)
			} else {
				err = zr.(zlib.Resetter).Reset(src, nil)
			}
			if err != nil {
				break
			}
			out, _ := io.ReadAll(io.LimitReader(zr, left+1))
			left -= int64(len(out)) + src.Size() - int64(src.Len())
			if left < 0 {
				return errTooLarge
			}
			layer = out
		}
	}
	return nil
}

// isZlib reports whether b starts with a plausible zlib header: deflate, with
// a valid check value. zlib.NewReader checks the rest.
func isZlib(b []byte) bool {
	return len(b) >= 2 && b[0]&0x0f == 8 && (uint16(b[0])<<8|uint16(b[1]))%31 == 0
}

// chargePage reads, and charges to left, every stream GetTextByRow interprets
// for p: its content streams and each of its fonts' ToUnicode CMap. They go
// through the library's own decoding, so a filter chain or an encrypted file
// checkInflated cannot see into is counted exactly.
func chargePage(p pdf.Page, left *int64) error {
	contents := p.V.Key("Contents")
	streams := []pdf.Value{contents}
	if contents.Kind() == pdf.Array {
		streams = nil
		for i := 0; i < contents.Len(); i++ {
			streams = append(streams, contents.Index(i))
		}
	}
	fonts := p.Resources().Key("Font")
	for _, name := range fonts.Keys() {
		streams = append(streams, fonts.Key(name).Key("ToUnicode"))
	}
	for _, s := range streams {
		if s.Kind() != pdf.Stream {
			continue
		}
		n, _ := io.Copy(io.Discard, io.LimitReader(s.Reader(), *left+1))
		if *left -= n; *left < 0 {
			return errTooLarge
		}
	}
	return nil
}
