package releash

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Cases for evaluation against fake Unleash servers, from the test plan in
// spec 0001. The integration scenarios repeat the important ones against a
// real Unleash.
//   - an enabled flag is true and a disabled one false, with nothing on
//     stderr even with --loud
//   - the token goes in the Authorization header, and appName is the name
//     releash registers with
//   - each context field reaches evaluation, and so do custom properties
//   - without currentTime, date constraints use the time of the check
//   - fallbacks, each false with its own --loud line and nothing without it:
//     unknown flag, 401, 403, another error status, malformed JSON, JSON of
//     the wrong shape, unreachable server, unresponsive server (within
//     --timeout), a 304 on the first fetch, a flag the SDK cannot evaluate
//   - the --loud line for a redirected feature fetch names the status of the
//     last hop
//   - a rejected register request does not change the answer and shows only
//     under --loud
//   - a register request answered with 300 counts as registered, as the SDK
//     counts it
//   - a feature fetch caught in a redirect loop falls back at once
//   - a register request that fails after redirect hops does not change the
//     answer
//   - a register request that never finishes, directly or after a redirect,
//     does not change the answer or hold back a fetch failure past its own
//     deadline
//   - the SDK fetches the flags once, so no later refresh can end the check
//   - after ready, OnError passes on only errors without a URL
//   - no files are written to the temp directory

const testToken = "default:production.test-token"

// fakeUnleash starts a server whose /api/client/features answers features
// and whose other paths answer register, keyed by path, or 202. It returns
// the API URL.
func fakeUnleash(t *testing.T, features http.HandlerFunc, register http.HandlerFunc) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/client/features":
			features(w, r)
		case r.URL.Path == "/api/client/register" && register != nil:
			register(w, r)
		default:
			w.WriteHeader(http.StatusAccepted)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/api"
}

// serve answers with status and body.
func serve(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = fmt.Fprint(w, body)
	}
}

// featuresJSON is a Client API response holding the given features.
func featuresJSON(features ...string) string {
	return `{"version":2,"features":[` + strings.Join(features, ",") + `]}`
}

// constrained is an enabled flag whose one strategy applies only when
// contextName is in values.
func constrained(name, contextName string, values ...string) string {
	return fmt.Sprintf(`{"name":%q,"enabled":true,"strategies":[{"name":"default","constraints":[{"contextName":%q,"operator":"IN","values":[%s]}]}]}`,
		name, contextName, quoteAll(values))
}

func quoteAll(values []string) string {
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = fmt.Sprintf("%q", v)
	}
	return strings.Join(quoted, ",")
}

const (
	enabledFlag  = `{"name":"on","enabled":true,"strategies":[{"name":"default"}]}`
	disabledFlag = `{"name":"off","enabled":false,"strategies":[{"name":"default"}]}`
)

// runCheck calls Check with opts against url and returns stdout, stderr and
// the exit code.
func runCheck(t *testing.T, url, flag string, loud bool, edits ...func(*CheckOptions)) (string, string, int) {
	t.Helper()
	opts := with(all(edits...))
	opts.URL, opts.Token, opts.Flag, opts.Loud = url, testToken, flag, loud
	var stdout, stderr bytes.Buffer
	r := New()
	r.Out, r.Err = &stdout, &stderr
	code := r.Check(opts)
	return stdout.String(), stderr.String(), code
}

func TestCheckAnswersFromUnleash(t *testing.T) {
	url := fakeUnleash(t, serve(http.StatusOK, featuresJSON(enabledFlag, disabledFlag)), nil)

	for flag, want := range map[string]string{"on": "true\n", "off": "false\n"} {
		for _, loud := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/loud=%t", flag, loud), func(t *testing.T) {
				stdout, stderr, code := runCheck(t, url, flag, loud)

				assert.Equal(t, ExitOK, code, "exit code")
				assert.Equal(t, want, stdout, "stdout")
				assert.Empty(t, stderr, "stderr")
			})
		}
	}
}

