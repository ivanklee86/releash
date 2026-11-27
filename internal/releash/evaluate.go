package releash

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	unleash "github.com/Unleash/unleash-go-sdk/v6"
	"github.com/Unleash/unleash-go-sdk/v6/api"
	unleashcontext "github.com/Unleash/unleash-go-sdk/v6/context"
)

// Client API paths, relative to the Unleash API URL, that evaluate tells
// apart. The SDK reports errors from all of them on one channel.
const (
	featuresPath = "/client/features"
	registerPath = "/client/register"
	metricsPath  = "/client/metrics"
)

// outcome is the answer evaluate gives for one check.
type outcome struct {
	// enabled is the answer. It is false whenever fallback is set.
	enabled bool
	// fallback says why the answer fell back to false instead of being
	// evaluated, as a --loud line without the "releash: " prefix. It is empty
	// for an evaluated answer.
	fallback string
	// notes are --loud lines, without the "releash: " prefix, about problems
	// that did not change the answer, such as a rejected registration.
	notes []string
}

// evaluator answers a validated check. Releash uses evaluate unless a test
// replaces it.
type evaluator func(ctx context.Context, req checkRequest) outcome

// evaluate fetches the flags req's token can see from Unleash, once, and
// evaluates req.flag against req.context with the Unleash Go SDK.
//
// The whole call, including registering with Unleash and closing the SDK
// client, finishes within req.timeout. The answer is false, with fallback
// set, when the first feature fetch fails or does not finish in time, or when
// the flag is not in the fetched flags. Fetched flags are kept in memory
// only, so evaluate never reads or writes files, and nothing carries over
// from one call to the next.
func evaluate(ctx context.Context, req checkRequest) outcome {
	runCtx, cancel := context.WithTimeout(ctx, req.timeout)
	defer cancel()

	base := http.DefaultTransport.(*http.Transport).Clone()
	defer base.CloseIdleConnections()
	log := &requestLog{}
	httpClient := &http.Client{
		Timeout:   req.timeout,
		Transport: &boundedTransport{base: base, run: runCtx, log: log},
	}
	events := newSDKListener()

	// NewClient registers with Unleash before it returns, so it blocks until
	// that request ends, at the latest when runCtx does. The first feature
	// fetch runs alongside on the SDK's own goroutine.
	client, err := unleash.NewClient(
		unleash.WithUrl(req.url.String()),
		unleash.WithAppName(req.context.appName),
		unleash.WithEnvironment(req.context.environment),
		unleash.WithCustomHeaders(http.Header{"Authorization": {req.token}}),
		unleash.WithHttpClient(httpClient),
		unleash.WithStorage(memoryStorage{}),
		unleash.WithListener(events),
	)
	if err != nil {
		return outcome{fallback: fmt.Sprintf("cannot start the Unleash client: %v; returning false", err)}
	}

	var out outcome
	fetched := false
	select {
	case <-events.ready:
		fetched = true
		out = evaluateFlag(client, req)
	case err := <-events.fetchFailed:
		out = outcome{fallback: fetchFailure(runCtx, req.timeout, log.outcome(featuresPath), err)}
	case <-runCtx.Done():
		out = outcome{fallback: timedOut(req.timeout)}
	}
	if out.fallback != "" {
		// No flag gave the answer, so there is no usage worth waiting to report.
		cancel()
	}
	// Close stops the SDK's goroutines, flushes metrics within what is left of
	// runCtx, and returns once every listener callback has run.
	_ = client.Close()

	// When the fetch failed, the register request usually failed for the same
	// reason, and the fallback line already gives it.
	if fetched {
		if note := registerFailure(log.outcome(registerPath)); note != "" {
			out.notes = append(out.notes, note)
		}
	}
	return out
}

// evaluateFlag evaluates req.flag with a client whose first fetch succeeded.
func evaluateFlag(client *unleash.Client, req checkRequest) outcome {
	found := true
	enabled := client.IsEnabled(req.flag, unleash.FeatureOptions{
		Ctx: sdkContext(req.context),
		// The SDK calls FallbackFunc only when the fetched flags do not
		// include the flag, and otherwise reports nothing.
		FallbackFunc: func(string, *unleashcontext.Context) bool {
			found = false
			return false
		},
	})
	if !found {
		return outcome{fallback: fmt.Sprintf("flag %q not found; returning false", req.flag)}
	}
	return outcome{enabled: enabled}
}

// sdkContext converts c to the SDK's context. An unset currentTime stays
// empty, so the SDK compares date constraints against the time of
// evaluation.
func sdkContext(c evalContext) unleashcontext.Context {
	sc := unleashcontext.Context{
		UserId:        c.userID,
		SessionId:     c.sessionID,
		RemoteAddress: c.remoteAddress,
		Environment:   c.environment,
		AppName:       c.appName,
		Properties:    c.properties,
	}
	if c.currentTime != nil {
		sc.CurrentTime = c.currentTime.Format(time.RFC3339Nano)
	}
	return sc
}

// fetchFailure returns the fallback line for a first feature fetch that
// failed with err, given what the transport saw of that request.
func fetchFailure(runCtx context.Context, timeout time.Duration, seen requestOutcome, err error) string {
	switch {
	case runCtx.Err() != nil || isTimeout(err) || isTimeout(seen.err):
		return timedOut(timeout)
	case seen.err != nil:
		return fmt.Sprintf("unleash unreachable: %v; returning false", seen.err)
	case seen.status == http.StatusUnauthorized || seen.status == http.StatusForbidden:
		return fmt.Sprintf("unleash rejected the API token (HTTP %d); returning false", seen.status)
	case seen.status != 0 && (seen.status < 200 || seen.status > 299):
		return fmt.Sprintf("unleash answered the feature fetch with HTTP %d; returning false", seen.status)
	case seen.status != 0:
		return fmt.Sprintf("unleash sent a malformed feature response: %v; returning false", err)
	default:
		// The request never reached the transport, e.g. it could not be built.
		return fmt.Sprintf("unleash unreachable: %v; returning false", err)
	}
}

