package releash

import (
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	unleash "github.com/Unleash/unleash-go-sdk/v6"
	"github.com/Unleash/unleash-go-sdk/v6/api"
	"github.com/Unleash/unleash-go-sdk/v6/strategy"

	"github.com/ivanklee86/releash/internal/variant"
)

// Long names of the options that only the variant command has, without the
// leading "--".
const (
	OptionFormat = "format"
	OptionPrefix = "prefix"
)

// OutputOptions returns the releash options that only the variant command
// has, in the order help lists them after ReleashOptions. Giving one twice
// exits ExitUsage.
func OutputOptions() []string {
	return []string{OptionFormat, OptionPrefix}
}

// VariantOptions is what the command line and environment gave releash
// variant, before validation. Variant validates it and applies defaults.
type VariantOptions struct {
	// CheckOptions holds the options variant shares with check, validated as
	// Check validates them. Its Given need not count OptionFormat or
	// OptionPrefix.
	CheckOptions

	// Formats holds each --format value in the order given. None means
	// variant.Dotenv.
	Formats []string
	// Prefixes holds each --prefix value in the order given. None means the
	// default prefix of Flag.
	Prefixes []string
}

// Variant validates opts and prints the variant of opts.Flag that Unleash
// gives the context, as the variables of spec 0003 in the requested format.
//
// Variant writes each problem it finds in opts to Err as a "releash: " line,
// with or without opts.Loud, and returns the lowest exit code among them
// without contacting Unleash; Out then gets nothing for ExitUsage (ADR 0008)
// and the disabled variant for any other code. A valid invocation prints the
// evaluated variant and returns ExitOK. It prints the disabled variant and
// returns ExitOK when Unleash cannot answer, the flag is not in the fetched
// flags, the SDK cannot evaluate it, or the payload cannot be printed in the
// format, and then says why on Err under opts.Loud, or for a flag that is not
// in the fetched flags with or without it. When the flag is in the
// fetched flags and disabled for the context, it prints the disabled
// variant, writes an error naming releash check to Err with or without
// opts.Loud, and returns ExitDisabled. Under opts.Loud it also writes the
// notes evaluation made, such as a warning that the flag uses default
// stickiness while neither --user-id nor --session-id is set. It finishes
// within its timeout.
func (r *Releash) Variant(opts VariantOptions) int {
	req, format, prefix, problems := validateVariant(opts)
	if code := r.reportProblems("variant", problems); code != ExitOK {
		// Every problem with the flag name, format or prefix is ExitUsage, so
		// on any other code format and prefix are the ones that were asked for.
		if code != ExitUsage {
			_, _ = io.WriteString(r.Out, variant.Fallback(prefix, format))
		}
		return code
	}

	req.ask = askVariant(format, prefix)
	out := r.evaluateRequest(req)
	r.reportOutcome(req, out, "disabled variant")
	switch {
	case out.fallback != "":
		_, _ = io.WriteString(r.Out, variant.Fallback(prefix, format))
		return ExitOK
	case out.refused:
		_, _ = fmt.Fprintf(r.Err, "releash: flag %q is disabled; gate on \"releash check %s\" before loading its variant\n", req.flag, req.flag)
		_, _ = io.WriteString(r.Out, variant.Fallback(prefix, format))
		return ExitDisabled
	default:
		_, _ = io.WriteString(r.Out, out.output)
		return ExitOK
	}
}

// VariantFallback returns the disabled variant that releash variant prints
// after a recovered panic, when Variant may not have validated anything.
// flags are the positional arguments after the command, and formats and
// prefixes every --format and --prefix value in the order given.
//
// The format is dotenv without a --format, or the one every --format names.
// The prefix is the one every --prefix names, or without a --prefix the
// default prefix of the only flag name. It returns "" when there is no
// single valid format or prefix: an invalid value, two different values, or
// no --prefix and not exactly one non-empty flag name.
func VariantFallback(flags, formats, prefixes []string) string {
	format := variant.Dotenv
	if len(formats) > 0 {
		f, ok := variant.ParseFormat(formats[0])
		if !ok || !allEqual(formats) {
			return ""
		}
		format = f
	}

	var prefix string
	switch {
	case len(prefixes) > 0:
		if !variant.ValidPrefix(prefixes[0]) || !allEqual(prefixes) {
			return ""
		}
		prefix = prefixes[0]
	case len(flags) == 1 && flags[0] != "":
		prefix = variant.DefaultPrefix(flags[0])
	default:
		return ""
	}
	return variant.Fallback(prefix, format)
}

