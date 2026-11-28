//go:build integration

package releash_test

// Variant scenarios from spec 0003, written the way a pipeline loads a
// variant after gating on check. They use the helpers of
// scenarios_integration_test.go and the variant fixture flags in
// unleash/fixtures.json. Payloads the fixtures do not hold, such as one with
// duplicate keys, are created with integration.CreateVariantFlag. Invalid
// JSON and unknown payload types cannot be stored through the Unleash 8.3.1
// Admin API, and a missing segment needs a response Unleash never sends, so
// those fallbacks are unit tests in variant_test.go.

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ivanklee86/releash/internal/integration"
	"github.com/ivanklee86/releash/internal/releash"
)

// variantOf runs releash variant flag against the local stack with the
// production token, changed by opts.
func variantOf(t *testing.T, flag string, opts ...option) result {
	t.Helper()
	o := stackOptions(t, flag, opts)
	var stdout, stderr bytes.Buffer
	r := releash.NewWithConfig(releash.Config{})
	r.Out, r.Err = &stdout, &stderr
	code := r.Variant(o)
	return result{stdout: stdout.String(), stderr: stderr.String(), code: code}
}

func inFormat(format string) option {
	return func(o *releash.VariantOptions) { o.Formats = append(o.Formats, format) }
}

func withPrefix(prefix string) option {
	return func(o *releash.VariantOptions) { o.Prefixes = append(o.Prefixes, prefix) }
}

// lines joins lines with a newline after each, as releash prints variables.
func lines(lines ...string) string {
	return strings.Join(lines, "\n") + "\n"
}

// disabledVariant is the fallback releash variant prints in dotenv under
// prefix.
func disabledVariant(prefix string) string {
	return lines(prefix+"ENABLED=false", prefix+"VARIANT=disabled")
}

// assertVariant asserts that res printed want, exited 0 and wrote nothing to
// stderr.
func assertVariant(t *testing.T, want string, res result, msgAndArgs ...any) {
	t.Helper()
	assert.Equal(t, want, res.stdout, msgAndArgs...)
	assert.Equal(t, releash.ExitOK, res.code, msgAndArgs...)
	assert.Empty(t, res.stderr, msgAndArgs...)
}

// defaultStickinessWarning is the --loud line for flag when it assigns
// variants by default stickiness and no --user-id or --session-id is set.
func defaultStickinessWarning(flag string) string {
	return fmt.Sprintf("releash: flag %q assigns variants by default stickiness, but neither --user-id nor --session-id is set, so the variant can differ between runs; pass --user-id, e.g. the repository name, to keep it sticky\n", flag)
}

const repo = "ivanklee86/releash"

func TestDeployConfigLoadsJSONPayloadAsVariables(t *testing.T) {
	integration.RequireUnleash(t)
	user := given(releash.OptionUserID, repo)

	assertVariant(t, lines(
		"RELEASH_VARIANT_JSON_PAYLOAD_ENABLED=true",
		"RELEASH_VARIANT_JSON_PAYLOAD_VARIANT=treatment",
		`RELEASH_VARIANT_JSON_PAYLOAD_PAYLOAD={"image":{"tag":"v2"},"replicas":3,"regions":["eu","us"]}`,
		"RELEASH_VARIANT_JSON_PAYLOAD_PAYLOAD__IMAGE__TAG=v2",
		"RELEASH_VARIANT_JSON_PAYLOAD_PAYLOAD__REGIONS__0=eu",
		"RELEASH_VARIANT_JSON_PAYLOAD_PAYLOAD__REGIONS__1=us",
		"RELEASH_VARIANT_JSON_PAYLOAD_PAYLOAD__REPLICAS=3",
	), variantOf(t, "variant-json-payload", user, loud), "dotenv")

	assertVariant(t, lines(
		"export CHECKOUT_ENABLED='true'",
		"export CHECKOUT_VARIANT='treatment'",
		`export CHECKOUT_PAYLOAD='{"image":{"tag":"v2"},"replicas":3,"regions":["eu","us"]}'`,
		"export CHECKOUT_PAYLOAD__IMAGE__TAG='v2'",
		"export CHECKOUT_PAYLOAD__REGIONS__0='eu'",
		"export CHECKOUT_PAYLOAD__REGIONS__1='us'",
		"export CHECKOUT_PAYLOAD__REPLICAS='3'",
	), variantOf(t, "variant-json-payload", user, inFormat("shell"), withPrefix("CHECKOUT_")), "shell with --prefix")
}

