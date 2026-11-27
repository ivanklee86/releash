package releash

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Cases, from the exit code table and test plan in spec 0001:
//   - valid: minimal options, every option once, options set to their
//     defaults, valid durations, timestamps (including lowercase, leap seconds
//     and an explicit zero time) and addresses, '=' in property values, empty
//     property values; each reaches evaluation, which a stub answers here
//     (evaluate_test.go covers evaluation itself)
//   - 2: missing flag name, invalid --timeout, each releash option twice
//   - 3: missing URL, missing token
//   - 4: malformed URL or port, invalid --current-time (including offsets
//     outside the RFC) and --remote-address, empty
//     --env and --app-name, property without '=', empty property name,
//     property named after each standard field (with the hint)
//   - 5: each context option twice, a repeated property name
//   - precedence: the lowest code wins whatever the order of the mistakes
//   - every error prints exactly "false\n", reports errors without --loud,
//     and never reaches evaluation

// validOptions returns options that pass validation, with every field that
// has a default left unset.
func validOptions() CheckOptions {
	return CheckOptions{
		Flag:  "deploy-payments",
		URL:   "https://unleash.example.com/api",
		Token: "default:production.abc123",
		Given: map[string]int{},
	}
}

// with returns validOptions changed by edit.
func with(edit func(*CheckOptions)) CheckOptions {
	opts := validOptions()
	edit(&opts)
	return opts
}

// set returns an edit that gives option once with value, through field.
func set(option string, field func(*CheckOptions) *string, value string) func(*CheckOptions) {
	return func(o *CheckOptions) {
		*field(o) = value
		o.Given[option]++
	}
}

// twice returns an edit that counts option as given two times, with a valid
// value, as a flag library that keeps the last value would leave it.
func twice(option string) func(*CheckOptions) {
	return func(o *CheckOptions) {
		switch option {
		case OptionTimeout:
			o.Timeout = "10s"
		case OptionEnv:
			o.Env = "staging"
		case OptionAppName:
			o.AppName = "pipeline"
		case OptionRemoteAddress:
			o.RemoteAddress = "10.0.0.1"
		case OptionCurrentTime:
			o.CurrentTime = "2026-11-27T10:00:00Z"
		}
		o.Given[option] = 2
	}
}

// all combines edits.
func all(edits ...func(*CheckOptions)) func(*CheckOptions) {
	return func(o *CheckOptions) {
		for _, e := range edits {
			e(o)
		}
	}
}

func timeout(o *CheckOptions) *string       { return &o.Timeout }
func env(o *CheckOptions) *string           { return &o.Env }
func appName(o *CheckOptions) *string       { return &o.AppName }
func remoteAddress(o *CheckOptions) *string { return &o.RemoteAddress }
func currentTime(o *CheckOptions) *string   { return &o.CurrentTime }

func properties(values ...string) func(*CheckOptions) {
	return func(o *CheckOptions) { o.Properties = append(o.Properties, values...) }
}

const helpLine = "Run 'releash check --help' for usage."

// checkCase is one call of Check and the outcome it must have without --loud.
type checkCase struct {
	name       string
	opts       CheckOptions
	wantCode   int
	wantStderr []string
}

