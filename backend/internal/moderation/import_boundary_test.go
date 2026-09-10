package moderation

import (
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

func TestModerationProductionImportsOnlyContracts(t *testing.T) {
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
			if p == "backend/internal/identity" || (strings.HasPrefix(p, "backend/internal/identity/") && !strings.Contains(p, "/contracts")) {
				t.Fatalf("%s imports identity implementation %s", name, p)
			}
			if p == "backend/internal/listings" || (strings.HasPrefix(p, "backend/internal/listings/") && !strings.Contains(p, "/contracts")) {
				t.Fatalf("%s imports listings implementation %s", name, p)
			}
			if p == "backend/internal/trust" || strings.HasPrefix(p, "backend/internal/trust/") {
				t.Fatalf("%s imports trust %s", name, p)
			}
			if p == "backend/internal/reviews" || strings.HasPrefix(p, "backend/internal/reviews/") {
				t.Fatalf("%s imports reviews %s", name, p)
			}
			if p == "backend/internal/notifications" || (strings.HasPrefix(p, "backend/internal/notifications/") && !strings.Contains(p, "/contracts")) {
				t.Fatalf("%s imports notifications implementation %s", name, p)
			}
		}
	}
}
