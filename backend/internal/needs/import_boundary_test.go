package needs

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNeedsProductionDoesNotImportPeerImplementations(t *testing.T) {
	assertNoPeerImpl(t, ".")
	assertNoPeerImpl(t, filepath.Join("httpapi"))
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
			if p == "backend/internal/identity" || (strings.HasPrefix(p, "backend/internal/identity/") && !strings.Contains(p, "/contracts")) {
				t.Fatalf("%s imports identity implementation %s", path, p)
			}
			if p == "backend/internal/location" || (strings.HasPrefix(p, "backend/internal/location/") && !strings.Contains(p, "/contracts")) {
				t.Fatalf("%s imports location implementation %s", path, p)
			}
			if p == "backend/internal/businesses" || (strings.HasPrefix(p, "backend/internal/businesses/") && !strings.Contains(p, "/contracts")) {
				t.Fatalf("%s imports businesses implementation %s", path, p)
			}
			if p == "backend/internal/offers" || strings.HasPrefix(p, "backend/internal/offers/") {
				t.Fatalf("%s imports offers %s", path, p)
			}
			if p == "backend/internal/masterdata" || (strings.HasPrefix(p, "backend/internal/masterdata/") && !strings.Contains(p, "/contracts")) {
				t.Fatalf("%s imports masterdata implementation %s", path, p)
			}
		}
	}
}
