package releash

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Cases for releash variant against fake Unleash servers, from the test plan
// in spec 0003. internal/variant covers the variable names and formats in
// detail; these check that an evaluated variant reaches them, and the exit
// code and stderr contract around them:
//   - a json payload prints spec 0003's example in dotenv and shell; string,
//     number and csv payloads print only PAYLOAD; a variant without a
//     payload prints only ENABLED and VARIANT; --prefix replaces the default
//     prefix; nothing reaches stderr, even with --loud
//   - an enabled flag without variants prints ENABLED=true and
//     VARIANT=disabled, exits 0 and is silent with --loud
//   - a disabled flag, and one that is on but not for the context, print the
//     disabled variant in the requested format, write the error naming
//     releash check without --loud and exit 6
//   - fallbacks, each the disabled variant with exit 0, silent without --loud
//     and with its own --loud line: every fallback of spec 0001 (unknown
//     flag, 401, 403, other statuses, a 304, a malformed response, a flag the
//     SDK cannot evaluate, unreachable, no answer within --timeout), strategy
//     variants the SDK cannot evaluate, invalid JSON, colliding and duplicate
//     keys, and a newline in dotenv; the same newline survives in shell
//   - an evaluation error takes precedence over exit 6
//   - with a fixed --user-id, repeated runs get the same variant, and users
//     spread across variants; a custom stickiness property maps each value
//     to one variant every time, for feature and strategy variants
//   - --loud warns about default stickiness without --user-id or
//     --session-id, reading the stickiness the SDK uses: the first feature
//     variant's, and a strategy's stickiness parameter over its variants'
//   - a variant evaluation sends metrics with the variant count before it
//     returns; a fallback and exit 6 do not wait for metrics
//   - validation: an invalid or repeated --format or --prefix exits 2, and
//     every error prints the fallback in the format and prefix given, or
//     nothing when there is no single one; a command line error with a
//     disabled flag exits with the command line error's code, without
//     contacting Unleash
//   - VariantFallback picks the fallback's format and prefix as spec 0003's
//     scan rules say

// fakeVariant is a variant in a fake Client API response.
type fakeVariant struct {
	Name       string       `json:"name"`
	Weight     int          `json:"weight"`
	WeightType string       `json:"weightType"`
	Stickiness string       `json:"stickiness"`
	Payload    *fakePayload `json:"payload,omitempty"`
}

type fakePayload struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type fakeStrategy struct {
	Name       string            `json:"name"`
	Parameters map[string]string `json:"parameters,omitempty"`
	Segments   []int             `json:"segments,omitempty"`
	Variants   []fakeVariant     `json:"variants,omitempty"`
}

type fakeFeature struct {
	Name       string         `json:"name"`
	Enabled    bool           `json:"enabled"`
	Strategies []fakeStrategy `json:"strategies"`
	Variants   []fakeVariant  `json:"variants,omitempty"`
}

// encode returns f as JSON for featuresJSON.
func (f fakeFeature) encode(t *testing.T) string {
	t.Helper()
	b, err := json.Marshal(f)
	require.NoError(t, err)
	return string(b)
}

// withVariants returns a flag that is on for everyone and has variants as
// feature variants.
func withVariants(name string, variants ...fakeVariant) fakeFeature {
	return fakeFeature{Name: name, Enabled: true, Strategies: []fakeStrategy{{Name: "default"}}, Variants: variants}
}

// only returns a variant that gets all traffic, with the given payload type
// and value; an empty type means no payload.
func only(name, payloadType, value string) fakeVariant {
	v := fakeVariant{Name: name, Weight: 1000, WeightType: "variable", Stickiness: "default"}
	if payloadType != "" {
		v.Payload = &fakePayload{Type: payloadType, Value: value}
	}
	return v
}

// split returns variants a and b with half the traffic each, assigned by
// stickiness.
func split(stickiness string) []fakeVariant {
	return []fakeVariant{
		{Name: "a", Weight: 500, WeightType: "variable", Stickiness: stickiness},
		{Name: "b", Weight: 500, WeightType: "variable", Stickiness: stickiness},
	}
}

// rolloutWith returns a flexibleRollout strategy for everyone, with
// stickiness as its stickiness parameter, that assigns variants.
func rolloutWith(stickiness string, variants ...fakeVariant) fakeStrategy {
	return fakeStrategy{Name: "flexibleRollout", Parameters: map[string]string{"rollout": "100", "groupId": "g", "stickiness": stickiness}, Variants: variants}
}

// grouped returns a default strategy with a groupId and no stickiness
// parameter, that assigns variants by their own stickiness. Unleash does not
// create one, but the SDK evaluates it; a flexibleRollout without stickiness
// makes the SDK panic.
func grouped(variants ...fakeVariant) fakeStrategy {
	return fakeStrategy{Name: "default", Parameters: map[string]string{"groupId": "g"}, Variants: variants}
}

