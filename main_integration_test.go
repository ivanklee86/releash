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

func TestPipelineWithoutTokenExits3(t *testing.T) {
	t.Skip("M3: check with UNLEASH_API_TOKEN unset and no --token prints false, exits 3, error on stderr")
}

func TestPipelineWithEmptyURLExits3(t *testing.T) {
	t.Skip("M3: check with UNLEASH_URL set to the empty string, or with --url \"\", prints false, exits 3, error on stderr")
}

func TestPipelineWithEmptyTokenOptionExits3(t *testing.T) {
	t.Skip("M3: check with --token \"\" prints false, exits 3, error on stderr, even when UNLEASH_API_TOKEN is set")
}

func TestPipelineWithoutFlagNameExits2(t *testing.T) {
	t.Skip("M3: check with no flag name prints false, exits 2, error on stderr")
}

func TestPipelineWithExtraArgumentsExits2(t *testing.T) {
	t.Skip("M3: check enabled-everywhere extra prints false, exits 2, error on stderr")
}

func TestPipelineWithUnknownOptionExits2(t *testing.T) {
	t.Skip("M3: check enabled-everywhere --no-such-option prints false, exits 2, error on stderr")
}

func TestPipelineWithInvalidTimeoutExits2(t *testing.T) {
	t.Skip("M3: --timeout abc, --timeout 0 and --timeout -1s each print false, exit 2, error on stderr")
}

func TestPipelineWithRepeatedReleashOptionExits2(t *testing.T) {
	t.Skip("M3: --url, --token, --timeout and --loud each given twice print false, exit 2, error on stderr")
}

func TestPipelineWithMalformedURLExits4(t *testing.T) {
	t.Skip("M3: a --url or UNLEASH_URL that is not an absolute http or https URL, e.g. localhost:4242 or ftp://host/api, prints false, exits 4, error on stderr")
}

func TestPipelineWithInvalidCurrentTimeExits4(t *testing.T) {
	t.Skip("M3: --current-time tomorrow prints false, exits 4, and stderr says it must be an RFC 3339 timestamp")
}

func TestPipelineWithInvalidRemoteAddressExits4(t *testing.T) {
	t.Skip("M3: --remote-address not-an-ip prints false, exits 4, error on stderr")
}

func TestPipelineWithEmptyEnvOrAppNameExits4(t *testing.T) {
	t.Skip("M3: --env \"\" and --app-name \"\" each print false, exit 4, error on stderr")
}

func TestPipelineWithMalformedPropertyExits4(t *testing.T) {
	t.Skip("M3: --properties region (no =) and --properties =eu (empty name) each print false, exit 4, error on stderr")
}

func TestPipelineWithPropertyNamedAfterStandardFieldExits4(t *testing.T) {
	t.Skip("M3: --properties userId=alice prints false, exits 4, and stderr hints to use --user-id; the same holds for every standard field and its option")
}

func TestPipelineWithRepeatedContextOptionExits5(t *testing.T) {
	t.Skip("M3: each context option given twice, e.g. --env a --env b, prints false, exits 5, error on stderr")
}

func TestPipelineWithRepeatedPropertyExits5(t *testing.T) {
	t.Skip("M3: --properties region=eu --properties region=us prints false, exits 5, error on stderr")
}

func TestPipelineWithSeveralMistakesExitsWithTheLowestCode(t *testing.T) {
	t.Skip("M3: a missing token with --current-time tomorrow exits 3, and --timeout abc with a missing token exits 2; both print false")
}

func TestPipelineErrorsAreReportedWithoutLoud(t *testing.T) {
	t.Skip("M3: for exits 2 to 5, stderr has the error without --loud, and --loud changes neither stdout nor the exit code")
}

func TestPipelineDefaultsNeverCountAsDuplicates(t *testing.T) {
	t.Skip("M3: --env production, --app-name releash and --timeout 5s, each given once, are not repeats of their defaults and do not exit 5 or 2")
}

func TestPipelineDefaultContextIsProductionAndReleash(t *testing.T) {
	t.Skip("M4: without --env and --app-name, flags created through the Admin API with integration.AdminToken and constrained on environment IN production and appName IN releash print true")
}

func TestPipelinePropertyValueKeepsEqualsSign(t *testing.T) {
	t.Skip("M4: --properties build=a=b reaches evaluation as build a=b and --properties build= as an empty value, checked with flags created through the Admin API with integration.AdminToken")
}