func TestCheckSendsTokenAndAppName(t *testing.T) {
	var mu sync.Mutex
	var gotToken, gotFeaturesApp, gotRegisterApp string
	url := fakeUnleash(t,
		func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			gotToken, gotFeaturesApp = r.Header.Get("Authorization"), r.Header.Get("Unleash-Appname")
			mu.Unlock()
			serve(http.StatusOK, featuresJSON(enabledFlag))(w, r)
		},
		func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			gotRegisterApp = r.Header.Get("Unleash-Appname")
			mu.Unlock()
			w.WriteHeader(http.StatusAccepted)
		},
	)

	stdout, _, _ := runCheck(t, url, "on", false, set(OptionAppName, appName, "pipeline"))

	assert.Equal(t, "true\n", stdout)
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, testToken, gotToken, "Authorization")
	assert.Equal(t, "pipeline", gotFeaturesApp, "appName of the feature fetch")
	assert.Equal(t, "pipeline", gotRegisterApp, "appName releash registers with")
}

func TestCheckEvaluatesAgainstTheContext(t *testing.T) {
	url := fakeUnleash(t, serve(http.StatusOK, featuresJSON(
		constrained("by-user", "userId", "alice"),
		constrained("by-session", "sessionId", "s-1"),
		constrained("by-environment", "environment", "staging"),
		constrained("by-default-environment", "environment", DefaultEnvironment),
		constrained("by-app", "appName", "pipeline"),
		constrained("by-default-app", "appName", DefaultAppName),
		constrained("by-region", "region", "eu"),
		constrained("by-build", "build", "a=b"),
		`{"name":"by-address","enabled":true,"strategies":[{"name":"remoteAddress","parameters":{"IPs":"10.1.2.3"}}]}`,
		`{"name":"after-2099","enabled":true,"strategies":[{"name":"default","constraints":[{"contextName":"currentTime","operator":"DATE_AFTER","value":"2099-01-01T00:00:00.000Z"}]}]}`,
	)), nil)

	tests := []struct {
		flag  string
		match func(*CheckOptions)
		other func(*CheckOptions)
	}{
		{"by-user", set(OptionUserID, func(o *CheckOptions) *string { return &o.UserID }, "alice"), set(OptionUserID, func(o *CheckOptions) *string { return &o.UserID }, "carol")},
		{"by-session", set(OptionSessionID, func(o *CheckOptions) *string { return &o.SessionID }, "s-1"), set(OptionSessionID, func(o *CheckOptions) *string { return &o.SessionID }, "s-2")},
		{"by-environment", set(OptionEnv, env, "staging"), set(OptionEnv, env, "qa")},
		{"by-default-environment", func(*CheckOptions) {}, set(OptionEnv, env, "staging")},
		{"by-app", set(OptionAppName, appName, "pipeline"), set(OptionAppName, appName, "other")},
		{"by-default-app", func(*CheckOptions) {}, set(OptionAppName, appName, "pipeline")},
		{"by-region", properties("region=eu"), properties("region=us")},
		{"by-build", properties("build=a=b"), properties("build=a")},
		{"by-address", set(OptionRemoteAddress, remoteAddress, "10.1.2.3"), set(OptionRemoteAddress, remoteAddress, "10.1.2.4")},
		{"after-2099", set(OptionCurrentTime, currentTime, "2100-01-01T00:00:00Z"), set(OptionCurrentTime, currentTime, "2098-01-01T00:00:00+01:00")},
	}
	for _, tt := range tests {
		t.Run(tt.flag, func(t *testing.T) {
			stdout, stderr, code := runCheck(t, url, tt.flag, true, tt.match)
			assert.Equal(t, "true\n", stdout, "matching context")
			assert.Empty(t, stderr)
			assert.Equal(t, ExitOK, code)

			stdout, _, _ = runCheck(t, url, tt.flag, true, tt.other)
			assert.Equal(t, "false\n", stdout, "other context")
		})
	}
}