// runVariant calls Variant with opts against url and returns stdout, stderr
// and the exit code. Each edit may change opts after the URL, token, flag and
// --loud are set.
func runVariant(t *testing.T, url, flag string, loud bool, edits ...func(*VariantOptions)) (string, string, int) {
	t.Helper()
	opts := VariantOptions{CheckOptions: validOptions()}
	opts.URL, opts.Token, opts.Flag, opts.Loud = url, testToken, flag, loud
	for _, e := range edits {
		e(&opts)
	}
	var stdout, stderr bytes.Buffer
	r := New()
	r.Out, r.Err = &stdout, &stderr
	code := r.Variant(opts)
	return stdout.String(), stderr.String(), code
}

// checkEdits applies CheckOptions edits to VariantOptions.
func checkEdits(edits ...func(*CheckOptions)) func(*VariantOptions) {
	return func(o *VariantOptions) { all(edits...)(&o.CheckOptions) }
}

func formats(values ...string) func(*VariantOptions) {
	return func(o *VariantOptions) { o.Formats = append(o.Formats, values...) }
}

func prefixes(values ...string) func(*VariantOptions) {
	return func(o *VariantOptions) { o.Prefixes = append(o.Prefixes, values...) }
}

func userID(id string) func(*VariantOptions) {
	return checkEdits(set(OptionUserID, func(o *CheckOptions) *string { return &o.UserID }, id))
}

func sessionID(id string) func(*VariantOptions) {
	return checkEdits(set(OptionSessionID, func(o *CheckOptions) *string { return &o.SessionID }, id))
}

const specPayload = `{"image": {"tag": "v2"}, "replicas": 3, "regions": ["eu", "us"]}`

func TestVariantPrintsTheEvaluatedVariant(t *testing.T) {
	url := fakeUnleash(t, serve(http.StatusOK, featuresJSON(
		withVariants("checkout-redesign", only("treatment", "json", specPayload)).encode(t),
		withVariants("string", only("s", "string", "it's $HOME")).encode(t),
		withVariants("number", only("n", "number", "1.50")).encode(t),
		withVariants("csv", only("c", "csv", "a,b,c")).encode(t),
		withVariants("bare", only("plain", "", "")).encode(t),
		fakeFeature{Name: "strategy-variant", Enabled: true, Strategies: []fakeStrategy{rolloutWith("default", only("sv", "string", "from the strategy"))}, Variants: []fakeVariant{only("fv", "string", "from the flag")}}.encode(t),
	)), nil)

	tests := []struct {
		name  string
		flag  string
		edits []func(*VariantOptions)
		want  string
	}{
		{name: "json payload", flag: "checkout-redesign", want: `RELEASH_CHECKOUT_REDESIGN_ENABLED=true
RELEASH_CHECKOUT_REDESIGN_VARIANT=treatment
RELEASH_CHECKOUT_REDESIGN_PAYLOAD={"image":{"tag":"v2"},"replicas":3,"regions":["eu","us"]}
RELEASH_CHECKOUT_REDESIGN_PAYLOAD__IMAGE__TAG=v2
RELEASH_CHECKOUT_REDESIGN_PAYLOAD__REGIONS__0=eu
RELEASH_CHECKOUT_REDESIGN_PAYLOAD__REGIONS__1=us
RELEASH_CHECKOUT_REDESIGN_PAYLOAD__REPLICAS=3
`},
		{name: "json payload in shell", flag: "checkout-redesign", edits: []func(*VariantOptions){formats("shell")}, want: `export RELEASH_CHECKOUT_REDESIGN_ENABLED='true'
export RELEASH_CHECKOUT_REDESIGN_VARIANT='treatment'
export RELEASH_CHECKOUT_REDESIGN_PAYLOAD='{"image":{"tag":"v2"},"replicas":3,"regions":["eu","us"]}'
export RELEASH_CHECKOUT_REDESIGN_PAYLOAD__IMAGE__TAG='v2'
export RELEASH_CHECKOUT_REDESIGN_PAYLOAD__REGIONS__0='eu'
export RELEASH_CHECKOUT_REDESIGN_PAYLOAD__REGIONS__1='us'
export RELEASH_CHECKOUT_REDESIGN_PAYLOAD__REPLICAS='3'
`},
		{name: "--prefix", flag: "checkout-redesign", edits: []func(*VariantOptions){prefixes("CHECKOUT_")}, want: `CHECKOUT_ENABLED=true
CHECKOUT_VARIANT=treatment
CHECKOUT_PAYLOAD={"image":{"tag":"v2"},"replicas":3,"regions":["eu","us"]}
CHECKOUT_PAYLOAD__IMAGE__TAG=v2
CHECKOUT_PAYLOAD__REGIONS__0=eu
CHECKOUT_PAYLOAD__REGIONS__1=us
CHECKOUT_PAYLOAD__REPLICAS=3
`},
		{name: "string payload", flag: "string", want: "RELEASH_STRING_ENABLED=true\nRELEASH_STRING_VARIANT=s\nRELEASH_STRING_PAYLOAD=it's $HOME\n"},
		{name: "string payload in shell", flag: "string", edits: []func(*VariantOptions){formats("shell")}, want: "export RELEASH_STRING_ENABLED='true'\nexport RELEASH_STRING_VARIANT='s'\nexport RELEASH_STRING_PAYLOAD='it'\\''s $HOME'\n"},
		{name: "number payload keeps its text", flag: "number", want: "RELEASH_NUMBER_ENABLED=true\nRELEASH_NUMBER_VARIANT=n\nRELEASH_NUMBER_PAYLOAD=1.50\n"},
		{name: "csv payload", flag: "csv", want: "RELEASH_CSV_ENABLED=true\nRELEASH_CSV_VARIANT=c\nRELEASH_CSV_PAYLOAD=a,b,c\n"},
		{name: "no payload", flag: "bare", want: "RELEASH_BARE_ENABLED=true\nRELEASH_BARE_VARIANT=plain\n"},
		{name: "strategy variants win over feature variants", flag: "strategy-variant", want: "RELEASH_STRATEGY_VARIANT_ENABLED=true\nRELEASH_STRATEGY_VARIANT_VARIANT=sv\nRELEASH_STRATEGY_VARIANT_PAYLOAD=from the strategy\n"},
	}
	for _, tt := range tests {
		for _, loud := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/loud=%t", tt.name, loud), func(t *testing.T) {
				// A fixed user keeps the default-stickiness warning away.
				stdout, stderr, code := runVariant(t, url, tt.flag, loud, append([]func(*VariantOptions){userID("repo/a")}, tt.edits...)...)

				assert.Equal(t, ExitOK, code, "exit code")
				assert.Equal(t, tt.want, stdout, "stdout")
				assert.Empty(t, stderr, "stderr")
			})
		}
	}
}

