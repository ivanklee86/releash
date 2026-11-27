//go:build integration

package main

// Pipeline scenarios that go through the root command, to check that options
// and environment variables reach the config. They run against the local
// stack (task services), and the helpers are in internal/integration. The
// release scenarios themselves are in internal/releash. M3 and M4 fill them
// in.

import "testing"

func TestPipelineWithoutTokenSecretExits3(t *testing.T) {
	t.Skip("M3: check with UNLEASH_API_TOKEN set to the empty string prints false, exits 3, error on stderr")
}

func TestPipelineWithoutURLExits3(t *testing.T) {
	t.Skip("M3: check with UNLEASH_URL unset and no --url prints false, exits 3, error on stderr")
}

func TestPipelineReadsURLAndTokenFromEnvironment(t *testing.T) {
	t.Skip("M4: check enabled-everywhere with only UNLEASH_URL and UNLEASH_API_TOKEN set prints true")
}

func TestPipelineOptionsOverrideEnvironment(t *testing.T) {
	t.Skip("M4: --url and --token override a wrong UNLEASH_URL and UNLEASH_API_TOKEN, so enabled-everywhere prints true")
}

func TestPipelineContextOptionsReachEvaluation(t *testing.T) {
	t.Skip("M4: check region-eu-only --properties region=eu prints true, and region=us prints false")
}
