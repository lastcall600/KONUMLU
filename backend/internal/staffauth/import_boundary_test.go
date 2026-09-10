package staffauth

import (
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

func TestStaffAuthProductionDoesNotImportPeerImplementations(t *testing.T) {
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
			if p == "backend/internal/identity" || strings.HasPrefix(p, "backend/internal/identity/") {
				t.Fatalf("%s imports identity %s", name, p)
			}
			if p == "backend/internal/infrastructure" || strings.HasPrefix(p, "backend/internal/infrastructure/") {
				t.Fatalf("%s imports infrastructure %s", name, p)
			}
			if p == "backend/internal/moderation" || strings.HasPrefix(p, "backend/internal/moderation/") {
				t.Fatalf("%s imports moderation %s", name, p)
			}
			if p == "backend/internal/disputes" || strings.HasPrefix(p, "backend/internal/disputes/") {
				t.Fatalf("%s imports disputes %s", name, p)
			}
		}
	}
}