func TestVariantOfAnEnabledFlagWithoutVariantsIsAnAnswer(t *testing.T) {
	url := fakeUnleash(t, serve(http.StatusOK, featuresJSON(enabledFlag)), nil)

	for _, loud := range []bool{false, true} {
		t.Run(fmt.Sprintf("loud=%t", loud), func(t *testing.T) {
			stdout, stderr, code := runVariant(t, url, "on", loud)

			assert.Equal(t, ExitOK, code, "exit code")
			assert.Equal(t, "RELEASH_ON_ENABLED=true\nRELEASH_ON_VARIANT=disabled\n", stdout, "stdout")
			assert.Empty(t, stderr, "stderr")
		})
	}
}

func TestVariantOfADisabledFlagExits6(t *testing.T) {
	url := fakeUnleash(t, serve(http.StatusOK, featuresJSON(
		fakeFeature{Name: "off", Enabled: false, Strategies: []fakeStrategy{{Name: "default"}}, Variants: []fakeVariant{only("t", "string", "x")}}.encode(t),
		fakeFeature{Name: "alice-only", Enabled: true, Strategies: []fakeStrategy{{Name: "userWithId", Parameters: map[string]string{"userIds": "alice"}}}, Variants: []fakeVariant{only("t", "string", "x")}}.encode(t),
	)), nil)

	tests := []struct {
		flag   string
		format string
		want   string
	}{
		{"off", "dotenv", "RELEASH_OFF_ENABLED=false\nRELEASH_OFF_VARIANT=disabled\n"},
		{"off", "shell", "export RELEASH_OFF_ENABLED='false'\nexport RELEASH_OFF_VARIANT='disabled'\n"},
		{"alice-only", "dotenv", "RELEASH_ALICE_ONLY_ENABLED=false\nRELEASH_ALICE_ONLY_VARIANT=disabled\n"},
	}
	for _, tt := range tests {
		for _, loud := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%s/loud=%t", tt.flag, tt.format, loud), func(t *testing.T) {
				stdout, stderr, code := runVariant(t, url, tt.flag, loud, formats(tt.format), userID("bob"))

				assert.Equal(t, ExitDisabled, code, "exit code")
				assert.Equal(t, tt.want, stdout, "stdout")
				assert.Equal(t, fmt.Sprintf("releash: flag %q is disabled; gate on \"releash check %s\" before loading its variant\n", tt.flag, tt.flag), stderr, "stderr")
			})
		}
	}
}