func TestDeployConfigLoadsScalarPayloadUnchanged(t *testing.T) {
	integration.RequireUnleash(t)

	tests := []struct {
		flag, variant, payload string
	}{
		{"variant-string-payload", "blue", "deploy blue"},
		{"variant-number-payload", "ratio", "1.50"},
		{"variant-csv-payload", "regions", "eu,us"},
	}
	for _, tt := range tests {
		t.Run(tt.flag, func(t *testing.T) {
			assertVariant(t, lines("P_ENABLED=true", "P_VARIANT="+tt.variant, "P_PAYLOAD="+tt.payload),
				variantOf(t, tt.flag, withPrefix("P_"), given(releash.OptionUserID, repo), loud))
		})
	}
}

func TestEnabledFlagWithoutVariantsLoadsNoConfig(t *testing.T) {
	integration.RequireUnleash(t)
	want := lines("RELEASH_ENABLED_EVERYWHERE_ENABLED=true", "RELEASH_ENABLED_EVERYWHERE_VARIANT=disabled")

	assertVariant(t, want, variantOf(t, "enabled-everywhere"))
	assertVariant(t, want, variantOf(t, "enabled-everywhere", loud), "with --loud")
}

func TestWeightedSplitGivesRepositoriesBothVariants(t *testing.T) {
	integration.RequireUnleash(t)
	control := lines("S_ENABLED=true", "S_VARIANT=control")
	treatment := lines("S_ENABLED=true", "S_VARIANT=treatment", "S_PAYLOAD=v2")

	seen := map[string]int{}
	for i := range 40 {
		res := variantOf(t, "variant-weighted-split", withPrefix("S_"), given(releash.OptionUserID, fmt.Sprintf("repo-%d", i)))
		assert.Contains(t, []string{control, treatment}, res.stdout, "repo-%d", i)
		assert.Equal(t, releash.ExitOK, res.code, "repo-%d", i)
		assert.Empty(t, res.stderr, "repo-%d", i)
		seen[res.stdout]++
	}
	// Both outputs are exact, so this also covers a variant without a payload,
	// which prints only ENABLED and VARIANT.
	assert.Positive(t, seen[control], "no repository got control")
	assert.Positive(t, seen[treatment], "no repository got treatment")
}

func TestFixedUserIDKeepsTheVariantAcrossRuns(t *testing.T) {
	integration.RequireUnleash(t)

	control := lines("S_ENABLED=true", "S_VARIANT=control")
	treatment := lines("S_ENABLED=true", "S_VARIANT=treatment", "S_PAYLOAD=v2")

	for i := range 5 {
		user := given(releash.OptionUserID, fmt.Sprintf("repo-%d", i))
		first := variantOf(t, "variant-weighted-split", withPrefix("S_"), user)
		assert.Contains(t, []string{control, treatment}, first.stdout, "repo-%d, run 1", i)
		assert.Equal(t, releash.ExitOK, first.code, "repo-%d, run 1", i)
		assert.Empty(t, first.stderr, "repo-%d, run 1", i)
		// Default stickiness falls back to remoteAddress, which is the same
		// for every run in this process, so each repeat comes from another
		// address and only --user-id can keep the variant.
		for run := range 3 {
			addr := given(releash.OptionRemoteAddress, fmt.Sprintf("192.0.2.%d", run+1))
			assertVariant(t, first.stdout, variantOf(t, "variant-weighted-split", withPrefix("S_"), user, addr), "repo-%d, run %d", i, run+2)
		}
	}
}

