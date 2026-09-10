package listings

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListingsProductionDoesNotImportPeerImplementations(t *testing.T) {
	assertNoPeerImpl(t, ".")
	assertNoPeerImpl(t, filepath.Join("contracts"))
}

func assertNoPeerImpl(t *testing.T, dir string) {
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
			if p == "backend/internal/location" || (strings.HasPrefix(p, "backend/internal/location/") && !strings.Contains(p, "/contracts")) {
				t.Fatalf("%s imports location implementation %s", path, p)
			}
			if p == "backend/internal/media" || (strings.HasPrefix(p, "backend/internal/media/") && !strings.Contains(p, "/contracts")) {
				t.Fatalf("%s imports media implementation %s", path, p)
			}
			if p == "backend/internal/masterdata" || (strings.HasPrefix(p, "backend/internal/masterdata/") && !strings.Contains(p, "/contracts")) {
				t.Fatalf("%s imports masterdata implementation %s", path, p)
			}
			if p == "backend/internal/eids" || (strings.HasPrefix(p, "backend/internal/eids/") && !strings.Contains(p, "/contracts")) {
				t.Fatalf("%s imports eids implementation %s", path, p)
			}
			if dir != "." && (p == "backend/internal/listings" || (strings.HasPrefix(p, "backend/internal/listings/") && !strings.Contains(p, "/contracts"))) {
				t.Fatalf("contracts %s imports listings implementation %s", path, p)
			}
		}
	}
}
