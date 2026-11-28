package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanklee86/releash/internal/releash"
)

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

const (
	variantHelp    = "Run 'releash variant --help' for usage.\n"
	dotenvFallback = "RELEASH_X_ENABLED=false\nRELEASH_X_VARIANT=disabled\n"
	shellFallback  = "export RELEASH_X_ENABLED='false'\nexport RELEASH_X_VARIANT='disabled'\n"
	variantLoud    = "releash: flag \"x\" not found; returning disabled variant\n"
)

func buildInfo(version string) func() (*debug.BuildInfo, bool) {
	return func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{Main: debug.Module{Path: "github.com/ivanklee86/releash", Version: version}}, true
	}
}

func noBuildInfo() (*debug.BuildInfo, bool) {
	return nil, false
}

func TestExecute(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		version    string
		wantCode   int
		wantStdout string
		// wantStdoutContains and wantStderrContains are used when the exact
		// text is cobra's to decide.
		wantStdoutContains []string
		wantStderrContains []string
		wantStdoutLacks    []string
	}{
		{
			name:               "no arguments prints help",
			args:               nil,
			version:            "dev",
			wantCode:           releash.ExitOK,
			wantStdoutContains: []string{"Usage:", "releash [flags]", "--help", "--version"},
			// pflag v1.1.0 renders bool flags as --help[=true|false].
			wantStdoutLacks: []string{"[=true|false]"},
		},
		{
			name:               "--help prints help",
			args:               []string{"--help"},
			version:            "dev",
			wantCode:           releash.ExitOK,
			wantStdoutContains: []string{"Usage:", "releash [flags]", "--help", "--version"},
			// pflag v1.1.0 renders bool flags as --help[=true|false].
			wantStdoutLacks: []string{"[=true|false]"},
		},
		{
			name:               "help --help prints the help command's usage",
			args:               []string{"help", "--help"},
			version:            "dev",
			wantCode:           releash.ExitOK,
			wantStdoutContains: []string{"Usage:", "releash help [command]", "--help"},
			// cobra adds the help command when it runs, after NewRootCommand.
			wantStdoutLacks: []string{"[=true|false]"},
		},
		{
			name:       "--version prints the injected version",
			args:       []string{"--version"},
			version:    resolveVersion("v1.2.3", buildInfo("v0.0.0-20261127120000-abcdef123456")),
			wantCode:   releash.ExitOK,
			wantStdout: "releash version v1.2.3\n",
		},
		{
			name:       "--version falls back to the build info version",
			args:       []string{"--version"},
			version:    resolveVersion("", buildInfo("v0.1.0")),
			wantCode:   releash.ExitOK,
			wantStdout: "releash version v0.1.0\n",
		},
		{
			name:       "--version falls back to dev without build info",
			args:       []string{"--version"},
			version:    resolveVersion("", noBuildInfo),
			wantCode:   releash.ExitOK,
			wantStdout: "releash version dev\n",
		},
		{
			name:               "unknown command exits 2",
			args:               []string{"bogus"},
			version:            "dev",
			wantCode:           releash.ExitUsage,
			wantStderrContains: []string{`unknown command "bogus"`, "releash --help"},
		},
		{
			name:               "completion command is disabled",
			args:               []string{"completion", "bash"},
			version:            "dev",
			wantCode:           releash.ExitUsage,
			wantStderrContains: []string{`unknown command "completion"`},
		},
		// The shared options are persistent flags of the root command, but
		// they need a command to apply to.
		{
			name:               "shared option without a command exits 2",
			args:               []string{"--loud"},
			version:            "dev",
			wantCode:           releash.ExitUsage,
			wantStderrContains: []string{"--loud needs a command", "releash --help"},
		},
		{
			name:               "invalid shared option without a command exits 2",
			args:               []string{"--timeout", "abc"},
			version:            "dev",
			wantCode:           releash.ExitUsage,
			wantStderrContains: []string{"--timeout needs a command"},
		},
		{
			name:               "unknown option exits 2",
			args:               []string{"--bogus"},
			version:            "dev",
			wantCode:           releash.ExitUsage,
			wantStderrContains: []string{"unknown flag: --bogus", "releash --help"},
		},
		// M1 allows a stray argument next to --help or --version: cobra
		// handles those flags before it validates arguments.
		{
			name:               "stray argument with --help prints help",
			args:               []string{"bogus", "--help"},
			version:            "dev",
			wantCode:           releash.ExitOK,
			wantStdoutContains: []string{"Usage:", "releash [flags]"},
		},
		{
			name:       "stray argument with --version prints the version",
			args:       []string{"bogus", "--version"},
			version:    "v1.2.3",
			wantCode:   releash.ExitOK,
			wantStdout: "releash version v1.2.3\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer

			code := execute(tt.args, &stdout, &stderr, tt.version)

			assert.Equal(t, tt.wantCode, code, "exit code")
			if tt.wantCode == releash.ExitOK {
				assert.Empty(t, stderr.String(), "stderr")
			} else {
				assert.Empty(t, stdout.String(), "stdout")
			}
			if tt.wantStdout != "" {
				assert.Equal(t, tt.wantStdout, stdout.String(), "stdout")
			}
			for _, want := range tt.wantStdoutContains {
				assert.Contains(t, stdout.String(), want, "stdout")
			}
			for _, unwanted := range tt.wantStdoutLacks {
				assert.NotContains(t, stdout.String(), unwanted, "stdout")
			}
			for _, want := range tt.wantStderrContains {
				assert.Contains(t, stderr.String(), want, "stderr")
			}
		})
	}
}

