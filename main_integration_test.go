//go:build integration

package main

// Pipeline scenarios that go through the root command, to check that options
// and environment variables reach evaluation. They run against the local
// stack (task services), and the helpers are in internal/integration. The
// release scenarios themselves are in internal/releash. The command contract
// (exit codes 2 to 5, defaults, environment fallback) needs no stack and is
// covered by the unit tests in check_test.go.

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ivanklee86/releash/internal/integration"
	"github.com/ivanklee86/releash/internal/releash"
)

// pipeline runs releash with args and the stack's URL and production token
// in the environment, and asserts that it prints want, exits 0 and writes
// nothing to stderr.
func pipeline(t *testing.T, want string, args ...string) {
	t.Helper()
	unleashEnv(t, map[string]string{
		"UNLEASH_URL":       integration.URL(),
		"UNLEASH_API_TOKEN": integration.ClientToken(t, integration.Production),
	})
	var stdout, stderr bytes.Buffer

	code := execute(args, &stdout, &stderr, "dev")

	assert.Equal(t, want, stdout.String(), "stdout of %v", args)
	assert.Equal(t, releash.ExitOK, code, "exit code of %v", args)
	assert.Empty(t, stderr.String(), "stderr of %v", args)
}

func TestPipelineReadsURLAndTokenFromEnvironment(t *testing.T) {
	integration.RequireUnleash(t)

	pipeline(t, "true\n", "check", "enabled-everywhere")
}

func TestPipelineOptionsOverrideEnvironment(t *testing.T) {
	integration.RequireUnleash(t)
	// Read before unleashEnv, because integration.URL reads UNLEASH_URL.
	url, token := integration.URL(), integration.ClientToken(t, integration.Production)
	unleashEnv(t, map[string]string{
		"UNLEASH_URL":       integration.UnreachableURL(t),
		"UNLEASH_API_TOKEN": "default:production.not-a-real-token",
	})
	var stdout, stderr bytes.Buffer

	code := execute([]string{"check", "enabled-everywhere", "--url", url, "--token", token}, &stdout, &stderr, "dev")

	assert.Equal(t, "true\n", stdout.String())
	assert.Equal(t, releash.ExitOK, code)
	assert.Empty(t, stderr.String())
}

func TestPipelineContextOptionsReachEvaluation(t *testing.T) {
	integration.RequireUnleash(t)

	pipeline(t, "true\n", "check", "region-eu-only", "--properties", "region=eu")
	pipeline(t, "false\n", "check", "region-eu-only", "--properties", "region=us")
	pipeline(t, "true\n", "check", "user-allowlist", "--user-id", "bob")
}

func TestPipelineDefaultContextIsProductionAndReleash(t *testing.T) {
	integration.RequireUnleash(t)
	inProduction := integration.CreateFlag(t, integration.RolloutWith(
		integration.Constraint{ContextName: "environment", Operator: "IN", Values: []string{"production"}}))
	inReleash := integration.CreateFlag(t, integration.RolloutWith(
		integration.Constraint{ContextName: "appName", Operator: "IN", Values: []string{"releash"}}))

	pipeline(t, "true\n", "check", inProduction)
	pipeline(t, "true\n", "check", inReleash)
	// The constraints do decide: other values turn the flags off.
	pipeline(t, "false\n", "check", inProduction, "--env", "staging")
	pipeline(t, "false\n", "check", inReleash, "--app-name", "pipeline")
}

func TestPipelinePropertyValueKeepsEqualsSign(t *testing.T) {
	integration.RequireUnleash(t)
	isAB := integration.CreateFlag(t, integration.RolloutWith(
		integration.Constraint{ContextName: "build", Operator: "IN", Values: []string{"a=b"}}))
	// Unleash rejects an empty value in a constraint, and the SDK reads an
	// empty property as unset, so this only checks that an empty value is
	// accepted and leaves the answer unaffected. The unit tests check that the
	// empty value reaches evaluation.
	notA := integration.CreateFlag(t, integration.RolloutWith(
		integration.Constraint{ContextName: "build", Operator: "NOT_IN", Values: []string{"a"}}))

	pipeline(t, "true\n", "check", isAB, "--properties", "build=a=b")
	pipeline(t, "false\n", "check", isAB, "--properties", "build=a")
	pipeline(t, "true\n", "check", notA, "--properties", "build=")
	pipeline(t, "false\n", "check", notA, "--properties", "build=a")
}