func TestCheckComparesDatesWithTheTimeOfTheCheck(t *testing.T) {
	stamp := func(d time.Duration) string { return time.Now().Add(d).UTC().Format(time.RFC3339) }
	dated := func(name, operator, value string) string {
		return fmt.Sprintf(`{"name":%q,"enabled":true,"strategies":[{"name":"default","constraints":[{"contextName":"currentTime","operator":%q,"value":%q}]}]}`, name, operator, value)
	}
	url := fakeUnleash(t, serve(http.StatusOK, featuresJSON(
		dated("started", "DATE_AFTER", stamp(-5*time.Minute)),
		dated("not-started", "DATE_AFTER", stamp(5*time.Minute)),
		dated("not-ended", "DATE_BEFORE", stamp(5*time.Minute)),
		dated("ended", "DATE_BEFORE", stamp(-5*time.Minute)),
	)), nil)

	for flag, want := range map[string]string{"started": "true\n", "not-started": "false\n", "not-ended": "true\n", "ended": "false\n"} {
		t.Run(flag, func(t *testing.T) {
			stdout, _, _ := runCheck(t, url, flag, false)
			assert.Equal(t, want, stdout)
		})
	}
}

func TestCheckFallsBackToFalse(t *testing.T) {
	const malformed = "malformed"
	tests := []struct {
		name     string
		features http.HandlerFunc
		flag     string
		wantLine string
		// alwaysReported is set for the one fallback reported without --loud
		// (ADR 0009); every other fallback is silent without it.
		alwaysReported bool
	}{
		{name: "unknown flag", features: serve(http.StatusOK, featuresJSON(enabledFlag)), flag: "missing", wantLine: `releash: flag "missing" not found; returning false`, alwaysReported: true},
		{name: "no flags at all", features: serve(http.StatusOK, featuresJSON()), flag: "missing", wantLine: `releash: flag "missing" not found (unleash returned no flags for this token's project and environment); returning false`, alwaysReported: true},
		{name: "401", features: serve(http.StatusUnauthorized, `{}`), wantLine: "releash: unleash rejected the API token (HTTP 401); returning false"},
		{name: "403", features: serve(http.StatusForbidden, `{}`), wantLine: "releash: unleash rejected the API token (HTTP 403); returning false"},
		{name: "500", features: serve(http.StatusInternalServerError, ``), wantLine: "releash: unleash answered the feature fetch with HTTP 500; returning false"},
		{name: "404", features: serve(http.StatusNotFound, ``), wantLine: "releash: unleash answered the feature fetch with HTTP 404; returning false"},
		{name: "invalid JSON", features: serve(http.StatusOK, `{"features": [`), wantLine: malformed},
		{name: "JSON of the wrong shape", features: serve(http.StatusOK, `{"features": "on"}`), wantLine: malformed},
		{name: "304 on the first fetch", features: serve(http.StatusNotModified, ``), wantLine: "releash: unleash answered the feature fetch with HTTP 304; returning false"},
		{name: "unevaluable flag", features: serve(http.StatusOK, featuresJSON(`{"name":"on","enabled":true,"strategies":[{"name":"default","segments":[99]}]}`)), wantLine: `releash: unleash could not evaluate flag "on": segment does not exist; returning false`},
	}
	for _, tt := range tests {
		flag := tt.flag
		if flag == "" {
			flag = "on"
		}
		url := fakeUnleash(t, tt.features, nil)
		for _, loud := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/loud=%t", tt.name, loud), func(t *testing.T) {
				start := time.Now()
				stdout, stderr, code := runCheck(t, url, flag, loud)

				assert.Equal(t, ExitOK, code, "exit code")
				assert.Equal(t, "false\n", stdout, "stdout")
				switch {
				case !loud && !tt.alwaysReported:
					assert.Empty(t, stderr, "stderr")
				case tt.wantLine == malformed:
					// The rest of the line is the SDK's decode error.
					assert.Regexp(t, `^releash: unleash sent a malformed feature response: .+; returning false\n$`, stderr, "stderr")
				default:
					assert.Equal(t, tt.wantLine+"\n", stderr, "stderr")
				}
				assert.Less(t, time.Since(start), time.Second, "falls back without waiting for the timeout")
			})
		}
	}
}

func TestCheckFallsBackWhenUnleashIsUnreachable(t *testing.T) {
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())

	stdout, stderr, code := runCheck(t, "http://"+addr+"/api", "on", true)

	assert.Equal(t, ExitOK, code)
	assert.Equal(t, "false\n", stdout)
	assert.Equal(t, fmt.Sprintf("releash: unleash unreachable: dial tcp %s: connect: connection refused; returning false\n", addr), stderr)

	stdout, stderr, code = runCheck(t, "http://"+addr+"/api", "on", false)
	assert.Equal(t, ExitOK, code)
	assert.Equal(t, "false\n", stdout)
	assert.Empty(t, stderr)
}

