package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

const firstMigrationFile = "000001_enable_postgis.up.sql"

var (
	errUsage           = errors.New("usage: migrate up | down 1 | version")
	errDatabaseURL     = errors.New("DATABASE_URL must not be empty")
	errMigrationsDir   = errors.New("migrations directory not found")
	errMigrationFailed = errors.New("migration failed")
)

type action int

const (
	actionUp action = iota
	actionDownOne
	actionVersion
)

func main() {
	if err := run(os.Args[1:], os.Getenv("DATABASE_URL"), os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, databaseURL string, stdout io.Writer) error {
	act, err := parseArgs(args)
	if err != nil {
		return err
	}
	if databaseURL == "" {
		return errDatabaseURL
	}
	dir, err := findMigrationsDir()
	if err != nil {
		return err
	}
	m, err := openMigrator(dir, databaseURL)
	if err != nil {
		return errMigrationFailed
	}
	defer m.Close()
	return apply(m, act, stdout)
}

func parseArgs(args []string) (action, error) {
	if len(args) == 0 {
		return 0, errUsage
	}
	switch args[0] {
	case "up":
		if len(args) != 1 {
			return 0, errUsage
		}
		return actionUp, nil
	case "down":
		if len(args) != 2 || args[1] != "1" {
			return 0, errUsage
		}
		return actionDownOne, nil
	case "version":
		if len(args) != 1 {
			return 0, errUsage
		}
		return actionVersion, nil
	default:
		return 0, errUsage
	}
}

func findMigrationsDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", errMigrationsDir
	}
	dir := wd
	for {
		for _, rel := range []string{"migrations", filepath.Join("backend", "migrations")} {
			candidate := filepath.Join(dir, rel)
			if isMigrationsDir(candidate) {
				return candidate, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", errMigrationsDir
}

func isMigrationsDir(dir string) bool {
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		return false
	}
	_, err = os.Stat(filepath.Join(dir, firstMigrationFile))
	return err == nil
}

func openMigrator(dir, databaseURL string) (*migrate.Migrate, error) {
	src, err := iofs.New(os.DirFS(dir), ".")
	if err != nil {
		return nil, err
	}
	return migrate.NewWithSourceInstance("iofs", src, databaseURL)
}

type migrator interface {
	Up() error
	Steps(n int) error
	Version() (uint, bool, error)
}

func apply(m migrator, act action, stdout io.Writer) error {
	switch act {
	case actionUp:
		return ignoreNoChange(m.Up())
	case actionDownOne:
		return ignoreNoChange(m.Steps(-1))
	case actionVersion:
		v, dirty, err := m.Version()
		if errors.Is(err, migrate.ErrNilVersion) {
			fmt.Fprintln(stdout, "none")
			return nil
		}
		if err != nil {
			return errMigrationFailed
		}
		if dirty {
			fmt.Fprintf(stdout, "%d dirty\n", v)
			return nil
		}
		fmt.Fprintf(stdout, "%d\n", v)
		return nil
	default:
		return errUsage
	}
}

func ignoreNoChange(err error) error {
	if err == nil || errors.Is(err, migrate.ErrNoChange) {
		return nil
	}
	return errMigrationFailed
}
