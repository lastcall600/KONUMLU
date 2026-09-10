package search

import (
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

func TestSearchProductionImportsOnlyContracts(t *testing.T) {
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
			if p == "backend/internal/listings" || (strings.HasPrefix(p, "backend/internal/listings/") && !strings.Contains(p, "/contracts")) {
				t.Fatalf("%s imports listings implementation %s", name, p)
			}
			if p == "backend/internal/location" || (strings.HasPrefix(p, "backend/internal/location/") && !strings.Contains(p, "/contracts")) {
				t.Fatalf("%s imports location implementation %s", name, p)
			}
			if p == "backend/internal/masterdata" || (strings.HasPrefix(p, "backend/internal/masterdata/") && !strings.Contains(p, "/contracts")) {
				t.Fatalf("%s imports masterdata implementation %s", name, p)
			}
			lower := strings.ToLower(p)
			if strings.Contains(lower, "opensearch") || strings.Contains(lower, "typesense") ||
				strings.Contains(lower, "meilisearch") || strings.Contains(lower, "/h3") ||
				strings.HasSuffix(lower, "h3") {
				t.Fatalf("%s imports forbidden search engine or H3 %s", name, p)
			}
		}
	}
}