func TestCheckFallsBackWithinTheTimeoutWhenUnleashNeverAnswers(t *testing.T) {
	url := unresponsiveServer(t)

	for _, loud := range []bool{false, true} {
		t.Run(fmt.Sprintf("loud=%t", loud), func(t *testing.T) {
			start := time.Now()
			stdout, stderr, code := runCheck(t, url, "on", loud, set(OptionTimeout, timeout, "500ms"))
			elapsed := time.Since(start)

			assert.Equal(t, ExitOK, code)
			assert.Equal(t, "false\n", stdout)
			want := ""
			if loud {
				want = "releash: unleash did not answer within 500ms; returning false\n"
			}
			assert.Equal(t, want, stderr)
			assert.GreaterOrEqual(t, elapsed, 500*time.Millisecond)
			assert.Less(t, elapsed, 750*time.Millisecond, "the whole check, Close included, stays within --timeout")
		})
	}
}

func TestCheckIgnoresARejectedRegistration(t *testing.T) {
	url := fakeUnleash(t, serve(http.StatusOK, featuresJSON(enabledFlag)), serve(http.StatusUnauthorized, ``))

	stdout, stderr, code := runCheck(t, url, "on", false)
	assert.Equal(t, ExitOK, code)
	assert.Equal(t, "true\n", stdout)
	assert.Empty(t, stderr)

	stdout, stderr, code = runCheck(t, url, "on", true)
	assert.Equal(t, ExitOK, code)
	assert.Equal(t, "true\n", stdout)
	assert.Equal(t, "releash: registering with unleash failed: HTTP 401; the answer is unaffected\n", stderr)
}

// hang blocks until the client gives up on the request. The server notices
// that only once the request body has been read.
func hang(_ http.ResponseWriter, r *http.Request) {
	_, _ = io.Copy(io.Discard, r.Body)
	<-r.Context().Done()
}

func TestCheckDoesNotWaitForAHangingRegistration(t *testing.T) {
	// With --timeout 1s the register request may take 250ms.
	tests := []struct {
		name       string
		features   http.HandlerFunc
		wantStdout string
		wantStderr string
	}{
		{
			name:       "features served",
			features:   serve(http.StatusOK, featuresJSON(enabledFlag)),
			wantStdout: "true\n",
			wantStderr: "releash: registering with unleash did not finish within 250ms; the answer is unaffected\n",
		},
		{
			name:       "token rejected",
			features:   serve(http.StatusUnauthorized, `{}`),
			wantStdout: "false\n",
			wantStderr: "releash: unleash rejected the API token (HTTP 401); returning false\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := fakeUnleash(t, tt.features, hang)

			start := time.Now()
			stdout, stderr, code := runCheck(t, url, "on", true, set(OptionTimeout, timeout, "1s"))
			elapsed := time.Since(start)

			assert.Equal(t, ExitOK, code)
			assert.Equal(t, tt.wantStdout, stdout)
			assert.Equal(t, tt.wantStderr, stderr)
			assert.Less(t, elapsed, 750*time.Millisecond, "answers soon after the register deadline")
		})
	}
}

func TestCheckTreatsRegisterHTTP300AsRegistered(t *testing.T) {
	url := fakeUnleash(t, serve(http.StatusOK, featuresJSON(enabledFlag)), serve(http.StatusMultipleChoices, ``))

	stdout, stderr, code := runCheck(t, url, "on", true)

	assert.Equal(t, ExitOK, code)
	assert.Equal(t, "true\n", stdout)
	assert.Empty(t, stderr)
}

