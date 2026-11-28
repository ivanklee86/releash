//go:build integration

package main

// Pipeline scenarios that go through the root command, to check that options
// and environment variables reach evaluation. They run against the local
// stack (task services), and the helpers are in internal/integration. The
// release scenarios themselves are in internal/releash. The command contract
// (exit codes 2 to 5, defaults, environment fallback, the raw-argument scan)
// needs no stack and is covered by the unit tests in main_test.go.

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

// The README's GitHub Actions example: gate on check, then load the variant
// for $GITHUB_ENV, passing both the same --user-id. The two repositories get
// different variants of the weighted split, so the variant shows that
// --user-id reaches its evaluation.
func TestPipelineGatesThenLoadsTheVariant(t *testing.T) {
	integration.RequireUnleash(t)

	for repo, want := range map[string]string{
		"repo-0": "RELEASH_VARIANT_WEIGHTED_SPLIT_ENABLED=true\nRELEASH_VARIANT_WEIGHTED_SPLIT_VARIANT=control\n",
		"repo-2": "RELEASH_VARIANT_WEIGHTED_SPLIT_ENABLED=true\nRELEASH_VARIANT_WEIGHTED_SPLIT_VARIANT=treatment\nRELEASH_VARIANT_WEIGHTED_SPLIT_PAYLOAD=v2\n",
	} {
		pipeline(t, "true\n", "check", "variant-weighted-split", "--user-id", repo)
		pipeline(t, want, "variant", "variant-weighted-split", "--user-id", repo)
	}
}

// The README's custom stickiness: --properties picks the variant of a flag
// whose variants stick to the repo property.
func TestPipelinePropertiesPickTheVariant(t *testing.T) {
	integration.RequireUnleash(t)

	for repo, variant := range map[string]string{"org/repo-0": "a", "ivanklee86/releash": "b"} {
		want := strings.Join([]string{
			"RELEASH_VARIANT_CUSTOM_STICKINESS_ENABLED=true",
			"RELEASH_VARIANT_CUSTOM_STICKINESS_VARIANT=" + variant,
		}, "\n") + "\n"
		pipeline(t, want, "variant", "variant-custom-stickiness", "--properties", "repo="+repo)
	}
}

// The README's shell example: capture the shell format first, then eval it,
// so later commands read the payload.
func TestPipelineEvalsTheShellFormat(t *testing.T) {
	integration.RequireUnleash(t)
	unleashEnv(t, map[string]string{
		"UNLEASH_URL":       integration.URL(),
		"UNLEASH_API_TOKEN": integration.ClientToken(t, integration.Production),
	})
	var stdout, stderr bytes.Buffer

	code := execute([]string{"variant", "variant-string-payload", "--user-id", "ivanklee86/releash", "--format", "shell"}, &stdout, &stderr, "dev")
	require.Equal(t, releash.ExitOK, code, "stderr: %s", stderr.String())

	script := `vars=$1 && eval "$vars" && printf '%s|%s' "$RELEASH_VARIANT_STRING_PAYLOAD_VARIANT" "$RELEASH_VARIANT_STRING_PAYLOAD_PAYLOAD"`
	out, err := exec.CommandContext(t.Context(), "/bin/sh", "-c", script, "sh", stdout.String()).Output() //nolint:gosec // G204: the test evals releash's shell output on purpose.
	require.NoError(t, err)
	assert.Equal(t, "blue|deploy blue", string(out))
}

func TestPipelineVariantOfDisabledFlagFailsTheStep(t *testing.T) {
	integration.RequireUnleash(t)
	unleashEnv(t, map[string]string{
		"UNLEASH_URL":       integration.URL(),
		"UNLEASH_API_TOKEN": integration.ClientToken(t, integration.Production),
	})
	var stdout, stderr bytes.Buffer

	code := execute([]string{"variant", "variant-disabled-everywhere", "--format", "shell"}, &stdout, &stderr, "dev")

	assert.Equal(t, "export RELEASH_VARIANT_DISABLED_EVERYWHERE_ENABLED='false'\nexport RELEASH_VARIANT_DISABLED_EVERYWHERE_VARIANT='disabled'\n", stdout.String())
	assert.Equal(t, `releash: flag "variant-disabled-everywhere" is disabled; gate on "releash check variant-disabled-everywhere" before loading its variant`+"\n", stderr.String())
	assert.Equal(t, releash.ExitDisabled, code)
}
