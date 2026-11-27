package variant_test

import (
	"errors"
	"os/exec"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanklee86/releash/internal/variant"
)

// Cases, from spec 0003's "Variables", "Payload types", "Flattening", "Output
// formats" and test plan:
//   - prefix: valid and invalid --prefix values; the default prefix from flag
//     names with '-', '.', spaces, mixed case, digits, non-ASCII code points
//     of every UTF-8 length and invalid UTF-8
//   - formats: the two names parse, anything else does not
//   - payload types: json, string, number, csv and an unknown type; no
//     payload; an enabled flag without variants; values kept byte for byte
//   - flattening: the spec's example, nesting, arrays, arrays of objects,
//     null, booleans, empty containers, scalar roots, number text, escaped
//     strings, member names that need normalising, byte-order sorting
//   - collisions: names that normalise alike, case only, "__" against
//     nesting, containers, inside arrays, duplicate member names at any depth
//   - invalid JSON of several kinds
//   - unrepresentable values per format and the order they are found in
//   - shell output survives eval in /bin/sh, and is exported
//   - the fallback in both formats

const prefix = "RELEASH_CHECKOUT_REDESIGN_"

func TestValidPrefix(t *testing.T) {
	valid := []string{"A", "CHECKOUT_", "RELEASH_CHECKOUT_REDESIGN_", "A1", "A_1_B", "X__"}
	invalid := []string{"", "_A", "1A", "a", "checkout_", "Checkout_", "A-B", "A.B", "A B", "A\n", "Ä", "A=B", " A", "A "}
	for _, p := range valid {
		assert.Truef(t, variant.ValidPrefix(p), "ValidPrefix(%q)", p)
	}
	for _, p := range invalid {
		assert.Falsef(t, variant.ValidPrefix(p), "ValidPrefix(%q)", p)
	}
}

func TestDefaultPrefix(t *testing.T) {
	tests := []struct {
		flag string
		want string
	}{
		{"checkout-redesign", "RELEASH_CHECKOUT_REDESIGN_"},
		{"checkout.redesign", "RELEASH_CHECKOUT_REDESIGN_"},
		{"checkout redesign", "RELEASH_CHECKOUT_REDESIGN_"},
		{"Checkout_Redesign", "RELEASH_CHECKOUT_REDESIGN_"},
		{"deploy-v2", "RELEASH_DEPLOY_V2_"},
		{"2026-launch", "RELEASH_2026_LAUNCH_"},
		{"a--b", "RELEASH_A__B_"},
		// One '_' per code point, however many bytes it takes in UTF-8.
		{"größe", "RELEASH_GR__E_"},
		{"日本", "RELEASH____"},
		{"go🚀", "RELEASH_GO__"},
		// Each byte of invalid UTF-8 is one code point.
		{"a\xff\xfeb", "RELEASH_A__B_"},
	}
	for _, tt := range tests {
		got := variant.DefaultPrefix(tt.flag)
		assert.Equalf(t, tt.want, got, "DefaultPrefix(%q)", tt.flag)
		assert.Truef(t, variant.ValidPrefix(got), "DefaultPrefix(%q) = %q is not a valid prefix", tt.flag, got)
	}
}

func TestParseFormat(t *testing.T) {
	for _, name := range variant.Formats() {
		f, ok := variant.ParseFormat(name)
		assert.Truef(t, ok, "ParseFormat(%q)", name)
		assert.Equal(t, name, string(f))
	}
	assert.Equal(t, []string{"dotenv", "shell"}, variant.Formats())
	for _, name := range []string{"", "json", "github", "DOTENV", "Shell", " shell", "env"} {
		_, ok := variant.ParseFormat(name)
		assert.Falsef(t, ok, "ParseFormat(%q)", name)
	}
}

// dotenv renders r under prefix as dotenv and fails the test on an error.
func dotenv(t *testing.T, r variant.Result) string {
	t.Helper()
	out, err := variant.Render(prefix, variant.Dotenv, r)
	require.NoError(t, err)
	return out
}

// lines joins lines with a newline after each, as Render writes them.
func lines(ls ...string) string {
	return strings.Join(ls, "\n") + "\n"
}

