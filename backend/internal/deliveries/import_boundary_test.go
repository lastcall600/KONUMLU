package deliveries

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeliveriesProductionDoesNotImportPeerImplementations(t *testing.T) {
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
			for _, peer := range []string{
				"identity", "needs", "offers", "businesses", "listings", "trust",
				"messaging", "notifications", "transactions", "payments", "verified", "reviews",
			} {
				if p == "backend/internal/"+peer || (strings.HasPrefix(p, "backend/internal/"+peer+"/") && !strings.Contains(p, "/contracts")) {
					t.Fatalf("%s imports %s implementation %s", path, peer, p)
				}
			}
		}
	}
}