func TestCustomStickinessPropertyPinsTheVariant(t *testing.T) {
	integration.RequireUnleash(t)
	a := lines("C_ENABLED=true", "C_VARIANT=a")
	b := lines("C_ENABLED=true", "C_VARIANT=b")

	seen := map[string]int{}
	for i := range 20 {
		value := property(fmt.Sprintf("repo=org/repo-%d", i))
		first := variantOf(t, "variant-custom-stickiness", withPrefix("C_"), value)
		assert.Contains(t, []string{a, b}, first.stdout, "repo=org/repo-%d", i)
		assert.Equal(t, releash.ExitOK, first.code, "repo=org/repo-%d", i)
		assert.Empty(t, first.stderr, "repo=org/repo-%d", i)
		// A user ID does not change a variant assigned by another property.
		for _, user := range []string{"alice", "bob"} {
			res := variantOf(t, "variant-custom-stickiness", withPrefix("C_"), value, given(releash.OptionUserID, user))
			assertVariant(t, first.stdout, res, "repo=org/repo-%d, --user-id %s", i, user)
		}
		seen[first.stdout]++
	}
	assert.Positive(t, seen[a], "no repository got a")
	assert.Positive(t, seen[b], "no repository got b")

	res := variantOf(t, "variant-custom-stickiness", withPrefix("C_"), property("repo=org/repo-0"), loud)
	assert.Empty(t, res.stderr, "--loud warns although the flag has custom stickiness")
}

func TestLoudWarnsWhenTheVariantIsNotSticky(t *testing.T) {
	integration.RequireUnleash(t)
	control := lines("S_ENABLED=true", "S_VARIANT=control")
	treatment := lines("S_ENABLED=true", "S_VARIANT=treatment", "S_PAYLOAD=v2")

	res := variantOf(t, "variant-weighted-split", withPrefix("S_"), loud)
	assert.Contains(t, []string{control, treatment}, res.stdout)
	assert.Equal(t, releash.ExitOK, res.code)
	assert.Equal(t, defaultStickinessWarning("variant-weighted-split"), res.stderr)

	quiet := variantOf(t, "variant-weighted-split", withPrefix("S_"))
	assert.Empty(t, quiet.stderr, "warns without --loud")
	for _, sticky := range []option{given(releash.OptionUserID, repo), given(releash.OptionSessionID, "run-1")} {
		res := variantOf(t, "variant-weighted-split", withPrefix("S_"), sticky, loud)
		assert.Contains(t, []string{control, treatment}, res.stdout)
		assert.Empty(t, res.stderr, "warns with a user or session ID")
	}
}

func TestStrategyVariantsWinOverFeatureVariants(t *testing.T) {
	integration.RequireUnleash(t)
	rollout := integration.RolloutWith()
	rollout.Variants = []integration.Variant{{
		Name: "sv", Weight: 1000, WeightType: "variable", Stickiness: "default",
		Payload: &integration.Payload{Type: "json", Value: `{"tag": "strategy"}`},
	}}
	flag := integration.CreateVariantFlag(t, []integration.Variant{{
		Name: "fv", Weight: 1000, WeightType: "variable", Stickiness: "default",
		Payload: &integration.Payload{Type: "json", Value: `{"tag": "feature"}`},
	}}, rollout)

	assertVariant(t, lines("X_ENABLED=true", "X_VARIANT=sv", `X_PAYLOAD={"tag":"strategy"}`, "X_PAYLOAD__TAG=strategy"),
		variantOf(t, flag, withPrefix("X_"), given(releash.OptionUserID, repo), loud))
}