func TestRunRecoversPanics(t *testing.T) {
	var stdout, stderr bytes.Buffer
	cmd := &cobra.Command{
		Use: "panics",
		Run: func(*cobra.Command, []string) { panic("boom") },
	}
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	code := run(cmd, nil, &stdout, &stderr)

	assert.Equal(t, releash.ExitInternal, code, "exit code")
	assert.Empty(t, stdout.String(), "stdout")
	assert.Contains(t, stderr.String(), "boom", "stderr")
}

func TestResolveVersion(t *testing.T) {
	tests := []struct {
		name          string
		injected      string
		readBuildInfo func() (*debug.BuildInfo, bool)
		want          string
	}{
		{name: "injected wins over build info", injected: "v1.2.3", readBuildInfo: buildInfo("v0.1.0"), want: "v1.2.3"},
		{name: "module version from go install", readBuildInfo: buildInfo("v0.1.0"), want: "v0.1.0"},
		{name: "pseudo-version from go build", readBuildInfo: buildInfo("v0.0.0-20261127120000-abcdef123456+dirty"), want: "v0.0.0-20261127120000-abcdef123456+dirty"},
		{name: "(devel) is not a version", readBuildInfo: buildInfo("(devel)"), want: "dev"},
		{name: "empty build info version", readBuildInfo: buildInfo(""), want: "dev"},
		{name: "no build info", readBuildInfo: noBuildInfo, want: "dev"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, resolveVersion(tt.injected, tt.readBuildInfo))
		})
	}
}

// newEnvCommand returns a command with a --url flag that falls back to
// TEST_RELEASH_URL, an int --timeout flag that falls back to
// TEST_RELEASH_TIMEOUT, and a --plain flag with no environment variable.
func newEnvCommand(t *testing.T) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().String("url", "", "")
	cmd.Flags().Int("timeout", 0, "")
	cmd.Flags().String("plain", "", "")
	require.NoError(t, cmd.Flags().SetAnnotation("url", envAnnotation, []string{"TEST_RELEASH_URL"}))
	require.NoError(t, cmd.Flags().SetAnnotation("timeout", envAnnotation, []string{"TEST_RELEASH_TIMEOUT"}))
	return cmd
}

