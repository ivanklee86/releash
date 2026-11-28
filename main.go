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

// answerAnnotation marks a command whose stdout always holds an answer
// (ADR 0004), so run prints the fallback answer for errors and panics in it.
// Its value says which kind of answer: booleanAnswer or variantAnswer.
const answerAnnotation = "releash_answer"

// The values of answerAnnotation.
const (
	// booleanAnswer is check's answer: exactly "true\n" or "false\n".
	booleanAnswer = "boolean"
	// variantAnswer is variant's answer: the variables of spec 0003, with the
	// disabled variant as the fallback (see variantFallback).
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
// command line exit 2, as does an option given before the check or variant
// command (see optionBeforeCommand), and a panic in a command is reported on
// stderr and exits 1 (ADR 0004), rather than Go's default of 2, which would
// look like a bad command line. For a command with answerAnnotation, both
// also print the command's fallback answer on stdout (see fallbackAnswer).
func run(root *cobra.Command, args []string, stdout, stderr io.Writer) (code int) {
	defer func() {
		if p := recover(); p != nil {
			code = releash.ExitInternal
			// ExecuteC never returned, so look the command up the way it does.
			if c, _, err := root.Find(args); err == nil {
				if answer, ok := fallbackAnswer(c, args); ok {
					_, _ = fmt.Fprintf(stderr, "releash: internal error: %v\n", p)
					_, _ = io.WriteString(stdout, answer)
					return
				}
			}
			_, _ = fmt.Fprintf(stderr, "Error: internal error: %v\n", p)
		}
	}()

	if name, opt, ok := optionBeforeCommand(root, args); ok {
		_, _ = fmt.Fprintf(stderr, "releash: %s given before %s; put options after '%s <flag>'\nRun 'releash %s --help' for usage.\n", opt, name, name, name)
		if c, _, err := root.Find([]string{name}); err == nil {
			answer, _ := fallbackAnswer(c, args)
			_, _ = io.WriteString(stdout, answer)
		}
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
	if answer, ok := fallbackAnswer(cmd, args); ok {
		_, _ = fmt.Fprintf(stderr, "releash: %v\nRun '%s --help' for usage.\n", err, cmd.CommandPath())
		_, _ = io.WriteString(stdout, answer)
		return releash.ExitUsage
	}
	_, _ = fmt.Fprintf(stderr, "Error: %v\nRun 'releash --help' for usage.\n", err)
	return releash.ExitUsage
}

// answerCommands are the commands that carry answerAnnotation, which
// optionBeforeCommand looks for.
var answerCommands = []string{"check", "variant"}

// optionBeforeCommand returns the name of the check or variant command in
// args and the first option that comes before it, other than --help, -h,
// --version and -v (also written as --help=... or --version=...). The root
// command has no other options, and cobra's lookup of the subcommand guesses
// whether an unknown option takes a value, so without this some such
// invocations would run the command and others would fail on the root command
// with empty stdout.
//
// The command is the first "check" or "variant" in args that is not the value
// of an option: an option that takes a value in check or variant consumes the
// next argument when it is written without "=", as it does for cobra. If every
// command name was consumed that way, as in "--token check x" with an empty
// $TOKEN, the first one consumed is the command, so the answer is still
// printed although cobra would fail on the root command.
func optionBeforeCommand(root *cobra.Command, args []string) (name, option string, ok bool) {
	var sets []*pflag.FlagSet
	for _, n := range answerCommands {
		if c, _, err := root.Find([]string{n}); err == nil && c != root {
			sets = append(sets, c.Flags())
		}
	}
	takesValue := func(lookup func(*pflag.FlagSet) *pflag.Flag) bool {
		return slices.ContainsFunc(sets, func(fs *pflag.FlagSet) bool {
			fl := lookup(fs)
			return fl != nil && fl.NoOptDefVal == ""
		})
	}

	// consumed is the first command name taken as an option's value.
	var consumed string
	skipValue := func(i int) int {
		if i+1 < len(args) && consumed == "" && slices.Contains(answerCommands, args[i+1]) {
			consumed = args[i+1]
		}
		return i + 1
	}
	notFound := func() (string, string, bool) {
		if option != "" && consumed != "" {
			return consumed, option, true
		}
		return "", "", false
	}

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			return notFound()
		case slices.Contains(answerCommands, a):
			return a, option, option != ""
		case a == "-h", a == "-v",
			a == "--help", strings.HasPrefix(a, "--help="),
			a == "--version", strings.HasPrefix(a, "--version="):
		case strings.HasPrefix(a, "--"):
			if option == "" {
				option = a
			}
			n, _, hasValue := strings.Cut(a[2:], "=")
			if !hasValue && takesValue(func(fs *pflag.FlagSet) *pflag.Flag { return fs.Lookup(n) }) {
				i = skipValue(i)
			}
		case strings.HasPrefix(a, "-"):
			if option == "" {
				option = a
			}
			// A run of shorthands ends at the first one that takes a value,
			// which takes the rest of the argument or the next one.
			for j := 1; j < len(a); j++ {
				short := a[j : j+1]
				if takesValue(func(fs *pflag.FlagSet) *pflag.Flag { return fs.ShorthandLookup(short) }) {
					if j == len(a)-1 {
						i = skipValue(i)
					}
					break
				}
			}
		}
	}
	return notFound()
}