func TestVariantFallsBackToTheDisabledVariant(t *testing.T) {
	const malformed, invalidJSON = "malformed", "invalid JSON"
	flags := func(t *testing.T) string {
		return featuresJSON(
			withVariants("on", only("t", "string", "x")).encode(t),
			fakeFeature{Name: "unevaluable", Enabled: true, Strategies: []fakeStrategy{{Name: "default", Segments: []int{99}}}, Variants: []fakeVariant{only("t", "string", "x")}}.encode(t),
			fakeFeature{Name: "no-group-id", Enabled: true, Strategies: []fakeStrategy{{Name: "default", Variants: []fakeVariant{only("t", "string", "x")}}}}.encode(t),
			withVariants("broken-json", only("t", "json", `{"a": `)).encode(t),
			withVariants("colliding", only("t", "json", `{"a-b": 1, "a_b": 2}`)).encode(t),
			withVariants("duplicate", only("t", "json", `{"a": 1, "a": 2}`)).encode(t),
			withVariants("multi-line", only("t", "string", "line 1\nline 2")).encode(t),
		)
	}
	tests := []struct {
		name     string
		features func(*testing.T) http.HandlerFunc
		flag     string
		wantLine string
		// alwaysReported is set for the one fallback reported without --loud
		// (ADR 0009); every other fallback is silent without it.
		alwaysReported bool
	}{
		{name: "unknown flag", flag: "missing", wantLine: `releash: flag "missing" not found; returning disabled variant`, alwaysReported: true},
		{name: "no flags at all", features: func(*testing.T) http.HandlerFunc { return serve(http.StatusOK, featuresJSON()) }, flag: "missing", wantLine: `releash: flag "missing" not found (unleash returned no flags for this token's project and environment); returning disabled variant`, alwaysReported: true},
		{name: "401", features: func(*testing.T) http.HandlerFunc { return serve(http.StatusUnauthorized, `{}`) }, wantLine: "releash: unleash rejected the API token (HTTP 401); returning disabled variant"},
		{name: "403", features: func(*testing.T) http.HandlerFunc { return serve(http.StatusForbidden, `{}`) }, wantLine: "releash: unleash rejected the API token (HTTP 403); returning disabled variant"},
		{name: "500", features: func(*testing.T) http.HandlerFunc { return serve(http.StatusInternalServerError, ``) }, wantLine: "releash: unleash answered the feature fetch with HTTP 500; returning disabled variant"},
		{name: "304 on the first fetch", features: func(*testing.T) http.HandlerFunc { return serve(http.StatusNotModified, ``) }, wantLine: "releash: unleash answered the feature fetch with HTTP 304; returning disabled variant"},
		{name: "malformed response", features: func(*testing.T) http.HandlerFunc { return serve(http.StatusOK, `{"features": [`) }, wantLine: malformed},
		// The SDK reports the flag disabled as well, so this also shows the
		// evaluation error winning over exit 6.
		{name: "missing segment", flag: "unevaluable", wantLine: `releash: unleash could not evaluate flag "unevaluable": segment does not exist; returning disabled variant`},
		{name: "strategy variants without a groupId", flag: "no-group-id", wantLine: `releash: unleash could not evaluate flag "no-group-id": invalid groupId type for feature no-group-id; returning disabled variant`},
		{name: "invalid JSON payload", flag: "broken-json", wantLine: invalidJSON},
		{name: "colliding keys", flag: "colliding", wantLine: `releash: payload of variant "t" has colliding keys "a-b" and "a_b"; returning disabled variant`},
		{name: "duplicate key", flag: "duplicate", wantLine: `releash: payload of variant "t" has duplicate key "a"; returning disabled variant`},
		{name: "newline in dotenv", flag: "multi-line", wantLine: `releash: variable RELEASH_MULTI_LINE_PAYLOAD of variant "t" contains a newline, which the dotenv format cannot represent; returning disabled variant`},
	}
	for _, tt := range tests {
		features := serve(http.StatusOK, flags(t))
		if tt.features != nil {
			features = tt.features(t)
		}
		flag := tt.flag
		if flag == "" {
			flag = "on"
		}
		url := fakeUnleash(t, features, nil)
		for _, loud := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/loud=%t", tt.name, loud), func(t *testing.T) {
				start := time.Now()
				stdout, stderr, code := runVariant(t, url, flag, loud, userID("repo/a"))

				assert.Equal(t, ExitOK, code, "exit code")
				assert.Equal(t, fmt.Sprintf("RELEASH_%s_ENABLED=false\nRELEASH_%s_VARIANT=disabled\n", envName(flag), envName(flag)), stdout, "stdout")
				switch {
				case !loud && !tt.alwaysReported:
					assert.Empty(t, stderr, "stderr")
				case tt.wantLine == malformed:
					assert.Regexp(t, `^releash: unleash sent a malformed feature response: .+; returning disabled variant\n$`, stderr, "stderr")
				case tt.wantLine == invalidJSON:
					assert.Regexp(t, `^releash: payload of variant "t" is not valid JSON: .+; returning disabled variant\n$`, stderr, "stderr")
				default:
					assert.Equal(t, tt.wantLine+"\n", stderr, "stderr")
				}
				assert.Less(t, time.Since(start), time.Second, "falls back without waiting for the timeout or metrics")
			})
		}
	}
}

// envName is flag as the default prefix spells it, for flag names made of
// lower-case letters and dashes.
func envName(flag string) string {
	return strings.ToUpper(strings.ReplaceAll(flag, "-", "_"))
}

func TestVariantKeepsANewlineInShell(t *testing.T) {
	url := fakeUnleash(t, serve(http.StatusOK, featuresJSON(withVariants("multi-line", only("t", "string", "line 1\nline 2")).encode(t))), nil)

	stdout, stderr, code := runVariant(t, url, "multi-line", true, formats("shell"), userID("repo/a"))

	assert.Equal(t, ExitOK, code, "exit code")
	assert.Equal(t, "export RELEASH_MULTI_LINE_ENABLED='true'\nexport RELEASH_MULTI_LINE_VARIANT='t'\nexport RELEASH_MULTI_LINE_PAYLOAD='line 1\nline 2'\n", stdout, "stdout")
	assert.Empty(t, stderr, "stderr")
}

