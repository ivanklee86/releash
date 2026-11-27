package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanklee86/releash/internal/releash"
)

// Cases for releash variant through the root command, from milestone M6 and
// the test plan in spec 0003. internal/releash covers evaluation and
// validation; these check the command line around them:
//   - options and environment variables reach Variant, and an evaluated
//     variant is printed
//   - every exit prints the fallback in the format and with the prefix the
//     raw arguments give, or nothing when they give no single one: cobra's
//     parse errors before and after --format, an option given before the
//     command, a missing or extra flag name, a trailing --format, --format
//     after --, a --format that is another option's value, and every code of
//     spec 0001; --loud changes neither stdout nor the exit code
//   - --help lists the releash options, including --format and --prefix,
//     the context options and the other options under check's headings
//   - --version prints the version and exits 0
//   - a panic prints the fallback and exits 1

const (
	variantHelp    = "Run 'releash variant --help' for usage.\n"
	dotenvFallback = "RELEASH_X_ENABLED=false\nRELEASH_X_VARIANT=disabled\n"
	shellFallback  = "export RELEASH_X_ENABLED='false'\nexport RELEASH_X_VARIANT='disabled'\n"
	variantLoud    = "releash: flag \"x\" not found; returning disabled variant\n"
)

func TestVariantCommand(t *testing.T) {
	configured := map[string]string{"UNLEASH_URL": testURL, "UNLEASH_API_TOKEN": testToken}

	tests := []struct {
		name string
		env  map[string]string
		args []string
		// loud is set for cases that pass --loud themselves or where an
		// appended --loud would change the meaning; the others also run with
		// --loud added.
		loud       bool
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{name: "URL and token from the environment", env: configured, args: []string{"variant", "x"}, wantCode: releash.ExitOK, wantStdout: dotenvFallback},
		{name: "URL and token from options", args: []string{"variant", "x", "--url", testURL, "--token", testToken, "--format", "shell"}, wantCode: releash.ExitOK, wantStdout: shellFallback},
		{name: "--loud explains the fallback", env: configured, args: []string{"variant", "x", "--loud"}, loud: true, wantCode: releash.ExitOK, wantStdout: dotenvFallback, wantStderr: variantLoud},
		{
			name:       "every option once",
			env:        configured,
			args:       []string{"variant", "x", "--timeout", "30s", "--format", "dotenv", "--prefix", "P_", "--env", "staging", "--app-name", "pipeline", "--user-id", "alice", "--session-id", "s-1", "--remote-address", "::1", "--current-time", "2026-11-27T10:00:00Z", "--properties", "region=eu"},
			wantCode:   releash.ExitOK,
			wantStdout: "P_ENABLED=false\nP_VARIANT=disabled\n",
		},
		{name: "--format= and --prefix= forms", env: configured, args: []string{"variant", "x", "--format=shell", "--prefix=P_"}, wantCode: releash.ExitOK, wantStdout: "export P_ENABLED='false'\nexport P_VARIANT='disabled'\n"},

		// The scan of the raw arguments decides the fallback of parse errors.
		{name: "unknown option before --format", env: configured, args: []string{"variant", "x", "--bogus", "--format", "shell"}, wantCode: releash.ExitUsage, wantStdout: shellFallback, wantStderr: "releash: unknown flag: --bogus\n" + variantHelp},
		{name: "unknown option after --format", env: configured, args: []string{"variant", "x", "--format", "shell", "--bogus"}, wantCode: releash.ExitUsage, wantStdout: shellFallback, wantStderr: "releash: unknown flag: --bogus\n" + variantHelp},
		{name: "unknown shorthand", env: configured, args: []string{"variant", "x", "-z", "--format=shell"}, wantCode: releash.ExitUsage, wantStdout: shellFallback, wantStderr: "releash: unknown shorthand flag: 'z' in -z\n" + variantHelp},
		{name: "trailing --format", env: configured, args: []string{"variant", "x", "--format"}, loud: true, wantCode: releash.ExitUsage, wantStderr: "releash: flag needs an argument: --format\n" + variantHelp},
		{name: "--format after --", env: configured, args: []string{"variant", "x", "--bogus", "--", "--format", "shell"}, wantCode: releash.ExitUsage, wantStderr: "releash: unknown flag: --bogus\n" + variantHelp},
		{name: "--format after -- with --prefix before it", env: configured, args: []string{"variant", "x", "--prefix", "P_", "--bogus", "--", "--format", "shell"}, wantCode: releash.ExitUsage, wantStdout: "P_ENABLED=false\nP_VARIANT=disabled\n", wantStderr: "releash: unknown flag: --bogus\n" + variantHelp},
		{name: "--user-id --format is a user ID", env: configured, args: []string{"variant", "x", "--user-id", "--format", "--bogus"}, wantCode: releash.ExitUsage, wantStdout: dotenvFallback, wantStderr: "releash: unknown flag: --bogus\n" + variantHelp},
		{name: "--format whose value starts with -", env: configured, args: []string{"variant", "x", "--format", "--loud", "--bogus"}, wantCode: releash.ExitUsage, wantStderr: "releash: unknown flag: --bogus\n" + variantHelp},
		{name: "invalid --format with a parse error", env: configured, args: []string{"variant", "x", "--format", "json", "--bogus"}, wantCode: releash.ExitUsage, wantStderr: "releash: unknown flag: --bogus\n" + variantHelp},
		{name: "two different formats with a parse error", env: configured, args: []string{"variant", "x", "--format", "shell", "--format=dotenv", "--bogus"}, wantCode: releash.ExitUsage, wantStderr: "releash: unknown flag: --bogus\n" + variantHelp},
		{name: "the same --format twice with a parse error", env: configured, args: []string{"variant", "x", "--format", "shell", "--format=shell", "--bogus"}, wantCode: releash.ExitUsage, wantStdout: shellFallback, wantStderr: "releash: unknown flag: --bogus\n" + variantHelp},
		{name: "invalid --prefix with a parse error", env: configured, args: []string{"variant", "x", "--prefix", "p_", "--bogus"}, wantCode: releash.ExitUsage, wantStderr: "releash: unknown flag: --bogus\n" + variantHelp},
		{name: "two different prefixes with a parse error", env: configured, args: []string{"variant", "x", "--prefix", "P_", "--prefix", "Q_", "--bogus"}, wantCode: releash.ExitUsage, wantStderr: "releash: unknown flag: --bogus\n" + variantHelp},
		{name: "missing flag name", env: configured, args: []string{"variant"}, wantCode: releash.ExitUsage, wantStderr: "releash: missing flag name\n" + variantHelp},
		{name: "missing flag name with --prefix", env: configured, args: []string{"variant", "--prefix", "P_"}, wantCode: releash.ExitUsage, wantStdout: "P_ENABLED=false\nP_VARIANT=disabled\n", wantStderr: "releash: missing flag name\n" + variantHelp},
		{name: "empty flag name", env: configured, args: []string{"variant", ""}, wantCode: releash.ExitUsage, wantStderr: "releash: missing flag name\n" + variantHelp},
		{name: "two flag names", env: configured, args: []string{"variant", "x", "y"}, wantCode: releash.ExitUsage, wantStderr: "releash: expected one flag name, got 2 arguments: x y\n" + variantHelp},
		{name: "two flag names with --prefix", env: configured, args: []string{"variant", "x", "y", "--prefix", "P_", "--format", "shell"}, wantCode: releash.ExitUsage, wantStdout: "export P_ENABLED='false'\nexport P_VARIANT='disabled'\n", wantStderr: "releash: expected one flag name, got 2 arguments: x y\n" + variantHelp},
		{name: "a flag named variant", env: configured, args: []string{"variant", "variant", "--bogus"}, wantCode: releash.ExitUsage, wantStdout: "RELEASH_VARIANT_ENABLED=false\nRELEASH_VARIANT_VARIANT=disabled\n", wantStderr: "releash: unknown flag: --bogus\n" + variantHelp},

		// Options before the command.
		{name: "boolean option before variant", env: configured, args: []string{"--loud", "variant", "x"}, loud: true, wantCode: releash.ExitUsage, wantStdout: dotenvFallback, wantStderr: "releash: --loud given before variant; put options after 'variant <flag>'\n" + variantHelp},
		{name: "--format before variant", env: configured, args: []string{"--format", "shell", "variant", "x"}, wantCode: releash.ExitUsage, wantStdout: shellFallback, wantStderr: "releash: --format given before variant; put options after 'variant <flag>'\n" + variantHelp},
		{name: "option with a value before variant", env: configured, args: []string{"--user-id", "variant", "variant", "x"}, wantCode: releash.ExitUsage, wantStdout: dotenvFallback, wantStderr: "releash: --user-id given before variant; put options after 'variant <flag>'\n" + variantHelp},
		{name: "variant before a flag named check", env: configured, args: []string{"variant", "--loud", "check"}, loud: true, wantCode: releash.ExitOK, wantStdout: "RELEASH_CHECK_ENABLED=false\nRELEASH_CHECK_VARIANT=disabled\n", wantStderr: "releash: flag \"check\" not found; returning disabled variant\n"},

		// Validation in Variant.
		{name: "invalid --format", env: configured, args: []string{"variant", "x", "--format", "json"}, wantCode: releash.ExitUsage, wantStderr: "releash: invalid --format \"json\": must be one of \"dotenv\" or \"shell\"\n" + variantHelp},
		{name: "invalid --prefix", env: configured, args: []string{"variant", "x", "--prefix", "lower_"}, wantCode: releash.ExitUsage, wantStderr: "releash: invalid --prefix \"lower_\": must match ^[A-Z][A-Z0-9_]*$\n" + variantHelp},
		{name: "empty --prefix", env: configured, args: []string{"variant", "x", "--prefix="}, wantCode: releash.ExitUsage, wantStderr: "releash: invalid --prefix \"\": must match ^[A-Z][A-Z0-9_]*$\n" + variantHelp},
		{name: "--format twice with the same value", env: configured, args: []string{"variant", "x", "--format", "shell", "--format", "shell"}, wantCode: releash.ExitUsage, wantStdout: shellFallback, wantStderr: "releash: --format given 2 times; give it at most once\n" + variantHelp},
		{name: "--prefix twice with different values", env: configured, args: []string{"variant", "x", "--prefix", "P_", "--prefix", "Q_"}, wantCode: releash.ExitUsage, wantStderr: "releash: --prefix given 2 times; give it at most once\n" + variantHelp},
		{name: "--url twice", env: configured, args: []string{"variant", "x", "--url", testURL, "--url", testURL, "--format", "shell"}, wantCode: releash.ExitUsage, wantStdout: shellFallback, wantStderr: "releash: --url given 2 times; give it at most once\n" + variantHelp},
		{name: "nothing configured", args: []string{"variant", "x", "--format", "shell"}, wantCode: releash.ExitNotConfigured, wantStdout: shellFallback, wantStderr: noURL + noToken},
		{name: "invalid --remote-address", env: configured, args: []string{"variant", "x", "--remote-address", "nope", "--prefix", "P_"}, wantCode: releash.ExitInvalid, wantStdout: "P_ENABLED=false\nP_VARIANT=disabled\n", wantStderr: "releash: invalid --remote-address \"nope\": must be an IPv4 or IPv6 address\n"},
		{name: "repeated property name", env: configured, args: []string{"variant", "x", "--properties", "a=1", "--properties", "a=2"}, wantCode: releash.ExitDuplicate, wantStdout: dotenvFallback, wantStderr: "releash: property \"a\" given 2 times in --properties; give each name once\n"},
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
				assert.Equal(t, tt.wantStdout, stdout.String(), "stdout")
				wantStderr := tt.wantStderr
				if suffix != "" && tt.wantCode == releash.ExitOK {
					wantStderr = variantLoud
				}
				assert.Equal(t, wantStderr, stderr.String(), "stderr")
			})
		}
	}
}