func TestVariantOfDisabledFlagFailsTheStep(t *testing.T) {
	integration.RequireUnleash(t)
	refusal := func(flag string) string {
		return fmt.Sprintf("releash: flag %q is disabled; gate on \"releash check %s\" before loading its variant\n", flag, flag)
	}

	tests := []struct {
		name string
		flag string
		opts []option
	}{
		{"turned off", "variant-disabled-everywhere", nil},
		// check and variant were given different context.
		{"off for the context", "region-eu-only", []option{property("region=us")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prefix := "RELEASH_" + strings.ToUpper(strings.ReplaceAll(tt.flag, "-", "_")) + "_"
			for _, opts := range [][]option{tt.opts, append(tt.opts, loud)} {
				res := variantOf(t, tt.flag, opts...)
				assert.Equal(t, disabledVariant(prefix), res.stdout)
				assert.Equal(t, refusal(tt.flag), res.stderr)
				assert.Equal(t, releash.ExitDisabled, res.code)
			}
		})
	}

	t.Run("with a command line error", func(t *testing.T) {
		res := variantOf(t, "variant-disabled-everywhere", given(releash.OptionTimeout, "soon"))
		assert.Empty(t, res.stdout, "a bad command line prints nothing")
		assert.Equal(t, releash.ExitUsage, res.code, "the lower code wins")
		assert.NotContains(t, res.stderr, "is disabled", "the flag is evaluated")
	})
}

