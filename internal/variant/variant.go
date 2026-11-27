// Package variant turns an evaluated Unleash variant into the environment
// variables of spec 0003 and renders them as dotenv or shell assignments. It
// only formats: evaluating the flag and choosing exit codes is up to the
// caller.
package variant

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// DisabledVariant is the variant name Unleash gives a flag that is disabled
// or has no variants, and the name the fallback output carries.
const DisabledVariant = "disabled"

// PrefixPattern is the pattern a --prefix must match, as spec 0003 and ADR
// 0006 define it.
const PrefixPattern = `^[A-Z][A-Z0-9_]*$`

var prefixRE = regexp.MustCompile(PrefixPattern)

// ErrUnusable is matched, with errors.Is, by every error Render returns for a
// variant whose output cannot be printed: a json payload that is not valid
// JSON or whose flattened names collide, or a value the format cannot
// represent. Spec 0003 falls back to the disabled variant for each of them.
var ErrUnusable = errors.New("unusable variant")

// Format is an output format of the variant command.
type Format string

// The output formats of spec 0003.
const (
	// Dotenv prints NAME=value lines with no quoting, for $GITHUB_ENV, GitLab
	// dotenv reports and docker --env-file. It cannot represent a newline, a
	// carriage return or a NUL.
	Dotenv Format = "dotenv"
	// Shell prints export NAME='value' lines for eval in a POSIX shell. It
	// cannot represent a NUL.
	Shell Format = "shell"
)

// Formats returns the names of the valid formats, default first.
func Formats() []string {
	return []string{string(Dotenv), string(Shell)}
}

// ParseFormat returns the Format named s, or false if s names none. Names are
// case-sensitive.
func ParseFormat(s string) (Format, bool) {
	switch Format(s) {
	case Dotenv, Shell:
		return Format(s), true
	}
	return "", false
}

// ValidPrefix reports whether prefix matches PrefixPattern. The empty prefix
// is not valid.
func ValidPrefix(prefix string) bool {
	return prefixRE.MatchString(prefix)
}

// DefaultPrefix returns the prefix used when --prefix is not given:
// "RELEASH_", the normalised flag name and "_". The result is a valid prefix
// for any flag name.
func DefaultPrefix(flag string) string {
	return "RELEASH_" + normalize(flag) + "_"
}

// Payload is a variant's payload as Unleash serves it. An empty Type means
// the variant has no payload, which is how the Unleash Go SDK reports one
// without. Type "json" is flattened; any other type, known or not, is printed
// unchanged.
type Payload struct {
	Type  string
	Value string
}

// Result is an evaluated variant.
type Result struct {
	// Enabled is whether the flag is enabled for the context. It can be true
	// while Variant is DisabledVariant, for a flag without variants.
	Enabled bool
	// Variant is the variant name.
	Variant string
	// Payload is the variant's payload, if any.
	Payload Payload
}

// Render returns the variables for r in format, one line each, each ending in
// a newline, in the order of spec 0003: ENABLED, VARIANT, PAYLOAD, then the
// flattened json payload variables sorted by name in byte order. Every name
// starts with prefix, which the caller must have checked with ValidPrefix or
// taken from DefaultPrefix.
//
// If the output cannot be printed, Render returns "" and an error matching
// ErrUnusable whose message describes the problem, e.g. `payload of variant
// "treatment" has colliding keys "a-b" and "a_b"`; the caller then prints
// Fallback instead. An unknown format is an error that does not match
// ErrUnusable.
func Render(prefix string, format Format, r Result) (string, error) {
	if _, ok := ParseFormat(string(format)); !ok {
		return "", fmt.Errorf("unknown format %q", format)
	}
	vars, err := variables(prefix, r)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, v := range vars {
		if bad := unrepresentable(format, v.value); bad != "" {
			return "", unusable("variable %s of variant %q contains %s, which the %s format cannot represent", v.name, r.Variant, bad, format)
		}
		switch format {
		case Dotenv:
			b.WriteString(v.name + "=" + v.value + "\n")
		case Shell:
			b.WriteString("export " + v.name + "=" + shellQuote(v.value) + "\n")
		}
	}
	return b.String(), nil
}

// Fallback returns the disabled variant in format: ENABLED=false and
// VARIANT=disabled under prefix, with no payload. It is what the variant
// command prints when it has no evaluated variant to print. An unknown format
// gives "".
func Fallback(prefix string, format Format) string {
	out, err := Render(prefix, format, Result{Variant: DisabledVariant})
	if err != nil {
		return ""
	}
	return out
}

// variable is one environment variable assignment.
type variable struct {
	name  string
	value string
}

// variables returns r's variables in output order, without checking that the
// values suit a format.
func variables(prefix string, r Result) ([]variable, error) {
	vars := []variable{
		{name: prefix + "ENABLED", value: strconv.FormatBool(r.Enabled)},
		{name: prefix + "VARIANT", value: r.Variant},
	}
	switch r.Payload.Type {
	case "":
		return vars, nil
	case "json":
		compact, leaves, err := flatten(r.Variant, r.Payload.Value)
		if err != nil {
			return nil, err
		}
		vars = append(vars, variable{name: prefix + "PAYLOAD", value: compact})
		for _, l := range leaves {
			vars = append(vars, variable{name: prefix + "PAYLOAD" + l.name, value: l.value})
		}
		return vars, nil
	default:
		return append(vars, variable{name: prefix + "PAYLOAD", value: r.Payload.Value}), nil
	}
}

// unrepresentable names a character in value that format cannot carry, or
// returns "" if format can carry all of value.
func unrepresentable(format Format, value string) string {
	switch {
	case strings.ContainsRune(value, 0):
		return "a NUL character"
	case format == Dotenv && strings.ContainsRune(value, '\n'):
		return "a newline"
	case format == Dotenv && strings.ContainsRune(value, '\r'):
		return "a carriage return"
	}
	return ""
}

// shellQuote quotes s for a POSIX shell: inside single quotes every character
// is literal, so only the quote itself needs closing, escaping and reopening.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// normalize converts ASCII letters to upper case and replaces every Unicode
// code point outside [A-Z0-9_] with one '_'. Each byte of invalid UTF-8
// counts as one code point.
func normalize(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - 'a' + 'A')
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// unusableError is a reason the variant's output cannot be printed.
type unusableError struct {
	msg string
}

func (e *unusableError) Error() string { return e.msg }

func (e *unusableError) Is(target error) bool { return target == ErrUnusable }

func unusable(format string, args ...any) error {
	return &unusableError{msg: fmt.Sprintf(format, args...)}
}