func jsonResult(payload string) variant.Result {
	return variant.Result{Enabled: true, Variant: "treatment", Payload: variant.Payload{Type: "json", Value: payload}}
}

func TestRenderSpecExample(t *testing.T) {
	r := jsonResult(`{"image": {"tag": "v2"}, "replicas": 3, "regions": ["eu", "us"]}`)
	assert.Equal(t, lines(
		`RELEASH_CHECKOUT_REDESIGN_ENABLED=true`,
		`RELEASH_CHECKOUT_REDESIGN_VARIANT=treatment`,
		`RELEASH_CHECKOUT_REDESIGN_PAYLOAD={"image":{"tag":"v2"},"replicas":3,"regions":["eu","us"]}`,
		`RELEASH_CHECKOUT_REDESIGN_PAYLOAD__IMAGE__TAG=v2`,
		`RELEASH_CHECKOUT_REDESIGN_PAYLOAD__REGIONS__0=eu`,
		`RELEASH_CHECKOUT_REDESIGN_PAYLOAD__REGIONS__1=us`,
		`RELEASH_CHECKOUT_REDESIGN_PAYLOAD__REPLICAS=3`,
	), dotenv(t, r))

	out, err := variant.Render("CHECKOUT_", variant.Shell, r)
	require.NoError(t, err)
	assert.Equal(t, lines(
		`export CHECKOUT_ENABLED='true'`,
		`export CHECKOUT_VARIANT='treatment'`,
		`export CHECKOUT_PAYLOAD='{"image":{"tag":"v2"},"replicas":3,"regions":["eu","us"]}'`,
		`export CHECKOUT_PAYLOAD__IMAGE__TAG='v2'`,
		`export CHECKOUT_PAYLOAD__REGIONS__0='eu'`,
		`export CHECKOUT_PAYLOAD__REGIONS__1='us'`,
		`export CHECKOUT_PAYLOAD__REPLICAS='3'`,
	), out)
}

func TestRenderPayloadTypes(t *testing.T) {
	tests := []struct {
		name string
		r    variant.Result
		want string
	}{
		{
			name: "string payload unchanged",
			r:    variant.Result{Enabled: true, Variant: "blue", Payload: variant.Payload{Type: "string", Value: ` a=b "c" {"x":1} `}},
			want: lines(prefix+"ENABLED=true", prefix+"VARIANT=blue", prefix+`PAYLOAD= a=b "c" {"x":1} `),
		},
		{
			name: "number payload text unchanged",
			r:    variant.Result{Enabled: true, Variant: "n", Payload: variant.Payload{Type: "number", Value: "1.50"}},
			want: lines(prefix+"ENABLED=true", prefix+"VARIANT=n", prefix+"PAYLOAD=1.50"),
		},
		{
			// Unleash 8.3.1 accepts and serves a number payload that is not a number.
			name: "non-numeric number payload unchanged",
			r:    variant.Result{Enabled: true, Variant: "n", Payload: variant.Payload{Type: "number", Value: "abc"}},
			want: lines(prefix+"ENABLED=true", prefix+"VARIANT=n", prefix+"PAYLOAD=abc"),
		},
		{
			name: "csv payload unchanged",
			r:    variant.Result{Enabled: true, Variant: "c", Payload: variant.Payload{Type: "csv", Value: "eu, us,ap"}},
			want: lines(prefix+"ENABLED=true", prefix+"VARIANT=c", prefix+"PAYLOAD=eu, us,ap"),
		},
		{
			name: "unknown type treated as string",
			r:    variant.Result{Enabled: true, Variant: "y", Payload: variant.Payload{Type: "yaml", Value: `{"a": 1}`}},
			want: lines(prefix+"ENABLED=true", prefix+"VARIANT=y", prefix+`PAYLOAD={"a": 1}`),
		},
		{
			// Only "json" is flattened; type names are case-sensitive.
			name: "JSON in upper case treated as string",
			r:    variant.Result{Enabled: true, Variant: "y", Payload: variant.Payload{Type: "JSON", Value: `{"a": 1}`}},
			want: lines(prefix+"ENABLED=true", prefix+"VARIANT=y", prefix+`PAYLOAD={"a": 1}`),
		},
		{
			name: "empty string payload",
			r:    variant.Result{Enabled: true, Variant: "e", Payload: variant.Payload{Type: "string", Value: ""}},
			want: lines(prefix+"ENABLED=true", prefix+"VARIANT=e", prefix+"PAYLOAD="),
		},
		{
			name: "no payload",
			r:    variant.Result{Enabled: true, Variant: "control"},
			want: lines(prefix+"ENABLED=true", prefix+"VARIANT=control"),
		},
		{
			name: "enabled flag without variants",
			r:    variant.Result{Enabled: true, Variant: variant.DisabledVariant},
			want: lines(prefix+"ENABLED=true", prefix+"VARIANT=disabled"),
		},
		{
			name: "disabled",
			r:    variant.Result{Variant: variant.DisabledVariant},
			want: lines(prefix+"ENABLED=false", prefix+"VARIANT=disabled"),
		},
		{
			name: "variant name unchanged",
			r:    variant.Result{Enabled: true, Variant: "Tréatment B=1"},
			want: lines(prefix+"ENABLED=true", prefix+"VARIANT=Tréatment B=1"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, dotenv(t, tt.r))
		})
	}
}