func TestVariantFallsBackWhenUnleashIsUnreachable(t *testing.T) {
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())

	stdout, stderr, code := runVariant(t, "http://"+addr+"/api", "on", true, formats("shell"))

	assert.Equal(t, ExitOK, code)
	assert.Equal(t, "export RELEASH_ON_ENABLED='false'\nexport RELEASH_ON_VARIANT='disabled'\n", stdout)
	assert.Equal(t, fmt.Sprintf("releash: unleash unreachable: dial tcp %s: connect: connection refused; returning disabled variant\n", addr), stderr)
}

func TestVariantFallsBackWithinTheTimeoutWhenUnleashNeverAnswers(t *testing.T) {
	url := unresponsiveServer(t)

	for _, loud := range []bool{false, true} {
		t.Run(fmt.Sprintf("loud=%t", loud), func(t *testing.T) {
			start := time.Now()
			stdout, stderr, code := runVariant(t, url, "on", loud, checkEdits(set(OptionTimeout, timeout, "500ms")))
			elapsed := time.Since(start)

			assert.Equal(t, ExitOK, code)
			assert.Equal(t, "RELEASH_ON_ENABLED=false\nRELEASH_ON_VARIANT=disabled\n", stdout)
			want := ""
			if loud {
				want = "releash: unleash did not answer within 500ms; returning disabled variant\n"
			}
			assert.Equal(t, want, stderr)
			assert.GreaterOrEqual(t, elapsed, 500*time.Millisecond)
			assert.Less(t, elapsed, 750*time.Millisecond, "the whole call, Close included, stays within --timeout")
		})
	}
}

// variantName returns the VARIANT value in dotenv output.
func variantName(t *testing.T, stdout string) string {
	t.Helper()
	for line := range strings.SplitSeq(stdout, "\n") {
		if name, value, ok := strings.Cut(line, "="); ok && strings.HasSuffix(name, "_VARIANT") {
			return value
		}
	}
	t.Fatalf("no VARIANT in %q", stdout)
	return ""
}

func TestVariantIsStickyForAFixedUserID(t *testing.T) {
	url := fakeUnleash(t, serve(http.StatusOK, featuresJSON(withVariants("split", split("default")...).encode(t))), nil)

	seen := map[string]bool{}
	for _, user := range []string{"repo/a", "repo/b", "repo/c", "repo/d", "repo/e", "repo/f"} {
		first, _, code := runVariant(t, url, "split", false, userID(user))
		require.Equal(t, ExitOK, code)
		for range 2 {
			again, _, _ := runVariant(t, url, "split", false, userID(user))
			assert.Equal(t, variantName(t, first), variantName(t, again), "user %s", user)
		}
		seen[variantName(t, first)] = true
	}
	assert.Equal(t, map[string]bool{"a": true, "b": true}, seen, "users spread across both variants")
}

func TestVariantIsStickyForACustomProperty(t *testing.T) {
	url := fakeUnleash(t, serve(http.StatusOK, featuresJSON(
		withVariants("feature-split", split("repo")...).encode(t),
		fakeFeature{Name: "strategy-split", Enabled: true, Strategies: []fakeStrategy{rolloutWith("repo", split("default")...)}}.encode(t),
	)), nil)

	for _, flag := range []string{"feature-split", "strategy-split"} {
		t.Run(flag, func(t *testing.T) {
			seen := map[string]bool{}
			for _, repo := range []string{"r1", "r2", "r3", "r4", "r5", "r6"} {
				// A changing userId shows that only the property decides.
				first, _, code := runVariant(t, url, flag, false, userID("u-"+repo), checkEdits(properties("repo="+repo)))
				require.Equal(t, ExitOK, code)
				again, _, _ := runVariant(t, url, flag, false, userID("other-"+repo), checkEdits(properties("repo="+repo)))
				assert.Equal(t, variantName(t, first), variantName(t, again), "repo %s", repo)
				seen[variantName(t, first)] = true
			}
			assert.Equal(t, map[string]bool{"a": true, "b": true}, seen, "repositories spread across both variants")
		})
	}
}

