// Command releash answers rollout questions from Unleash feature flags in
// CI/CD pipelines.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"slices"
	"strings"

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

// answerAnnotation marks a command whose stdout is always exactly "true\n" or
// "false\n" (ADR 0004), so run prints "false" for errors and panics in it.
const answerAnnotation = "releash_answer"

// checkSynopsis is the usage line of releash check from spec 0001.
const checkSynopsis = `  releash check <flag> [--url <url>] [--token <token>] [--timeout <duration>] [--loud]
                       [--env <name>] [--app-name <name>]
                       [--user-id <id>] [--session-id <id>]
                       [--remote-address <ip>] [--current-time <timestamp>]
                       [--properties <name>=<value>]...`

func main() {
	os.Exit(execute(os.Args[1:], os.Stdout, os.Stderr, resolveVersion(version, debug.ReadBuildInfo)))
}

// execute runs the CLI with args and returns the process exit code.
func execute(args []string, stdout, stderr io.Writer, version string) int {
	r := releash.New()
	r.Out = stdout
	r.Err = stderr

	cmd := NewRootCommand(r, version)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	return run(cmd, args, stdout, stderr)
}

// exitCodeError is returned by a command that has already reported its
// problems, to make run exit with that code.
type exitCodeError int

func (e exitCodeError) Error() string {
	return fmt.Sprintf("exit code %d", int(e))
}

// run executes root with args and returns its exit code. It is the only place
// that turns cobra errors and panics into exit codes: errors from parsing the
// command line exit 2, as does an option given before the check command (see
// optionBeforeCheck), and a panic in a command is reported on stderr and
// exits 1 (ADR 0004), rather than Go's default of 2, which would look like a
// bad command line. For a command with answerAnnotation, both also print
// "false" on stdout.
func run(root *cobra.Command, args []string, stdout, stderr io.Writer) (code int) {
	defer func() {
		if p := recover(); p != nil {
			code = releash.ExitInternal
			// ExecuteC never returned, so look the command up the way it does.
			if c, _, err := root.Find(args); err == nil && answers(c) {
				_, _ = fmt.Fprintf(stderr, "releash: internal error: %v\n", p)
				_, _ = fmt.Fprintln(stdout, "false")
				return
			}
			_, _ = fmt.Fprintf(stderr, "Error: internal error: %v\n", p)
		}
	}()

	if opt, ok := optionBeforeCheck(args); ok {
		_, _ = fmt.Fprintf(stderr, "releash: %s given before check; put options after 'check <flag>'\nRun 'releash check --help' for usage.\n", opt)
		_, _ = fmt.Fprintln(stdout, "false")
		return releash.ExitUsage
	}

	root.SetArgs(args)
	cmd, err := root.ExecuteC()
	if err == nil {
		return releash.ExitOK
	}
	if exit, ok := errors.AsType[exitCodeError](err); ok {
		return int(exit)
	}
	// Every other error comes from cobra parsing the command line.
	if answers(cmd) {
		_, _ = fmt.Fprintf(stderr, "releash: %v\nRun '%s --help' for usage.\n", err, cmd.CommandPath())
		_, _ = fmt.Fprintln(stdout, "false")
		return releash.ExitUsage
	}
	_, _ = fmt.Fprintf(stderr, "Error: %v\nRun 'releash --help' for usage.\n", err)
	return releash.ExitUsage
}

// optionBeforeCheck returns the first option that comes before the check
// command in args, other than --help, -h, --version and -v (also written as
// --help=... or --version=...). The root command has
// no other options, and cobra's lookup of the subcommand guesses whether an
// unknown option takes a value, so without this some such invocations would
// run check and others would fail on the root command with empty stdout.
func optionBeforeCheck(args []string) (string, bool) {
	i := slices.Index(args, "check")
	if i < 0 {
		return "", false
	}
	for _, a := range args[:i] {
		switch {
		case a == "--":
			return "", false
		case a == "-h", a == "-v",
			a == "--help", strings.HasPrefix(a, "--help="),
			a == "--version", strings.HasPrefix(a, "--version="):
		case strings.HasPrefix(a, "-"):
			return a, true
		}
	}
	return "", false
}

// answers reports whether cmd carries answerAnnotation.
func answers(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd.Annotations[answerAnnotation]
	return ok
}

// NewRootCommand returns the releash root command, which prints help when run
// without a subcommand. It and its check command print "releash version
// <version>" for --version. Its subcommands write through r.
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
		// run prints errors itself. Cobra would print usage to stdout, which
		// must hold nothing but the answer on errors.
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

	// Subcommands inherit the template. The default one would print
	// "releash check version ..." for check.
	cmd.SetVersionTemplate("releash version {{.Version}}\n")
	cmd.AddCommand(newCheckCommand(r, version))
	return cmd
}