func TestCheck(t *testing.T) {
	tests := []checkCase{
		{name: "valid invocation", opts: validOptions(), wantCode: ExitOK},
		{
			name: "every option given once",
			opts: with(all(
				set(OptionTimeout, timeout, "30s"),
				set(OptionEnv, env, "staging"),
				set(OptionAppName, appName, "pipeline"),
				set(OptionUserID, func(o *CheckOptions) *string { return &o.UserID }, "alice"),
				set(OptionSessionID, func(o *CheckOptions) *string { return &o.SessionID }, "s-1"),
				set(OptionRemoteAddress, remoteAddress, "10.0.0.1"),
				set(OptionCurrentTime, currentTime, "2026-11-27T10:00:00Z"),
				properties("region=eu", "cluster=prod-eu-1"),
				func(o *CheckOptions) { o.Given[OptionURL], o.Given[OptionToken] = 1, 1 },
			)),
			wantCode: ExitOK,
		},
		{
			name: "defaults given explicitly are not duplicates",
			opts: with(all(
				set(OptionTimeout, timeout, "5s"),
				set(OptionEnv, env, DefaultEnvironment),
				set(OptionAppName, appName, DefaultAppName),
			)),
			wantCode: ExitOK,
		},
		{name: "http URL", opts: with(func(o *CheckOptions) { o.URL = "http://localhost:4242/api" }), wantCode: ExitOK},
		{name: "IPv6 remote address", opts: with(set(OptionRemoteAddress, remoteAddress, "::1")), wantCode: ExitOK},
		{name: "current time with fraction and offset", opts: with(set(OptionCurrentTime, currentTime, "2026-11-27T10:00:00.123+01:00")), wantCode: ExitOK},
		{name: "current time in lowercase", opts: with(set(OptionCurrentTime, currentTime, "2026-01-01t00:00:00z")), wantCode: ExitOK},
		{name: "current time on a leap second", opts: with(set(OptionCurrentTime, currentTime, "2016-12-31T23:59:60Z")), wantCode: ExitOK},
		{name: "current time with the largest offsets", opts: with(set(OptionCurrentTime, currentTime, "2026-01-01T00:00:00-23:59")), wantCode: ExitOK},
		{name: "URL with port 65535", opts: with(func(o *CheckOptions) { o.URL = "http://localhost:65535/api" }), wantCode: ExitOK},
		{name: "empty user and session IDs", opts: with(all(
			set(OptionUserID, func(o *CheckOptions) *string { return &o.UserID }, ""),
			set(OptionSessionID, func(o *CheckOptions) *string { return &o.SessionID }, ""),
		)), wantCode: ExitOK},
		{name: "property values with = and empty values", opts: with(properties("build=a=b", "empty=")), wantCode: ExitOK},

		// Exit 2.
		{
			name:       "missing flag name",
			opts:       with(func(o *CheckOptions) { o.Flag = "" }),
			wantCode:   ExitUsage,
			wantStderr: []string{"releash: missing flag name", helpLine},
		},
		{
			name:       "timeout that is not a duration",
			opts:       with(set(OptionTimeout, timeout, "abc")),
			wantCode:   ExitUsage,
			wantStderr: []string{`releash: invalid --timeout "abc": must be a positive Go duration, e.g. 5s`, helpLine},
		},
		{
			name:       "zero timeout",
			opts:       with(set(OptionTimeout, timeout, "0")),
			wantCode:   ExitUsage,
			wantStderr: []string{`releash: invalid --timeout "0": must be a positive Go duration, e.g. 5s`, helpLine},
		},
		{
			name:       "negative timeout",
			opts:       with(set(OptionTimeout, timeout, "-1s")),
			wantCode:   ExitUsage,
			wantStderr: []string{`releash: invalid --timeout "-1s": must be a positive Go duration, e.g. 5s`, helpLine},
		},

		// Exit 3.
		{
			name:       "missing URL",
			opts:       with(func(o *CheckOptions) { o.URL = "" }),
			wantCode:   ExitNotConfigured,
			wantStderr: []string{"releash: no Unleash URL: set UNLEASH_URL or --url to the Unleash API URL"},
		},
		{
			name:       "missing token",
			opts:       with(func(o *CheckOptions) { o.Token = "" }),
			wantCode:   ExitNotConfigured,
			wantStderr: []string{"releash: no Unleash API token: set UNLEASH_API_TOKEN or --token to a client API token"},
		},

		// Exit 4.
		{
			name:       "invalid current time",
			opts:       with(set(OptionCurrentTime, currentTime, "tomorrow")),
			wantCode:   ExitInvalid,
			wantStderr: []string{`releash: invalid --current-time "tomorrow": must be an RFC 3339 timestamp`},
		},
		{
			name:       "current time with a 24-hour offset",
			opts:       with(set(OptionCurrentTime, currentTime, "2026-01-01T00:00:00+24:00")),
			wantCode:   ExitInvalid,
			wantStderr: []string{`releash: invalid --current-time "2026-01-01T00:00:00+24:00": must be an RFC 3339 timestamp`},
		},
		{
			name:       "current time with 60 offset minutes",
			opts:       with(set(OptionCurrentTime, currentTime, "2026-01-01T00:00:00+23:60")),
			wantCode:   ExitInvalid,
			wantStderr: []string{`releash: invalid --current-time "2026-01-01T00:00:00+23:60": must be an RFC 3339 timestamp`},
		},
		{
			name:       "current time without an offset",
			opts:       with(set(OptionCurrentTime, currentTime, "2026-01-01T00:00:00")),
			wantCode:   ExitInvalid,
			wantStderr: []string{`releash: invalid --current-time "2026-01-01T00:00:00": must be an RFC 3339 timestamp`},
		},
		{
			name:       "current time with second 61",
			opts:       with(set(OptionCurrentTime, currentTime, "2016-12-31T23:59:61Z")),
			wantCode:   ExitInvalid,
			wantStderr: []string{`releash: invalid --current-time "2016-12-31T23:59:61Z": must be an RFC 3339 timestamp`},
		},
		{
			name:       "URL with a port above 65535",
			opts:       with(func(o *CheckOptions) { o.URL = "http://host:99999/api" }),
			wantCode:   ExitInvalid,
			wantStderr: []string{`releash: invalid Unleash URL "http://host:99999/api": port 99999 is not between 1 and 65535`},
		},
		{
			name:       "URL with port 0",
			opts:       with(func(o *CheckOptions) { o.URL = "http://host:0/api" }),
			wantCode:   ExitInvalid,
			wantStderr: []string{`releash: invalid Unleash URL "http://host:0/api": port 0 is not between 1 and 65535`},
		},
		{
			name:       "invalid URL with a password does not print the password",
			opts:       with(func(o *CheckOptions) { o.URL = "http://user:s3cret@host:99999/api" }),
			wantCode:   ExitInvalid,
			wantStderr: []string{`releash: invalid Unleash URL "http://user:xxxxx@host:99999/api": port 99999 is not between 1 and 65535`},
		},
		{
			name:       "URL with a query",
			opts:       with(func(o *CheckOptions) { o.URL = "http://host/api?x=1" }),
			wantCode:   ExitInvalid,
			wantStderr: []string{`releash: invalid Unleash URL "http://host/api?x=1": must not have a query or fragment`},
		},
		{
			name:       "URL with an empty query",
			opts:       with(func(o *CheckOptions) { o.URL = "http://host/api?" }),
			wantCode:   ExitInvalid,
			wantStderr: []string{`releash: invalid Unleash URL "http://host/api?": must not have a query or fragment`},
		},
		{
			name:       "URL with a fragment",
			opts:       with(func(o *CheckOptions) { o.URL = "http://host/api#x" }),
			wantCode:   ExitInvalid,
			wantStderr: []string{`releash: invalid Unleash URL "http://host/api#x": must not have a query or fragment`},
		},
		{
			name:       "URL with an empty fragment",
			opts:       with(func(o *CheckOptions) { o.URL = "http://host/api#" }),
			wantCode:   ExitInvalid,
			wantStderr: []string{`releash: invalid Unleash URL "http://host/api#": must not have a query or fragment`},
		},
		{
			name:       "current time on a leap second that ends year 9999",
			opts:       with(set(OptionCurrentTime, currentTime, "9999-12-31T23:59:60Z")),
			wantCode:   ExitInvalid,
			wantStderr: []string{`releash: invalid --current-time "9999-12-31T23:59:60Z": must be an RFC 3339 timestamp`},
		},
		{
			name:       "current time with a one-digit hour",
			opts:       with(set(OptionCurrentTime, currentTime, "2026-01-01T1:00:00Z")),
			wantCode:   ExitInvalid,
			wantStderr: []string{`releash: invalid --current-time "2026-01-01T1:00:00Z": must be an RFC 3339 timestamp`},
		},
		{
			name:       "current time with a comma before the fraction",
			opts:       with(set(OptionCurrentTime, currentTime, "2026-01-01T00:00:00,5Z")),
			wantCode:   ExitInvalid,
			wantStderr: []string{`releash: invalid --current-time "2026-01-01T00:00:00,5Z": must be an RFC 3339 timestamp`},
		},
		{
			name:       "current time with a leap second outside 23:59 UTC",
			opts:       with(set(OptionCurrentTime, currentTime, "2026-01-01T00:00:60Z")),
			wantCode:   ExitInvalid,
			wantStderr: []string{`releash: invalid --current-time "2026-01-01T00:00:60Z": must be an RFC 3339 timestamp`},
		},
		{
			name:       "invalid remote address",
			opts:       with(set(OptionRemoteAddress, remoteAddress, "not-an-ip")),
			wantCode:   ExitInvalid,
			wantStderr: []string{`releash: invalid --remote-address "not-an-ip": must be an IPv4 or IPv6 address`},
		},
		{
			name:       "remote address with a zone",
			opts:       with(set(OptionRemoteAddress, remoteAddress, "fe80::1%eth0")),
			wantCode:   ExitInvalid,
			wantStderr: []string{`releash: invalid --remote-address "fe80::1%eth0": must be an IPv4 or IPv6 address`},
		},
		{
			name:       "empty env",
			opts:       with(set(OptionEnv, env, "")),
			wantCode:   ExitInvalid,
			wantStderr: []string{`releash: invalid --env "": must not be empty`},
		},
		{
			name:       "empty app name",
			opts:       with(set(OptionAppName, appName, "")),
			wantCode:   ExitInvalid,
			wantStderr: []string{`releash: invalid --app-name "": must not be empty`},
		},
		{
			name:       "property without =",
			opts:       with(properties("region")),
			wantCode:   ExitInvalid,
			wantStderr: []string{`releash: invalid --properties "region": must be name=value`},
		},
		{
			name:       "property with an empty name",
			opts:       with(properties("=eu")),
			wantCode:   ExitInvalid,
			wantStderr: []string{`releash: invalid --properties "=eu": the property name must not be empty`},
		},

		// Exit 5.
		{
			name:       "repeated property name",
			opts:       with(properties("region=eu", "other=x", "region=us")),
			wantCode:   ExitDuplicate,
			wantStderr: []string{`releash: property "region" given 2 times in --properties; give each name once`},
		},

		// Precedence.
		{
			name:     "missing token and invalid current time exits 3",
			opts:     with(all(func(o *CheckOptions) { o.Token = "" }, set(OptionCurrentTime, currentTime, "tomorrow"))),
			wantCode: ExitNotConfigured,
			wantStderr: []string{
				"releash: no Unleash API token: set UNLEASH_API_TOKEN or --token to a client API token",
				`releash: invalid --current-time "tomorrow": must be an RFC 3339 timestamp`,
			},
		},
		{
			name:     "invalid timeout and missing token exits 2",
			opts:     with(all(func(o *CheckOptions) { o.Token = "" }, set(OptionTimeout, timeout, "abc"))),
			wantCode: ExitUsage,
			wantStderr: []string{
				`releash: invalid --timeout "abc": must be a positive Go duration, e.g. 5s`,
				"releash: no Unleash API token: set UNLEASH_API_TOKEN or --token to a client API token",
				helpLine,
			},
		},
		{
			name:     "repeated context option and malformed URL exits 4",
			opts:     with(all(twice(OptionEnv), func(o *CheckOptions) { o.URL = "localhost:4242" })),
			wantCode: ExitInvalid,
			wantStderr: []string{
				`releash: invalid Unleash URL "localhost:4242": must be an absolute http or https URL`,
				"releash: --env given 2 times; give it at most once",
			},
		},
		{
			name:     "missing URL wins over 4 and 5, and every problem is reported",
			opts:     with(all(func(o *CheckOptions) { o.URL = "" }, properties("userId=alice", "a=1", "a=2"))),
			wantCode: ExitNotConfigured,
			wantStderr: []string{
				"releash: no Unleash URL: set UNLEASH_URL or --url to the Unleash API URL",
				`releash: invalid --properties "userId=alice": property "userId" is a standard field; use --user-id`,
				`releash: property "a" given 2 times in --properties; give each name once`,
			},
		},
	}

	for _, option := range ReleashOptions() {
		tests = append(tests, checkCase{
			name:       "--" + option + " given twice",
			opts:       with(twice(option)),
			wantCode:   ExitUsage,
			wantStderr: []string{fmt.Sprintf("releash: --%s given 2 times; give it at most once", option), helpLine},
		})
	}
	for _, option := range ContextOptions() {
		if option == OptionProperties {
			continue
		}
		tests = append(tests, checkCase{
			name:       "--" + option + " given twice",
			opts:       with(twice(option)),
			wantCode:   ExitDuplicate,
			wantStderr: []string{fmt.Sprintf("releash: --%s given 2 times; give it at most once", option)},
		})
	}
	for field, option := range map[string]string{
		"userId":        "--user-id",
		"sessionId":     "--session-id",
		"remoteAddress": "--remote-address",
		"environment":   "--env",
		"appName":       "--app-name",
		"currentTime":   "--current-time",
	} {
		tests = append(tests, checkCase{
			name:       "property named " + field,
			opts:       with(properties(field + "=x")),
			wantCode:   ExitInvalid,
			wantStderr: []string{fmt.Sprintf(`releash: invalid --properties "%s=x": property %q is a standard field; use %s`, field, field, option)},
		})
	}
	for _, u := range []string{"localhost:4242", "ftp://host/api", "/api", "http://", "https:///api", "://bad"} {
		tests = append(tests, checkCase{
			name:       "malformed URL " + u,
			opts:       with(func(o *CheckOptions) { o.URL = u }),
			wantCode:   ExitInvalid,
			wantStderr: []string{fmt.Sprintf("releash: invalid Unleash URL %q: must be an absolute http or https URL", u)},
		})
	}

	for _, tt := range tests {
		for _, loud := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/loud=%t", tt.name, loud), func(t *testing.T) {
				var stdout, stderr bytes.Buffer
				r := New()
				r.Out, r.Err = &stdout, &stderr
				evaluated := false
				r.evaluate = func(context.Context, checkRequest) outcome {
					evaluated = true
					return outcome{}
				}
				opts := tt.opts
				opts.Loud = loud

				code := r.Check(opts)

				assert.Equal(t, tt.wantCode, code, "exit code")
				assert.Equal(t, "false\n", stdout.String(), "stdout")
				assert.Equal(t, lines(tt.wantStderr), stderr.String(), "stderr")
				assert.Equal(t, tt.wantCode == ExitOK, evaluated, "Unleash is asked only for a valid invocation")
			})
		}
	}
}

