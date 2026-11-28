// Command releash answers rollout questions from Unleash feature flags in
// CI/CD pipelines.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"
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

// answerAnnotation marks a command whose stdout holds an answer (ADR 0004),
// so run prints the fallback answer for errors and panics in it. Its value
// says which kind of answer: booleanAnswer or variantAnswer.
const answerAnnotation = "releash_answer"

// The values of answerAnnotation.
const (
	// booleanAnswer is check's answer: exactly "true\n" or "false\n".
	booleanAnswer = "boolean"
	// variantAnswer is variant's answer: the variables of spec 0003, with the
	// disabled variant as the fallback, or nothing on exit 2 (see fallbackAnswer).
	variantAnswer = "variant"
)

// checkSynopsis is the usage line of releash check from spec 0001.
const checkSynopsis = `  releash check <flag> [--url <url>] [--token <token>] [--timeout <duration>] [--loud]
                       [--env <name>] [--app-name <name>]
                       [--user-id <id>] [--session-id <id>]
                       [--remote-address <ip>] [--current-time <timestamp>]
                       [--properties <name>=<value>]...`

// variantSynopsis is the usage line of releash variant from spec 0003.
const variantSynopsis = `  releash variant <flag> [--url <url>] [--token <token>] [--timeout <duration>] [--loud]
                         [--format dotenv|shell] [--prefix <prefix>]
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
// command line exit 2, and a panic in a command is reported on stderr and
// exits 1 (ADR 0004), rather than Go's default of 2, which would look like a
// bad command line. For a command with answerAnnotation, both also print the
// command's fallback answer on stdout (see fallbackAnswer).
func run(root *cobra.Command, args []string, stdout, stderr io.Writer) (code int) {
	defer func() {
		if p := recover(); p != nil {
			code = releash.ExitInternal
			// ExecuteC never returned, so look the command up the way it does.
			if c, _, err := root.Find(args); err == nil {
				if answer, ok := fallbackAnswer(c, code); ok {
					_, _ = fmt.Fprintf(stderr, "releash: internal error: %v\n", p)
					_, _ = io.WriteString(stdout, answer)
					return
				}
			}
			_, _ = fmt.Fprintf(stderr, "Error: internal error: %v\n", p)
		}
	}()

	root.SetArgs(args)
	cmd, err := root.ExecuteC()
	if err == nil {
		return releash.ExitOK
	}
	if exit, ok := errors.AsType[exitCodeError](err); ok {
		return int(exit)
	}
	// Every other error comes from cobra parsing the command line.
	hint := ""
	if cmd == root {
		var i int
		cmd, i = answerCommandIn(root, args)
		// cobra's error names the argument after the command, not the
		// option before it that took the command name as its value.
		if i > 0 && strings.HasPrefix(args[i-1], "-") && strings.HasPrefix(err.Error(), "unknown command") {
			hint = fmt.Sprintf("releash: %s may be missing its value and have taken %q instead\n", args[i-1], args[i])
		}
	}
	if answer, ok := fallbackAnswer(cmd, releash.ExitUsage); ok {
		_, _ = fmt.Fprintf(stderr, "releash: %v\n%sRun '%s --help' for usage.\n", err, hint, cmd.CommandPath())
		_, _ = io.WriteString(stdout, answer)
		return releash.ExitUsage
	}
	_, _ = fmt.Fprintf(stderr, "Error: %v\nRun 'releash --help' for usage.\n", err)
	return releash.ExitUsage
}

// answerCommandIn returns the first subcommand of root that carries
// answerAnnotation and whose name is one of args, with the index of that
// argument, or root and -1 if there is none. cobra blames root for some
// command lines meant for check or variant: an unknown option before the
// command, as in "releash --nope check x", and an option that takes the
// command name as its value, as in "releash --token $TOKEN check x" with an
// empty $TOKEN. The pipeline still expects that command's answer, so run
// prints its fallback.
func answerCommandIn(root *cobra.Command, args []string) (*cobra.Command, int) {
	for i, a := range args {
		for _, c := range root.Commands() {
			if c.Name() == a && c.Annotations[answerAnnotation] != "" {
				return c, i
			}
		}
	}
	return root, -1
}

// fallbackAnswer returns what cmd prints on stdout when it exits with code
// without giving an answer, and false if cmd does not carry answerAnnotation.
// For check it is "false\n". For variant it is nothing on ExitUsage (ADR
// 0008), and otherwise the disabled variant in the format and prefix that
// cmd's flags hold, or nothing if they hold no single valid one (see
// releash.VariantFallback).
func fallbackAnswer(cmd *cobra.Command, code int) (string, bool) {
	if cmd == nil {
		return "", false
	}
	switch cmd.Annotations[answerAnnotation] {
	case booleanAnswer:
		return "false\n", true
	case variantAnswer:
		if code == releash.ExitUsage {
			return "", true
		}
		// The flags are empty if the panic came before cobra parsed them.
		formats, _ := cmd.Flags().GetStringArray(releash.OptionFormat)
		prefixes, _ := cmd.Flags().GetStringArray(releash.OptionPrefix)
		return releash.VariantFallback(cmd.Flags().Args(), formats, prefixes), true
	}
	return "", false
}

// NewRootCommand returns the releash root command, which prints help when run
// without a subcommand. It and its check and variant commands print "releash
// version <version>" for --version. Its subcommands write through r.
//
// The options that check and variant share are persistent flags of the root
// command, so they may also go before the command name, as in
// "releash --loud check x". They are hidden from the root command's help,
// which lists only --help and --version; each command's help lists them.
func NewRootCommand(r *releash.Releash, version string) *cobra.Command {
	var opts releash.CheckOptions
	given := make(map[string]int)

	cmd := &cobra.Command{
		Use:     "releash",
		Short:   "Drive CI/CD changes with Unleash feature flags.",
		Long:    "releash asks Unleash whether a feature flag is enabled, so CI/CD pipelines can gate rollout steps on it, and loads a flag's variant as environment variables to configure those steps.",
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
			if !cmd.HasParent() {
				return optionWithoutCommand(cmd)
			}
			r.Out = cmd.OutOrStdout()
			r.Err = cmd.ErrOrStderr()
			return initializeConfig(cmd)
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	pf := cmd.PersistentFlags()
	pf.SortFlags = false
	addCheckFlags(pf, &opts)
	countValues(pf, given)
	pf.VisitAll(func(fl *pflag.Flag) { fl.Hidden = true })

	// Subcommands inherit the template. The default one would print
	// "releash check version ..." for check.
	cmd.SetVersionTemplate("releash version {{.Version}}\n")
	cmd.AddCommand(newCheckCommand(r, version, &opts, given), newVariantCommand(r, version, &opts, given))
	plainHelpAndVersion(cmd)
	// cobra would add its help command only when it runs, too late for
	// plainHelpAndVersion. Adding it here makes cobra reuse this one.
	cmd.InitDefaultHelpCmd()
	for _, sub := range cmd.Commands() {
		if sub.Name() == "help" {
			plainHelpAndVersion(sub)
		}
	}
	return cmd
}

// optionWithoutCommand returns an error naming the first shared option given
// to root itself, as in "releash --loud", or nil if there is none. It must
// run before bindFlags, which marks options set from the environment as
// changed.
func optionWithoutCommand(root *cobra.Command) error {
	var name string
	root.PersistentFlags().VisitAll(func(fl *pflag.Flag) {
		if name == "" && fl.Changed {
			name = fl.Name
		}
	})
	if name == "" {
		return nil
	}
	return fmt.Errorf("--%s needs a command: releash check <flag> or releash variant <flag>", name)
}

// plainHelpAndVersion adds cobra's --help and --version flags to cmd now,
// rather than when it runs, and wraps their values so that help lists them
// as "--help", not "--help[=true|false]": pflag 1.1.0 adds that suffix to any
// option whose value reports IsBoolFlag. They still take no value, because
// pflag set their NoOptDefVal when cobra added them. --loud already prints
// plainly because countedValue hides IsBoolFlag too.
func plainHelpAndVersion(cmd *cobra.Command) {
	cmd.InitDefaultHelpFlag()
	cmd.InitDefaultVersionFlag()
	for _, name := range []string{"help", "version"} {
		if fl := cmd.Flags().Lookup(name); fl != nil {
			fl.Value = plainValue{Value: fl.Value}
		}
	}
}

// plainValue is a pflag.Value that hides the IsBoolFlag method of the value
// it wraps, so pflag's help prints the option without "[=true|false]".
type plainValue struct {
	pflag.Value
}

// newCheckCommand returns the check command, which calls r.Check with the
// flag name and the shared options that the root command parses into opts and
// counts into given. With --version it prints version and exits 0 without
// validating anything, as spec 0001 allows.
func newCheckCommand(r *releash.Releash, version string, opts *releash.CheckOptions, given map[string]int) *cobra.Command {
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
		Annotations: map[string]string{answerAnnotation: booleanAnswer},
		RunE: func(_ *cobra.Command, args []string) error {
			o := *opts
			o.Flag = args[0]
			o.Given = given
			if code := r.Check(o); code != releash.ExitOK {
				return exitCodeError(code)
			}
			return nil
		},
	}

	cmd.SetUsageFunc(usage(checkSynopsis, releash.ReleashOptions(), "explain on stderr why the answer is false"))
	plainHelpAndVersion(cmd)
	return cmd
}

// addCheckFlags defines the connection and context options that check and
// variant share in f, bound to opts. --url and --token fall back to
// UNLEASH_URL and UNLEASH_API_TOKEN. The help text of --loud differs per
// command, so each command's usage function sets it.
func addCheckFlags(f *pflag.FlagSet, opts *releash.CheckOptions) {
	f.StringVar(&opts.URL, releash.OptionURL, "", "Unleash API `url`, including /api (env UNLEASH_URL)")
	f.StringVar(&opts.Token, releash.OptionToken, "", "Unleash client API `token` (env UNLEASH_API_TOKEN, which keeps it out of process listings)")
	f.StringVar(&opts.Timeout, releash.OptionTimeout, releash.DefaultTimeout.String(), "how long to wait for Unleash, as a positive Go `duration`")
	f.BoolVar(&opts.Loud, releash.OptionLoud, false, "explain fallbacks on stderr")
	f.StringVar(&opts.Env, releash.OptionEnv, releash.DefaultEnvironment, "environment in the context, as a `name`")
	f.StringVar(&opts.AppName, releash.OptionAppName, releash.DefaultAppName, "appName in the context, and the `name` releash registers with")
	f.StringVar(&opts.UserID, releash.OptionUserID, "", "userId in the context, as an `id`")
	f.StringVar(&opts.SessionID, releash.OptionSessionID, "", "sessionId in the context, as an `id`")
	f.StringVar(&opts.RemoteAddress, releash.OptionRemoteAddress, "", "remoteAddress in the context, as an `ip` address, IPv4 or IPv6 (default: the local address of the connection to Unleash)")
	f.StringVar(&opts.CurrentTime, releash.OptionCurrentTime, "", "currentTime in the context, as an RFC 3339 `timestamp` (default: the time of the check)")
	f.StringArrayVar(&opts.Properties, releash.OptionProperties, nil, "custom property in the context, as `name=value`; repeat for more")

	mustAnnotate(f, releash.OptionURL, envAnnotation, "UNLEASH_URL")
	mustAnnotate(f, releash.OptionToken, envAnnotation, "UNLEASH_API_TOKEN")
}

// countValues wraps every flag in f in a countedValue that counts into
// given, except the string array flags, which keep every value themselves.
// pflag keeps only the last value of a repeated option, so the counts are
// how validation finds repeats. Wrapping an array flag would also hide its
// empty default from pflag's help.
func countValues(f *pflag.FlagSet, given map[string]int) {
	f.VisitAll(func(fl *pflag.Flag) {
		if fl.Value.Type() != "stringArray" {
			fl.Value = &countedValue{Value: fl.Value, count: given, name: fl.Name}
		}
	})
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

// usage returns the usage function of the check or variant command: synopsis,
// then releashOptions, the context options of spec 0001 and the other
// options (--help, --version), each under its own heading, in that order.
// It lists the shared options although the root command hides them, and
// gives --loud the help text loudUsage.
func usage(synopsis string, releashOptions []string, loudUsage string) func(*cobra.Command) error {
	return func(cmd *cobra.Command) error {
		var b strings.Builder
		fmt.Fprintf(&b, "Usage:\n%s\n", synopsis)
		groups := []struct {
			title string
			names []string
		}{
			{"Releash options", releashOptions},
			{"Context options", releash.ContextOptions()},
			{"Other options", []string{"help", "version"}},
		}
		for _, g := range groups {
			fs := pflag.NewFlagSet(g.title, pflag.ContinueOnError)
			fs.SortFlags = false
			for _, name := range g.names {
				fl := cmd.Flag(name)
				if fl == nil {
					continue
				}
				shown := *fl
				shown.Hidden = false
				if name == releash.OptionLoud {
					shown.Usage = loudUsage
				}
				fs.AddFlag(&shown)
			}
			if fs.HasFlags() {
				fmt.Fprintf(&b, "\n%s:\n%s", g.title, fs.FlagUsages())
			}
		}
		_, err := io.WriteString(cmd.OutOrStderr(), b.String())
		return err
	}
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

// newVariantCommand returns the variant command, which calls r.Variant with
// the flag name, its own --format and --prefix, and the shared options that
// the root command parses into opts and counts into given. With --version it
// prints version and exits 0 without validating anything, as check does.
func newVariantCommand(r *releash.Releash, version string, opts *releash.CheckOptions, given map[string]int) *cobra.Command {
	var formats, prefixes []string

	cmd := &cobra.Command{
		Use:   "variant <flag>",
		Short: "Print a feature flag's variant and payload as environment variables.",
		Long: `releash variant prints the variant Unleash gives a feature flag, with its