func TestVariantPrintsTheEvaluatedVariant(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/client/features" {
			_, _ = io.WriteString(w, `{"version":2,"features":[
				{"name":"checkout-redesign","enabled":true,"strategies":[{"name":"default"}],
				 "variants":[{"name":"treatment","weight":1000,"weightType":"variable","stickiness":"default","payload":{"type":"json","value":"{\"image\":{\"tag\":\"v2\"}}"}}]},
				{"name":"off","enabled":false,"strategies":[{"name":"default"}]}]}`)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(srv.Close)
	unleashEnv(t, map[string]string{"UNLEASH_URL": srv.URL + "/api", "UNLEASH_API_TOKEN": testToken})

	t.Run("enabled", func(t *testing.T) {
		var stdout, stderr bytes.Buffer

		code := execute([]string{"variant", "checkout-redesign", "--user-id", "repo/a", "--format", "shell", "--loud"}, &stdout, &stderr, "dev")

		assert.Equal(t, releash.ExitOK, code, "exit code")
		assert.Equal(t, `export RELEASH_CHECKOUT_REDESIGN_ENABLED='true'
export RELEASH_CHECKOUT_REDESIGN_VARIANT='treatment'
export RELEASH_CHECKOUT_REDESIGN_PAYLOAD='{"image":{"tag":"v2"}}'
export RELEASH_CHECKOUT_REDESIGN_PAYLOAD__IMAGE__TAG='v2'
`, stdout.String(), "stdout")
		assert.Empty(t, stderr.String(), "stderr")
	})
	t.Run("disabled", func(t *testing.T) {
		var stdout, stderr bytes.Buffer

		code := execute([]string{"variant", "off"}, &stdout, &stderr, "dev")

		assert.Equal(t, releash.ExitDisabled, code, "exit code")
		assert.Equal(t, "RELEASH_OFF_ENABLED=false\nRELEASH_OFF_VARIANT=disabled\n", stdout.String(), "stdout")
		assert.Equal(t, "releash: flag \"off\" is disabled; gate on \"releash check off\" before loading its variant\n", stderr.String(), "stderr")
	})
	t.Run("enabled without variants", func(t *testing.T) {
		unleashEnv(t, map[string]string{"UNLEASH_URL": testURL, "UNLEASH_API_TOKEN": testToken})
		var stdout, stderr bytes.Buffer

		code := execute([]string{"variant", "enabled-everywhere", "--loud"}, &stdout, &stderr, "dev")

		assert.Equal(t, releash.ExitOK, code, "exit code")
		assert.Equal(t, "RELEASH_ENABLED_EVERYWHERE_ENABLED=true\nRELEASH_ENABLED_EVERYWHERE_VARIANT=disabled\n", stdout.String(), "stdout")
		assert.Empty(t, stderr.String(), "stderr")
	})
}

