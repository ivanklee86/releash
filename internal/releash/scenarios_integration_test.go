//go:build integration

package releash_test

// Release scenarios from spec 0001, written the way a pipeline uses releash.
// Each drives a Releash built with NewWithConfig against the local stack
// (task services), with buffers for Out and Err, and asserts stdout, stderr
// and the exit code. The fixture flags they name are in unleash/fixtures.json,
// and the helpers are in internal/integration. Flags the fixtures cannot hold
// are created through the Admin API with integration.CreateFlag. Failures
// the stack cannot produce on demand use integration.FakeUnleash,
// integration.UnresponsiveServer or integration.UnreachableURL. A panic
// during check needs no stack and is a unit test in the root package
// (TestPanicDuringCheckStillPrintsFalse).

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanklee86/releash/internal/integration"
	"github.com/ivanklee86/releash/internal/releash"
)

// result is what one check printed and returned.
type result struct {
	stdout string
	stderr string
	code   int
}

// option changes the options of one check.
type option func(*releash.CheckOptions)

// given sets the option called name to value, counting it as given once, as
// the command line would.
func given(name, value string) option {
	return func(o *releash.CheckOptions) {
		switch name {
		case releash.OptionURL:
			o.URL = value
		case releash.OptionToken:
			o.Token = value
		case releash.OptionTimeout:
			o.Timeout = value
		case releash.OptionEnv:
			o.Env = value
		case releash.OptionAppName:
			o.AppName = value
		case releash.OptionUserID:
			o.UserID = value
		case releash.OptionSessionID:
			o.SessionID = value
		case releash.OptionRemoteAddress:
			o.RemoteAddress = value
		case releash.OptionCurrentTime:
			o.CurrentTime = value
		default:
			panic("given: unsupported option " + name)
		}
		o.Given[name]++
	}
}

func property(nameValue string) option {
	return func(o *releash.CheckOptions) { o.Properties = append(o.Properties, nameValue) }
}

var loud option = func(o *releash.CheckOptions) { o.Loud = true }

// check runs releash check flag against the local stack with the production
// token, changed by opts.
func check(t *testing.T, flag string, opts ...option) result {
	t.Helper()
	o := releash.CheckOptions{
		Flag:  flag,
		URL:   integration.URL(),
		Token: integration.ClientToken(t, integration.Production),
		Given: map[string]int{},
	}
	for _, opt := range opts {
		opt(&o)
	}
	var stdout, stderr bytes.Buffer
	r := releash.NewWithConfig(releash.Config{})
	r.Out, r.Err = &stdout, &stderr
	code := r.Check(o)
	return result{stdout: stdout.String(), stderr: stderr.String(), code: code}
}

// assertAnswer asserts that res is the answer want, exit 0 and nothing on
// stderr.
func assertAnswer(t *testing.T, want bool, res result, msgAndArgs ...any) {
	t.Helper()
	assert.Equal(t, fmt.Sprintln(want), res.stdout, msgAndArgs...)
	assert.Equal(t, releash.ExitOK, res.code, msgAndArgs...)
	assert.Empty(t, res.stderr, msgAndArgs...)
}

// assertFallback asserts that flag falls back to false with exit 0, silently
// without --loud, and with exactly wantLine on stderr with it.
func assertFallback(t *testing.T, flag, wantLine string, opts ...option) {
	t.Helper()
	quiet := check(t, flag, opts...)
	assertAnswer(t, false, quiet, "without --loud")

	noisy := check(t, flag, append(opts, loud)...)
	assert.Equal(t, quiet.stdout, noisy.stdout, "--loud changes stdout")
	assert.Equal(t, quiet.code, noisy.code, "--loud changes the exit code")
	assert.Equal(t, wantLine+"\n", noisy.stderr, "--loud line")
}

func TestDeployGateOpensForEnabledFlag(t *testing.T) {
	integration.RequireUnleash(t)

	assertAnswer(t, true, check(t, "enabled-everywhere"))
	assertAnswer(t, true, check(t, "enabled-everywhere", loud), "with --loud")
}

func TestDeployGateStaysClosedForDisabledFlag(t *testing.T) {
	integration.RequireUnleash(t)

	assertAnswer(t, false, check(t, "disabled-everywhere"))
	assertAnswer(t, false, check(t, "disabled-everywhere", loud), "with --loud")
}