payload, as environment variables for $GITHUB_ENV, a dotenv file or eval. A
json payload also gets one variable per leaf. Gate on releash check first: for
a disabled flag it prints the disabled variant, writes an error to stderr and
exits 6.

If Unleash cannot answer, or the payload cannot be printed, it prints the
disabled variant and exits 0; --loud says why on stderr.

A bad command line prints nothing, writes an error to stderr and exits 2.
Any other mistake in the command or its configuration prints the disabled
variant, writes an error to stderr and exits non-zero: 3 for a missing
Unleash URL or token, 4 for an invalid value and 5 for a value given twice.`,
		Version:     version,
		Args:        checkArgs,
		Annotations: map[string]string{answerAnnotation: variantAnswer},
		RunE: func(_ *cobra.Command, args []string) error {
			o := releash.VariantOptions{CheckOptions: *opts, Formats: formats, Prefixes: prefixes}
			o.Flag = args[0]
			o.Given = given
			if code := r.Variant(o); code != releash.ExitOK {
				return exitCodeError(code)
			}
			return nil
		},
	}

	f := cmd.Flags()
	f.SortFlags = false
	// String arrays keep every value, so validation can count repeats and
	// the panic fallback can tell whether every value is the same.
	f.StringArrayVar(&formats, releash.OptionFormat, nil, "output `format`: dotenv for NAME=value lines, or shell for export statements (default dotenv)")
	f.StringArrayVar(&prefixes, releash.OptionPrefix, nil, "`prefix` of every variable name, matching ^[A-Z][A-Z0-9_]*$ (default: RELEASH_, then the flag name in upper case with every other character as _, then _)")

	cmd.SetUsageFunc(usage(variantSynopsis, append(releash.ReleashOptions(), releash.OutputOptions()...),
		"explain on stderr why the variant fell back to disabled, and warn when it may differ between runs"))
	plainHelpAndVersion(cmd)
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