// redirectingRegister returns a server whose register request is redirected
// through hops paths before last answers it. Features are served normally.
func redirectingRegister(t *testing.T, last http.HandlerFunc, hops ...string) string {
	t.Helper()
	features := serve(http.StatusOK, featuresJSON(enabledFlag))
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next := map[string]string{"/api/client/register": hops[0]}
		for i := 0; i+1 < len(hops); i++ {
			next[hops[i]] = hops[i+1]
		}
		switch {
		case r.URL.Path == "/api/client/features":
			features(w, r)
		case next[r.URL.Path] != "":
			_, _ = io.Copy(io.Discard, r.Body)
			http.Redirect(w, r, srv.URL+next[r.URL.Path], http.StatusTemporaryRedirect)
		case r.URL.Path == hops[len(hops)-1]:
			last(w, r)
		default:
			w.WriteHeader(http.StatusAccepted)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/api"
}

func TestCheckHoldsARedirectedRegistrationToItsDeadline(t *testing.T) {
	url := redirectingRegister(t, hang, "/v2/register")

	start := time.Now()
	stdout, stderr, code := runCheck(t, url, "on", true, set(OptionTimeout, timeout, "1s"))
	elapsed := time.Since(start)

	assert.Equal(t, ExitOK, code)
	assert.Equal(t, "true\n", stdout)
	assert.Equal(t, "releash: registering with unleash did not finish within 250ms; the answer is unaffected\n", stderr)
	assert.Less(t, elapsed, 750*time.Millisecond, "answers soon after the register deadline")
}

func TestCheckIgnoresARegistrationThatFailsAfterRedirects(t *testing.T) {
	// Closing the connection without an answer makes a transport error that
	// net/http names after the previous hop, /a, not the register path.
	hangUp := func(w http.ResponseWriter, _ *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = conn.Close()
		}
	}
	url := redirectingRegister(t, hangUp, "/a", "/b")

	for range 5 {
		stdout, stderr, code := runCheck(t, url, "on", true)

		assert.Equal(t, ExitOK, code)
		assert.Equal(t, "true\n", stdout)
		assert.Contains(t, stderr, "releash: registering with unleash failed: ")
		assert.NotContains(t, stderr, "returning false")
	}
}

func TestCheckReportsTheLastHopOfARedirectedFetch(t *testing.T) {
	tests := []struct {
		name       string
		target     http.HandlerFunc
		wantStdout string
		wantStderr string
	}{
		{"token rejected", serve(http.StatusUnauthorized, `{}`), "false\n", "releash: unleash rejected the API token (HTTP 401); returning false\n"},
		{"features served", serve(http.StatusOK, featuresJSON(enabledFlag)), "true\n", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := fakeUnleash(t, tt.target, nil)
			redirecting := fakeUnleash(t, func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, target+"/client/features", http.StatusFound)
			}, nil)

			stdout, stderr, code := runCheck(t, redirecting, "on", true)

			assert.Equal(t, ExitOK, code)
			assert.Equal(t, tt.wantStdout, stdout)
			assert.Equal(t, tt.wantStderr, stderr)
		})
	}
}

func TestCheckWritesNoFiles(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	require.Equal(t, tmp, os.TempDir())
	url := fakeUnleash(t, serve(http.StatusOK, featuresJSON(enabledFlag)), nil)

	stdout, _, _ := runCheck(t, url, "on", false)

	assert.Equal(t, "true\n", stdout)
	entries, err := os.ReadDir(tmp)
	require.NoError(t, err)
	assert.Empty(t, entries, "files in the temp directory")
}

func TestOnErrorPassesOnOnlyFetchAndEvaluationErrorsBeforeReady(t *testing.T) {
	urlErr := func(path string) error {
		return &url.Error{Op: "Post", URL: "http://unleash.example.com/api" + path, Err: errors.New("connection refused")}
	}
	for _, tt := range []struct {
		name string
		err  error
		want bool
	}{
		{"register transport error", urlErr("/client/register"), false},
		{"metrics transport error", urlErr("/client/metrics"), false},
		{"feature fetch transport error", urlErr("/client/features"), true},
		{"decode error", errors.New("unexpected EOF"), true},
		{"evaluation error", errors.New("segment does not exist"), true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			l := newSDKListener(nil)
			l.OnError(tt.err)
			select {
			case got := <-l.failed:
				assert.True(t, tt.want, "passed on")
				assert.Equal(t, tt.err, got)
			default:
				assert.False(t, tt.want, "not passed on")
			}
		})
	}
}

