//go:build integration

package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Fixtures is the catalog of fixture flags that unleash/seed.py creates on
// the local stack, read from unleash/fixtures.json.
type Fixtures struct {
	// Project is the Unleash project that holds every fixture flag.
	Project string `json:"project"`
	// Flags lists the fixture flags in file order.
	Flags []Flag `json:"flags"`
}

// Flag is one fixture flag. Strategies are kept as raw JSON, because tests
// assert on evaluation results rather than on strategy definitions.
type Flag struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Enabled     map[string]bool   `json:"enabled"`
	Strategies  []json.RawMessage `json:"strategies"`
}

// EnabledIn reports whether the flag is switched on in env. It says nothing
// about whether its strategies match a given context.
func (f Flag) EnabledIn(env string) bool {
	return f.Enabled[env]
}

// LoadFixtures reads unleash/fixtures.json from the repository and fails the
// test if it cannot be read or parsed.
func LoadFixtures(t testing.TB) Fixtures {
	t.Helper()
	data, err := os.ReadFile(fixturesPath(t))
	if err != nil {
		t.Fatalf("reading fixture flags: %v", err)
	}
	var f Fixtures
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("parsing fixture flags: %v", err)
	}
	return f
}

// Flag returns the fixture flag called name and fails the test if there is
// none, so a renamed fixture breaks the tests that rely on it.
func (f Fixtures) Flag(t testing.TB, name string) Flag {
	t.Helper()
	for _, flag := range f.Flags {
		if flag.Name == name {
			return flag
		}
	}
	t.Fatalf("no fixture flag %q in unleash/fixtures.json", name)
	return Flag{}
}

// Names returns the names of every fixture flag, in file order.
func (f Fixtures) Names() []string {
	names := make([]string, 0, len(f.Flags))
	for _, flag := range f.Flags {
		names = append(names, flag.Name)
	}
	return names
}

// fixturesPath locates unleash/fixtures.json relative to this source file,
// because go test runs each package in its own directory.
func fixturesPath(t testing.TB) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locating unleash/fixtures.json: no caller information")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "unleash", "fixtures.json")
}
