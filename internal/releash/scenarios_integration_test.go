//go:build integration

package releash_test

// Release scenarios from spec 0001, written the way a pipeline uses releash.
// Each drives a Releash built with NewWithConfig against the local stack
// (task services), with buffers for Out and Err, and asserts stdout, stderr
// and the exit code. The fixture flags they name are in unleash/fixtures.json,
// and the helpers are in internal/integration. M3 and M4 fill them in.

import "testing"

func TestDeployGateOpensForEnabledFlag(t *testing.T) {
	t.Skip("M4: enabled-everywhere prints true, exits 0, stderr empty")
}

func TestDeployGateStaysClosedForDisabledFlag(t *testing.T) {
	t.Skip("M4: disabled-everywhere prints false, exits 0, stderr empty")
}

func TestDeployGateStaysClosedWhenTokenIsRejected(t *testing.T) {
	t.Skip("M4: a token Unleash rejects with 401 or 403 prints false, exits 0, silent without --loud")
}

// A flag in a project the token cannot see is covered by this case too:
// open-source Unleash has only the default project, so there is no fixture
// for it (spec 0001, test plan).
func TestDeployGateStaysClosedForUnknownFlag(t *testing.T) {
	t.Skip("M4: a flag that is not in the fixtures prints false, exits 0, silent without --loud")
}

func TestDeployGateStaysClosedWhenUnleashIsUnreachable(t *testing.T) {
	t.Skip("M4: a URL with nothing listening prints false, exits 0, finishes within --timeout")
}

func TestDeployGateStaysClosedWhenUnleashNeverResponds(t *testing.T) {
	t.Skip("M4: integration.UnresponsiveServer prints false, exits 0, finishes within --timeout")
}

func TestDeployGateStaysClosedOnMalformedResponse(t *testing.T) {
	t.Skip("M4: a server answering the feature fetch with invalid JSON prints false, exits 0")
}

func TestLoudExplainsWhyTheGateStayedClosed(t *testing.T) {
	t.Skip("M4: each fallback reason writes its own stderr line under --loud; stdout and exit code are unchanged")
}

func TestRegionGatedRolloutOpensOnlyForEU(t *testing.T) {
	t.Skip("M4: region-eu-only is true with --properties region=eu and false with region=us or no region")
}

func TestUserTargetedRolloutOpensOnlyForAllowlistedUsers(t *testing.T) {
	t.Skip("M4: user-allowlist is true for --user-id alice or bob and false for anyone else or no user")
}

func TestScheduledReleaseFollowsTheClock(t *testing.T) {
	// The 2020 and 2099 fixtures would pass with any clock this century, so they
	// cannot show that the time of the check is the one used.
	t.Skip("M4: creates flags with DATE_AFTER and DATE_BEFORE constraints a few minutes either side of now through the Admin API with integration.AdminToken, and without --current-time each gives the result for the time of the check; the 2020 and 2099 fixtures (date-after-past, date-before-future true; date-after-future, date-before-past false) are a coarse check only")
}

func TestScheduledReleaseCanBeCheckedAheadOfTime(t *testing.T) {
	t.Skip("M4: --current-time after 2099-01-01 makes date-after-future true and date-before-future false")
}

func TestIPTargetedRolloutOpensForTheRunnerAddress(t *testing.T) {
	t.Skip("M4: without --remote-address, ip-allowlist is true for the address releash detects")
}

func TestRemoteAddressOptionOverridesTheRunnerAddress(t *testing.T) {
	t.Skip("M4: --remote-address outside the allowlist makes ip-allowlist false")
}

func TestTokenPicksTheEnvironment(t *testing.T) {
	t.Skip("M4: development-only is true with the development token and false with the production token")
}

func TestStandardContextOptionsReachEvaluation(t *testing.T) {
	t.Skip("M4: --env, --app-name, --session-id and the other standard fields each change the result of a constraint on them")
}

func TestNoStateCarriesOverBetweenChecks(t *testing.T) {
	t.Skip("M4: after a check that prints true, a check with a bad token or an unreachable URL prints false, and no files are written")
}

func TestRegisterErrorDoesNotChangeTheAnswer(t *testing.T) {
	t.Skip("M4: a server that rejects register but serves features gives the evaluated answer; the error shows only under --loud")
}

func TestUsageMetricsAreSentBeforeExit(t *testing.T) {
	t.Skip("M4: a check sends usage metrics for the flag before it returns")
}

func TestFailingMetricsEndpointDoesNotDelayTheGate(t *testing.T) {
	t.Skip("M4: a failing or hanging metrics endpoint changes neither stdout nor the exit code, and exit stays within --timeout")
}

func TestPanicDuringCheckStillPrintsFalse(t *testing.T) {
	t.Skip("M3: a panic on releash's own goroutine during check prints false, writes the error to stderr without --loud, and exits 1")
}
