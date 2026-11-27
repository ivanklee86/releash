//go:build integration

package main

// Pipeline scenarios that go through the root command, to check that options
// and environment variables reach evaluation. They run against the local
// stack (task services), and the helpers are in internal/integration. The
// release scenarios themselves are in internal/releash. M4 fills them in. The
// command contract (exit codes 2 to 5, defaults, environment fallback) needs
// no stack and is covered by the unit tests in check_test.go.

import "testing"

func TestPipelineReadsURLAndTokenFromEnvironment(t *testing.T) {
	t.Skip("M4: check enabled-everywhere with only UNLEASH_URL and UNLEASH_API_TOKEN set prints true")
}

func TestPipelineOptionsOverrideEnvironment(t *testing.T) {
	t.Skip("M4: --url and --token override a wrong UNLEASH_URL and UNLEASH_API_TOKEN, so enabled-everywhere prints true")
}

func TestPipelineContextOptionsReachEvaluation(t *testing.T) {
	t.Skip("M4: check region-eu-only --properties region=eu prints true, and region=us prints false")
}

func TestPipelineDefaultContextIsProductionAndReleash(t *testing.T) {
	t.Skip("M4: without --env and --app-name, flags created through the Admin API with integration.AdminToken and constrained on environment IN production and appName IN releash print true")
}

func TestPipelinePropertyValueKeepsEqualsSign(t *testing.T) {
	t.Skip("M4: --properties build=a=b reaches evaluation as build a=b and --properties build= as an empty value, checked with flags created through the Admin API with integration.AdminToken")
}
