package main

import (
	"strings"
	"testing"
)

const testModule = "backend"

func TestEvaluateCurrentLayoutHasNoViolations(t *testing.T) {
	pkgs := []Pkg{
		{ImportPath: "backend/cmd/server", Imports: []string{
			"backend/internal/platform/config",
			"backend/internal/platform/health",
		}},
		{ImportPath: "backend/cmd/archcheck"},
		{ImportPath: "backend/internal/platform/config"},
		{ImportPath: "backend/internal/platform/health"},
	}
	if vs := Evaluate(testModule, pkgs); len(vs) != 0 {
		t.Fatalf("unexpected violations: %v", vs)
	}
}

func TestCmdMayWireDomainsAndInfrastructure(t *testing.T) {
	pkgs := []Pkg{{
		ImportPath: "backend/cmd/server",
		Imports: []string{
			"backend/internal/identity",
			"backend/internal/identity/contracts",
			"backend/internal/platform/config",
			"backend/internal/infrastructure/storage",
		},
	}}
	if vs := Evaluate(testModule, pkgs); len(vs) != 0 {
		t.Fatalf("cmd wiring should be allowed: %v", vs)
	}
}

func TestDomainMayImportOtherDomainContractsOnly(t *testing.T) {
	ok := []Pkg{{
		ImportPath: "backend/internal/listings",
		Imports: []string{
			"backend/internal/identity/contracts",
			"backend/internal/platform/config",
		},
	}}
	if vs := Evaluate(testModule, ok); len(vs) != 0 {
		t.Fatalf("contract import should be allowed: %v", vs)
	}

	bad := []Pkg{{
		ImportPath: "backend/internal/listings",
		Imports:    []string{"backend/internal/identity"},
	}}
	vs := Evaluate(testModule, bad)
	if !hasRule(vs, "another domain implementation") {
		t.Fatalf("want domain implementation violation, got %v", vs)
	}
}

func TestDomainMustNotImportInfrastructure(t *testing.T) {
	pkgs := []Pkg{{
		ImportPath: "backend/internal/media",
		Imports:    []string{"backend/internal/infrastructure/storage"},
	}}
	vs := Evaluate(testModule, pkgs)
	if !hasRule(vs, "must not import internal/infrastructure") {
		t.Fatalf("want infrastructure violation, got %v", vs)
	}
}

func TestPlatformMustNotImportDomainOrInfrastructure(t *testing.T) {
	pkgs := []Pkg{
		{ImportPath: "backend/internal/platform/config", Imports: []string{"backend/internal/identity"}},
		{ImportPath: "backend/internal/platform/health", Imports: []string{"backend/internal/identity/contracts"}},
		{ImportPath: "backend/internal/platform/db", Imports: []string{"backend/internal/infrastructure/storage"}},
	}
	vs := Evaluate(testModule, pkgs)
	if !hasRule(vs, "platform must not import a domain package") {
		t.Fatalf("want domain import violation, got %v", vs)
	}
	if !hasRule(vs, "platform must not import internal/infrastructure") {
		t.Fatalf("want infrastructure import violation, got %v", vs)
	}
}

func TestMgmtMayImportContractsNotImplementation(t *testing.T) {
	ok := []Pkg{{
		ImportPath: "backend/internal/mgmt",
		Imports:    []string{"backend/internal/listings/contracts"},
	}}
	if vs := Evaluate(testModule, ok); len(vs) != 0 {
		t.Fatalf("mgmt contract import should be allowed: %v", vs)
	}

	bad := []Pkg{{
		ImportPath: "backend/internal/mgmt",
		Imports:    []string{"backend/internal/listings"},
	}}
	vs := Evaluate(testModule, bad)
	if !hasRule(vs, "mgmt must not import a domain implementation") {
		t.Fatalf("want mgmt implementation violation, got %v", vs)
	}
}

func TestForbiddenInternalRoots(t *testing.T) {
	for _, root := range []string{"shared", "common", "util"} {
		pkgs := []Pkg{{ImportPath: "backend/internal/" + root}}
		vs := Evaluate(testModule, pkgs)
		if !hasRule(vs, "forbidden internal package root") {
			t.Fatalf("root %s: want forbidden-root violation, got %v", root, vs)
		}
	}
}

func TestContractsMustNotImportOwnImplementation(t *testing.T) {
	pkgs := []Pkg{{
		ImportPath: "backend/internal/identity/contracts",
		Imports:    []string{"backend/internal/identity"},
	}}
	vs := Evaluate(testModule, pkgs)
	if !hasRule(vs, "domain contracts must not import a domain implementation") {
		t.Fatalf("want contracts->impl violation, got %v", vs)
	}
}

func hasRule(vs []Violation, substr string) bool {
	for _, v := range vs {
		if strings.Contains(v.String(), substr) {
			return true
		}
	}
	return false
}
