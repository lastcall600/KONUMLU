package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
)

func TestParseArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    action
		wantErr error
	}{
		{name: "up", args: []string{"up"}, want: actionUp},
		{name: "down 1", args: []string{"down", "1"}, want: actionDownOne},
		{name: "version", args: []string{"version"}, want: actionVersion},
		{name: "empty", args: nil, wantErr: errUsage},
		{name: "unknown", args: []string{"status"}, wantErr: errUsage},
		{name: "up extra", args: []string{"up", "1"}, wantErr: errUsage},
		{name: "down missing n", args: []string{"down"}, wantErr: errUsage},
		{name: "down all rejected", args: []string{"down", "-all"}, wantErr: errUsage},
		{name: "down 2 rejected", args: []string{"down", "2"}, wantErr: errUsage},
		{name: "version extra", args: []string{"version", "1"}, wantErr: errUsage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseArgs(tt.args)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
			if got != tt.want {
				t.Fatalf("action = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRunRequiresDatabaseURL(t *testing.T) {
	err := run([]string{"up"}, "", &bytes.Buffer{})
	if !errors.Is(err, errDatabaseURL) {
		t.Fatalf("err = %v, want %v", err, errDatabaseURL)
	}
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "postgres") {
		t.Fatalf("error leaked connection details: %v", err)
	}
}

func TestRunRejectsUnknownCommandBeforeURL(t *testing.T) {
	err := run([]string{"reset"}, "postgres://user:secret@localhost/db", &bytes.Buffer{})
	if !errors.Is(err, errUsage) {
		t.Fatalf("err = %v, want %v", err, errUsage)
	}
	if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "postgres://") {
		t.Fatalf("error leaked DATABASE_URL: %v", err)
	}
}

func TestFindMigrationsDir(t *testing.T) {
	dir, err := findMigrationsDir()
	if err != nil {
		t.Fatalf("findMigrationsDir: %v", err)
	}
	if !isMigrationsDir(dir) {
		t.Fatalf("found %q but it is not a migrations directory", dir)
	}
}

func TestApplyVersionNone(t *testing.T) {
	var buf bytes.Buffer
	fake := &stubMigrator{versionErr: migrate.ErrNilVersion}
	if err := apply(fake, actionVersion, &buf); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got := strings.TrimSpace(buf.String()); got != "none" {
		t.Fatalf("stdout = %q, want none", got)
	}
}

func TestApplyVersionDirty(t *testing.T) {
	var buf bytes.Buffer
	fake := &stubMigrator{version: 1, dirty: true}
	if err := apply(fake, actionVersion, &buf); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got := strings.TrimSpace(buf.String()); got != "1 dirty" {
		t.Fatalf("stdout = %q, want %q", got, "1 dirty")
	}
}

func TestApplyMapsDriverErrorsWithoutFakingSuccess(t *testing.T) {
	fake := &stubMigrator{upErr: errors.New("postgres://user:secret@localhost/db failed")}
	err := apply(fake, actionUp, &bytes.Buffer{})
	if !errors.Is(err, errMigrationFailed) {
		t.Fatalf("err = %v, want %v", err, errMigrationFailed)
	}
	if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "postgres://") {
		t.Fatalf("error leaked DATABASE_URL: %v", err)
	}
}

func TestIgnoreNoChange(t *testing.T) {
	if err := ignoreNoChange(migrate.ErrNoChange); err != nil {
		t.Fatalf("ErrNoChange should succeed: %v", err)
	}
	if err := ignoreNoChange(errors.New("boom")); !errors.Is(err, errMigrationFailed) {
		t.Fatalf("err = %v, want %v", err, errMigrationFailed)
	}
}

type stubMigrator struct {
	upErr      error
	stepsErr   error
	version    uint
	dirty      bool
	versionErr error
}

func (s *stubMigrator) Up() error { return s.upErr }

func (s *stubMigrator) Steps(int) error { return s.stepsErr }

func (s *stubMigrator) Version() (uint, bool, error) {
	return s.version, s.dirty, s.versionErr
}
