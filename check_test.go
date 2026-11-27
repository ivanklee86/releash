package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanklee86/releash/internal/releash"
)

// Cases for releash check through the root command, from milestone M3 and the
// test plan in spec 0001. internal/releash covers validation in detail; these
// check that options and environment variables reach it and that cobra's own
// errors keep the stdout and exit code contract:
//   - environment fallback for --url and --token, and options winning over it
//   - missing or empty URL or token from either source exits 3
//   - missing flag name, extra arguments, unknown or malformed options, an
//     option before the check command, an invalid --timeout and a repeated
//     releash option exit 2
//   - invalid context values exit 4, repeated context options exit 5
//   - defaults are not duplicates, property values keep '=' and commas
//   - the lowest exit code wins
//   - every exit prints exactly "false\n" (valid cases check the unknown flag
//     x against a fake Unleash), and --loud never changes stdout, the exit
//     code or the error lines
//   - an enabled flag prints "true\n" and nothing on stderr, even with --loud
//   - --help shows releash options, then context options
//   - --version prints the version and exits 0 on check as on the root
//   - a panic prints "false" and exits 1

const (
	testToken = "default:production.abc123"
	loudLine  = "releash: flag \"x\" not found; returning false\n"
	checkHelp = "Run 'releash check --help' for usage.\n"
	noURL     = "releash: no Unleash URL: set UNLEASH_URL or --url to the Unleash API URL\n"
	noToken   = "releash: no Unleash API token: set UNLEASH_API_TOKEN or --token to a client API token\n"
)

// testURL is the API URL of a fake Unleash that TestMain starts, so valid
// checks need no network. Its only flag is enabled-everywhere, which is on
// for everyone.
var testURL string

func TestMain(m *testing.M) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/client/features" {
			_, _ = io.WriteString(w, `{"version":2,"features":[{"name":"enabled-everywhere","enabled":true,"strategies":[{"name":"default"}]}]}`)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	testURL = srv.URL + "/api"
	code := m.Run()
	srv.Close()
	os.Exit(code)
}

// unleashEnv sets UNLEASH_URL and UNLEASH_API_TOKEN to the values in env for
// the rest of the test, and unsets whichever env leaves out.
func unleashEnv(t *testing.T, env map[string]string) {
	t.Helper()
	for _, key := range []string{"UNLEASH_URL", "UNLEASH_API_TOKEN"} {
		// t.Setenv restores the original value when the test ends.
		t.Setenv(key, "")
		value, ok := env[key]
		if ok {
			t.Setenv(key, value)
		} else {
			require.NoError(t, os.Unsetenv(key))
		}
	}
}