func TestRenderFlattening(t *testing.T) {
	head := func(compact string) []string {
		return []string{prefix + "ENABLED=true", prefix + "VARIANT=treatment", prefix + "PAYLOAD=" + compact}
	}
	tests := []struct {
		name    string
		payload string
		want    string
	}{
		{
			name:    "nested objects",
			payload: `{"a": {"b": {"c": "deep"}}, "d": "top"}`,
			want:    lines(append(head(`{"a":{"b":{"c":"deep"}},"d":"top"}`), prefix+"PAYLOAD__A__B__C=deep", prefix+"PAYLOAD__D=top")...),
		},
		{
			name:    "array of objects and nested arrays",
			payload: `{"svc": [{"name": "api", "ports": [80, 443]}, {"name": "web"}]}`,
			want: lines(append(head(`{"svc":[{"name":"api","ports":[80,443]},{"name":"web"}]}`),
				prefix+"PAYLOAD__SVC__0__NAME=api",
				prefix+"PAYLOAD__SVC__0__PORTS__0=80",
				prefix+"PAYLOAD__SVC__0__PORTS__1=443",
				prefix+"PAYLOAD__SVC__1__NAME=web",
			)...),
		},
		{
			name:    "root array",
			payload: `["x", ["y"]]`,
			want:    lines(append(head(`["x",["y"]]`), prefix+"PAYLOAD__0=x", prefix+"PAYLOAD__1__0=y")...),
		},
		{
			name:    "null is empty, booleans are words",
			payload: `{"none": null, "yes": true, "no": false, "list": [null]}`,
			want: lines(append(head(`{"none":null,"yes":true,"no":false,"list":[null]}`),
				prefix+"PAYLOAD__LIST__0=",
				prefix+"PAYLOAD__NO=false",
				prefix+"PAYLOAD__NONE=",
				prefix+"PAYLOAD__YES=true",
			)...),
		},
		{
			name:    "empty containers produce no variable",
			payload: `{"o": {}, "a": [], "nested": {"o": {}, "a": [[]]}, "k": 1}`,
			want:    lines(append(head(`{"o":{},"a":[],"nested":{"o":{},"a":[[]]},"k":1}`), prefix+"PAYLOAD__K=1")...),
		},
		{name: "empty root object", payload: ` { } `, want: lines(head(`{}`)...)},
		{name: "empty root array", payload: `[]`, want: lines(head(`[]`)...)},
		{name: "string root", payload: ` "v2" `, want: lines(head(`"v2"`)...)},
		{name: "number root", payload: `1.0`, want: lines(head(`1.0`)...)},
		{name: "true root", payload: `true`, want: lines(head(`true`)...)},
		{name: "null root", payload: `null`, want: lines(head(`null`)...)},
		{
			name:    "number text preserved",
			payload: `{"f": 1.0, "big": 12345678901234567890, "neg": -0, "exp": 1E+2, "small": 1e-7, "frac": 0.10}`,
			want: lines(append(head(`{"f":1.0,"big":12345678901234567890,"neg":-0,"exp":1E+2,"small":1e-7,"frac":0.10}`),
				prefix+"PAYLOAD__BIG=12345678901234567890",
				prefix+"PAYLOAD__EXP=1E+2",
				prefix+"PAYLOAD__F=1.0",
				prefix+"PAYLOAD__FRAC=0.10",
				prefix+"PAYLOAD__NEG=-0",
				prefix+"PAYLOAD__SMALL=1e-7",
			)...),
		},
		{
			// PAYLOAD keeps escapes as written; leaves are the decoded strings.
			name:    "escaped strings",
			payload: `{"s": "café \"q\" a\/b \\ \t"}`,
			want:    lines(append(head(`{"s":"café \"q\" a\/b \\ \t"}`), prefix+"PAYLOAD__S=café \"q\" a/b \\ \t")...),
		},
		{
			name:    "member names normalised",
			payload: `{"image-tag": 1, "log.level": 2, "max replicas": 3, "größe": 4, "camelCase": 5, "snake_case": 6, "9lives": 7}`,
			want: lines(append(head(`{"image-tag":1,"log.level":2,"max replicas":3,"größe":4,"camelCase":5,"snake_case":6,"9lives":7}`),
				prefix+"PAYLOAD__9LIVES=7",
				prefix+"PAYLOAD__CAMELCASE=5",
				prefix+"PAYLOAD__GR__E=4",
				prefix+"PAYLOAD__IMAGE_TAG=1",
				prefix+"PAYLOAD__LOG_LEVEL=2",
				prefix+"PAYLOAD__MAX_REPLICAS=3",
				prefix+"PAYLOAD__SNAKE_CASE=6",
			)...),
		},
		{
			// A name with "__" that nothing collides with is kept, and looks nested.
			name:    "double underscore without a collision",
			payload: `{"a__b": 1}`,
			want:    lines(append(head(`{"a__b":1}`), prefix+"PAYLOAD__A__B=1")...),
		},
		{
			name:    "empty member name",
			payload: `{"": "x"}`,
			want:    lines(append(head(`{"":"x"}`), prefix+"PAYLOAD__=x")...),
		},
		{
			// Byte order: "10" before "2", and "_" (0x5F) after letters and digits.
			name:    "sorted by name in byte order",
			payload: `{"b": 1, "a_b": 2, "ab": 3, "a": 4, "l": [0,1,2,3,4,5,6,7,8,9,10]}`,
			want: lines(append(head(`{"b":1,"a_b":2,"ab":3,"a":4,"l":[0,1,2,3,4,5,6,7,8,9,10]}`),
				prefix+"PAYLOAD__A=4",
				prefix+"PAYLOAD__AB=3",
				prefix+"PAYLOAD__A_B=2",
				prefix+"PAYLOAD__B=1",
				prefix+"PAYLOAD__L__0=0",
				prefix+"PAYLOAD__L__1=1",
				prefix+"PAYLOAD__L__10=10",
				prefix+"PAYLOAD__L__2=2",
				prefix+"PAYLOAD__L__3=3",
				prefix+"PAYLOAD__L__4=4",
				prefix+"PAYLOAD__L__5=5",
				prefix+"PAYLOAD__L__6=6",
				prefix+"PAYLOAD__L__7=7",
				prefix+"PAYLOAD__L__8=8",
				prefix+"PAYLOAD__L__9=9",
			)...),
		},
		{
			name:    "same member name in different objects",
			payload: `{"x": {"a": 1}, "y": {"a": 2}, "z": [{"a": 3}, {"a": 4}]}`,
			want: lines(append(head(`{"x":{"a":1},"y":{"a":2},"z":[{"a":3},{"a":4}]}`),
				prefix+"PAYLOAD__X__A=1",
				prefix+"PAYLOAD__Y__A=2",
				prefix+"PAYLOAD__Z__0__A=3",
				prefix+"PAYLOAD__Z__1__A=4",
			)...),
		},
		{
			name:    "pretty-printed payload compacted",
			payload: "{\n  \"a\": [\n    1,\n    2\n  ]\n}\n",
			want:    lines(append(head(`{"a":[1,2]}`), prefix+"PAYLOAD__A__0=1", prefix+"PAYLOAD__A__1=2")...),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, dotenv(t, jsonResult(tt.payload)))
		})
	}
}

