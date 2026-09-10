package main

import (
	"fmt"
	"sort"
	"strings"
)

const (
	internalPrefix = "internal/"

	rootPlatform       = "platform"
	rootInfrastructure = "infrastructure"
	rootMgmt           = "mgmt"
)

var forbiddenRoots = map[string]struct{}{
	"shared": {},
	"common": {},
	"util":   {},
}

type kind int

const (
	kindOther kind = iota
	kindCmd
	kindPlatform
	kindInfrastructure
	kindMgmt
	kindDomainImpl
	kindDomainContracts
	kindForbiddenRoot
)

func (k kind) String() string {
	switch k {
	case kindCmd:
		return "cmd"
	case kindPlatform:
		return "platform"
	case kindInfrastructure:
		return "infrastructure"
	case kindMgmt:
		return "mgmt"
	case kindDomainImpl:
		return "domain"
	case kindDomainContracts:
		return "domain-contracts"
	case kindForbiddenRoot:
		return "forbidden-root"
	default:
		return "other"
	}
}

// Pkg is a module package and its production imports.
type Pkg struct {
	ImportPath string
	Imports    []string
}

// Violation is a single architecture-rule failure.
type Violation struct {
	From string
	To   string
	Rule string
}

func (v Violation) String() string {
	if v.To == "" {
		return fmt.Sprintf("%s: %s", v.From, v.Rule)
	}
	return fmt.Sprintf("%s -> %s: %s", v.From, v.To, v.Rule)
}

func relative(module, importPath string) string {
	if importPath == module {
		return ""
	}
	prefix := module + "/"
	if strings.HasPrefix(importPath, prefix) {
		return strings.TrimPrefix(importPath, prefix)
	}
	return ""
}

func inModule(module, importPath string) bool {
	return importPath == module || strings.HasPrefix(importPath, module+"/")
}

func internalRoot(rel string) string {
	if !strings.HasPrefix(rel, internalPrefix) {
		return ""
	}
	rest := strings.TrimPrefix(rel, internalPrefix)
	root, _, _ := strings.Cut(rest, "/")
	return root
}

func isContracts(rel, domain string) bool {
	prefix := internalPrefix + domain + "/contracts"
	return rel == prefix || strings.HasPrefix(rel, prefix+"/")
}

func classify(rel string) (kind, string) {
	if rel == "" {
		return kindOther, ""
	}
	if rel == "cmd" || strings.HasPrefix(rel, "cmd/") {
		return kindCmd, ""
	}
	root := internalRoot(rel)
	if root == "" {
		return kindOther, ""
	}
	if _, banned := forbiddenRoots[root]; banned {
		return kindForbiddenRoot, root
	}
	switch root {
	case rootPlatform:
		return kindPlatform, root
	case rootInfrastructure:
		return kindInfrastructure, root
	case rootMgmt:
		return kindMgmt, root
	}
	if isContracts(rel, root) {
		return kindDomainContracts, root
	}
	return kindDomainImpl, root
}

func isDomain(k kind) bool {
	return k == kindDomainImpl || k == kindDomainContracts
}

// Evaluate reports architecture-boundary violations among module packages.
func Evaluate(module string, pkgs []Pkg) []Violation {
	var vs []Violation
	for _, p := range pkgs {
		if !inModule(module, p.ImportPath) {
			continue
		}
		fromRel := relative(module, p.ImportPath)
		fromKind, fromDomain := classify(fromRel)
		if fromKind == kindForbiddenRoot {
			vs = append(vs, Violation{
				From: p.ImportPath,
				Rule: fmt.Sprintf("forbidden internal package root %q", fromDomain),
			})
		}
		for _, imp := range p.Imports {
			if !inModule(module, imp) {
				continue
			}
			toRel := relative(module, imp)
			toKind, toDomain := classify(toRel)
			if v, ok := checkImport(p.ImportPath, imp, fromKind, fromDomain, toKind, toDomain); ok {
				vs = append(vs, v)
			}
		}
	}
	sort.Slice(vs, func(i, j int) bool {
		if vs[i].From != vs[j].From {
			return vs[i].From < vs[j].From
		}
		if vs[i].To != vs[j].To {
			return vs[i].To < vs[j].To
		}
		return vs[i].Rule < vs[j].Rule
	})
	return vs
}

func checkImport(fromPath, toPath string, fromKind kind, fromDomain string, toKind kind, toDomain string) (Violation, bool) {
	if toKind == kindForbiddenRoot {
		return Violation{
			From: fromPath,
			To:   toPath,
			Rule: fmt.Sprintf("must not import forbidden internal package root %q", toDomain),
		}, true
	}

	switch fromKind {
	case kindCmd:
		return Violation{}, false

	case kindPlatform:
		if isDomain(toKind) {
			return Violation{From: fromPath, To: toPath, Rule: "platform must not import a domain package"}, true
		}
		if toKind == kindInfrastructure {
			return Violation{From: fromPath, To: toPath, Rule: "platform must not import internal/infrastructure"}, true
		}
		if toKind == kindMgmt {
			return Violation{From: fromPath, To: toPath, Rule: "platform must not import internal/mgmt"}, true
		}

	case kindDomainImpl, kindDomainContracts:
		if toKind == kindInfrastructure {
			return Violation{From: fromPath, To: toPath, Rule: "domain must not import internal/infrastructure"}, true
		}
		if toKind == kindDomainImpl && toDomain != fromDomain {
			return Violation{
				From: fromPath,
				To:   toPath,
				Rule: fmt.Sprintf("domain must not import another domain implementation (use internal/%s/contracts)", toDomain),
			}, true
		}
		if fromKind == kindDomainContracts && toKind == kindDomainImpl {
			return Violation{From: fromPath, To: toPath, Rule: "domain contracts must not import a domain implementation package"}, true
		}

	case kindMgmt:
		if toKind == kindDomainImpl {
			return Violation{From: fromPath, To: toPath, Rule: "mgmt must not import a domain implementation (use contracts)"}, true
		}
	}

	return Violation{}, false
}
