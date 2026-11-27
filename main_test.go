package main

import (
	"bytes"
	"runtime/debug"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanklee86/releash/internal/releash"
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
		{
			name:               "unknown option exits 2",
			args:               []string{"--bogus"},
			version:            "dev",
			wantCode:           releash.ExitUsage,
			wantStderrContains: []string{"unknown flag: --bogus", "releash --help"},
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
	cmd.SetArgs(nil)
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	code := run(cmd, &stderr)

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
