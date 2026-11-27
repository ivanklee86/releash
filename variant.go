package main

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/ivanklee86/releash/internal/releash"
)

// variantSynopsis is the usage line of releash variant from spec 0003.
const variantSynopsis = `  releash variant <flag> [--url <url>] [--token <token>] [--timeout <duration>] [--loud]
                         [--format dotenv|shell] [--prefix <prefix>]
                         [--env <name>] [--app-name <name>]
                         [--user-id <id>] [--session-id <id>]
                         [--remote-address <ip>] [--current-time <timestamp>]
                         [--properties <name>=<value>]...`

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

A mistake in the command or its configuration prints the disabled variant,
writes an error to stderr and exits non-zero: 2 for a bad command line, 3 for
a missing Unleash URL or token, 4 for an invalid value and 5 for a value
given twice.`,
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