func TestBindFlags(t *testing.T) {
	t.Run("environment variable fills an unset flag", func(t *testing.T) {
		t.Setenv("TEST_RELEASH_URL", "https://env.example.com")
		cmd := newEnvCommand(t)
		require.NoError(t, cmd.ParseFlags(nil))

		require.NoError(t, bindFlags(cmd, viper.New()))

		got, err := cmd.Flags().GetString("url")
		require.NoError(t, err)
		assert.Equal(t, "https://env.example.com", got)
	})

	t.Run("option takes precedence over environment variable", func(t *testing.T) {
		t.Setenv("TEST_RELEASH_URL", "https://env.example.com")
		cmd := newEnvCommand(t)
		require.NoError(t, cmd.ParseFlags([]string{"--url", "https://flag.example.com"}))

		require.NoError(t, bindFlags(cmd, viper.New()))

		got, err := cmd.Flags().GetString("url")
		require.NoError(t, err)
		assert.Equal(t, "https://flag.example.com", got)
	})

	t.Run("empty environment variable leaves the flag unset", func(t *testing.T) {
		t.Setenv("TEST_RELEASH_URL", "")
		cmd := newEnvCommand(t)
		require.NoError(t, cmd.ParseFlags(nil))

		require.NoError(t, bindFlags(cmd, viper.New()))

		assert.False(t, cmd.Flags().Changed("url"))
	})

	t.Run("flag without annotation ignores the environment", func(t *testing.T) {
		t.Setenv("PLAIN", "from-env")
		cmd := newEnvCommand(t)
		require.NoError(t, cmd.ParseFlags(nil))

		require.NoError(t, bindFlags(cmd, viper.New()))

		assert.False(t, cmd.Flags().Changed("plain"))
	})

	t.Run("invalid environment value is an error", func(t *testing.T) {
		t.Setenv("TEST_RELEASH_TIMEOUT", "soon")
		cmd := newEnvCommand(t)
		require.NoError(t, cmd.ParseFlags(nil))

		err := bindFlags(cmd, viper.New())

		require.Error(t, err)
		assert.Contains(t, err.Error(), "--timeout")
		assert.Contains(t, err.Error(), "TEST_RELEASH_TIMEOUT")
	})
}

// Cases for releash check through the root command, from milestone M3 and the
// test plan in spec 0001. internal/releash covers validation in detail; these
// check that options and environment variables reach it and that cobra's own
// errors keep the stdout and exit code contract:
//   - environment fallback for --url and --token, and options winning over it
//   - missing or empty URL or token from either source exits 3
//   - options before the check command work as they do after it
//   - missing flag name, extra arguments, unknown or malformed options (also
//     before the check command, where cobra blames the root command), an
//     invalid --timeout and a repeated releash option exit 2
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
		{name: "boolean option before check", env: configured, args: []string{"--loud", "check", "x"}, loud: true, wantCode: releash.ExitOK, wantStderr: loudLine},
		{name: "option with a value before check", args: []string{"--url", testURL, "--token=" + testToken, "check", "x"}, wantCode: releash.ExitOK},
		// --user-id takes "variant" as its value, so the command is check.
		{name: "command name as the value of an option before check", env: configured, args: []string{"--user-id", "variant", "check", "x"}, wantCode: releash.ExitOK},
		{name: "option before check and given again after it", env: configured, args: []string{"--env", "a", "check", "x", "--env", "b"}, wantCode: releash.ExitDuplicate, wantStderr: "releash: --env given 2 times; give it at most once\n"},
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
		// cobra blames the root command for these, and check's answer
		// contract still holds.
		{name: "unknown option before check", env: configured, args: []string{"--nope", "check", "x"}, wantCode: releash.ExitUsage, wantStderr: "releash: unknown flag: --nope\n" + checkHelp},
		// An empty $TOKEN in "--token $TOKEN check x" leaves check as the
		// value of --token.
		{name: "command name is the value of an option before it", env: configured, args: []string{"--token", "check", "x"}, wantCode: releash.ExitUsage, wantStderr: "releash: unknown command \"x\" for \"releash\"\nreleash: --token may be missing its value and have taken \"check\" instead\n" + checkHelp},
		// A mistyped command before check still gets check's answer.
		{name: "unknown command before check", env: configured, args: []string{"foo", "check"}, wantCode: releash.ExitUsage, wantStderr: "releash: unknown command \"foo\" for \"releash\"\n" + checkHelp},
		{name: "variant option before check", env: configured, args: []string{"--format", "shell", "check", "x"}, wantCode: releash.ExitUsage, wantStderr: "releash: unknown flag: --format\n" + checkHelp},
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
			assert.Contains(t, out, "(default: the local address of the connection to Unleash)", "--remote-address default")
			assert.Contains(t, out, "(default: the time of the check)", "--current-time default")
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

