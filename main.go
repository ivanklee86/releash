// Command releash answers rollout questions from Unleash feature flags in
// CI/CD pipelines.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/ivanklee86/releash/internal/releash"
)

// version is injected at build time with -ldflags "-X main.version=<version>".
var version string

// envAnnotation is the flag annotation that names the environment variables a
// flag falls back to. releash uses the names from spec 0001 (UNLEASH_URL,
// UNLEASH_API_TOKEN) rather than a common prefix, so each flag names its own.
const envAnnotation = "releash_env"

func main() {
	os.Exit(execute(os.Args[1:], os.Stdout, os.Stderr, resolveVersion(version, debug.ReadBuildInfo)))
}

// execute runs the CLI with args and returns the process exit code.
func execute(args []string, stdout, stderr io.Writer, version string) int {
	r := releash.New()
	r.Out = stdout
	r.Err = stderr

	cmd := NewRootCommand(r, version)
	cmd.SetArgs(args)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	return run(cmd, stderr)
}

// run executes cmd and returns its exit code. It is the only place that turns
// cobra errors and panics into exit codes: a panic in a command is reported on
// stderr and exits 1 (ADR 0004), rather than Go's default of 2, which would
// look like a bad command line.
func run(cmd *cobra.Command, stderr io.Writer) (code int) {
	defer func() {
		if p := recover(); p != nil {
			_, _ = fmt.Fprintf(stderr, "Error: internal error: %v\n", p)
			code = releash.ExitInternal
		}
	}()

	if err := cmd.Execute(); err != nil {
		// Until releash has commands, every error cobra returns comes from
		// parsing the command line. Commands will need their own exit codes.
		_, _ = fmt.Fprintf(stderr, "Error: %v\nRun 'releash --help' for usage.\n", err)
		return releash.ExitUsage
	}
	return releash.ExitOK
}

// NewRootCommand returns the releash root command, which prints help when run
// without a subcommand and reports version for --version. Commands write
// through r.
func NewRootCommand(r *releash.Releash, version string) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "releash",
		Short:   "Drive CI/CD changes with Unleash feature flags.",
		Long:    "releash asks Unleash whether a feature flag is enabled, so CI/CD pipelines can gate rollout steps on it.",
		Version: version,
		Args:    cobra.NoArgs,
		// Completion is not part of the M1 contract, and cobra's completion
		// command exits 0 on unknown subcommands.
		CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
		// execute prints errors itself. Cobra would print usage to stdout,
		// which must stay empty on errors.
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			r.Out = cmd.OutOrStdout()
			r.Err = cmd.ErrOrStderr()
			return initializeConfig(cmd)
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	return cmd
}

// resolveVersion returns injected if it is set. Otherwise it returns the main
// module's version from readBuildInfo, which is the module version for
// go install and a VCS pseudo-version for go build in a git checkout. If
// neither is available, it returns "dev".
func resolveVersion(injected string, readBuildInfo func() (*debug.BuildInfo, bool)) string {
	if injected != "" {
		return injected
	}
	if info, ok := readBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

// initializeConfig fills unset flags of cmd from their environment variables.
// Options take precedence over environment variables. releash has no config
// file.
func initializeConfig(cmd *cobra.Command) error {
	return bindFlags(cmd, viper.New())
}

// bindFlags binds every flag of cmd that carries envAnnotation to the
// environment variables it names, and sets each flag that was not given on
// the command line from v. It returns an error if a value from the
// environment is not valid for its flag.
func bindFlags(cmd *cobra.Command, v *viper.Viper) error {
	var errs []error
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		envVars, ok := f.Annotations[envAnnotation]
		if !ok {
			return
		}
		if err := v.BindEnv(append([]string{f.Name}, envVars...)...); err != nil {
			errs = append(errs, fmt.Errorf("binding --%s to %v: %w", f.Name, envVars, err))
			return
		}
		if !f.Changed && v.IsSet(f.Name) {
			if err := cmd.Flags().Set(f.Name, v.GetString(f.Name)); err != nil {
				errs = append(errs, fmt.Errorf("setting --%s from %v: %w", f.Name, envVars, err))
			}
		}
	})
	return errors.Join(errs...)
}