// fallbackAnswer returns what cmd prints on stdout when releash cannot run it
// with args, and false if cmd does not carry answerAnnotation: "false\n" for
// check, and for variant the disabled variant in the format and prefix that
// args give, or nothing when they give none (see variantFallback).
func fallbackAnswer(cmd *cobra.Command, args []string) (string, bool) {
	if cmd == nil {
		return "", false
	}
	switch cmd.Annotations[answerAnnotation] {
	case booleanAnswer:
		return "false\n", true
	case variantAnswer:
		return variantFallback(cmd, args), true
	}
	return "", false
}

// NewRootCommand returns the releash root command, which prints help when run
// without a subcommand. It and its check and variant commands print "releash
// version <version>" for --version. Its subcommands write through r.
func NewRootCommand(r *releash.Releash, version string) *cobra.Command {
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
	cmd.AddCommand(newCheckCommand(r, version), newVariantCommand(r, version))
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
		Annotations: map[string]string{answerAnnotation: booleanAnswer},
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
	addCheckFlags(f, &opts, "explain on stderr why the answer is false")
	countValues(f, given)

	cmd.SetUsageFunc(usage(checkSynopsis, releash.ReleashOptions()))
	plainHelpAndVersion(cmd)
	return cmd
}

// addCheckFlags defines the connection and context options that check and
// variant share in f, bound to opts, with loudUsage as the help text of
// --loud. --url and --token fall back to UNLEASH_URL and UNLEASH_API_TOKEN.
func addCheckFlags(f *pflag.FlagSet, opts *releash.CheckOptions, loudUsage string) {
	f.StringVar(&opts.URL, releash.OptionURL, "", "Unleash API `url`, including /api (env UNLEASH_URL)")
	f.StringVar(&opts.Token, releash.OptionToken, "", "Unleash client API `token` (env UNLEASH_API_TOKEN, which keeps it out of process listings)")
	f.StringVar(&opts.Timeout, releash.OptionTimeout, releash.DefaultTimeout.String(), "how long to wait for Unleash, as a positive Go `duration`")
	f.BoolVar(&opts.Loud, releash.OptionLoud, false, loudUsage)
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
func usage(synopsis string, releashOptions []string) func(*cobra.Command) error {
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

// newVariantCommand returns the variant command, which maps its arguments and
// options to releash.VariantOptions and calls r.Variant. It shares every
// option of check and adds --format and --prefix. With --version it prints
// version and exits 0 without validating anything, as check does.
func newVariantCommand(r *releash.Releash, version string) *cobra.Command {
	var opts releash.VariantOptions
	given := make(map[string]int)

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

A mistake in the command or its configuration prints the disabled variant, or
nothing when the format, prefix or flag name is invalid or ambiguous, writes
an error to stderr and exits non-zero: 2 for a bad command line, 3 for a
missing Unleash URL or token, 4 for an invalid value and 5 for a value given
twice.`,
		Version:     version,
		Args:        checkArgs,
		Annotations: map[string]string{answerAnnotation: variantAnswer},
		RunE: func(_ *cobra.Command, args []string) error {
			opts.Flag = args[0]
			opts.Given = given
			if code := r.Variant(opts); code != releash.ExitOK {
				return exitCodeError(code)
			}
			return nil
		},
	}

	f := cmd.Flags()
	f.SortFlags = false
	addCheckFlags(f, &opts.CheckOptions, "explain on stderr why the variant fell back to disabled, and warn when it may differ between runs")
	// String arrays keep every value, so a repeat can be told from a
	// repeat of the same value, which still decides the fallback's format.
	f.StringArrayVar(&opts.Formats, releash.OptionFormat, nil, "output `format`: dotenv for NAME=value lines, or shell for export statements (default dotenv)")
	f.StringArrayVar(&opts.Prefixes, releash.OptionPrefix, nil, "`prefix` of every variable name, matching ^[A-Z][A-Z0-9_]*$ (default: RELEASH_, then the flag name in upper case with every other character as _, then _)")
	countValues(f, given)

	cmd.SetUsageFunc(usage(variantSynopsis, append(releash.ReleashOptions(), releash.OutputOptions()...)))
	plainHelpAndVersion(cmd)
	return cmd
}

// variantFallback returns the disabled variant that the variant command cmd
// prints when it cannot run with args, or "" when args name no single
// format and prefix (see releash.VariantFallback). It reads args as pflag
// parses them for cmd, without parsing them, because the error may be in the
// command line itself.
func variantFallback(cmd *cobra.Command, args []string) string {
	flags, formats, prefixes := scanArgs(cmd.Flags(), cmd.Name(), args)
	return releash.VariantFallback(flags, formats, prefixes)
}

// scanArgs reads args the way pflag parses them for fs and returns the
// positional arguments, apart from the first one that equals command, and
// the values of every --format and --prefix, in order.
//
// An option that takes a value consumes the next argument when it is written
// without "=", even one that starts with "-", and a trailing one gets the
// empty value. Every argument after "--" is positional. An unknown option is
// taken to have no value, so the scan goes on past it.
func scanArgs(fs *pflag.FlagSet, command string, args []string) (positional, formats, prefixes []string) {
	takesValue := func(fl *pflag.Flag) bool { return fl != nil && fl.NoOptDefVal == "" }
	commandSeen := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			positional = append(positional, args[i+1:]...)
			return positional, formats, prefixes
		case strings.HasPrefix(a, "--"):
			name, value, hasValue := strings.Cut(a[2:], "=")
			if !hasValue && takesValue(fs.Lookup(name)) && i+1 < len(args) {
				i++
				value = args[i]
			}
			switch name {
			case releash.OptionFormat:
				formats = append(formats, value)
			case releash.OptionPrefix:
				prefixes = append(prefixes, value)
			}
		case strings.HasPrefix(a, "-") && len(a) > 1:
			// A run of shorthands ends at the first one that takes a value,
			// which takes the rest of the argument or the next one.
			for j := 1; j < len(a); j++ {
				if takesValue(fs.ShorthandLookup(a[j : j+1])) {
					if j == len(a)-1 && i+1 < len(args) {
						i++
					}
					break
				}
			}
		case !commandSeen && a == command:
			commandSeen = true
		default:
			positional = append(positional, a)
		}
	}
	return positional, formats, prefixes
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
