package masterdata

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMasterDataProductionDoesNotImportPeerImplementations(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(".", name)
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
			if p == "backend/internal/listings" || strings.HasPrefix(p, "backend/internal/listings/") {
				t.Fatalf("%s imports listings %s", path, p)
			}
			if p == "backend/internal/location" || strings.HasPrefix(p, "backend/internal/location/") {
				t.Fatalf("%s imports location %s", path, p)
			}
		}
	}
	assertContractsDoNotImportImpl(t)
}

func assertContractsDoNotImportImpl(t *testing.T) {
	t.Helper()
	dir := filepath.Join("contracts")
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
			if p == "backend/internal/masterdata" || (strings.HasPrefix(p, "backend/internal/masterdata/") && !strings.Contains(p, "/contracts")) {
				t.Fatalf("contracts %s imports masterdata implementation %s", path, p)
			}
			if p == "backend/internal/listings" || strings.HasPrefix(p, "backend/internal/listings/") {
				t.Fatalf("contracts %s imports listings %s", path, p)
			}
		}
	}
}