func TestCheckCommand(t *testing.T) {
	configured := map[string]string{"UNLEASH_URL": testURL, "UNLEASH_API_TOKEN": testToken}

	tests := []struct {
		name string
		env  map[string]string
		args []string
		// loud is set for cases that pass --loud themselves; the others also
		// run with --loud added.
		loud     bool
		wantCode int
		// wantStderr is the exact stderr without --loud. If wantStderrPrefix
		// is set, stderr must start with it and end with wantStderr instead.
		wantStderr       string
		wantStderrPrefix string
	}{
		{name: "URL and token from the environment", env: configured, args: []string{"check", "x"}, wantCode: releash.ExitOK},
		{name: "URL and token from options", args: []string{"check", "x", "--url", testURL, "--token", testToken}, wantCode: releash.ExitOK},
		{name: "--loud explains the answer", env: configured, args: []string{"check", "x", "--loud"}, loud: true, wantCode: releash.ExitOK, wantStderr: loudLine},
		{name: "options before the flag name", env: configured, args: []string{"check", "--loud", "--env", "staging", "x"}, loud: true, wantCode: releash.ExitOK, wantStderr: loudLine},
		{
			name:     "every option once",
			env:      configured,
			args:     []string{"check", "x", "--timeout", "30s", "--env", "staging", "--app-name", "pipeline", "--user-id", "alice", "--session-id", "s-1", "--remote-address", "::1", "--current-time", "2026-11-27T10:00:00Z", "--properties", "region=eu", "--properties", "cluster=prod-eu-1"},
			wantCode: releash.ExitOK,
		},
		{name: "defaults given once are not duplicates", env: configured, args: []string{"check", "x", "--env", "production", "--app-name", "releash", "--timeout", "5s"}, wantCode: releash.ExitOK},
		{name: "property values keep = and commas and may be empty", env: configured, args: []string{"check", "x", "--properties", "build=a=b", "--properties", "list=a,b", "--properties", "empty="}, wantCode: releash.ExitOK},

		// Options take precedence over the environment.
		{name: "--url wins over a malformed UNLEASH_URL", env: map[string]string{"UNLEASH_URL": "localhost:4242", "UNLEASH_API_TOKEN": testToken}, args: []string{"check", "x", "--url", testURL}, wantCode: releash.ExitOK},
		{
			name:       "a malformed --url wins over UNLEASH_URL",
			env:        configured,
			args:       []string{"check", "x", "--url", "ftp://host/api"},
			wantCode:   releash.ExitInvalid,
			wantStderr: "releash: invalid Unleash URL \"ftp://host/api\": must be an absolute http or https URL\n",
		},
		{name: "an empty --token wins over UNLEASH_API_TOKEN", env: configured, args: []string{"check", "x", "--token", ""}, wantCode: releash.ExitNotConfigured, wantStderr: noToken},
		{name: "an empty --url wins over UNLEASH_URL", env: configured, args: []string{"check", "x", "--url="}, wantCode: releash.ExitNotConfigured, wantStderr: noURL},
		{name: "a token from the environment and --token once is not a repeat", env: configured, args: []string{"check", "x", "--token", "other"}, wantCode: releash.ExitOK},

		// Exit 3.
		{name: "UNLEASH_API_TOKEN unset", env: map[string]string{"UNLEASH_URL": testURL}, args: []string{"check", "x"}, wantCode: releash.ExitNotConfigured, wantStderr: noToken},
		{name: "UNLEASH_API_TOKEN empty, as from an unset CI secret", env: map[string]string{"UNLEASH_URL": testURL, "UNLEASH_API_TOKEN": ""}, args: []string{"check", "x"}, wantCode: releash.ExitNotConfigured, wantStderr: noToken},
		{name: "UNLEASH_URL unset", env: map[string]string{"UNLEASH_API_TOKEN": testToken}, args: []string{"check", "x"}, wantCode: releash.ExitNotConfigured, wantStderr: noURL},
		{name: "UNLEASH_URL empty", env: map[string]string{"UNLEASH_URL": "", "UNLEASH_API_TOKEN": testToken}, args: []string{"check", "x"}, wantCode: releash.ExitNotConfigured, wantStderr: noURL},
		{name: "nothing configured", args: []string{"check", "x"}, wantCode: releash.ExitNotConfigured, wantStderr: noURL + noToken},

		// Exit 2.
		{name: "missing flag name", env: configured, args: []string{"check"}, wantCode: releash.ExitUsage, wantStderr: "releash: missing flag name\n" + checkHelp},
		{name: "empty flag name", env: configured, args: []string{"check", ""}, wantCode: releash.ExitUsage, wantStderr: "releash: missing flag name\n" + checkHelp},
		{name: "extra arguments", env: configured, args: []string{"check", "enabled-everywhere", "extra"}, wantCode: releash.ExitUsage, wantStderr: "releash: expected one flag name, got 2 arguments: enabled-everywhere extra\n" + checkHelp},
		{name: "unknown option", env: configured, args: []string{"check", "x", "--no-such-option"}, wantCode: releash.ExitUsage, wantStderr: "releash: unknown flag: --no-such-option\n" + checkHelp},
		// Appending --loud would make it the value of --env.
		{name: "option without a value", env: configured, args: []string{"check", "x", "--env"}, loud: true, wantCode: releash.ExitUsage, wantStderr: "releash: flag needs an argument: --env\n" + checkHelp},
		// pflag's message continues with strconv's error, which is not ours.
		{name: "--loud with a non-boolean value", env: configured, args: []string{"check", "x", "--loud=maybe"}, loud: true, wantCode: releash.ExitUsage, wantStderrPrefix: `releash: invalid argument "maybe" for "--loud" flag`, wantStderr: checkHelp},
		// Options before the check command are rejected whether or not they
		// take a value, and the answer contract still holds.
		{name: "boolean option before check", env: configured, args: []string{"--loud", "check", "x"}, loud: true, wantCode: releash.ExitUsage, wantStderr: "releash: --loud given before check; put options after 'check <flag>'\n" + checkHelp},
		{name: "option with a value before check", env: configured, args: []string{"--url", testURL, "check", "x"}, wantCode: releash.ExitUsage, wantStderr: "releash: --url given before check; put options after 'check <flag>'\n" + checkHelp},
		{name: "option with = before check", env: configured, args: []string{"--env=staging", "check", "x"}, wantCode: releash.ExitUsage, wantStderr: "releash: --env=staging given before check; put options after 'check <flag>'\n" + checkHelp},
		{name: "--timeout abc", env: configured, args: []string{"check", "x", "--timeout", "abc"}, wantCode: releash.ExitUsage, wantStderr: "releash: invalid --timeout \"abc\": must be a positive Go duration, e.g. 5s\n" + checkHelp},
		{name: "--timeout 0", env: configured, args: []string{"check", "x", "--timeout", "0"}, wantCode: releash.ExitUsage, wantStderr: "releash: invalid --timeout \"0\": must be a positive Go duration, e.g. 5s\n" + checkHelp},
		{name: "--timeout -1s", env: configured, args: []string{"check", "x", "--timeout=-1s"}, wantCode: releash.ExitUsage, wantStderr: "releash: invalid --timeout \"-1s\": must be a positive Go duration, e.g. 5s\n" + checkHelp},
		{name: "--url twice", env: configured, args: []string{"check", "x", "--url", testURL, "--url", testURL}, wantCode: releash.ExitUsage, wantStderr: "releash: --url given 2 times; give it at most once\n" + checkHelp},
		{name: "--token twice", env: configured, args: []string{"check", "x", "--token", "a", "--token", "b"}, wantCode: releash.ExitUsage, wantStderr: "releash: --token given 2 times; give it at most once\n" + checkHelp},
		{name: "--timeout twice", env: configured, args: []string{"check", "x", "--timeout", "5s", "--timeout", "5s"}, wantCode: releash.ExitUsage, wantStderr: "releash: --timeout given 2 times; give it at most once\n" + checkHelp},
		{name: "--loud twice", env: configured, args: []string{"check", "x", "--loud", "--loud"}, loud: true, wantCode: releash.ExitUsage, wantStderr: "releash: --loud given 2 times; give it at most once\n" + checkHelp},

		// Exit 4.
		{name: "--current-time tomorrow", env: configured, args: []string{"check", "x", "--current-time", "tomorrow"}, wantCode: releash.ExitInvalid, wantStderr: "releash: invalid --current-time \"tomorrow\": must be an RFC 3339 timestamp\n"},
		{name: "--remote-address not-an-ip", env: configured, args: []string{"check", "x", "--remote-address", "not-an-ip"}, wantCode: releash.ExitInvalid, wantStderr: "releash: invalid --remote-address \"not-an-ip\": must be an IPv4 or IPv6 address\n"},
		{name: "empty --env", env: configured, args: []string{"check", "x", "--env", ""}, wantCode: releash.ExitInvalid, wantStderr: "releash: invalid --env \"\": must not be empty\n"},
		{name: "empty --app-name", env: configured, args: []string{"check", "x", "--app-name="}, wantCode: releash.ExitInvalid, wantStderr: "releash: invalid --app-name \"\": must not be empty\n"},
		{name: "property without =", env: configured, args: []string{"check", "x", "--properties", "region"}, wantCode: releash.ExitInvalid, wantStderr: "releash: invalid --properties \"region\": must be name=value\n"},
		{name: "property with an empty name", env: configured, args: []string{"check", "x", "--properties", "=eu"}, wantCode: releash.ExitInvalid, wantStderr: "releash: invalid --properties \"=eu\": the property name must not be empty\n"},
		{name: "property named after a standard field", env: configured, args: []string{"check", "x", "--properties", "userId=alice"}, wantCode: releash.ExitInvalid, wantStderr: "releash: invalid --properties \"userId=alice\": property \"userId\" is a standard field; use --user-id\n"},
		{name: "malformed UNLEASH_URL", env: map[string]string{"UNLEASH_URL": "localhost:4242", "UNLEASH_API_TOKEN": testToken}, args: []string{"check", "x"}, wantCode: releash.ExitInvalid, wantStderr: "releash: invalid Unleash URL \"localhost:4242\": must be an absolute http or https URL\n"},

		// Exit 5.
		{name: "--env twice", env: configured, args: []string{"check", "x", "--env", "a", "--env", "b"}, wantCode: releash.ExitDuplicate, wantStderr: "releash: --env given 2 times; give it at most once\n"},
		{name: "--app-name twice", env: configured, args: []string{"check", "x", "--app-name", "a", "--app-name", "a"}, wantCode: releash.ExitDuplicate, wantStderr: "releash: --app-name given 2 times; give it at most once\n"},
		{name: "--user-id twice", env: configured, args: []string{"check", "x", "--user-id", "a", "--user-id", "b"}, wantCode: releash.ExitDuplicate, wantStderr: "releash: --user-id given 2 times; give it at most once\n"},
		{name: "--session-id twice", env: configured, args: []string{"check", "x", "--session-id", "a", "--session-id", "b"}, wantCode: releash.ExitDuplicate, wantStderr: "releash: --session-id given 2 times; give it at most once\n"},
		{name: "--remote-address twice", env: configured, args: []string{"check", "x", "--remote-address", "::1", "--remote-address", "::1"}, wantCode: releash.ExitDuplicate, wantStderr: "releash: --remote-address given 2 times; give it at most once\n"},
		{name: "--current-time twice", env: configured, args: []string{"check", "x", "--current-time", "2026-11-27T10:00:00Z", "--current-time", "2026-11-27T10:00:00Z"}, wantCode: releash.ExitDuplicate, wantStderr: "releash: --current-time given 2 times; give it at most once\n"},
		{name: "repeated property name", env: configured, args: []string{"check", "x", "--properties", "region=eu", "--properties", "region=us"}, wantCode: releash.ExitDuplicate, wantStderr: "releash: property \"region\" given 2 times in --properties; give each name once\n"},

		// Precedence.
		{
			name:       "missing token and invalid --current-time exits 3",
			env:        map[string]string{"UNLEASH_URL": testURL},
			args:       []string{"check", "x", "--current-time", "tomorrow"},
			wantCode:   releash.ExitNotConfigured,
			wantStderr: noToken + "releash: invalid --current-time \"tomorrow\": must be an RFC 3339 timestamp\n",
		},
		{
			name:       "invalid --timeout and missing token exits 2",
			env:        map[string]string{"UNLEASH_URL": testURL},
			args:       []string{"check", "x", "--timeout", "abc"},
			wantCode:   releash.ExitUsage,
			wantStderr: "releash: invalid --timeout \"abc\": must be a positive Go duration, e.g. 5s\n" + noToken + checkHelp,
		},
	}

	for _, tt := range tests {
		variants := map[string][]string{"": tt.args}
		if !tt.loud {
			variants[" with --loud"] = append(append([]string{}, tt.args...), "--loud")
		}
		for suffix, args := range variants {
			t.Run(tt.name+suffix, func(t *testing.T) {
				unleashEnv(t, tt.env)
				var stdout, stderr bytes.Buffer

				code := execute(args, &stdout, &stderr, "dev")

				assert.Equal(t, tt.wantCode, code, "exit code")
				assert.Equal(t, "false\n", stdout.String(), "stdout")
				wantStderr := tt.wantStderr
				if suffix != "" && tt.wantCode == releash.ExitOK {
					wantStderr = loudLine
				}
				if tt.wantStderrPrefix != "" {
					assert.True(t, strings.HasPrefix(stderr.String(), tt.wantStderrPrefix), "stderr: %q", stderr.String())
					assert.True(t, strings.HasSuffix(stderr.String(), wantStderr), "stderr: %q", stderr.String())
					return
				}
				assert.Equal(t, wantStderr, stderr.String(), "stderr")
			})
		}
	}
}