func TestVariantWarnsAboutDefaultStickiness(t *testing.T) {
	url := fakeUnleash(t, serve(http.StatusOK, featuresJSON(
		withVariants("feature-default", split("default")...).encode(t),
		withVariants("feature-empty", split("")...).encode(t),
		withVariants("feature-custom", split("repo")...).encode(t),
		fakeFeature{Name: "strategy-param-default", Enabled: true, Strategies: []fakeStrategy{rolloutWith("default", split("repo")...)}}.encode(t),
		fakeFeature{Name: "strategy-param-custom", Enabled: true, Strategies: []fakeStrategy{rolloutWith("repo", split("default")...)}}.encode(t),
		fakeFeature{Name: "strategy-no-param", Enabled: true, Strategies: []fakeStrategy{grouped(split("default")...)}}.encode(t),
		fakeFeature{Name: "strategy-no-param-custom", Enabled: true, Strategies: []fakeStrategy{grouped(split("repo")...)}}.encode(t),
	)), nil)
	warning := func(flag string) string {
		return fmt.Sprintf("releash: flag %q assigns variants by default stickiness, but neither --user-id nor --session-id is set, so the variant can differ between runs; pass --user-id, e.g. the repository name, to keep it sticky\n", flag)
	}

	tests := []struct {
		name  string
		flag  string
		edits []func(*VariantOptions)
		warn  bool
	}{
		{name: "feature variants with default stickiness", flag: "feature-default", warn: true},
		{name: "feature variants with empty stickiness", flag: "feature-empty", warn: true},
		{name: "feature variants with --user-id", flag: "feature-default", edits: []func(*VariantOptions){userID("repo/a")}},
		{name: "feature variants with --session-id", flag: "feature-default", edits: []func(*VariantOptions){sessionID("s-1")}},
		{name: "feature variants with custom stickiness", flag: "feature-custom", edits: []func(*VariantOptions){checkEdits(properties("repo=r1"))}},
		{name: "strategy stickiness default over custom variants", flag: "strategy-param-default", warn: true},
		{name: "strategy stickiness custom over default variants", flag: "strategy-param-custom", edits: []func(*VariantOptions){checkEdits(properties("repo=r1"))}},
		{name: "strategy without stickiness and default variants", flag: "strategy-no-param", warn: true},
		{name: "strategy without stickiness and custom variants", flag: "strategy-no-param-custom", edits: []func(*VariantOptions){checkEdits(properties("repo=r1"))}},
	}
	for _, tt := range tests {
		for _, loud := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/loud=%t", tt.name, loud), func(t *testing.T) {
				stdout, stderr, code := runVariant(t, url, tt.flag, loud, tt.edits...)

				assert.Equal(t, ExitOK, code, "exit code")
				assert.Contains(t, []string{"a", "b"}, variantName(t, stdout), "stdout")
				want := ""
				if loud && tt.warn {
					want = warning(tt.flag)
				}
				assert.Equal(t, want, stderr, "stderr")
			})
		}
	}
}

// variantMetrics records the toggles of each metrics request a fake Unleash
// received, with their variant counts. It is safe for concurrent use.
type variantMetrics struct {
	mu    sync.Mutex
	posts []map[string]toggleCount
}

type toggleCount struct {
	Yes      int            `json:"yes"`
	No       int            `json:"no"`
	Variants map[string]int `json:"variants"`
}

func (m *variantMetrics) received() []map[string]toggleCount {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]map[string]toggleCount(nil), m.posts...)
}

