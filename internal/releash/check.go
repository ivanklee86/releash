package releash

import (
	"fmt"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Defaults that Check applies when an option is not given (spec 0001).
const (
	DefaultTimeout     = 5 * time.Second
	DefaultEnvironment = "production"
	DefaultAppName     = "releash"
)

// Long names of the check command's options, without the leading "--".
// CheckOptions.Given is keyed by them.
const (
	OptionURL           = "url"
	OptionToken         = "token"
	OptionTimeout       = "timeout"
	OptionLoud          = "loud"
	OptionEnv           = "env"
	OptionAppName       = "app-name"
	OptionUserID        = "user-id"
	OptionSessionID     = "session-id"
	OptionRemoteAddress = "remote-address"
	OptionCurrentTime   = "current-time"
	OptionProperties    = "properties"
)

// ReleashOptions returns the options that control how releash connects and
// reports, in the order help lists them. Giving one twice exits ExitUsage.
func ReleashOptions() []string {
	return []string{OptionURL, OptionToken, OptionTimeout, OptionLoud}
}

// ContextOptions returns the options that set fields of the Unleash context,
// in the order help lists them. Giving one other than OptionProperties twice
// exits ExitDuplicate.
func ContextOptions() []string {
	return []string{OptionEnv, OptionAppName, OptionUserID, OptionSessionID, OptionRemoteAddress, OptionCurrentTime, OptionProperties}
}

// standardFields maps each standard Unleash context field to the option that
// sets it. The SDK reads standard fields before properties, so a property with
// one of these names would be silently ignored.
var standardFields = map[string]string{
	"environment":   OptionEnv,
	"appName":       OptionAppName,
	"userId":        OptionUserID,
	"sessionId":     OptionSessionID,
	"remoteAddress": OptionRemoteAddress,
	"currentTime":   OptionCurrentTime,
}

// notImplementedLine is the --loud line for a valid check until evaluation
// lands in M4, which removes it.
const notImplementedLine = "releash: evaluation is not implemented yet (M4); returning false"

// CheckOptions is what the command line and environment gave releash check,
// before validation. Check validates it and applies defaults.
type CheckOptions struct {
	// Flag is the name of the feature flag to check.
	Flag string
	// URL is the Unleash API URL. Empty means not configured.
	URL string
	// Token is the Unleash client API token. Empty means not configured.
	Token string
	// Loud asks for diagnostics on Err.
	Loud bool

	// Timeout, Env, AppName, UserID, SessionID, RemoteAddress and CurrentTime
	// hold option values as given. Each is read only when Given counts its
	// option; otherwise the option takes its default or stays unset.
	Timeout       string
	Env           string
	AppName       string
	UserID        string
	SessionID     string
	RemoteAddress string
	CurrentTime   string

	// Properties holds each --properties value, "name=value", in the order
	// given.
	Properties []string

	// Given counts how many times each option was set, keyed by its Option
	// name. A missing key counts as zero. OptionProperties need not be
	// counted, because it may be repeated.
	Given map[string]int
}

// checkRequest is a validated CheckOptions with defaults applied.
type checkRequest struct {
	flag    string
	url     *url.URL
	token   string
	timeout time.Duration
	loud    bool
	context evalContext
}

// evalContext is the Unleash context a flag is evaluated against. Empty
// strings and a nil currentTime mean unset. currentTime is a pointer because
// the zero time is a valid --current-time.
type evalContext struct {
	environment   string
	appName       string
	userID        string
	sessionID     string
	remoteAddress string
	currentTime   *time.Time
	properties    map[string]string
}

// problem is one mistake in a CheckOptions and the exit code it maps to.
type problem struct {
	code int
	msg  string
}

// Check validates opts and answers whether opts.Flag is enabled. The answer
// is always false until evaluation lands in M4.
//
// Check writes exactly "false\n" to Out, whatever the outcome. It writes each
// problem it finds in opts to Err as a "releash: " line, with or without
// opts.Loud, and returns the lowest exit code among them (ADR 0004), so the
// code does not depend on validation order. A valid invocation returns ExitOK
// and writes nothing to Err unless opts.Loud is set.
func (r *Releash) Check(opts CheckOptions) int {
	_, problems := validateCheck(opts)

	code := ExitOK
	for _, p := range problems {
		_, _ = fmt.Fprintf(r.Err, "releash: %s\n", p.msg)
		if code == ExitOK || p.code < code {
			code = p.code
		}
	}
	switch {
	case code == ExitUsage:
		_, _ = fmt.Fprintln(r.Err, "Run 'releash check --help' for usage.")
	case code == ExitOK && opts.Loud:
		_, _ = fmt.Fprintln(r.Err, notImplementedLine)
	}

	_, _ = fmt.Fprintln(r.Out, "false")
	return code
}

// validateCheck returns opts as a checkRequest and every problem in it. The
// request is only meaningful when there are no problems.
func validateCheck(opts CheckOptions) (checkRequest, []problem) {
	var problems []problem
	report := func(code int, format string, args ...any) {
		problems = append(problems, problem{code: code, msg: fmt.Sprintf(format, args...)})
	}
	given := func(option string) bool { return opts.Given[option] > 0 }

	req := checkRequest{
		flag:    opts.Flag,
		token:   opts.Token,
		timeout: DefaultTimeout,
		loud:    opts.Loud,
		context: evalContext{
			environment: DefaultEnvironment,
			appName:     DefaultAppName,
			userID:      opts.UserID,
			sessionID:   opts.SessionID,
		},
	}

	if opts.Flag == "" {
		report(ExitUsage, "missing flag name")
	}
	for _, option := range ReleashOptions() {
		if n := opts.Given[option]; n > 1 {
			report(ExitUsage, "--%s given %d times; give it at most once", option, n)
		}
	}
	if given(OptionTimeout) {
		d, err := time.ParseDuration(opts.Timeout)
		if err != nil || d <= 0 {
			report(ExitUsage, "invalid --%s %q: must be a positive Go duration, e.g. 5s", OptionTimeout, opts.Timeout)
		}
		req.timeout = d
	}

	if opts.URL == "" {
		report(ExitNotConfigured, "no Unleash URL: set UNLEASH_URL or --%s to the Unleash API URL", OptionURL)
	} else if u, err := parseUnleashURL(opts.URL); err != nil {
		report(ExitInvalid, "invalid Unleash URL %q: must be an absolute http or https URL", opts.URL)
	} else {
		req.url = u
	}
	if opts.Token == "" {
		report(ExitNotConfigured, "no Unleash API token: set UNLEASH_API_TOKEN or --%s to a client API token", OptionToken)
	}

	for _, option := range ContextOptions() {
		if n := opts.Given[option]; n > 1 && option != OptionProperties {
			report(ExitDuplicate, "--%s given %d times; give it at most once", option, n)
		}
	}
	if given(OptionEnv) {
		if opts.Env == "" {
			report(ExitInvalid, "invalid --%s \"\": must not be empty", OptionEnv)
		}
		req.context.environment = opts.Env
	}
	if given(OptionAppName) {
		if opts.AppName == "" {
			report(ExitInvalid, "invalid --%s \"\": must not be empty", OptionAppName)
		}
		req.context.appName = opts.AppName
	}
	if given(OptionRemoteAddress) {
		addr, err := netip.ParseAddr(opts.RemoteAddress)
		if err != nil || addr.Zone() != "" {
			report(ExitInvalid, "invalid --%s %q: must be an IPv4 or IPv6 address", OptionRemoteAddress, opts.RemoteAddress)
		} else {
			req.context.remoteAddress = addr.String()
		}
	}
	if given(OptionCurrentTime) {
		if t, err := parseRFC3339(opts.CurrentTime); err != nil {
			report(ExitInvalid, "invalid --%s %q: must be an RFC 3339 timestamp", OptionCurrentTime, opts.CurrentTime)
		} else {
			req.context.currentTime = &t
		}
	}

	properties := make(map[string]string, len(opts.Properties))
	repeated := make(map[string]int)
	for _, p := range opts.Properties {
		name, value, ok := strings.Cut(p, "=")
		switch {
		case !ok:
			report(ExitInvalid, "invalid --%s %q: must be name=value", OptionProperties, p)
		case name == "":
			report(ExitInvalid, "invalid --%s %q: the property name must not be empty", OptionProperties, p)
		case standardFields[name] != "":
			report(ExitInvalid, "invalid --%s %q: property %q is a standard field; use --%s", OptionProperties, p, name, standardFields[name])
		default:
			if _, seen := properties[name]; seen {
				repeated[name]++
			}
			properties[name] = value
		}
	}
	// Report repeats in the order the names were first given, so messages are
	// stable across runs.
	for _, p := range opts.Properties {
		name, _, _ := strings.Cut(p, "=")
		if n := repeated[name]; n > 0 {
			report(ExitDuplicate, "property %q given %d times in --%s; give each name once", name, n+1, OptionProperties)
			delete(repeated, name)
		}
	}
	req.context.properties = properties

	return req, problems
}

// parseUnleashURL parses s as an absolute http or https URL with a host.
func parseUnleashURL(s string) (*url.URL, error) {
	u, err := url.Parse(s)
	if err != nil {
		return nil, err
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return nil, fmt.Errorf("%q is not an absolute http or https URL", s)
	}
	// url.Parse accepts any run of digits as a port.
	if p := u.Port(); p != "" {
		if n, err := strconv.Atoi(p); err != nil || n < 1 || n > 65535 {
			return nil, fmt.Errorf("%q has an invalid port", s)
		}
	}
	return u, nil
}

// parseRFC3339 parses s as an RFC 3339 date-time. It differs from time.Parse
// with time.RFC3339Nano where that differs from the RFC: it accepts a
// lowercase "t" and "z" and a leap second (:60, read as the first instant of
// the next minute, since time.Time has no leap seconds), and rejects offsets
// whose hours exceed 23 or whose minutes exceed 59.
func parseRFC3339(s string) (time.Time, error) {
	s = strings.ToUpper(s)
	if !strings.HasSuffix(s, "Z") {
		// A numeric offset is the last six characters, "+hh:mm" or "-hh:mm".
		if len(s) < 6 || (s[len(s)-6] != '+' && s[len(s)-6] != '-') || s[len(s)-3] != ':' {
			return time.Time{}, fmt.Errorf("%q has no valid time offset", s)
		}
		hh, errH := strconv.Atoi(s[len(s)-5 : len(s)-3])
		mm, errM := strconv.Atoi(s[len(s)-2:])
		if errH != nil || errM != nil || hh > 23 || mm > 59 {
			return time.Time{}, fmt.Errorf("%q has an invalid time offset", s)
		}
	}
	// "YYYY-MM-DDTHH:MM:60" puts the seconds at bytes 17 and 18.
	leap := len(s) > 19 && s[16] == ':' && s[17:19] == "60"
	if leap {
		s = s[:17] + "59" + s[19:]
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, err
	}
	if leap {
		t = t.Add(time.Second)
	}
	return t, nil
}
