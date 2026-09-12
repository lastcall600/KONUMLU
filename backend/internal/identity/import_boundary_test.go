package identity

import (
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

func TestIdentityProductionDoesNotImportTrustOrStaff(t *testing.T) {
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
			if p == "backend/internal/trust" || strings.HasPrefix(p, "backend/internal/trust/") {
				t.Fatalf("%s imports trust %s", name, p)
			}
			if p == "backend/internal/staffauth" || strings.HasPrefix(p, "backend/internal/staffauth/") {
				t.Fatalf("%s imports staffauth %s", name, p)
			}
		}
	}
}