func timedOut(timeout time.Duration) string {
	return fmt.Sprintf("unleash did not answer within %s; returning false", timeout)
}

// registerFailure returns the --loud note for a failed register request, or
// "" if it succeeded or was never sent.
func registerFailure(seen requestOutcome) string {
	switch {
	case seen.err != nil:
		return fmt.Sprintf("registering with unleash failed: %v; the answer is unaffected", seen.err)
	case seen.status != 0 && (seen.status < 200 || seen.status > 299):
		return fmt.Sprintf("registering with unleash failed: HTTP %d; the answer is unaffected", seen.status)
	default:
		return ""
	}
}

// isTimeout reports whether err is a timeout or a cancelled request.
func isTimeout(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

// memoryStorage is an unleash.Storage that keeps nothing. Without it the SDK
// writes the fetched flags to a backup file in the temp directory and serves
// them when Unleash cannot be reached, so a stale file on a shared runner
// could turn a gate on.
type memoryStorage struct{}

func (memoryStorage) Init(string, string) {}

// Load returns no state, which the SDK treats as "start empty".
func (memoryStorage) Load() (*api.FeatureResponse, error) { return nil, nil }

func (memoryStorage) Persist(*api.FeatureResponse) error { return nil }

// sdkListener turns the SDK's callbacks into channels that evaluate can wait
// on. The SDK runs every callback on its one sync goroutine, so none of them
// may block.
type sdkListener struct {
	readyOnce sync.Once
	ready     chan struct{}
	// fetchFailed receives the first error from the feature fetch.
	fetchFailed chan error
}

func newSDKListener() *sdkListener {
	return &sdkListener{ready: make(chan struct{}), fetchFailed: make(chan error, 1)}
}

// OnReady is called after the first successful feature fetch.
func (l *sdkListener) OnReady() { l.readyOnce.Do(func() { close(l.ready) }) }

// OnUpdate is called after later fetches that changed the flags.
func (l *sdkListener) OnUpdate() {}

// OnError receives errors from every SDK request and from evaluation. Only
// errors from the feature fetch are passed on: the SDK reports transport
// errors of register and metrics requests here too, as a *url.Error naming
// their path. Their HTTP status errors go to OnWarning.
func (l *sdkListener) OnError(err error) {
	if isRequestError(err, registerPath) || isRequestError(err, metricsPath) {
		return
	}
	select {
	case l.fetchFailed <- err:
	default:
	}
}

// OnWarning receives non-fatal problems, such as a register request that
// Unleash answered with an error status. evaluate learns of those from its
// transport instead.
func (l *sdkListener) OnWarning(error) {}

// isRequestError reports whether err is a *url.Error for a request to a URL
// whose path ends in path.
func isRequestError(err error, path string) bool {
	var ue *url.Error
	if !errors.As(err, &ue) {
		return false
	}
	u, perr := url.Parse(ue.URL)
	return perr == nil && strings.HasSuffix(u.Path, path)
}

// requestOutcome is what the transport saw of one request: an HTTP status, or
// the error that kept it from getting one. The zero value means the request
// was not sent.
type requestOutcome struct {
	status int
	err    error
}

// requestLog keeps the outcome of the first request to featuresPath and to
// registerPath. It is safe for concurrent use.
type requestLog struct {
	mu    sync.Mutex
	first map[string]requestOutcome
}

func (l *requestLog) record(path string, o requestOutcome) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, p := range []string{featuresPath, registerPath} {
		if _, seen := l.first[p]; strings.HasSuffix(path, p) && !seen {
			if l.first == nil {
				l.first = make(map[string]requestOutcome)
			}
			l.first[p] = o
		}
	}
}

// outcome returns the outcome of the first request to path, which is
// featuresPath or registerPath, or the zero value if there was none.
func (l *requestLog) outcome(path string) requestOutcome {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.first[path]
}

// boundedTransport sends requests through base, cancels each one when run
// ends, and records outcomes in log. http.Client.Timeout bounds each request
// on its own, so without run the register request and the metrics flush in
// Close could each take a full timeout.
type boundedTransport struct {
	base http.RoundTripper
	run  context.Context
	log  *requestLog
}

func (t *boundedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx, cancel := context.WithCancel(req.Context())
	stop := context.AfterFunc(t.run, cancel)
	release := func() {
		stop()
		cancel()
	}

	resp, err := t.base.RoundTrip(req.WithContext(ctx))
	if err != nil {
		release()
		t.log.record(req.URL.Path, requestOutcome{err: err})
		return nil, err
	}
	t.log.record(req.URL.Path, requestOutcome{status: resp.StatusCode})
	// The request context must outlive RoundTrip until the body is read.
	resp.Body = &releasingBody{ReadCloser: resp.Body, release: release}
	return resp, nil
}

// releasingBody calls release once the body is closed.
type releasingBody struct {
	io.ReadCloser
	once    sync.Once
	release func()
}

func (b *releasingBody) Close() error {
	err := b.ReadCloser.Close()
	b.once.Do(b.release)
	return err
}