// requireUnusable checks that Render fails with an ErrUnusable error whose
// message is want, and prints nothing.
func requireUnusable(t *testing.T, format variant.Format, r variant.Result, want string) {
	t.Helper()
	out, err := variant.Render(prefix, format, r)
	require.Error(t, err)
	assert.True(t, errors.Is(err, variant.ErrUnusable), "error %v does not match ErrUnusable", err)
	assert.Equal(t, want, err.Error())
	assert.Empty(t, out)
}

func TestRenderCollisions(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    string
	}{
		{"punctuation", `{"a-b": 1, "a_b": 2}`, `payload of variant "treatment" has colliding keys "a-b" and "a_b"`},
		{"dot and space", `{"a.b": 1, "a b": 2}`, `payload of variant "treatment" has colliding keys "a.b" and "a b"`},
		{"case only", `{"imageTag": 1, "imagetag": 2}`, `payload of variant "treatment" has colliding keys "imageTag" and "imagetag"`},
		{"non-ASCII", `{"ö": 1, "ß": 2}`, `payload of variant "treatment" has colliding keys "ö" and "ß"`},
		{"double underscore against nesting", `{"a__b": 1, "a": {"b": 2}}`, `payload of variant "treatment" has colliding keys "a__b" and "a/b"`},
		{"nesting against double underscore", `{"a": {"b": 2}, "a__b": 1}`, `payload of variant "treatment" has colliding keys "a/b" and "a__b"`},
		{"double underscore against an index", `{"a__0": 1, "a": [2]}`, `payload of variant "treatment" has colliding keys "a__0" and "a/0"`},
		{"containers", `{"a-b": {"c": 1}, "a_b": {"d": 2}}`, `payload of variant "treatment" has colliding keys "a-b" and "a_b"`},
		{"empty containers", `{"a-b": {}, "a_b": []}`, `payload of variant "treatment" has colliding keys "a-b" and "a_b"`},
		{"inside an array", `[{"x": 1}, {"a-b": 1, "a_b": 2}]`, `payload of variant "treatment" has colliding keys "1/a-b" and "1/a_b"`},
		{"deep", `{"a": {"b": {"c-d": 1, "c_d": 2}}}`, `payload of variant "treatment" has colliding keys "a/b/c-d" and "a/b/c_d"`},
		{"slash and tilde in paths", `{"a/b": 1, "a~b": 2}`, `payload of variant "treatment" has colliding keys "a~1b" and "a~0b"`},
		{"duplicate member", `{"a": 1, "a": 2}`, `payload of variant "treatment" has duplicate key "a"`},
		{"duplicate member with the same value", `{"a": 1, "a": 1}`, `payload of variant "treatment" has duplicate key "a"`},
		{"duplicate written with an escape", `{"a": 1, "a": 2}`, `payload of variant "treatment" has duplicate key "a"`},
		{"duplicate nested", `{"x": [{"a": {}, "a": {}}]}`, `payload of variant "treatment" has duplicate key "x/0/a"`},
		{"duplicate empty name", `{"": 1, "": 2}`, `payload of variant "treatment" has duplicate key ""`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, format := range []variant.Format{variant.Dotenv, variant.Shell} {
				requireUnusable(t, format, jsonResult(tt.payload), tt.want)
			}
		})
	}
}