// newCheckCommand returns the check command, which maps its arguments and
// options to releash.CheckOptions and calls r.Check. With --version it prints
// version and exits 0 without validating anything, as spec 0001 allows.
func newCheckCommand(r *releash.Releash, version string) *cobra.Command {
	var opts releash.CheckOptions
	given := make(map[string]int)

	cmd := &cobra.Command{
		Use:   "check <flag>",
		Short: "Print true if a feature flag is enabled, false otherwise.",
		Long: `releash check prints true if the Unleash feature flag is enabled and false
otherwise, and exits 0 for both. If Unleash cannot answer, it prints false
and exits 0; --loud says why on stderr.

A mistake in the command or its configuration prints false, writes an error
to stderr and exits non-zero: 2 for a bad command line, 3 for a missing
Unleash URL or token, 4 for an invalid value and 5 for a value given twice.`,
		Version:     version,
		Args:        checkArgs,
		Annotations: map[string]string{answerAnnotation: ""},
		RunE: func(_ *cobra.Command, args []string) error {
			opts.Flag = args[0]
			opts.Given = given
			if code := r.Check(opts); code != releash.ExitOK {
				return exitCodeError(code)
			}
			return nil
		},
	}

	f := cmd.Flags()
	f.SortFlags = false
	f.StringVar(&opts.URL, releash.OptionURL, "", "Unleash API `url`, including /api (env UNLEASH_URL)")
	f.StringVar(&opts.Token, releash.OptionToken, "", "Unleash client API `token` (env UNLEASH_API_TOKEN, which keeps it out of process listings)")
	f.StringVar(&opts.Timeout, releash.OptionTimeout, releash.DefaultTimeout.String(), "how long to wait for Unleash, as a positive Go `duration`")
	f.BoolVar(&opts.Loud, releash.OptionLoud, false, "explain on stderr why the answer is false")
	f.StringVar(&opts.Env, releash.OptionEnv, releash.DefaultEnvironment, "environment in the context, as a `name`")
	f.StringVar(&opts.AppName, releash.OptionAppName, releash.DefaultAppName, "appName in the context, and the `name` releash registers with")
	f.StringVar(&opts.UserID, releash.OptionUserID, "", "userId in the context, as an `id`")
	f.StringVar(&opts.SessionID, releash.OptionSessionID, "", "sessionId in the context, as an `id`")
	f.StringVar(&opts.RemoteAddress, releash.OptionRemoteAddress, "", "remoteAddress in the context, as an `ip` address, IPv4 or IPv6 (default: the local address of the connection to Unleash)")
	f.StringVar(&opts.CurrentTime, releash.OptionCurrentTime, "", "currentTime in the context, as an RFC 3339 `timestamp` (default: the time of the check)")
	f.StringArrayVar(&opts.Properties, releash.OptionProperties, nil, "custom property in the context, as `name=value`; repeat for more")

	mustAnnotate(f, releash.OptionURL, envAnnotation, "UNLEASH_URL")
	mustAnnotate(f, releash.OptionToken, envAnnotation, "UNLEASH_API_TOKEN")
	// pflag keeps only the last value of a repeated option, so count the
	// values as they are set. --properties may be repeated, and wrapping it
	// would also hide its empty default from pflag's help.
	f.VisitAll(func(fl *pflag.Flag) {
		if fl.Name != releash.OptionProperties {
			fl.Value = &countedValue{Value: fl.Value, count: given, name: fl.Name}
		}
	})

	cmd.SetUsageFunc(checkUsage)
	return cmd
}

// checkArgs requires exactly one argument, the flag name.
func checkArgs(_ *cobra.Command, args []string) error {
	switch len(args) {
	case 0:
		return errors.New("missing flag name")
	case 1:
		return nil
	default:
		return fmt.Errorf("expected one flag name, got %d arguments: %s", len(args), strings.Join(args, " "))
	}
}

// checkUsage writes the usage of the check command, with releash options and
// context options under separate headings, in the order spec 0001 gives.
func checkUsage(cmd *cobra.Command) error {
	var b strings.Builder
	fmt.Fprintf(&b, "Usage:\n%s\n", checkSynopsis)
	groups := []struct {
		title string
		names []string
	}{
		{"Releash options", releash.ReleashOptions()},
		{"Context options", releash.ContextOptions()},
		{"Other options", []string{"help", "version"}},
	}
	for _, g := range groups {
		fs := pflag.NewFlagSet(g.title, pflag.ContinueOnError)
		fs.SortFlags = false
		for _, name := range g.names {
			if fl := cmd.Flags().Lookup(name); fl != nil {
				fs.AddFlag(fl)
			}
		}
		if fs.HasFlags() {
			fmt.Fprintf(&b, "\n%s:\n%s", g.title, fs.FlagUsages())
		}
	}
	_, err := io.WriteString(cmd.OutOrStderr(), b.String())
	return err
}

// countedValue wraps a flag value and counts how many times it is set, by
// flag name. Defaults are not set through Set, so they are never counted.
type countedValue struct {
	pflag.Value
	count map[string]int
	name  string
}

func (v *countedValue) Set(s string) error {
	v.count[v.name]++
	return v.Value.Set(s)
}

// mustAnnotate sets an annotation on a flag defined in the same function. It
// panics if the flag does not exist, which is a programming error.
func mustAnnotate(f *pflag.FlagSet, name, key string, values ...string) {
	if err := f.SetAnnotation(name, key, values); err != nil {
		panic(err)
	}
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
// the command line from v. An empty environment variable counts as unset. It
// returns an error if a value from the environment is not valid for its flag.
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
