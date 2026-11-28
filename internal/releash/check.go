package releash

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"regexp"
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
func ReleashOptions() []string { //nolint:revive // exported: the name matches the "Releash options" help heading.
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

// checkRequest is a validated CheckOptions with defaults applied, and the
// question to ask once the flags are fetched.
type checkRequest struct {
	flag    string
	url     *url.URL
	token   string
	timeout time.Duration
	loud    bool
	context evalContext
	// ask is the final call of the evaluation. Nil means evaluateFlag, the
	// check command's question.
	ask question
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

// Check validates opts and answers whether opts.Flag is enabled in Unleash.
//
// Check writes exactly "true\n" or "false\n" to Out. It writes each problem
// it finds in opts to Err as a "releash: " line, with or without opts.Loud,
// and returns the lowest exit code among them (ADR 0004), so the code does
// not depend on validation order; the answer is then false and Unleash is not
// contacted. A valid invocation returns ExitOK, also when the answer falls
// back to false because Unleash could not give one (spec 0001), and finishes
// within its timeout. Under opts.Loud it writes one line to Err for each
// problem that did not change the answer and one for the reason the answer
// fell back to false. A flag that is not in the fetched flags gets its line
// without opts.Loud too (ADR 0009); otherwise a valid invocation writes
// nothing to Err.
func (r *Releash) Check(opts CheckOptions) int {
	req, problems := validateCheck(opts)
	if code := r.reportProblems("check", problems); code != ExitOK {
		_, _ = fmt.Fprintln(r.Out, "false")
		return code
	}

	out := r.evaluateRequest(req)
	r.reportOutcome(req, out, "false")
	_, _ = fmt.Fprintln(r.Out, out.enabled && out.fallback == "")
	return ExitOK
}

// reportProblems writes each problem to Err as a "releash: " line, and the
// pointer to "releash <command> --help" if any is a bad command line. It
// returns the lowest exit code among the problems, or ExitOK if there are
// none.
func (r *Releash) reportProblems(command string, problems []problem) int {
	code := ExitOK
	for _, p := range problems {
		_, _ = fmt.Fprintf(r.Err, "releash: %s\n", p.msg)
		if code == ExitOK || p.code < code {
			code = p.code
		}
	}
	if code == ExitUsage {
		_, _ = fmt.Fprintf(r.Err, "Run 'releash %s --help' for usage.\n", command)
	}
	return code
}

// evaluateRequest answers req with r.evaluate, or evaluate when it is nil.
func (r *Releash) evaluateRequest(req checkRequest) outcome {
	eval := r.evaluate
	if eval == nil {
		eval = evaluate
	}
	return eval(context.Background(), req)
}

// reportOutcome writes the notes of out under --loud, then the fallback line,
// which ends by saying the command is returning answer. The fallback line is
// written without --loud too when out.alwaysReported is set.
func (r *Releash) reportOutcome(req checkRequest, out outcome, answer string) {
	if req.loud {
		for _, note := range out.notes {
			_, _ = fmt.Fprintf(r.Err, "releash: %s\n", note)
		}
	}
	if out.fallback != "" && (req.loud || out.alwaysReported) {
		_, _ = fmt.Fprintf(r.Err, "releash: %s; returning %s\n", out.fallback, answer)
	}
}

// validateCheck returns opts as a checkRequest and every problem in it. The
// request is only meaningful when there are no problems.
func validateCheck(opts CheckOptions) (checkRequest, []problem) {
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
	var p problems
	p.checkConnection(opts, &req)
	p.checkContext(opts, &req.context)
	req.context.properties = p.checkProperties(opts.Properties)
	return req, p.list
}

// problems collects the problems validateCheck finds, in the order it finds
// them.
type problems struct {
	list []problem
}

func (p *problems) report(code int, format string, args ...any) {
	p.list = append(p.list, problem{code: code, msg: fmt.Sprintf(format, args...)})
}

// checkConnection checks the flag name and the releash options, and sets the
// timeout and URL of req.
func (p *problems) checkConnection(opts CheckOptions, req *checkRequest) {
	if opts.Flag == "" {
		p.report(ExitUsage, "missing flag name")
	}
	for _, option := range ReleashOptions() {
		if n := opts.Given[option]; n > 1 {
			p.report(ExitUsage, "--%s given %d times; give it at most once", option, n)
		}
	}
	if opts.Given[OptionTimeout] > 0 {
		d, err := time.ParseDuration(opts.Timeout)
		if err != nil || d <= 0 {
			p.report(ExitUsage, "invalid --%s %q: must be a positive Go duration, e.g. 5s", OptionTimeout, opts.Timeout)
		}
		req.timeout = d
	}

	if opts.URL == "" {
		p.report(ExitNotConfigured, "no Unleash URL: set UNLEASH_URL or --%s to the Unleash API URL", OptionURL)
	} else if u, err := parseUnleashURL(opts.URL); err != nil {
		p.report(ExitInvalid, "invalid Unleash URL %s: %v", redactURL(opts.URL), err)
	} else {
		req.url = u
	}
	if opts.Token == "" {
		p.report(ExitNotConfigured, "no Unleash API token: set UNLEASH_API_TOKEN or --%s to a client API token", OptionToken)
	}
}

// checkContext checks the context options other than --properties and sets the
// ones that were given in c.
func (p *problems) checkContext(opts CheckOptions, c *evalContext) {
	given := func(option string) bool { return opts.Given[option] > 0 }
	for _, option := range ContextOptions() {
		if n := opts.Given[option]; n > 1 && option != OptionProperties {
			p.report(ExitDuplicate, "--%s given %d times; give it at most once", option, n)
		}
	}
	if given(OptionEnv) {
		if opts.Env == "" {
			p.report(ExitInvalid, "invalid --%s \"\": must not be empty", OptionEnv)
		}
		c.environment = opts.Env
	}
	if given(OptionAppName) {
		if opts.AppName == "" {
			p.report(ExitInvalid, "invalid --%s \"\": must not be empty", OptionAppName)
		}
		c.appName = opts.AppName
	}
	if given(OptionRemoteAddress) {
		addr, err := netip.ParseAddr(opts.RemoteAddress)
		if err != nil || addr.Zone() != "" {
			p.report(ExitInvalid, "invalid --%s %q: must be an IPv4 or IPv6 address", OptionRemoteAddress, opts.RemoteAddress)
		} else {
			// Unmapped, so it reads the same as a detected address for that host.
			c.remoteAddress = addr.Unmap().String()
		}
	}
	if given(OptionCurrentTime) {
		if t, err := parseRFC3339(opts.CurrentTime); err != nil {
			p.report(ExitInvalid, "invalid --%s %q: must be an RFC 3339 timestamp", OptionCurrentTime, opts.CurrentTime)
		} else {
			c.currentTime = &t
		}
	}
}

// checkProperties checks each --properties value and returns the valid ones by
// name. A repeated name keeps its last value.
func (p *problems) checkProperties(given []string) map[string]string {
	properties := make(map[string]string, len(given))
	repeated := make(map[string]int)
	for _, prop := range given {
		name, value, ok := strings.Cut(prop, "=")
		switch {
		case !ok:
			p.report(ExitInvalid, "invalid --%s %q: must be name=value", OptionProperties, prop)
		case name == "":
			p.report(ExitInvalid, "invalid --%s %q: the property name must not be empty", OptionProperties, prop)
		case standardFields[name] != "":
			p.report(ExitInvalid, "invalid --%s %q: property %q is a standard field; use --%s", OptionProperties, prop, name, standardFields[name])
		default:
			if _, seen := properties[name]; seen {
				repeated[name]++
			}
			properties[name] = value
		}
	}
	// Report repeats in the order the names were first given, so messages are
	// stable across runs.
	for _, prop := range given {
		name, _, _ := strings.Cut(prop, "=")
		if n := repeated[name]; n > 0 {
			p.report(ExitDuplicate, "property %q given %d times in --%s; give each name once", name, n+1, OptionProperties)
			delete(repeated, name)
		}
	}
	return properties
}

// parseUnleashURL parses s as an absolute http or https URL with a host.
func parseUnleashURL(s string) (*url.URL, error) {
	u, err := url.Parse(s)
	if err != nil {
		return nil, errors.New("must be an absolute http or https URL")
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return nil, errors.New("must be an absolute http or https URL")
	}
	// The SDK appends Client API paths to the whole URL string, so a query or
	// fragment would swallow them.
	if u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.HasSuffix(s, "#") {
		return nil, errors.New("must not have a query or fragment")
	}
	// url.Parse accepts any run of digits as a port.
	if p := u.Port(); p != "" {
		if n, err := strconv.Atoi(p); err != nil || n < 1 || n > 65535 {
			return nil, fmt.Errorf("port %s is not between 1 and 65535", p)
		}
	}
	return u, nil
}

// redactURL quotes s for an error message with any password replaced by
// "xxxxx", because the URL may come from a CI secret and errors are printed
// without --loud. A URL that does not parse but contains "@", and so may hold
// credentials, is not echoed at all.
func redactURL(s string) string {
	u, err := url.Parse(s)
	if err == nil {
		// Redacted also normalises the URL, so use it only when it hides something.
		if _, hasPassword := u.User.Password(); hasPassword {
			return strconv.Quote(u.Redacted())
		}
		return strconv.Quote(s)
	}
	if strings.Contains(s, "@") {
		return "(not shown, because it may contain credentials)"
	}
	return strconv.Quote(s)
}

// rfc3339Shape is the date-time grammar of RFC 3339 section 5.6, checked
// before time.Parse, which also accepts one-digit hours and a comma before the
// fraction.
var rfc3339Shape = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})$`)

// parseRFC3339 parses s as an RFC 3339 date-time. It differs from time.Parse
// with time.RFC3339Nano where that differs from the RFC: it requires the exact
// shape of the grammar, accepts a lowercase "t" and "z", rejects offsets whose
// hours exceed 23 or whose minutes exceed 59, and accepts a leap second (:60)
// only at 23:59 UTC. A leap second reads as the first instant of the next
// minute, since time.Time has no leap seconds.
func parseRFC3339(s string) (time.Time, error) {
	s = strings.ToUpper(s)
	if !rfc3339Shape.MatchString(s) {
		return time.Time{}, fmt.Errorf("%q is not shaped like YYYY-MM-DDTHH:MM:SS[.frac](Z|+hh:mm)", s)
	}
	if !strings.HasSuffix(s, "Z") {
		hh, _ := strconv.Atoi(s[len(s)-5 : len(s)-3])
		mm, _ := strconv.Atoi(s[len(s)-2:])
		if hh > 23 || mm > 59 {
			return time.Time{}, fmt.Errorf("%q has an invalid time offset", s)
		}
	}
	// The shape check puts the seconds at bytes 17 and 18.
	leap := s[17:19] == "60"
	if leap {
		s = s[:17] + "59" + s[19:]
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, err
	}
	if leap {
		if u := t.UTC(); u.Hour() != 23 || u.Minute() != 59 {
			return time.Time{}, fmt.Errorf("%q has a leap second that is not at 23:59:60 UTC", s)
		}
		t = t.Add(time.Second)
		// The SDK parses currentTime as RFC 3339, which has four-digit years.
		if t.Year() > 9999 {
			return time.Time{}, fmt.Errorf("%q is after the year 9999", s)
		}
	}
	return t, nil
}