// unresponsiveServer returns the URL of a loopback server that accepts
// connections and never answers.
func unresponsiveServer(t *testing.T) string {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	var mu sync.Mutex
	var conns []net.Conn
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, c := range conns {
			_ = c.Close()
		}
	})
	return "http://" + ln.Addr().String() + "/api"
}

func TestOnErrorPassesOnOnlyEvaluationErrorsAfterReady(t *testing.T) {
	// After the one feature fetch, a *url.Error can only be from a register or
	// metrics request, even when a redirect makes it name another path.
	urlErr := func(path string) error {
		return &url.Error{Op: "Post", URL: "http://unleash.example.com" + path, Err: errors.New("connection reset")}
	}
	for _, tt := range []struct {
		name        string
		err         error
		wantFailed  bool
		wantProblem bool
	}{
		{"metrics error naming a shared redirect hop", urlErr("/forward"), false, true},
		{"error naming the features path", urlErr("/api/client/features"), false, true},
		{"evaluation error", errors.New("segment does not exist"), true, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			l := newSDKListener(nil)
			l.OnReady()
			l.OnError(tt.err)
			assert.Equal(t, tt.wantFailed, len(l.failed) == 1, "passed on as an evaluation error")
			assert.Equal(t, tt.wantProblem, len(l.metricsProblem) == 1, "signalled as a metrics problem")
		})
	}
}

func TestCheckFetchesTheFlagsOnlyOnce(t *testing.T) {
	// A later refresh that failed while the check waits for metrics would
	// otherwise look like an evaluation error.
	var mu sync.Mutex
	var intervals []string
	url := fakeUnleash(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		intervals = append(intervals, r.Header.Get("Unleash-Interval"))
		mu.Unlock()
		serve(http.StatusOK, featuresJSON(enabledFlag))(w, r)
	}, nil)

	stdout, _, _ := runCheck(t, url, "on", false)

	assert.Equal(t, "true\n", stdout)
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"86400000"}, intervals, "the refresh interval the SDK announces, in ms")
}

func TestCheckIgnoresARegistrationThatFailsOnAHopTheFetchShares(t *testing.T) {
	// A proxy sends every Client API request through /forward. The fetch is
	// served there; the register request has its connection closed there, so
	// its error names /forward, a path the fetch also went through.
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/forward" && r.Method == http.MethodGet:
			serve(http.StatusOK, featuresJSON(enabledFlag))(w, r)
		case r.URL.Path == "/forward":
			_, _ = io.Copy(io.Discard, r.Body)
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close()
			}
		case r.URL.Path == "/api/client/features" || r.URL.Path == "/api/client/register":
			_, _ = io.Copy(io.Discard, r.Body)
			http.Redirect(w, r, srv.URL+"/forward", http.StatusTemporaryRedirect)
		default:
			w.WriteHeader(http.StatusAccepted)
		}
	}))
	t.Cleanup(srv.Close)

	for range 10 {
		stdout, stderr, code := runCheck(t, srv.URL+"/api", "on", true)

		assert.Equal(t, ExitOK, code)
		assert.Equal(t, "true\n", stdout)
		assert.NotContains(t, stderr, "returning false")
	}
}

func TestCheckFallsBackAtOnceWhenTheFetchRedirectsInALoop(t *testing.T) {
	// http.Client gives up after ten redirects with an error that names a hop
	// only the feature fetch took, with no transport error behind it.
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/client/features", "/b":
			http.Redirect(w, r, srv.URL+"/a", http.StatusTemporaryRedirect)
		case "/a":
			http.Redirect(w, r, srv.URL+"/b", http.StatusTemporaryRedirect)
		default:
			w.WriteHeader(http.StatusAccepted)
		}
	}))
	t.Cleanup(srv.Close)

	start := time.Now()
	stdout, stderr, code := runCheck(t, srv.URL+"/api", "on", true, set(OptionTimeout, timeout, "2s"))
	elapsed := time.Since(start)

	assert.Equal(t, ExitOK, code)
	assert.Equal(t, "false\n", stdout)
	assert.Equal(t, "releash: unleash answered the feature fetch with HTTP 307; returning false\n", stderr)
	assert.Less(t, elapsed, time.Second, "falls back without waiting for --timeout")
}