// Cases for releash variant through the root command, from milestone M6 and
// the test plan in spec 0003. internal/releash covers evaluation and
// validation; these check the command line around them:
//   - options and environment variables reach Variant, and an evaluated
//     variant is printed
//   - options before the command work as they do after it
//   - a bad command line exits 2 with empty stdout, whatever --format and
//     --prefix say: cobra's parse errors, also before the command, a missing
//     or extra flag name and a repeated or invalid option
//   - exits 3 to 5 print the fallback in the format and with the prefix
//     given; --loud changes neither stdout nor the exit code
//   - --help lists the releash options, including --format and --prefix,
//     the context options and the other options under check's headings
//   - --version prints the version and exits 0
//   - a panic prints the fallback and exits 1

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

		// A bad command line prints nothing (ADR 0008), whatever --format
		// and --prefix say.
		{name: "unknown option with --format", env: configured, args: []string{"variant", "x", "--bogus", "--format", "shell"}, wantCode: releash.ExitUsage, wantStderr: "releash: unknown flag: --bogus\n" + variantHelp},
		{name: "unknown shorthand", env: configured, args: []string{"variant", "x", "-z", "--format=shell"}, wantCode: releash.ExitUsage, wantStderr: "releash: unknown shorthand flag: 'z' in -z\n" + variantHelp},
		{name: "trailing --format", env: configured, args: []string{"variant", "x", "--format"}, loud: true, wantCode: releash.ExitUsage, wantStderr: "releash: flag needs an argument: --format\n" + variantHelp},
		{name: "missing flag name", env: configured, args: []string{"variant"}, wantCode: releash.ExitUsage, wantStderr: "releash: missing flag name\n" + variantHelp},
		{name: "missing flag name with --prefix", env: configured, args: []string{"variant", "--prefix", "P_"}, wantCode: releash.ExitUsage, wantStderr: "releash: missing flag name\n" + variantHelp},
		{name: "empty flag name", env: configured, args: []string{"variant", ""}, wantCode: releash.ExitUsage, wantStderr: "releash: missing flag name\n" + variantHelp},
		{name: "two flag names", env: configured, args: []string{"variant", "x", "y", "--prefix", "P_", "--format", "shell"}, wantCode: releash.ExitUsage, wantStderr: "releash: expected one flag name, got 2 arguments: x y\n" + variantHelp},

		// Options before the command.
		{name: "boolean option before variant", env: configured, args: []string{"--loud", "variant", "x"}, loud: true, wantCode: releash.ExitOK, wantStdout: dotenvFallback, wantStderr: variantLoud},
		{name: "--format before variant", env: configured, args: []string{"--format", "shell", "variant", "x"}, wantCode: releash.ExitOK, wantStdout: shellFallback},
		// --user-id takes the first "variant" as its value.
		{name: "command name as the value of an option before variant", env: configured, args: []string{"--user-id", "variant", "variant", "x"}, wantCode: releash.ExitOK, wantStdout: dotenvFallback},
		{name: "check as the value of an option before variant", env: configured, args: []string{"--app-name", "check", "variant", "x"}, wantCode: releash.ExitOK, wantStdout: dotenvFallback},
		{name: "unknown option before variant", env: configured, args: []string{"--nope", "variant", "x"}, wantCode: releash.ExitUsage, wantStderr: "releash: unknown flag: --nope\n" + variantHelp},
		{name: "only command name is the value of an option before it", env: configured, args: []string{"--token", "variant", "x"}, wantCode: releash.ExitUsage, wantStderr: "releash: unknown command \"x\" for \"releash\"\nreleash: --token may be missing its value and have taken \"variant\" instead\n" + variantHelp},
		{name: "variant before a flag named check", env: configured, args: []string{"variant", "--loud", "check"}, loud: true, wantCode: releash.ExitOK, wantStdout: "RELEASH_CHECK_ENABLED=false\nRELEASH_CHECK_VARIANT=disabled\n", wantStderr: "releash: flag \"check\" not found; returning disabled variant\n"},

		// Validation in Variant.
		{name: "invalid --format", env: configured, args: []string{"variant", "x", "--format", "json"}, wantCode: releash.ExitUsage, wantStderr: "releash: invalid --format \"json\": must be one of \"dotenv\" or \"shell\"\n" + variantHelp},
		{name: "invalid --prefix", env: configured, args: []string{"variant", "x", "--prefix", "lower_"}, wantCode: releash.ExitUsage, wantStderr: "releash: invalid --prefix \"lower_\": must match ^[A-Z][A-Z0-9_]*$\n" + variantHelp},
		{name: "empty --prefix", env: configured, args: []string{"variant", "x", "--prefix="}, wantCode: releash.ExitUsage, wantStderr: "releash: invalid --prefix \"\": must match ^[A-Z][A-Z0-9_]*$\n" + variantHelp},
		{name: "--format twice with the same value", env: configured, args: []string{"variant", "x", "--format", "shell", "--format", "shell"}, wantCode: releash.ExitUsage, wantStderr: "releash: --format given 2 times; give it at most once\n" + variantHelp},
		{name: "--prefix twice with different values", env: configured, args: []string{"variant", "x", "--prefix", "P_", "--prefix", "Q_"}, wantCode: releash.ExitUsage, wantStderr: "releash: --prefix given 2 times; give it at most once\n" + variantHelp},
		{name: "--url twice", env: configured, args: []string{"variant", "x", "--url", testURL, "--url", testURL, "--format", "shell"}, wantCode: releash.ExitUsage, wantStderr: "releash: --url given 2 times; give it at most once\n" + variantHelp},
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
	assert.Regexp(t, `(?m)^\s+variant\s+Print a feature flag's variant`, stdout.String())
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

