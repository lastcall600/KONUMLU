package reviews

import (
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

func TestReviewsProductionImportsOnlyContracts(t *testing.T) {
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
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(fset, name, src, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if p == "backend/internal/verified" || (strings.HasPrefix(p, "backend/internal/verified/") && !strings.Contains(p, "/contracts")) {
				t.Fatalf("%s imports verified implementation %s", name, p)
			}
			if p == "backend/internal/identity" || (strings.HasPrefix(p, "backend/internal/identity/") && !strings.Contains(p, "/contracts")) {
				t.Fatalf("%s imports identity implementation %s", name, p)
			}
			if p == "backend/internal/listings" || (strings.HasPrefix(p, "backend/internal/listings/") && !strings.Contains(p, "/contracts")) {
				t.Fatalf("%s imports listings implementation %s", name, p)
			}
			if p == "backend/internal/media" || (strings.HasPrefix(p, "backend/internal/media/") && !strings.Contains(p, "/contracts")) {
				t.Fatalf("%s imports media implementation %s", name, p)
			}
			if p == "backend/internal/trust" || (strings.HasPrefix(p, "backend/internal/trust/") && !strings.Contains(p, "/contracts")) {
				t.Fatalf("%s imports trust implementation %s", name, p)
			}
		}
	}
}