// lines joins ls into newline-terminated lines.
func lines(ls []string) string {
	if len(ls) == 0 {
		return ""
	}
	return strings.Join(ls, "\n") + "\n"
}

// These cases check the request Check hands to evaluation; evaluate_test.go
// checks that each field reaches the SDK.
func TestValidateCheckAppliesDefaults(t *testing.T) {
	req, problems := validateCheck(validOptions())

	require.Empty(t, problems)
	assert.Equal(t, DefaultTimeout, req.timeout)
	assert.Equal(t, 5*time.Second, req.timeout)
	assert.Equal(t, "production", req.context.environment)
	assert.Equal(t, "releash", req.context.appName)
	assert.Empty(t, req.context.remoteAddress)
	assert.Nil(t, req.context.currentTime, "currentTime is left to the time of the check")
}

func TestValidateCheckKeepsAGivenZeroCurrentTime(t *testing.T) {
	req, problems := validateCheck(with(set(OptionCurrentTime, currentTime, "0001-01-01T00:00:00Z")))

	require.Empty(t, problems)
	require.NotNil(t, req.context.currentTime, "an explicit zero time must not read as unset")
	assert.True(t, req.context.currentTime.Equal(time.Time{}))
}

func TestValidateCheckParsesRFC3339Variants(t *testing.T) {
	tests := []struct {
		in   string
		want time.Time
	}{
		{"2026-01-01t10:00:00z", time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)},
		// time.Time has no leap seconds, so :60 is the next minute's first instant.
		{"2016-12-31T23:59:60Z", time.Date(2017, 1, 1, 0, 0, 0, 0, time.UTC)},
		{"2016-12-31T23:59:60.5Z", time.Date(2017, 1, 1, 0, 0, 0, 500_000_000, time.UTC)},
		// A leap second in a local offset is still at 23:59:60 UTC.
		{"2016-12-31T15:59:60-08:00", time.Date(2017, 1, 1, 0, 0, 0, 0, time.UTC)},
		{"2026-01-01T00:00:00+23:59", time.Date(2025, 12, 31, 0, 1, 0, 0, time.UTC)},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			req, problems := validateCheck(with(set(OptionCurrentTime, currentTime, tt.in)))

			require.Empty(t, problems)
			require.NotNil(t, req.context.currentTime)
			assert.True(t, tt.want.Equal(*req.context.currentTime), "got %v, want %v", *req.context.currentTime, tt.want)
		})
	}
}