// The shared options are root persistent flags so they may go before the
// command, but the root command's help lists only --help and --version, and
// the help command lists no global options.
func TestRootHelpHidesSharedOptions(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"help", "--help"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer

			code := execute(args, &stdout, &stderr, "dev")

			assert.Equal(t, releash.ExitOK, code, "exit code")
			assert.Empty(t, stderr.String(), "stderr")
			for _, unwanted := range []string{"--url", "--token", "--loud", "--user-id", "Global Flags"} {
				assert.NotContains(t, stdout.String(), unwanted, "stdout")
			}
		})
	}
}

func TestCheckHelpDescribesItsOwnLoud(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := execute([]string{"check", "--help"}, &stdout, &stderr, "dev")

	assert.Equal(t, releash.ExitOK, code, "exit code")
	assert.Contains(t, stdout.String(), "explain on stderr why the answer is false")
	assert.NotContains(t, stdout.String(), "variant fell back")
}

// bindFlags marks --url and --token as changed when it sets them from the
// environment, which must not count as an option given without a command.
func TestRootIgnoresSharedOptionsFromTheEnvironment(t *testing.T) {
	unleashEnv(t, map[string]string{"UNLEASH_URL": testURL, "UNLEASH_API_TOKEN": testToken})
	var stdout, stderr bytes.Buffer

	code := execute(nil, &stdout, &stderr, "dev")

	assert.Equal(t, releash.ExitOK, code, "exit code")
	assert.Contains(t, stdout.String(), "Usage:", "stdout")
	assert.Empty(t, stderr.String(), "stderr")
}