func TestDeployConfigFallsBackToTheDisabledVariant(t *testing.T) {
	integration.RequireUnleash(t)
	payloadFlag := func(t *testing.T, payload integration.Payload) string {
		return integration.CreateVariantFlag(t, []integration.Variant{
			{Name: "t", Weight: 1000, WeightType: "variable", Stickiness: "default", Payload: &payload},
		}, integration.RolloutWith())
	}

	tests := []struct {
		name string
		// setup returns the flag and the options of the run.
		setup func(t *testing.T) (string, []option)
		// want is a regular expression for the one --loud line.
		want string
		// minElapsed and maxElapsed bound each run, without --loud and with
		// it. A zero maxElapsed means fastFallback.
		minElapsed, maxElapsed time.Duration
	}{
		{
			name:  "unknown flag",
			setup: func(*testing.T) (string, []option) { return "no-such-flag", nil },
			want:  `^releash: flag "no-such-flag" not found; returning disabled variant\n$`,
		},
		{
			name: "rejected token",
			setup: func(*testing.T) (string, []option) {
				return "variant-json-payload", []option{given(releash.OptionToken, "default:production.not-a-real-token")}
			},
			want: `^releash: unleash rejected the API token \(HTTP 401\); returning disabled variant\n$`,
		},
		{
			name: "unreachable",
			setup: func(t *testing.T) (string, []option) {
				return "variant-json-payload", []option{given(releash.OptionURL, integration.UnreachableURL(t))}
			},
			want: `^releash: unleash unreachable: .*connection refused; returning disabled variant\n$`,
		},
		{
			name: "unresponsive",
			setup: func(t *testing.T) (string, []option) {
				return "variant-json-payload", []option{given(releash.OptionURL, integration.UnresponsiveServer(t)), given(releash.OptionTimeout, "500ms")}
			},
			want:       `^releash: unleash did not answer within 500ms; returning disabled variant\n$`,
			minElapsed: 500 * time.Millisecond,
			maxElapsed: time.Second,
		},
		{
			name: "server error",
			setup: func(t *testing.T) (string, []option) {
				return "variant-json-payload", []option{given(releash.OptionURL, integration.FakeUnleash{FeaturesStatus: http.StatusServiceUnavailable}.Start(t))}
			},
			want: `^releash: unleash answered the feature fetch with HTTP 503; returning disabled variant\n$`,
		},
		{
			name: "malformed response",
			setup: func(t *testing.T) (string, []option) {
				return "variant-json-payload", []option{given(releash.OptionURL, integration.FakeUnleash{FeaturesBody: "{"}.Start(t))}
			},
			want: `^releash: unleash sent a malformed feature response: .+; returning disabled variant\n$`,
		},
		{
			// The SDK reports an evaluation error and the flag is not enabled,
			// so this also shows that the error wins over exit 6.
			name: "strategy variants without a groupId",
			setup: func(t *testing.T) (string, []option) {
				return integration.CreateFlag(t, integration.Strategy{Name: "default", Variants: []integration.Variant{
					{Name: "sv", Weight: 1000, WeightType: "variable", Stickiness: "default"},
				}}), nil
			},
			want: `^releash: unleash could not evaluate flag "scenario-[^"]+": invalid groupId type for feature scenario-\S+; returning disabled variant\n$`,
		},
		{
			name: "duplicate key",
			setup: func(t *testing.T) (string, []option) {
				return payloadFlag(t, integration.Payload{Type: "json", Value: `{"a": 1, "a": 2}`}), nil
			},
			want: `^releash: payload of variant "t" has duplicate key "a"; returning disabled variant\n$`,
		},
		{
			name: "colliding keys",
			setup: func(t *testing.T) (string, []option) {
				return payloadFlag(t, integration.Payload{Type: "json", Value: `{"a-b": 1, "a_b": 2}`}), nil
			},
			want: `^releash: payload of variant "t" has colliding keys "a-b" and "a_b"; returning disabled variant\n$`,
		},
		{
			name: "newline in dotenv",
			setup: func(t *testing.T) (string, []option) {
				return payloadFlag(t, integration.Payload{Type: "string", Value: "line 1\nline 2"}), nil
			},
			want: `^releash: variable F_PAYLOAD of variant "t" contains a newline, which the dotenv format cannot represent; returning disabled variant\n$`,
		},
	}
	// A fallback that needs no timeout must not wait for one: fastFallback is
	// well under the default --timeout.
	const fastFallback = 2 * time.Second
	seen := map[string]string{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flag, opts := tt.setup(t)
			opts = append(opts, withPrefix("F_"), given(releash.OptionUserID, repo))
			maxElapsed := tt.maxElapsed
			if maxElapsed == 0 {
				maxElapsed = fastFallback
			}
			timed := func(opts ...option) result {
				t.Helper()
				start := time.Now()
				res := variantOf(t, flag, opts...)
				elapsed := time.Since(start)
				assert.GreaterOrEqual(t, elapsed, tt.minElapsed, "waits for --timeout")
				assert.Less(t, elapsed, maxElapsed, "finishes within its bound")
				return res
			}

			quiet := timed(opts...)
			noisy := timed(append(opts, loud)...)

			assertVariant(t, disabledVariant("F_"), quiet, "without --loud")
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

func TestShellFormatCarriesAMultiLinePayload(t *testing.T) {
	integration.RequireUnleash(t)
	flag := integration.CreateVariantFlag(t, []integration.Variant{{
		Name: "t", Weight: 1000, WeightType: "variable", Stickiness: "default",
		Payload: &integration.Payload{Type: "string", Value: "line 1\nit's $HOME"},
	}}, integration.RolloutWith())

	assertVariant(t, lines("export M_ENABLED='true'", "export M_VARIANT='t'", "export M_PAYLOAD='line 1", `it'\''s $HOME'`),
		variantOf(t, flag, withPrefix("M_"), inFormat("shell"), given(releash.OptionUserID, repo)))
}

// The run uses an appName of its own and a fixture flag, for the reasons
// given at TestUsageMetricsAreSentBeforeExit.
func TestVariantUsageMetricsCountTheVariant(t *testing.T) {
	integration.RequireUnleash(t)
	app := fmt.Sprintf("releash-variant-scenario-%d", time.Now().UnixNano())

	res := variantOf(t, "variant-json-payload", given(releash.OptionAppName, app), given(releash.OptionUserID, repo), loud)
	assert.Equal(t, releash.ExitOK, res.code)
	assert.Empty(t, res.stderr, "--loud reports no metrics problem")

	usage := integration.WaitForUsage(t, "variant-json-payload", integration.Production, app)
	assert.Equal(t, integration.Usage{Yes: 1, Variants: map[string]int{"treatment": 1}}, usage)
}
