package pdftext

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/ascii85"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

// stream is an indirect stream object's body: its dictionary, with dict added,
// and data.
func stream(dict, data string) string {
	return fmt.Sprintf("<< /Length %d %s >>\nstream\n%s\nendstream", len(data), dict, data)
}

// onePagePDF assembles a minimal one-page PDF whose page draws the given
// content stream object, followed by any extra objects nothing refers to.
func onePagePDF(contents string, extra ...string) []byte {
	return buildPDF(append([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		contents,
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}, extra...))
}

// buildPDF writes objects as objects 1, 2, ... of a PDF whose root is object
// 1. ledongthuc/pdf needs a real xref table, so object offsets are recorded
// while the body is written.
func buildPDF(objects []string) []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects))
	for i, object := range objects {
		offsets[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}

	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return buf.Bytes()
}

func TestRows(t *testing.T) {
	// Two cells on the upper line, one on the lower line. Cells are placed
	// with Tm, like the real statements: that is the operator the extractor
	// reads positions from.
	content := strings.Join([]string{
		"BT /F1 12 Tf 1 0 0 1 10 150 Tm (Tarih) Tj 1 0 0 1 70 150 Tm (Islem) Tj ET",
		"BT /F1 12 Tf 1 0 0 1 10 100 Tm (2026-05-04) Tj ET",
	}, "\n")
	file := onePagePDF(stream("", content))

	rows, err := Rows(context.Background(), bytes.NewReader(file), int64(len(file)))
	if err != nil {
		t.Fatalf("Rows: %v", err)
	}
	want := [][]string{{"Tarih", "Islem"}, {"2026-05-04"}}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("Rows = %q, want %q", rows, want)
	}
}

func TestRows_StopsAPageCountLoop(t *testing.T) {
	// The page loop runs to the root's /Count, which a crafted file sets as
	// high as it likes: this one would loop for good.
	file := buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [] /Count 9223372036854775807 >>",
	})
	workerTimeout = time.Second
	t.Cleanup(func() { workerTimeout = 10 * time.Second })

	done := make(chan error, 1)
	go func() {
		_, err := Rows(context.Background(), bytes.NewReader(file), int64(len(file)))
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Rows returned no error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Rows still running after 5s")
	}
}

func TestRows_WaitsForAFreeWorkerSlot(t *testing.T) {
	// One slot, and the test holds it: Rows must wait instead of starting a
	// second worker, and give up when its context does.
	workers = make(chan struct{}, 1)
	t.Cleanup(func() { workers = make(chan struct{}, 2) })
	workers <- struct{}{}

	file := onePagePDF(stream("", "BT /F1 12 Tf 1 0 0 1 10 150 Tm (Tarih) Tj ET"))
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := Rows(ctx, bytes.NewReader(file), int64(len(file))); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("slot taken: err = %v, want context.DeadlineExceeded", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := Rows(context.Background(), bytes.NewReader(file), int64(len(file)))
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("Rows returned while the slot was taken: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	<-workers
	if err := <-done; err != nil {
		t.Fatalf("slot freed: Rows: %v", err)
	}
}

func TestRows_RejectsDecompressionBomb(t *testing.T) {
	flate := func(s string) string {
		var buf bytes.Buffer
		w := zlib.NewWriter(&buf)
		w.Write([]byte(s))
		w.Close()
		return buf.String()
	}
	ascii85Flate := func(s string) string {
		z := flate(s)
		buf := make([]byte, ascii85.MaxEncodedLen(len(z)))
		return string(buf[:ascii85.Encode(buf, []byte(z))]) + "~>"
	}
	text := "BT /F1 12 Tf 1 0 0 1 10 150 Tm (Tarih) Tj ET"
	// Just past the bound: the check stops there, so the test never holds
	// more than a few MB, while a real bomb decodes to gigabytes.
	bomb := strings.Repeat(" ", maxInflated) + text

	cases := []struct {
		name string
		pdf  func(content string) []byte
	}{
		{"flate content stream", func(c string) []byte {
			return onePagePDF(stream("/Filter /FlateDecode", flate(c)))
		}},
		// Not a zlib stream in the file's bytes; only the library's own
		// decoding sees what it inflates to.
		{"ascii85 flate content stream", func(c string) []byte {
			return onePagePDF(stream("/Filter [/ASCII85Decode /FlateDecode]", ascii85Flate(c)))
		}},
		// Stands in for the object and xref streams the library inflates
		// while resolving objects, which no page refers to either.
		{"stream outside the page tree", func(c string) []byte {
			return onePagePDF(stream("", text), stream("/Filter /FlateDecode", flate(c)))
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			file := c.pdf(text)
			rows, err := Rows(context.Background(), bytes.NewReader(file), int64(len(file)))
			if want := [][]string{{"Tarih"}}; err != nil || !reflect.DeepEqual(rows, want) {
				t.Fatalf("small stream: Rows = %q, %v; want %q", rows, err, want)
			}

			file = c.pdf(bomb)
			if _, err := extract(file); !errors.Is(err, errTooLarge) {
				t.Errorf("bomb (%d byte file): err = %v, want errTooLarge", len(file), err)
			}
		})
	}
}