func TestCheckPrintsTheEvaluatedAnswer(t *testing.T) {
	for _, args := range [][]string{{"check", "enabled-everywhere"}, {"check", "enabled-everywhere", "--loud"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			unleashEnv(t, map[string]string{"UNLEASH_URL": testURL, "UNLEASH_API_TOKEN": testToken})
			var stdout, stderr bytes.Buffer

			code := execute(args, &stdout, &stderr, "dev")

			assert.Equal(t, releash.ExitOK, code, "exit code")
			assert.Equal(t, "true\n", stdout.String(), "stdout")
			assert.Empty(t, stderr.String(), "stderr")
		})
	}
}

func TestCheckHelp(t *testing.T) {
	for _, args := range [][]string{{"check", "--help"}, {"check", "x", "-h"}, {"help", "check"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			unleashEnv(t, nil)
			var stdout, stderr bytes.Buffer

			code := execute(args, &stdout, &stderr, "dev")

			assert.Equal(t, releash.ExitOK, code, "exit code")
			assert.Empty(t, stderr.String(), "stderr")
			out := stdout.String()
			assert.True(t, strings.HasPrefix(out, "releash check prints true"), "stdout: %q", out)
			assert.NotContains(t, out, "[=true|false]", "stdout")
			// The synopsis, then each heading, then its options in spec order.
			want := []string{
				"releash check <flag> [--url <url>]",
				"Releash options:",
				"--url url", "--token token", "--timeout duration", "--loud",
				"Context options:",
				"--env name", "--app-name name", "--user-id id", "--session-id id", "--remote-address ip", "--current-time timestamp", "--properties name=value",
				"-h, --help", "-v, --version",
			}
			rest := out
			for _, w := range want {
				i := strings.Index(rest, w)
				if !assert.GreaterOrEqual(t, i, 0, "%q missing or out of order in:\n%s", w, out) {
					return
				}
				rest = rest[i+len(w):]
			}
			assert.Contains(t, out, `(default "production")`)
			assert.Contains(t, out, `(default "releash")`)
			assert.Contains(t, out, `(default "5s")`)
			assert.Contains(t, out, "UNLEASH_URL")
			assert.Contains(t, out, "UNLEASH_API_TOKEN")
		})
	}
}