func TestRenderInvalidJSON(t *testing.T) {
	payloads := []string{
		``,
		` `,
		`{`,
		`{"a": 1,}`,
		`{'a': 1}`,
		`{"a": 1} {"b": 2}`,
		`{"a": 1}x`,
		`[1, 2`,
		`NaN`,
		`01`,
		`nul`,
		`{"a": "unterminated}`,
		"{\"a\": \"raw\nnewline\"}",
		`v2`,
		strings.Repeat("[", 10001) + strings.Repeat("]", 10001),
	}
	for _, p := range payloads {
		t.Run(p[:min(len(p), 20)], func(t *testing.T) {
			r := variant.Result{Enabled: true, Variant: "treatment", Payload: variant.Payload{Type: "json", Value: p}}
			for _, format := range []variant.Format{variant.Dotenv, variant.Shell} {
				out, err := variant.Render(prefix, format, r)
				require.Error(t, err)
				assert.True(t, errors.Is(err, variant.ErrUnusable), "error %v does not match ErrUnusable", err)
				assert.True(t, strings.HasPrefix(err.Error(), `payload of variant "treatment" is not valid JSON: `), "error %q", err)
				assert.Empty(t, out)
			}
		})
	}
}

func TestRenderUnrepresentable(t *testing.T) {
	str := func(v string) variant.Result {
		return variant.Result{Enabled: true, Variant: "treatment", Payload: variant.Payload{Type: "string", Value: v}}
	}
	tests := []struct {
		name   string
		format variant.Format
		r      variant.Result
		want   string
	}{
		{"newline in dotenv", variant.Dotenv, str("a\nb"), `variable RELEASH_CHECKOUT_REDESIGN_PAYLOAD of variant "treatment" contains a newline, which the dotenv format cannot represent`},
		{"trailing newline in dotenv", variant.Dotenv, str("a\n"), `variable RELEASH_CHECKOUT_REDESIGN_PAYLOAD of variant "treatment" contains a newline, which the dotenv format cannot represent`},
		{"carriage return in dotenv", variant.Dotenv, str("a\rb"), `variable RELEASH_CHECKOUT_REDESIGN_PAYLOAD of variant "treatment" contains a carriage return, which the dotenv format cannot represent`},
		{"NUL in dotenv", variant.Dotenv, str("a\x00b"), `variable RELEASH_CHECKOUT_REDESIGN_PAYLOAD of variant "treatment" contains a NUL character, which the dotenv format cannot represent`},
		{"NUL in shell", variant.Shell, str("a\x00b"), `variable RELEASH_CHECKOUT_REDESIGN_PAYLOAD of variant "treatment" contains a NUL character, which the shell format cannot represent`},
		{"newline in a csv payload", variant.Dotenv, variant.Result{Enabled: true, Variant: "c", Payload: variant.Payload{Type: "csv", Value: "a,b\nc,d"}}, `variable RELEASH_CHECKOUT_REDESIGN_PAYLOAD of variant "c" contains a newline, which the dotenv format cannot represent`},
		{"newline in the variant name", variant.Dotenv, variant.Result{Enabled: true, Variant: "a\nb"}, `variable RELEASH_CHECKOUT_REDESIGN_VARIANT of variant "a\nb" contains a newline, which the dotenv format cannot represent`},
		// The compact PAYLOAD keeps "\n" escaped, so only the leaf fails.
		{"newline in a json leaf", variant.Dotenv, jsonResult(`{"note": "a\nb"}`), `variable RELEASH_CHECKOUT_REDESIGN_PAYLOAD__NOTE of variant "treatment" contains a newline, which the dotenv format cannot represent`},
		{"NUL in a json leaf in shell", variant.Shell, jsonResult(`{"note": "a\u0000b"}`), `variable RELEASH_CHECKOUT_REDESIGN_PAYLOAD__NOTE of variant "treatment" contains a NUL character, which the shell format cannot represent`},
		// The first variable in output order is reported.
		{"first in output order", variant.Dotenv, jsonResult(`{"z": "a\nb", "a": "c\rd"}`), `variable RELEASH_CHECKOUT_REDESIGN_PAYLOAD__A of variant "treatment" contains a carriage return, which the dotenv format cannot represent`},
		{"NUL before newline", variant.Dotenv, str("a\nb\x00"), `variable RELEASH_CHECKOUT_REDESIGN_PAYLOAD of variant "treatment" contains a NUL character, which the dotenv format cannot represent`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requireUnusable(t, tt.format, tt.r, tt.want)
		})
	}
}