func TestDeployGateStaysClosedWhenTokenIsRejected(t *testing.T) {
	integration.RequireUnleash(t)

	t.Run("401 from Unleash", func(t *testing.T) {
		assertFallback(t, "enabled-everywhere", "releash: unleash rejected the API token (HTTP 401); returning false",
			given(releash.OptionToken, "default:production.not-a-real-token"))
	})
	t.Run("403", func(t *testing.T) {
		url := integration.FakeUnleash{FeaturesStatus: http.StatusForbidden, RegisterStatus: http.StatusForbidden}.Start(t)
		assertFallback(t, "enabled-everywhere", "releash: unleash rejected the API token (HTTP 403); returning false",
			given(releash.OptionURL, url))
	})
}

// A flag in a project the token cannot see is covered by this case too:
// open-source Unleash has only the default project, so there is no fixture
// for it (spec 0001, test plan).
func TestDeployGateStaysClosedForUnknownFlag(t *testing.T) {
	integration.RequireUnleash(t)

	assertFallback(t, "no-such-flag", `releash: flag "no-such-flag" not found; returning false`)
}

func TestDeployGateStaysClosedWhenUnleashIsUnreachable(t *testing.T) {
	url := integration.UnreachableURL(t)

	start := time.Now()
	res := check(t, "enabled-everywhere", given(releash.OptionURL, url), given(releash.OptionTimeout, "2s"))

	assertAnswer(t, false, res)
	assert.Less(t, time.Since(start), 2*time.Second, "falls back without waiting for --timeout")
}

func TestDeployGateStaysClosedWhenUnleashNeverResponds(t *testing.T) {
	url := integration.UnresponsiveServer(t)

	start := time.Now()
	res := check(t, "enabled-everywhere", given(releash.OptionURL, url), given(releash.OptionTimeout, "1s"))
	elapsed := time.Since(start)

	assertAnswer(t, false, res)
	assert.GreaterOrEqual(t, elapsed, time.Second)
	assert.Less(t, elapsed, 1500*time.Millisecond, "finishes within --timeout")
}

func TestDeployGateStaysClosedOnMalformedResponse(t *testing.T) {
	for name, body := range map[string]string{
		"invalid JSON":    `{"features": [`,
		"wrong shape":     `{"features": "enabled-everywhere"}`,
		"not JSON at all": "<html>maintenance</html>",
	} {
		t.Run(name, func(t *testing.T) {
			url := integration.FakeUnleash{FeaturesBody: body}.Start(t)

			assertAnswer(t, false, check(t, "enabled-everywhere", given(releash.OptionURL, url)))
			res := check(t, "enabled-everywhere", given(releash.OptionURL, url), loud)
			assert.Equal(t, "false\n", res.stdout)
			assert.Regexp(t, `^releash: unleash sent a malformed feature response: .+; returning false\n$`, res.stderr)
		})
	}
}

