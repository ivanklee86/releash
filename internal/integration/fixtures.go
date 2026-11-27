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

// Flag is one fixture flag: how the seed creates it, and so what the Client
// API should return for it.
type Flag struct {
	// Name is the flag name in Unleash, and says what the flag tests.
	Name string `json:"name"`
	// Description is the flag description the seed sets in Unleash.
	Description string `json:"description"`
	// Enabled says, per environment name, whether the flag is switched on.
	// Use EnabledIn rather than reading it directly.
	Enabled map[string]bool `json:"enabled"`
	// Strategies are the activation strategies the seed adds to the flag in
	// every environment, in order.
	Strategies []Strategy `json:"strategies"`
}

// Strategy is one activation strategy of a fixture flag, in the shape both
// the Admin API and the Client API use.
type Strategy struct {
	// Name is the strategy type, such as flexibleRollout or remoteAddress.
	Name string `json:"name"`
	// Parameters are the strategy's parameters. Unleash sends every value as
	// a string.
	Parameters map[string]string `json:"parameters,omitempty"`
	// Constraints must all match for the strategy to apply.
	Constraints []Constraint `json:"constraints,omitempty"`
}

// Constraint is one constraint of a strategy.
type Constraint struct {
	// ContextName is the context field the constraint reads, such as region
	// or currentTime.
	ContextName string `json:"contextName"`
	// Operator is the Unleash operator, such as IN or DATE_AFTER.
	Operator string `json:"operator"`
	// Values holds the operands of list operators such as IN.
	Values []string `json:"values,omitempty"`
	// Value holds the operand of single-value operators such as DATE_AFTER.
	Value string `json:"value,omitempty"`
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