// fakeWithVariantMetrics starts a server that serves features, records
// every metrics request in rec, and answers it with metrics. It returns the
// API URL.
func fakeWithVariantMetrics(t *testing.T, features string, rec *variantMetrics, metrics http.HandlerFunc) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/client/features":
			serve(http.StatusOK, features)(w, r)
		case "/api/client/metrics":
			var body struct {
				Bucket struct {
					Toggles map[string]toggleCount `json:"toggles"`
				} `json:"bucket"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decoding metrics request: %v", err)
			}
			rec.mu.Lock()
			rec.posts = append(rec.posts, body.Bucket.Toggles)
			rec.mu.Unlock()
			metrics(w, r)
		default:
			w.WriteHeader(http.StatusAccepted)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/api"
}

func TestVariantSendsUsageMetricsWithTheVariantBeforeReturning(t *testing.T) {
	rec := &variantMetrics{}
	url := fakeWithVariantMetrics(t, featuresJSON(withVariants("on", only("t", "string", "x")).encode(t)), rec, serve(http.StatusAccepted, ""))

	stdout, stderr, code := runVariant(t, url, "on", true, userID("repo/a"))

	assert.Equal(t, ExitOK, code)
	assert.Equal(t, "RELEASH_ON_ENABLED=true\nRELEASH_ON_VARIANT=t\nRELEASH_ON_PAYLOAD=x\n", stdout)
	assert.Empty(t, stderr)
	posts := rec.received()
	require.Len(t, posts, 1, "one metrics request, sent before Variant returned")
	assert.Equal(t, map[string]toggleCount{"on": {Yes: 1, Variants: map[string]int{"t": 1}}}, posts[0])
}

func TestVariantDoesNotWaitForMetricsAfterAFallbackOrExit6(t *testing.T) {
	hang := func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() }
	features := func(t *testing.T) string {
		return featuresJSON(
			fakeFeature{Name: "off", Enabled: false, Strategies: []fakeStrategy{{Name: "default"}}}.encode(t),
			withVariants("colliding", only("t", "json", `{"a-b": 1, "a_b": 2}`)).encode(t),
		)
	}
	for flag, wantCode := range map[string]int{"off": ExitDisabled, "colliding": ExitOK, "missing": ExitOK} {
		t.Run(flag, func(t *testing.T) {
			url := fakeWithVariantMetrics(t, features(t), &variantMetrics{}, hang)
			start := time.Now()

			stdout, stderr, code := runVariant(t, url, flag, true, userID("repo/a"), checkEdits(set(OptionTimeout, timeout, "3s")))

			assert.Equal(t, wantCode, code, "exit code")
			assert.Equal(t, fmt.Sprintf("RELEASH_%s_ENABLED=false\nRELEASH_%s_VARIANT=disabled\n", envName(flag), envName(flag)), stdout)
			assert.NotContains(t, stderr, "metrics", "stderr")
			assert.Less(t, time.Since(start), time.Second, "does not wait for the hanging metrics request")
		})
	}
}

func TestVariantValidation(t *testing.T) {
	const (
		help        = "Run 'releash variant --help' for usage.\n"
		dotenvFalse = "RELEASH_DEPLOY_PAYMENTS_ENABLED=false\nRELEASH_DEPLOY_PAYMENTS_VARIANT=disabled\n"
		shellFalse  = "export RELEASH_DEPLOY_PAYMENTS_ENABLED='false'\nexport RELEASH_DEPLOY_PAYMENTS_VARIANT='disabled'\n"
	)
	tests := []struct {
		name       string
		edits      []func(*VariantOptions)
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{name: "valid with defaults", wantCode: ExitOK, wantStdout: "evaluated"},
		{name: "valid --format and --prefix", edits: []func(*VariantOptions){formats("shell"), prefixes("P_")}, wantCode: ExitOK, wantStdout: "evaluated"},
		{name: "invalid --format", edits: []func(*VariantOptions){formats("json")}, wantCode: ExitUsage, wantStderr: "releash: invalid --format \"json\": must be one of \"dotenv\" or \"shell\"\n" + help},
		{name: "--format in upper case", edits: []func(*VariantOptions){formats("SHELL")}, wantCode: ExitUsage, wantStderr: "releash: invalid --format \"SHELL\": must be one of \"dotenv\" or \"shell\"\n" + help},
		{name: "empty --format", edits: []func(*VariantOptions){formats("")}, wantCode: ExitUsage, wantStderr: "releash: invalid --format \"\": must be one of \"dotenv\" or \"shell\"\n" + help},
		{name: "--format twice with the same value", edits: []func(*VariantOptions){formats("shell", "shell")}, wantCode: ExitUsage, wantStderr: "releash: --format given 2 times; give it at most once\n" + help},
		{name: "--format twice with different values", edits: []func(*VariantOptions){formats("shell", "dotenv")}, wantCode: ExitUsage, wantStderr: "releash: --format given 2 times; give it at most once\n" + help},
		{name: "empty --prefix", edits: []func(*VariantOptions){prefixes("")}, wantCode: ExitUsage, wantStderr: "releash: invalid --prefix \"\": must match ^[A-Z][A-Z0-9_]*$\n" + help},
		{name: "lower-case --prefix", edits: []func(*VariantOptions){prefixes("app_")}, wantCode: ExitUsage, wantStderr: "releash: invalid --prefix \"app_\": must match ^[A-Z][A-Z0-9_]*$\n" + help},
		{name: "--prefix starting with a digit", edits: []func(*VariantOptions){prefixes("1A")}, wantCode: ExitUsage, wantStderr: "releash: invalid --prefix \"1A\": must match ^[A-Z][A-Z0-9_]*$\n" + help},
		{name: "--prefix starting with _", edits: []func(*VariantOptions){prefixes("_A")}, wantCode: ExitUsage, wantStderr: "releash: invalid --prefix \"_A\": must match ^[A-Z][A-Z0-9_]*$\n" + help},
		{name: "--prefix with a dash", edits: []func(*VariantOptions){prefixes("A-B")}, wantCode: ExitUsage, wantStderr: "releash: invalid --prefix \"A-B\": must match ^[A-Z][A-Z0-9_]*$\n" + help},
		{name: "--prefix twice with the same value", edits: []func(*VariantOptions){prefixes("P_", "P_")}, wantCode: ExitUsage, wantStderr: "releash: --prefix given 2 times; give it at most once\n" + help},
		{name: "--prefix twice with different values", edits: []func(*VariantOptions){prefixes("P_", "Q_")}, wantCode: ExitUsage, wantStderr: "releash: --prefix given 2 times; give it at most once\n" + help},
		{name: "missing flag name", edits: []func(*VariantOptions){func(o *VariantOptions) { o.Flag = "" }}, wantCode: ExitUsage, wantStderr: "releash: missing flag name\n" + help},
		{name: "missing flag name with --prefix", edits: []func(*VariantOptions){func(o *VariantOptions) { o.Flag = "" }, prefixes("P_")}, wantCode: ExitUsage, wantStderr: "releash: missing flag name\n" + help},
		{name: "invalid --timeout", edits: []func(*VariantOptions){checkEdits(set(OptionTimeout, timeout, "abc"))}, wantCode: ExitUsage, wantStderr: "releash: invalid --timeout \"abc\": must be a positive Go duration, e.g. 5s\n" + help},
		{name: "missing token in shell", edits: []func(*VariantOptions){func(o *VariantOptions) { o.Token = "" }, formats("shell")}, wantCode: ExitNotConfigured, wantStdout: shellFalse, wantStderr: "releash: no Unleash API token: set UNLEASH_API_TOKEN or --token to a client API token\n"},
		{name: "invalid --current-time in shell", edits: []func(*VariantOptions){checkEdits(set(OptionCurrentTime, currentTime, "tomorrow")), formats("shell")}, wantCode: ExitInvalid, wantStdout: shellFalse, wantStderr: "releash: invalid --current-time \"tomorrow\": must be an RFC 3339 timestamp\n"},
		{name: "--env twice", edits: []func(*VariantOptions){checkEdits(twice(OptionEnv))}, wantCode: ExitDuplicate, wantStdout: dotenvFalse, wantStderr: "releash: --env given 2 times; give it at most once\n"},
		{name: "--env twice under --prefix", edits: []func(*VariantOptions){checkEdits(twice(OptionEnv)), prefixes("P_")}, wantCode: ExitDuplicate, wantStdout: "P_ENABLED=false\nP_VARIANT=disabled\n", wantStderr: "releash: --env given 2 times; give it at most once\n"},
		{name: "invalid --format and missing token exits 2", edits: []func(*VariantOptions){func(o *VariantOptions) { o.Token = "" }, formats("yaml")}, wantCode: ExitUsage, wantStderr: "releash: no Unleash API token: set UNLEASH_API_TOKEN or --token to a client API token\nreleash: invalid --format \"yaml\": must be one of \"dotenv\" or \"shell\"\n" + help},
	}
	for _, tt := range tests {
		for _, loud := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/loud=%t", tt.name, loud), func(t *testing.T) {
				var stdout, stderr bytes.Buffer
				r := New()
				r.Out, r.Err = &stdout, &stderr
				evaluated := false
				r.evaluate = func(_ context.Context, req checkRequest) outcome {
					evaluated = true
					assert.NotNil(t, req.ask, "variant asks its own question")
					return outcome{output: "evaluated"}
				}
				opts := VariantOptions{CheckOptions: validOptions()}
				for _, e := range tt.edits {
					e(&opts)
				}
				opts.Loud = loud

				code := r.Variant(opts)

				assert.Equal(t, tt.wantCode, code, "exit code")
				assert.Equal(t, tt.wantStdout, stdout.String(), "stdout")
				assert.Equal(t, tt.wantStderr, stderr.String(), "stderr")
				assert.Equal(t, tt.wantCode == ExitOK, evaluated, "Unleash is asked only for a valid invocation")
			})
		}
	}
}

func TestVariantOfADisabledFlagWithACommandLineErrorExitsWithTheLowerCode(t *testing.T) {
	url := fakeUnleash(t, serve(http.StatusOK, featuresJSON(disabledFlag)), nil)

	stdout, stderr, code := runVariant(t, url, "off", false, checkEdits(set(OptionTimeout, timeout, "abc")))

	assert.Equal(t, ExitUsage, code, "exit code")
	assert.Empty(t, stdout, "stdout")
	assert.Equal(t, "releash: invalid --timeout \"abc\": must be a positive Go duration, e.g. 5s\nRun 'releash variant --help' for usage.\n", stderr, "stderr")
}

func TestVariantFallback(t *testing.T) {
	tests := []struct {
		name     string
		flags    []string
		formats  []string
		prefixes []string
		want     string
	}{
		{name: "defaults", flags: []string{"checkout-redesign"}, want: "RELEASH_CHECKOUT_REDESIGN_ENABLED=false\nRELEASH_CHECKOUT_REDESIGN_VARIANT=disabled\n"},
		{name: "one --format", flags: []string{"x"}, formats: []string{"shell"}, want: "export RELEASH_X_ENABLED='false'\nexport RELEASH_X_VARIANT='disabled'\n"},
		{name: "the same --format twice", flags: []string{"x"}, formats: []string{"shell", "shell"}, want: "export RELEASH_X_ENABLED='false'\nexport RELEASH_X_VARIANT='disabled'\n"},
		{name: "two different formats", flags: []string{"x"}, formats: []string{"shell", "dotenv"}},
		{name: "an invalid format", flags: []string{"x"}, formats: []string{"yaml"}},
		{name: "a --format without a value", flags: []string{"x"}, formats: []string{""}},
		{name: "one --prefix", flags: []string{"x"}, prefixes: []string{"P_"}, want: "P_ENABLED=false\nP_VARIANT=disabled\n"},
		{name: "the same --prefix twice", flags: []string{"x"}, prefixes: []string{"P_", "P_"}, want: "P_ENABLED=false\nP_VARIANT=disabled\n"},
		{name: "two different prefixes", flags: []string{"x"}, prefixes: []string{"P_", "Q_"}},
		{name: "an invalid prefix", flags: []string{"x"}, prefixes: []string{"p_"}},
		{name: "a --prefix without a value", flags: []string{"x"}, prefixes: []string{""}},
		{name: "no flag name", flags: nil},
		{name: "an empty flag name", flags: []string{""}},
		{name: "two flag names", flags: []string{"x", "y"}},
		{name: "two flag names with --prefix", flags: []string{"x", "y"}, prefixes: []string{"P_"}, formats: []string{"shell"}, want: "export P_ENABLED='false'\nexport P_VARIANT='disabled'\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, VariantFallback(tt.flags, tt.formats, tt.prefixes))
		})
	}
}