func TestLoudExplainsWhyTheGateStayedClosed(t *testing.T) {
	integration.RequireUnleash(t)

	tests := []struct {
		name string
		opts func(t *testing.T) []option
		flag string
		// want is a regular expression for the one --loud line.
		want string
	}{
		{
			name: "unreachable",
			opts: func(t *testing.T) []option { return []option{given(releash.OptionURL, integration.UnreachableURL(t))} },
			want: `^releash: unleash unreachable: .*connection refused; returning false\n$`,
		},
		{
			name: "unresponsive",
			opts: func(t *testing.T) []option {
				return []option{given(releash.OptionURL, integration.UnresponsiveServer(t)), given(releash.OptionTimeout, "500ms")}
			},
			want: `^releash: unleash did not answer within 500ms; returning false\n$`,
		},
		{
			name: "rejected token",
			opts: func(*testing.T) []option {
				return []option{given(releash.OptionToken, "default:production.not-a-real-token")}
			},
			want: `^releash: unleash rejected the API token \(HTTP 401\); returning false\n$`,
		},
		{
			name: "server error",
			opts: func(t *testing.T) []option {
				return []option{given(releash.OptionURL, integration.FakeUnleash{FeaturesStatus: http.StatusServiceUnavailable}.Start(t))}
			},
			want: `^releash: unleash answered the feature fetch with HTTP 503; returning false\n$`,
		},
		{
			name: "malformed response",
			opts: func(t *testing.T) []option {
				return []option{given(releash.OptionURL, integration.FakeUnleash{FeaturesBody: "{"}.Start(t))}
			},
			want: `^releash: unleash sent a malformed feature response: .+; returning false\n$`,
		},
		{
			name: "unknown flag",
			opts: func(*testing.T) []option { return nil },
			flag: "no-such-flag",
			want: `^releash: flag "no-such-flag" not found; returning false\n$`,
		},
	}
	seen := map[string]string{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flag := tt.flag
			if flag == "" {
				flag = "enabled-everywhere"
			}
			opts := tt.opts(t)

			quiet := check(t, flag, opts...)
			noisy := check(t, flag, append(opts, loud)...)

			assertAnswer(t, false, quiet, "without --loud")
			assert.Equal(t, quiet.stdout, noisy.stdout, "--loud changes stdout")
			assert.Equal(t, quiet.code, noisy.code, "--loud changes the exit code")
			assert.Regexp(t, tt.want, noisy.stderr)
			for other, line := range seen {
				assert.NotEqual(t, line, noisy.stderr, "same line as %s", other)
			}
			seen[tt.name] = noisy.stderr
		})
	}
}

func TestRegionGatedRolloutOpensOnlyForEU(t *testing.T) {
	integration.RequireUnleash(t)

	assertAnswer(t, true, check(t, "region-eu-only", property("region=eu")), "region=eu")
	assertAnswer(t, false, check(t, "region-eu-only", property("region=us")), "region=us")
	assertAnswer(t, false, check(t, "region-eu-only"), "no region")
}

func TestUserTargetedRolloutOpensOnlyForAllowlistedUsers(t *testing.T) {
	integration.RequireUnleash(t)

	for user, want := range map[string]bool{"alice": true, "bob": true, "carol": false} {
		assertAnswer(t, want, check(t, "user-allowlist", given(releash.OptionUserID, user)), "--user-id %s", user)
	}
	assertAnswer(t, false, check(t, "user-allowlist"), "no user")
}

func TestScheduledReleaseFollowsTheClock(t *testing.T) {
	integration.RequireUnleash(t)
	at := func(d time.Duration) string { return time.Now().Add(d).UTC().Format("2006-01-02T15:04:05.000Z") }
	dated := func(operator string, d time.Duration) string {
		return integration.CreateFlag(t, integration.RolloutWith(integration.Constraint{ContextName: "currentTime", Operator: operator, Value: at(d)}))
	}
	// Minutes either side of now: only the time of the check can tell these
	// apart from each other.
	started := dated("DATE_AFTER", -5*time.Minute)
	notStarted := dated("DATE_AFTER", 5*time.Minute)
	notEnded := dated("DATE_BEFORE", 5*time.Minute)
	ended := dated("DATE_BEFORE", -5*time.Minute)

	assertAnswer(t, true, check(t, started), "start date 5 minutes ago")
	assertAnswer(t, false, check(t, notStarted), "start date in 5 minutes")
	assertAnswer(t, true, check(t, notEnded), "end date in 5 minutes")
	assertAnswer(t, false, check(t, ended), "end date 5 minutes ago")

	// The 2020 and 2099 fixtures pass with any clock this century, so they are
	// a coarse check only.
	assertAnswer(t, true, check(t, "date-after-past"))
	assertAnswer(t, true, check(t, "date-before-future"))
	assertAnswer(t, false, check(t, "date-after-future"))
	assertAnswer(t, false, check(t, "date-before-past"))
}

func TestScheduledReleaseCanBeCheckedAheadOfTime(t *testing.T) {
	integration.RequireUnleash(t)
	after2099 := given(releash.OptionCurrentTime, "2099-06-01T00:00:00Z")

	assertAnswer(t, true, check(t, "date-after-future", after2099))
	assertAnswer(t, false, check(t, "date-before-future", after2099))
	assertAnswer(t, false, check(t, "date-after-past", given(releash.OptionCurrentTime, "2019-12-31T23:59:59+00:00")))
}

