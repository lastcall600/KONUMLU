package media

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMediaImplementationDoesNotImportListings(t *testing.T) {
	assertNoListingsImport(t, ".")
	assertNoListingsImport(t, filepath.Join("contracts"))
	assertListingsContractsOnly(t, filepath.Join("httpapi"))
}

func assertNoListingsImport(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(dir, name)
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(fset, path, src, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if strings.Contains(p, "/listings") {
				t.Fatalf("%s imports %s", path, p)
			}
			if dir != "." && p == "backend/internal/media" {
				t.Fatalf("%s imports implementation %s", path, p)
			}
		}
	}
}

func assertListingsContractsOnly(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(dir, name)
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(fset, path, src, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if p == "backend/internal/listings" || (strings.HasPrefix(p, "backend/internal/listings/") && p != "backend/internal/listings/contracts") {
				t.Fatalf("%s imports listings implementation %s", path, p)
			}
		}
	}
}