func TestVariantHelp(t *testing.T) {
	for _, args := range [][]string{{"variant", "--help"}, {"variant", "x", "-h"}, {"help", "variant"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			unleashEnv(t, nil)
			var stdout, stderr bytes.Buffer

			code := execute(args, &stdout, &stderr, "dev")

			assert.Equal(t, releash.ExitOK, code, "exit code")
			assert.Empty(t, stderr.String(), "stderr")
			out := stdout.String()
			assert.True(t, strings.HasPrefix(out, "releash variant prints the variant"), "stdout: %q", out)
			assert.NotContains(t, out, "[=true|false]", "stdout")
			want := []string{
				"releash variant <flag> [--url <url>]", "[--format dotenv|shell] [--prefix <prefix>]",
				"Releash options:",
				"--url url", "--token token", "--timeout duration", "--loud", "--format format", "--prefix prefix",
				"Context options:",
				"--env name", "--app-name name", "--user-id id", "--session-id id", "--remote-address ip", "--current-time timestamp", "--properties name=value",
				"Other options:",
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
			assert.Contains(t, out, "(default dotenv)")
			assert.Contains(t, out, "^[A-Z][A-Z0-9_]*$")
			assert.Contains(t, out, `(default "production")`)
			assert.Contains(t, out, "UNLEASH_API_TOKEN")
		})
	}
}

