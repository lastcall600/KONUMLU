package main

import (
	"context"
	"fmt"
	"os"
)

func main() {
	dir := "."
	if len(os.Args) > 1 {
		dir = os.Args[1]
	}

	code, err := run(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	os.Exit(code)
}

func run(dir string) (int, error) {
	module, pkgs, err := loadPackages(context.Background(), dir)
	if err != nil {
		return 2, err
	}
	vs := Evaluate(module, pkgs)
	if len(vs) == 0 {
		fmt.Println("arch-check: ok")
		return 0, nil
	}
	for _, v := range vs {
		fmt.Fprintln(os.Stderr, v.String())
	}
	return 1, fmt.Errorf("arch-check: %d violation(s)", len(vs))
}
