package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

type listPackage struct {
	ImportPath string
	Imports    []string
	Module     *listModule
}

type listModule struct {
	Path string
}

func loadPackages(ctx context.Context, dir string) (module string, pkgs []Pkg, err error) {
	cmd := exec.CommandContext(ctx, "go", "list", "-json", "./...")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return "", nil, fmt.Errorf("go list -json ./...: %w\n%s", err, ee.Stderr)
		}
		return "", nil, fmt.Errorf("go list -json ./...: %w", err)
	}

	dec := json.NewDecoder(bytes.NewReader(out))
	for dec.More() {
		var lp listPackage
		if err := dec.Decode(&lp); err != nil {
			return "", nil, fmt.Errorf("decode go list json: %w", err)
		}
		if lp.Module != nil && lp.Module.Path != "" && module == "" {
			module = lp.Module.Path
		}
		pkgs = append(pkgs, Pkg{ImportPath: lp.ImportPath, Imports: lp.Imports})
	}
	if module == "" {
		return "", nil, fmt.Errorf("go list returned no module path")
	}
	return module, pkgs, nil
}