func TestVariantVersion(t *testing.T) {
	for _, args := range [][]string{
		{"variant", "--version"},
		{"variant", "x", "--version", "--format", "json"},
		{"--version", "variant", "x"},
		{"-v", "variant", "x"},
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

func TestRootHelpListsVariant(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := execute([]string{"--help"}, &stdout, &stderr, "dev")

	assert.Equal(t, releash.ExitOK, code)
	assert.Contains(t, stdout.String(), "variant")
}

func TestPanicDuringVariantPrintsTheFallback(t *testing.T) {
	tests := []struct {
		args       []string
		wantStdout string
	}{
		{[]string{"variant", "x", "--format", "shell"}, shellFallback},
		{[]string{"variant", "x", "--prefix", "P_"}, "P_ENABLED=false\nP_VARIANT=disabled\n"},
		{[]string{"variant", "x", "--format", "json"}, ""},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			unleashEnv(t, map[string]string{"UNLEASH_URL": testURL, "UNLEASH_API_TOKEN": testToken})
			var stdout, stderr bytes.Buffer
			r := releash.New()
			root := NewRootCommand(r, "dev")
			root.SetOut(&stdout)
			root.SetErr(&stderr)
			variant, _, err := root.Find([]string{"variant"})
			require.NoError(t, err)
			variant.RunE = func(*cobra.Command, []string) error { panic("boom") }

			code := run(root, tt.args, &stdout, &stderr)

			assert.Equal(t, releash.ExitInternal, code, "exit code")
			assert.Equal(t, tt.wantStdout, stdout.String(), "stdout")
			assert.Equal(t, "releash: internal error: boom\n", stderr.String(), "stderr")
		})
	}
}
