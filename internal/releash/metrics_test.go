package releash

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Cases for usage metrics, from the test plan in spec 0001:
//   - a check sends the flag's usage counts before it returns, under the
//     appName releash registers with, and says nothing about it with --loud
//   - a metrics endpoint that answers with an error status, drops the
//     connection or never answers changes neither stdout nor the exit code,
//     is silent without --loud, has its own --loud line, and does not hold
//     the check past --timeout
//   - a fallback does not wait for metrics
//   - a send answered with 300 counts as accepted, as the SDK counts it

// metricsPost is one request the metrics fake received.
type metricsPost struct {
	appName string
	toggles map[string]struct{ Yes, No int }
}

// metricsRecorder records the metrics requests a fake Unleash received. It is
// safe for concurrent use.
type metricsRecorder struct {
	mu    sync.Mutex
	posts []metricsPost
}

func (m *metricsRecorder) received() []metricsPost {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]metricsPost(nil), m.posts...)
}

// fakeWithMetrics starts a server that serves features, answers the register
// request with 202 and passes /api/client/metrics to metrics after recording
// it in rec. It returns the API URL.
func fakeWithMetrics(t *testing.T, features string, rec *metricsRecorder, metrics http.HandlerFunc) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/client/features":
			serve(http.StatusOK, features)(w, r)
		case "/api/client/metrics":
			var body struct {
				AppName string `json:"appName"`
				Bucket  struct {
					Toggles map[string]struct{ Yes, No int } `json:"toggles"`
				} `json:"bucket"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decoding metrics request: %v", err)
			}
			rec.mu.Lock()
			rec.posts = append(rec.posts, metricsPost{appName: body.AppName, toggles: body.Bucket.Toggles})
			rec.mu.Unlock()
			metrics(w, r)
		default:
			w.WriteHeader(http.StatusAccepted)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/api"
}

func TestCheckSendsUsageMetricsBeforeReturning(t *testing.T) {
	for flag, want := range map[string]struct{ Yes, No int }{"on": {Yes: 1}, "off": {No: 1}} {
		t.Run(flag, func(t *testing.T) {
			rec := &metricsRecorder{}
			url := fakeWithMetrics(t, featuresJSON(enabledFlag, disabledFlag), rec, serve(http.StatusAccepted, ""))

			stdout, stderr, code := runCheck(t, url, flag, true, set(OptionAppName, appName, "pipeline"))

			assert.Equal(t, ExitOK, code)
			assert.Equal(t, fmt.Sprintln(flag == "on"), stdout)
			assert.Empty(t, stderr)
			posts := rec.received()
			require.Len(t, posts, 1, "metrics requests received by the time Check returned")
			assert.Equal(t, "pipeline", posts[0].appName)
			assert.Equal(t, map[string]struct{ Yes, No int }{flag: want}, posts[0].toggles)
		})
	}
}

// dropConnection closes the connection without answering.
func dropConnection(w http.ResponseWriter, _ *http.Request) {
	conn, _, err := w.(http.Hijacker).Hijack()
	if err != nil {
		panic(err)
	}
	_ = conn.Close()
}

func TestCheckIgnoresAFailedMetricsSend(t *testing.T) {
	tests := []struct {
		name    string
		metrics http.HandlerFunc
		timeout string
		// want is a regular expression for the --loud line.
		want string
		// minElapsed is how long the check must take at least.
		minElapsed time.Duration
	}{
		{name: "500", metrics: serve(http.StatusInternalServerError, ""), timeout: "2s", want: `^releash: sending usage metrics to unleash failed: HTTP 500; the answer is unaffected\n$`},
		{name: "401", metrics: serve(http.StatusUnauthorized, ""), timeout: "2s", want: `^releash: sending usage metrics to unleash failed: HTTP 401; the answer is unaffected\n$`},
		{name: "connection dropped", metrics: dropConnection, timeout: "2s", want: `^releash: sending usage metrics to unleash failed: .+; the answer is unaffected\n$`},
		{name: "never answers", metrics: hang, timeout: "1s", want: `^releash: sending usage metrics to unleash did not finish within 1s; the answer is unaffected\n$`, minElapsed: time.Second},
	}
	for _, tt := range tests {
		for _, loud := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/loud=%t", tt.name, loud), func(t *testing.T) {
				url := fakeWithMetrics(t, featuresJSON(enabledFlag), &metricsRecorder{}, tt.metrics)

				start := time.Now()
				stdout, stderr, code := runCheck(t, url, "on", loud, set(OptionTimeout, timeout, tt.timeout))
				elapsed := time.Since(start)

				assert.Equal(t, ExitOK, code)
				assert.Equal(t, "true\n", stdout)
				if loud {
					assert.Regexp(t, tt.want, stderr)
				} else {
					assert.Empty(t, stderr)
				}
				assert.GreaterOrEqual(t, elapsed, tt.minElapsed)
				limit := 500 * time.Millisecond
				if tt.minElapsed > 0 {
					limit = tt.minElapsed + 250*time.Millisecond
				}
				assert.Less(t, elapsed, limit, "the check ends soon after the send fails, and within --timeout")
			})
		}
	}
}

func TestCheckDoesNotWaitForMetricsAfterAFallback(t *testing.T) {
	// The SDK cannot evaluate a strategy that names a segment it was not sent.
	unevaluable := `{"name":"unevaluable","enabled":true,"strategies":[{"name":"default","segments":[99]}]}`
	tests := []struct {
		name    string
		flag    string
		metrics http.HandlerFunc
		want    string
	}{
		{name: "unknown flag, metrics never answer", flag: "missing", metrics: hang, want: "releash: flag \"missing\" not found; returning false\n"},
		{name: "unevaluable flag, metrics never answer", flag: "unevaluable", metrics: hang, want: "releash: unleash could not evaluate flag \"unevaluable\": segment does not exist; returning false\n"},
		{name: "unevaluable flag, metrics fail", flag: "unevaluable", metrics: serve(http.StatusInternalServerError, ""), want: "releash: unleash could not evaluate flag \"unevaluable\": segment does not exist; returning false\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := fakeWithMetrics(t, featuresJSON(enabledFlag, unevaluable), &metricsRecorder{}, tt.metrics)

			start := time.Now()
			stdout, stderr, code := runCheck(t, url, tt.flag, true, set(OptionTimeout, timeout, "2s"))

			assert.Equal(t, ExitOK, code)
			assert.Equal(t, "false\n", stdout)
			assert.Equal(t, tt.want, stderr, "only the fallback line, with no metrics note")
			assert.Less(t, time.Since(start), 500*time.Millisecond)
		})
	}
}

func TestCheckCountsAMetricsSendAnsweredWith300AsAccepted(t *testing.T) {
	// The SDK accepts 300 as well as 2xx, so this is not a failed send.
	url := fakeWithMetrics(t, featuresJSON(enabledFlag), &metricsRecorder{}, serve(http.StatusMultipleChoices, ""))

	stdout, stderr, code := runCheck(t, url, "on", true)

	assert.Equal(t, ExitOK, code)
	assert.Equal(t, "true\n", stdout)
	assert.Empty(t, stderr)
}
