package server

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

var (
	// First argument of a mux.Handle/HandleFunc call, up to the comma.
	handleCall = regexp.MustCompile(`\.Handle(?:Func)?\(([^,]*),`)
	// "GET /api/x" literal, or "POST "+SomeConst.
	methodLiteral = regexp.MustCompile(`^"([A-Z]+) (/api/[^"]*)"$`)
	methodConst   = regexp.MustCompile(`^"([A-Z]+) "\s*\+\s*(\w+)$`)
	// A mount without a method, like "/api/admin/": not a route, skipped.
	mountLiteral = regexp.MustCompile(`^"/[^" ]*"$`)
	pathConst    = regexp.MustCompile(`\bconst\s+(\w+)\s*=\s*"(/api/[^"]*)"`)
	routerAnnot  = regexp.MustCompile(`@Router\s+(\S+)\s+\[(\w+)\]`)
)

// TestEveryRouteDocumented fails when a route registered under
// backend/internal has no swag @Router annotation, or an annotation names a
// route that is not registered (ADR 0013). It reads source only.
//
// ponytail: a pattern built any other way than a literal or "METHOD "+Const
// fails the test as unrecognized; extend the regexes if that ever happens.
// It checks path and method only, not schemas.
func TestEveryRouteDocumented(t *testing.T) {
	type call struct{ file, arg string }
	var calls []call
	consts := map[string]string{}
	annotated := map[string]bool{}

	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		src := string(b)
		for _, m := range handleCall.FindAllStringSubmatch(src, -1) {
			calls = append(calls, call{path, strings.TrimSpace(m[1])})
		}
		for _, m := range pathConst.FindAllStringSubmatch(src, -1) {
			consts[m[1]] = m[2]
		}
		for _, m := range routerAnnot.FindAllStringSubmatch(src, -1) {
			annotated[strings.ToUpper(m[2])+" /api"+m[1]] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	registered := map[string]bool{}
	for _, c := range calls {
		if m := methodLiteral.FindStringSubmatch(c.arg); m != nil {
			registered[m[1]+" "+m[2]] = true
		} else if m := methodConst.FindStringSubmatch(c.arg); m != nil && consts[m[2]] != "" {
			registered[m[1]+" "+consts[m[2]]] = true
		} else if !mountLiteral.MatchString(c.arg) {
			t.Errorf("%s: unrecognized route pattern %s", c.file, c.arg)
		}
	}
	if len(registered) == 0 {
		t.Fatal("no registered routes found; the regex is broken")
	}

	var undocumented, unregistered []string
	for r := range registered {
		if !annotated[r] {
			undocumented = append(undocumented, r)
		}
	}
	for r := range annotated {
		if !registered[r] {
			unregistered = append(unregistered, r)
		}
	}
	slices.Sort(undocumented)
	slices.Sort(unregistered)
	if len(undocumented) > 0 {
		t.Errorf("registered but not documented:\n  %s", strings.Join(undocumented, "\n  "))
	}
	if len(unregistered) > 0 {
		t.Errorf("documented but not registered:\n  %s", strings.Join(unregistered, "\n  "))
	}
}