// --version is informational on check as on the root command (spec 0001): it
// prints the version alone, exits 0, and skips validation, so it works with
// nothing configured and with a missing or invalid flag name.
func TestCheckVersion(t *testing.T) {
	for _, args := range [][]string{
		{"check", "--version"},
		{"check", "x", "--version"},
		{"check", "x", "extra", "--version", "--timeout", "abc"},
		{"--version", "check", "x"},
		{"-v", "check", "x"},
		{"--version=true", "check", "x"},
		{"--version"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			unleashEnv(t, nil)
			var stdout, stderr bytes.Buffer

			code := execute(args, &stdout, &stderr, "v1.2.3")

			assert.Equal(t, releash.ExitOK, code, "exit code")
			assert.Equal(t, "releash version v1.2.3\n", stdout.String(), "stdout")
			assert.Empty(t, stderr.String(), "stderr")
		})
	}
}

func TestRootHelpListsCheck(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := execute([]string{"--help"}, &stdout, &stderr, "dev")

	assert.Equal(t, releash.ExitOK, code)
	assert.Contains(t, stdout.String(), "check")
}

func TestPanicDuringCheckStillPrintsFalse(t *testing.T) {
	unleashEnv(t, map[string]string{"UNLEASH_URL": testURL, "UNLEASH_API_TOKEN": testToken})
	var stdout, stderr bytes.Buffer
	r := releash.New()
	root := NewRootCommand(r, "dev")
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	check, _, err := root.Find([]string{"check"})
	require.NoError(t, err)
	check.RunE = func(*cobra.Command, []string) error { panic("boom") }

	code := run(root, []string{"check", "x"}, &stdout, &stderr)

	assert.Equal(t, releash.ExitInternal, code, "exit code")
	assert.Equal(t, "false\n", stdout.String(), "stdout")
	assert.Equal(t, "releash: internal error: boom\n", stderr.String(), "stderr")
}