func TestValidateCheckUsesGivenValues(t *testing.T) {
	opts := with(all(
		set(OptionTimeout, timeout, "90s"),
		set(OptionEnv, env, "staging"),
		set(OptionAppName, appName, "pipeline"),
		set(OptionRemoteAddress, remoteAddress, "192.168.1.10"),
		set(OptionCurrentTime, currentTime, "2099-01-02T03:04:05Z"),
		properties("build=a=b", "empty=", "region=eu"),
	))

	req, problems := validateCheck(opts)

	require.Empty(t, problems)
	assert.Equal(t, 90*time.Second, req.timeout)
	assert.Equal(t, "staging", req.context.environment)
	assert.Equal(t, "pipeline", req.context.appName)
	assert.Equal(t, "192.168.1.10", req.context.remoteAddress)
	require.NotNil(t, req.context.currentTime)
	assert.Equal(t, time.Date(2099, 1, 2, 3, 4, 5, 0, time.UTC), *req.context.currentTime)
	assert.Equal(t, map[string]string{"build": "a=b", "empty": "", "region": "eu"}, req.context.properties)
	assert.Equal(t, "https://unleash.example.com/api", req.url.String())
}

func TestValidateCheckIgnoresValuesOfOptionsNotGiven(t *testing.T) {
	// Without Given, the fields are whatever the caller left in them, e.g. an
	// empty default from a flag library.
	opts := validOptions()
	opts.Env = ""
	opts.AppName = ""
	opts.Timeout = "abc"
	opts.CurrentTime = "tomorrow"
	opts.RemoteAddress = "not-an-ip"

	req, problems := validateCheck(opts)

	require.Empty(t, problems)
	assert.Equal(t, DefaultEnvironment, req.context.environment)
	assert.Equal(t, DefaultAppName, req.context.appName)
	assert.Equal(t, DefaultTimeout, req.timeout)
}