func TestRenderShellCarriesNewlines(t *testing.T) {
	out, err := variant.Render("X_", variant.Shell, jsonResult(`{"note": "a\nb\r\n"}`))
	require.NoError(t, err)
	assert.Equal(t, lines(
		`export X_ENABLED='true'`,
		`export X_VARIANT='treatment'`,
		`export X_PAYLOAD='{"note":"a\nb\r\n"}'`,
		"export X_PAYLOAD__NOTE='a\nb\r\n'",
	), out)
}

func TestRenderShellQuoting(t *testing.T) {
	out, err := variant.Render("X_", variant.Shell, variant.Result{Variant: "it's", Payload: variant.Payload{Type: "string", Value: `'$a' "b" \c`}})
	require.NoError(t, err)
	assert.Equal(t, lines(
		`export X_ENABLED='false'`,
		`export X_VARIANT='it'\''s'`,
		`export X_PAYLOAD=''\''$a'\'' "b" \c'`,
	), out)
}

// TestShellOutputSurvivesEval evals the shell output in /bin/sh and reads
// each variable back from a child process, so it also checks the variables
// are exported.
func TestShellOutputSurvivesEval(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs /bin/sh")
	}
	values := []string{
		``,
		`plain`,
		`'`,
		`''`,
		`it's`,
		`'\''`,
		`$HOME`,
		`${HOME}`,
		`$(echo pwned)`,
		"`echo pwned`",
		`\`,
		`\\`,
		`\'`,
		`\n`,
		`"double" quotes`,
		`  leading and trailing spaces  `,
		"tab\there",
		"line1\nline2",
		"trailing newline\n",
		"\n",
		"crlf\r\n",
		`!history *glob? [x] ~tilde #hash ;semi &amp |pipe <in >out`,
		`größe 日本 🚀`,
		`'$HOME' "$HOME" ` + "`id` \\ \n '",
	}
	for _, v := range values {
		t.Run(v, func(t *testing.T) {
			r := variant.Result{Enabled: true, Variant: v, Payload: variant.Payload{Type: "string", Value: v}}
			script, err := variant.Render("X_", variant.Shell, r)
			require.NoError(t, err)
			for name, want := range map[string]string{"X_ENABLED": "true", "X_VARIANT": v, "X_PAYLOAD": v} {
				assert.Equal(t, want, evalAndRead(t, script, name), "variable %s", name)
			}
		})
	}

	t.Run("flattened json", func(t *testing.T) {
		payload := `{"q": "it's", "d": "$HOME", "b": "` + "`id`" + `", "s": "a\\b", "n": "x\ny", "sp": " a b "}`
		script, err := variant.Render("X_", variant.Shell, jsonResult(payload))
		require.NoError(t, err)
		want := map[string]string{
			"X_PAYLOAD__Q":  `it's`,
			"X_PAYLOAD__D":  `$HOME`,
			"X_PAYLOAD__B":  "`id`",
			"X_PAYLOAD__S":  `a\b`,
			"X_PAYLOAD__N":  "x\ny",
			"X_PAYLOAD__SP": ` a b `,
			"X_PAYLOAD":     `{"q":"it's","d":"$HOME","b":"` + "`id`" + `","s":"a\\b","n":"x\ny","sp":" a b "}`,
		}
		for name, value := range want {
			assert.Equal(t, value, evalAndRead(t, script, name), "variable %s", name)
		}
	})
}