// allEqual reports whether every value in values is the same.
func allEqual(values []string) bool {
	return !slices.ContainsFunc(values, func(v string) bool { return v != values[0] })
}

// validateVariant returns opts as a checkRequest, the format and prefix to
// print in, and every problem in opts. The results are only meaningful when
// there are no problems.
func validateVariant(opts VariantOptions) (checkRequest, variant.Format, string, []problem) {
	req, problems := validateCheck(opts.CheckOptions)
	report := func(code int, format string, args ...any) {
		problems = append(problems, problem{code: code, msg: fmt.Sprintf(format, args...)})
	}

	for _, o := range []struct {
		name   string
		values []string
	}{{OptionFormat, opts.Formats}, {OptionPrefix, opts.Prefixes}} {
		if n := len(o.values); n > 1 {
			report(ExitUsage, "--%s given %d times; give it at most once", o.name, n)
		}
	}

	format := variant.Dotenv
	for _, f := range opts.Formats {
		parsed, ok := variant.ParseFormat(f)
		if !ok {
			report(ExitUsage, "invalid --%s %q: must be one of %s", OptionFormat, f, quotedList(variant.Formats()))
			continue
		}
		format = parsed
	}

	prefix := variant.DefaultPrefix(opts.Flag)
	for _, p := range opts.Prefixes {
		if !variant.ValidPrefix(p) {
			report(ExitUsage, "invalid --%s %q: must match %s", OptionPrefix, p, variant.PrefixPattern)
			continue
		}
		prefix = p
	}
	return req, format, prefix, problems
}

// quotedList writes names quoted and separated by "or", e.g. "a" or "b".
func quotedList(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = strconv.Quote(n)
	}
	return strings.Join(quoted, " or ")
}

// askVariant returns the variant command's question: the variant of req.flag
// rendered with prefix in format.
//
// It asks the SDK's GetVariant with no fallback variant, because the SDK
// would also use one for an enabled flag without variants. The SDK gives an
// unknown and a disabled flag the same disabled variant, so the fetched flags
// tell them apart: a flag that is not there falls back as not found, and one
// that is there and not enabled for the context is refused. A payload that
// variant.Render cannot print falls back too.
func askVariant(format variant.Format, prefix string) question {
	return func(client *unleash.Client, req checkRequest) outcome {
		v := client.GetVariant(req.flag, unleash.VariantOptions{Ctx: sdkContext(req.context)})
		features := client.ListFeatures()
		i := slices.IndexFunc(features, func(f api.Feature) bool { return f.Name == req.flag })
		if i < 0 {
			return notFound(req.flag)
		}
		if !v.FeatureEnabled {
			return outcome{refused: true}
		}
		rendered, err := variant.Render(prefix, format, variant.Result{
			Enabled: true,
			Variant: v.Name,
			Payload: variant.Payload{Type: v.Payload.Type, Value: v.Payload.Value},
		})
		if err != nil {
			return outcome{fallback: err.Error()}
		}
		out := outcome{output: rendered}
		if req.context.userID == "" && req.context.sessionID == "" && usesDefaultStickiness(features[i]) {
			out.notes = append(out.notes, fmt.Sprintf("flag %q assigns variants by default stickiness, but neither --%s nor --%s is set, so the variant can differ between runs; pass --%s, e.g. the repository name, to keep it sticky",
				req.flag, OptionUserID, OptionSessionID, OptionUserID))
		}
		return out
	}
}

// usesDefaultStickiness reports whether any variants of f are assigned by
// default stickiness, as the SDK decides it: feature variants by the
// stickiness of the first variant, and the variants of a strategy by the
// strategy's stickiness parameter, or without one by the stickiness of its
// first variant. An empty stickiness is default stickiness too.
func usesDefaultStickiness(f api.Feature) bool {
	isDefault := func(s string) bool { return s == "" || s == "default" }
	if len(f.Variants) > 0 && isDefault(f.Variants[0].Stickiness) {
		return true
	}
	for _, s := range f.Strategies {
		if len(s.Variants) == 0 {
			continue
		}
		stickiness := s.Variants[0].Stickiness
		if p, ok := s.Parameters[strategy.ParamStickiness].(string); ok {
			stickiness = p
		}
		if isDefault(stickiness) {
			return true
		}
	}
	return false
}
