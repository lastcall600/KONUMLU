package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

type message struct {
	Finding *finding `json:"finding"`
}

type finding struct {
	OSV   string  `json:"osv"`
	Trace []frame `json:"trace"`
}

type frame struct {
	Module string `json:"module"`
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: check_govulncheck <jsonl> <known.txt>")
		os.Exit(2)
	}
	found, err := reachableIDs(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	known, err := loadKnown(os.Args[2])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	fmt.Println("govulncheck reachable findings:", join(sorted(found)))
	if stale := sorted(subtract(known, found)); len(stale) > 0 {
		fmt.Println("known-list entries not observed (remove after confirming gone):", join(stale))
	}
	if newIDs := sorted(subtract(found, known)); len(newIDs) > 0 {
		fmt.Fprintln(os.Stderr, "NEW reachable findings (fail):", join(newIDs))
		os.Exit(1)
	}
}

func reachableIDs(path string) (map[string]struct{}, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string]struct{}{}
	dec := json.NewDecoder(f)
	for {
		var msg message
		if err := dec.Decode(&msg); err != nil {
			if err == io.EOF {
				return out, nil
			}
			return nil, fmt.Errorf("decode %s: %w", path, err)
		}
		if msg.Finding == nil || msg.Finding.OSV == "" {
			continue
		}
		for _, fr := range msg.Finding.Trace {
			if fr.Module == "backend" {
				out[msg.Finding.OSV] = struct{}{}
				break
			}
		}
	}
}

func loadKnown(path string) (map[string]struct{}, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	out := map[string]struct{}{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out[line] = struct{}{}
	}
	return out, nil
}

func subtract(a, b map[string]struct{}) map[string]struct{} {
	out := map[string]struct{}{}
	for k := range a {
		if _, ok := b[k]; !ok {
			out[k] = struct{}{}
		}
	}
	return out
}

func sorted(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func join(ids []string) string {
	if len(ids) == 0 {
		return "(none)"
	}
	return strings.Join(ids, ", ")
}