// evalAndRead evals script in /bin/sh and returns the value of the
// environment variable name as a child shell sees it.
func evalAndRead(t *testing.T, script, name string) string {
	t.Helper()
	// $1 is the script, $2 the name. The child only sees exported variables.
	const outer = `eval "$1" && exec /bin/sh -c 'eval "printf %s \"\${$0}\""' "$2"`
	cmd := exec.Command("/bin/sh", "-c", outer, "sh", script, name)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=/nonexistent"}
	out, err := cmd.Output()
	require.NoError(t, err)
	return string(out)
}

func TestFallback(t *testing.T) {
	assert.Equal(t, lines("CHECKOUT_ENABLED=false", "CHECKOUT_VARIANT=disabled"), variant.Fallback("CHECKOUT_", variant.Dotenv))
	assert.Equal(t, lines("export RELEASH_X_ENABLED='false'", "export RELEASH_X_VARIANT='disabled'"), variant.Fallback(variant.DefaultPrefix("x"), variant.Shell))
	assert.Empty(t, variant.Fallback("CHECKOUT_", variant.Format("json")))
}

func TestRenderUnknownFormat(t *testing.T) {
	out, err := variant.Render(prefix, variant.Format("json"), variant.Result{Variant: "a"})
	require.Error(t, err)
	assert.False(t, errors.Is(err, variant.ErrUnusable))
	assert.Empty(t, out)
}