func TestIPTargetedRolloutOpensForTheRunnerAddress(t *testing.T) {
	t.Skip("M4 PR 3: without --remote-address, ip-allowlist is true for the address releash detects")
}

func TestRemoteAddressOptionOverridesTheRunnerAddress(t *testing.T) {
	t.Skip("M4 PR 3: --remote-address outside the allowlist makes ip-allowlist false although the detected address is in it")
}

func TestTokenPicksTheEnvironment(t *testing.T) {
	integration.RequireUnleash(t)

	assertAnswer(t, true, check(t, "development-only", given(releash.OptionToken, integration.ClientToken(t, integration.Development))), "development token")
	assertAnswer(t, false, check(t, "development-only"), "production token")
}

func TestStandardContextOptionsReachEvaluation(t *testing.T) {
	integration.RequireUnleash(t)
	in := func(field, value string) string {
		return integration.CreateFlag(t, integration.RolloutWith(integration.Constraint{ContextName: field, Operator: "IN", Values: []string{value}}))
	}
	byEnv := in("environment", "staging")
	byApp := in("appName", "pipeline")
	bySession := in("sessionId", "s-1")

	tests := []struct {
		name         string
		flag         string
		match, other option
	}{
		{"--env", byEnv, given(releash.OptionEnv, "staging"), given(releash.OptionEnv, "qa")},
		{"--app-name", byApp, given(releash.OptionAppName, "pipeline"), given(releash.OptionAppName, "other")},
		{"--user-id", "user-allowlist", given(releash.OptionUserID, "alice"), given(releash.OptionUserID, "carol")},
		{"--session-id", bySession, given(releash.OptionSessionID, "s-1"), given(releash.OptionSessionID, "s-2")},
		{"--remote-address", "ip-allowlist", given(releash.OptionRemoteAddress, "10.1.2.3"), given(releash.OptionRemoteAddress, "8.8.8.8")},
		{"--current-time", "date-after-future", given(releash.OptionCurrentTime, "2100-01-01T00:00:00Z"), given(releash.OptionCurrentTime, "2098-01-01T00:00:00Z")},
		{"--properties", "region-eu-only", property("region=eu"), property("region=us")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertAnswer(t, true, check(t, tt.flag, tt.match), "matching value")
			assertAnswer(t, false, check(t, tt.flag, tt.other), "other value")
		})
	}
}

func TestNoStateCarriesOverBetweenChecks(t *testing.T) {
	integration.RequireUnleash(t)
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	require.Equal(t, tmp, os.TempDir(), "TMPDIR sets the temp directory")

	assertAnswer(t, true, check(t, "enabled-everywhere"), "first check")
	assertAnswer(t, false, check(t, "enabled-everywhere", given(releash.OptionToken, "default:production.not-a-real-token")), "bad token")
	assertAnswer(t, false, check(t, "enabled-everywhere", given(releash.OptionURL, integration.UnreachableURL(t))), "unreachable")

	entries, err := os.ReadDir(tmp)
	require.NoError(t, err)
	assert.Empty(t, entries, "files written to the temp directory")
}

func TestRegisterErrorDoesNotChangeTheAnswer(t *testing.T) {
	url := integration.FakeUnleash{
		FeaturesBody:   integration.EnabledFlagBody("enabled-everywhere"),
		RegisterStatus: http.StatusUnauthorized,
	}.Start(t)

	assertAnswer(t, true, check(t, "enabled-everywhere", given(releash.OptionURL, url)))
	res := check(t, "enabled-everywhere", given(releash.OptionURL, url), loud)
	assert.Equal(t, "true\n", res.stdout)
	assert.Equal(t, releash.ExitOK, res.code)
	assert.Equal(t, "releash: registering with unleash failed: HTTP 401; the answer is unaffected\n", res.stderr)
}

func TestUsageMetricsAreSentBeforeExit(t *testing.T) {
	t.Skip("M4 PR 2: a check sends usage metrics for the flag before it returns")
}

func TestFailingMetricsEndpointDoesNotDelayTheGate(t *testing.T) {
	t.Skip("M4 PR 2: a failing or hanging metrics endpoint changes neither stdout nor the exit code, and exit stays within --timeout")
}
