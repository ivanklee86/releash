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

// refreshInterval is far longer than any check, so the SDK fetches the flags
// once: a failed later refresh would reach OnError while the check waits for
// metrics and look like an evaluation error.
const refreshInterval = 24 * time.Hour

// outcome is the answer evaluate gives for one check or variant.
type outcome struct {
	// enabled is the answer to a check. It is false whenever fallback is set.
	enabled bool
	// output is the rendered variables of an evaluated variant. Only the
	// variant command's question sets it, and only when fallback is empty and
	// refused is false.
	output string
	// refused reports that the variant command found the flag in the fetched
	// flags and disabled for the context, which exits ExitDisabled. It is
	// false whenever fallback is set.
	refused bool
	// fallback says why the answer fell back to false, or to the disabled
	// variant, instead of being evaluated, as a --loud line without the
	// "releash: " prefix and without the "; returning ..." ending that the
	// command adds. It is empty for an evaluated answer.
	fallback string
	// notes are --loud lines, without the "releash: " prefix, about problems
	// that did not change the answer, such as a rejected registration.
	notes []string
}

// evaluator answers a validated check or variant. Releash uses evaluate
// unless a test replaces it.
type evaluator func(ctx context.Context, req checkRequest) outcome

// question is the final call of an evaluation: it evaluates req.flag with a
// client whose first fetch succeeded. It is the only part of evaluate that
// differs between check and variant.
type question func(client *unleash.Client, req checkRequest) outcome

// evaluate fetches the flags req's token can see from Unleash, once, and
// evaluates req.flag against req.context with the Unleash Go SDK by asking
// req.ask, or evaluateFlag when req.ask is nil.
//
// The whole call, including registering with Unleash and closing the SDK
// client, finishes within req.timeout. The register request may take only
// registerTimeout(req.timeout), so a slow register cannot use up the time
// the feature fetch needs. The answer falls back, with fallback set, when the
// first feature fetch fails, is not a 2xx response, or does not finish in
// time, when the question falls back, such as for a flag that is not in the
// fetched flags, or when the SDK cannot evaluate the flag. An evaluation
// error takes precedence over a refused outcome. When req.context has no
// remoteAddress, the flag is evaluated with the local IP address of the
// connection that fetched the flags, or, when there is none, without one and
// with a note saying so. Fetched flags are kept in memory only, so evaluate
// never reads or writes files, and nothing carries over from one call to the
// next.
//
// After an evaluated answer that is not refused, evaluate waits within
// req.timeout for Unleash to accept the usage metrics. A failed or unfinished
// send adds a note and never changes the answer.
func evaluate(ctx context.Context, req checkRequest) outcome {
	ask := req.ask
	if ask == nil {
		ask = evaluateFlag
	}
	runCtx, cancel := context.WithTimeout(ctx, req.timeout)
	defer cancel()

	base := http.DefaultTransport.(*http.Transport).Clone()
	defer base.CloseIdleConnections()
	log := &requestLog{}
	local := &localAddress{}
	httpClient := &http.Client{
		Timeout: req.timeout,
		Transport: &boundedTransport{
			base:            &localAddressTransport{base: base, addr: local},
			run:             runCtx,
			registerTimeout: registerTimeout(req.timeout),
			log:             log,
		},
	}
	events := newSDKListener(log)

	// NewClient registers with Unleash before it returns, and the SDK calls
	// the listener only after that, so the register request holds back the
	// answer until it ends. The first feature fetch runs alongside on the
	// SDK's own goroutine.
	client, err := unleash.NewClient(
		unleash.WithUrl(req.url.String()),
		unleash.WithAppName(req.context.appName),
		unleash.WithEnvironment(req.context.environment),
		unleash.WithCustomHeaders(http.Header{"Authorization": {req.token}}),
		unleash.WithHttpClient(httpClient),
		unleash.WithStorage(memoryStorage{}),
		unleash.WithListener(events),
		unleash.WithMetricsInterval(metricsInterval),
		unleash.WithRefreshInterval(refreshInterval),
	)
	if err != nil {
		return outcome{fallback: fmt.Sprintf("cannot start the Unleash client: %v", err)}
	}

	var out outcome
	var metricsNote, addressNote string
	fetched := false
	select {
	case <-events.ready:
		// The SDK also reports ready after a 304, which leaves it with no flags.
		if seen := log.outcome(featuresPath); seen.status != 0 && !isSuccess(seen.status) {
			out = outcome{fallback: fetchFailure(runCtx, req.timeout, seen, nil)}
			break
		}
		fetched = true
		req.context, addressNote = withRemoteAddress(req.context, local.address())
		out = ask(client, req)
		// A fallback has no usage worth waiting to report, and a refused
		// variant fails the pipeline step anyway.
		if out.fallback == "" && !out.refused {
			var err error
			if metricsNote, err = awaitMetrics(runCtx, events, log, req.timeout); err != nil {
				out = evaluationFailed(req.flag, err)
			}
		}
	case err := <-events.failed:
		out = outcome{fallback: fetchFailure(runCtx, req.timeout, log.outcome(featuresPath), err)}
	case <-runCtx.Done():
		out = outcome{fallback: timedOut(req.timeout)}
	}
	// Close flushes buffered metrics: nothing after a confirmed send, but a
	// retry after a failed one, which could take the rest of the timeout.
	// Cancelling first makes that flush fail at once. Close returns once
	// every listener callback has run.
	cancel()
	_ = client.Close()

	// A flag the SDK could not evaluate also looks disabled, so a refused
	// outcome holds only once this has found no error.
	if fetched && out.fallback == "" {
		if err := evaluationError(client, events); err != nil {
			out = evaluationFailed(req.flag, err)
		}
	}

	// The address plays no part in a fallback, such as an unknown flag or one
	// the SDK could not evaluate.
	if addressNote != "" && out.fallback == "" {
		out.notes = append(out.notes, addressNote)
	}

	// When the fetch failed, the register request usually failed for the same
	// reason, and the fallback line already gives it.
	if fetched {
		if note := registerFailure(log.outcome(registerPath), registerTimeout(req.timeout)); note != "" {
			out.notes = append(out.notes, note)
		}
	}
	// The SDK can report an evaluation error after the metrics wait ended,
	// and a note that the answer is unaffected would then be wrong.
	if metricsNote != "" && out.fallback == "" {
		out.notes = append(out.notes, metricsNote)
	}
	return out
}

// evaluationFailed returns the fallback for flag after the SDK reported err
// while evaluating it. Notes the question made about its answer no longer
// apply, so it has none.
func evaluationFailed(flag string, err error) outcome {
	return outcome{fallback: fmt.Sprintf("unleash could not evaluate flag %q: %v", flag, err)}
}

// evaluateFlag is the check command's question: whether req.flag is enabled.
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
		return notFound(req.flag)
	}
	return outcome{enabled: enabled}
}

// notFound is the fallback for a flag that is not in the fetched flags.
func notFound(flag string) outcome {
	return outcome{fallback: fmt.Sprintf("flag %q not found", flag)}
}

// evaluationError returns the error the SDK reported while client evaluated
// a flag, or nil. client must be closed: the SDK has then either passed the
// error to events or left it in client.Errors().
func evaluationError(client *unleash.Client, events *sdkListener) error {
	select {
	case err := <-events.failed:
		return err
	default:
	}
	for {
		select {
		case err := <-client.Errors():
			if isEvaluationError(err) {
				return err
			}
		default:
			return nil
		}
	}
}

// registerTimeout is how long the register request may take in a check
// bounded by timeout: a quarter of it, and at most one second.
func registerTimeout(timeout time.Duration) time.Duration {
	return min(timeout/4, time.Second)
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
		return fmt.Sprintf("unleash unreachable: %v", seen.err)
	case seen.status == http.StatusUnauthorized || seen.status == http.StatusForbidden:
		return fmt.Sprintf("unleash rejected the API token (HTTP %d)", seen.status)
	case seen.status != 0 && !isSuccess(seen.status):
		return fmt.Sprintf("unleash answered the feature fetch with HTTP %d", seen.status)
	case seen.status != 0:
		return fmt.Sprintf("unleash sent a malformed feature response: %v", err)
	default:
		// The request never reached the transport, e.g. it could not be built.
		return fmt.Sprintf("unleash unreachable: %v", err)
	}
}

func timedOut(timeout time.Duration) string {
	return fmt.Sprintf("unleash did not answer within %s", timeout)
}

// registerFailure returns the --loud note for a failed register request that
// was allowed to take limit, or "" if it succeeded or was never sent.
func registerFailure(seen requestOutcome, limit time.Duration) string {
	switch {
	case isTimeout(seen.err):
		return fmt.Sprintf("registering with unleash did not finish within %s; the answer is unaffected", limit)
	case seen.err != nil:
		return fmt.Sprintf("registering with unleash failed: %v; the answer is unaffected", seen.err)
	case seen.status != 0 && !sdkAccepted(seen.status):
		return fmt.Sprintf("registering with unleash failed: HTTP %d; the answer is unaffected", seen.status)
	default:
		return ""
	}
}

func isSuccess(status int) bool { return status >= 200 && status <= 299 }

// sdkAccepted reports whether the SDK counts status as a successful register
// or metrics send: it accepts 300 as well as 2xx.
func sdkAccepted(status int) bool { return isSuccess(status) || status == http.StatusMultipleChoices }

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
	// failed receives the first error that is not from a register or metrics
	// request: before ready, a failed feature fetch; after it, a failed
	// evaluation, which is never a *url.Error. The SDK fetches the flags only
	// once (see refreshInterval), so no later fetch errors arrive.
	failed chan error
	// log tells register and metrics errors apart after redirects. It may be
	// nil, and then only the path in the error is used.
	log *requestLog

	sentOnce sync.Once
	// sent is closed once Unleash has accepted a metrics send.
	sent chan struct{}
	// metricsProblem is signalled when a metrics send may have failed. It
	// carries no detail: the request log has the outcome.
	metricsProblem chan struct{}
}

func newSDKListener(log *requestLog) *sdkListener {
	return &sdkListener{
		ready:          make(chan struct{}),
		failed:         make(chan error, 1),
		log:            log,
		sent:           make(chan struct{}),
		metricsProblem: make(chan struct{}, 1),
	}
}

// OnReady is called after the first feature fetch that did not fail.
func (l *sdkListener) OnReady() { l.readyOnce.Do(func() { close(l.ready) }) }

func (l *sdkListener) isReady() bool {
	select {
	case <-l.ready:
		return true
	default:
		return false
	}
}

// isEvaluationError reports whether err, reported after the feature fetch,
// can be an evaluation error. Evaluation errors never carry a URL.
func isEvaluationError(err error) bool {
	var ue *url.Error
	return !errors.As(err, &ue)
}

// OnUpdate is called after later fetches that changed the flags.
func (l *sdkListener) OnUpdate() {}

// OnError receives errors from every SDK request and from evaluation. Only
// errors from the feature fetch and from evaluation are passed on: the SDK
// reports transport errors of register and metrics requests here too, as a
// *url.Error. Their HTTP status errors go to OnWarning. After ready, any
// *url.Error is from a register or metrics request, whatever its path, since
// the one feature fetch is done. Before ready, an error naming a redirect hop
// that both the fetch and a register or metrics request went through counts
// as theirs unless the transport saw the fetch itself fail.
func (l *sdkListener) OnError(err error) {
	if l.isReady() && !isEvaluationError(err) {
		l.signalMetricsProblem()
		return
	}
	switch l.log.backgroundRequest(err) {
	case metricsPath:
		l.signalMetricsProblem()
		return
	case registerPath:
		return
	}
	if l.log.notFromTheFetch(err) {
		// It may be a metrics error; awaitMetrics reads the outcome from the log.
		l.signalMetricsProblem()
		return
	}
	select {
	case l.failed <- err:
	default:
	}
}

// OnWarning receives non-fatal problems, such as a register request that
// Unleash answered with an error status. evaluate learns of those from its
// transport instead. Every warning signals metricsProblem, because the SDK
// gives no way to tell which request a warning is about without parsing its
// text.
func (l *sdkListener) OnWarning(error) { l.signalMetricsProblem() }

// backgroundRequest returns registerPath or metricsPath if err is a
// *url.Error from a register or metrics request, and "" otherwise. After a
// redirect, net/http may name another hop in the error, so a path that is
// neither counts when l saw it as a hop of a register or metrics request and
// never as a hop of a feature fetch. l may be nil.
func (l *requestLog) backgroundRequest(err error) string {
	var ue *url.Error
	if !errors.As(err, &ue) {
		return ""
	}
	u, perr := url.Parse(ue.URL)
	if perr != nil {
		return ""
	}
	for _, p := range []string{registerPath, metricsPath} {
		if strings.HasSuffix(u.Path, p) {
			return p
		}
	}
	if l == nil {
		return ""
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if kind := l.hops[u.Path]; kind != featuresPath {
		return kind
	}
	return ""
}

// notFromTheFetch reports whether err is a *url.Error naming a redirect hop
// that a register or metrics request went through, other than the features
// path, while the transport saw no error on the feature fetch. Such a hop
// path may be one the feature fetch shares. An error naming a hop only the
// fetch took, such as one http.Client raises itself after too many
// redirects, stays the fetch's. l may be nil.
func (l *requestLog) notFromTheFetch(err error) bool {
	var ue *url.Error
	if l == nil || !errors.As(err, &ue) {
		return false
	}
	u, perr := url.Parse(ue.URL)
	if perr != nil || strings.HasSuffix(u.Path, featuresPath) {
		return false
	}
	l.mu.Lock()
	background := l.backgroundHops[u.Path]
	l.mu.Unlock()
	return background && l.outcome(featuresPath).err == nil
}

// requestOutcome is what the transport saw of one request: an HTTP status, or
// the error that kept it from getting one. The zero value means the request
// was not sent.
type requestOutcome struct {
	status int
	err    error
}

// requestLog keeps, for featuresPath, registerPath and metricsPath, the
// outcome of the first request to that path. When http.Client follows
// redirects from that request, the outcome of the last hop replaces it,
// wherever the hop went. It is safe for concurrent use.
type requestLog struct {
	mu    sync.Mutex
	first map[string]loggedRequest
	// hops maps the URL path of every hop to the path of the request that
	// started its redirect chain: featuresPath, registerPath or metricsPath.
	// A path that a feature fetch went through stays featuresPath.
	hops map[string]string
	// backgroundHops holds every hop path of a register or metrics request,
	// including those a feature fetch also went through.
	backgroundHops map[string]bool
}

type loggedRequest struct {
	// origin is the request that started the redirect chain.
	origin  *http.Request
	outcome requestOutcome
}

// record stores o as the outcome of req, a request boundedTransport was given.
func (l *requestLog) record(req *http.Request, o requestOutcome) {
	origin := redirectOrigin(req)
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, p := range []string{featuresPath, registerPath, metricsPath} {
		if !strings.HasSuffix(origin.URL.Path, p) {
			continue
		}
		if l.hops == nil {
			l.hops = make(map[string]string)
		}
		if l.hops[req.URL.Path] != featuresPath {
			l.hops[req.URL.Path] = p
		}
		if p != featuresPath {
			if l.backgroundHops == nil {
				l.backgroundHops = make(map[string]bool)
			}
			l.backgroundHops[req.URL.Path] = true
		}
		if prev, seen := l.first[p]; seen && prev.origin != origin {
			continue
		}
		if l.first == nil {
			l.first = make(map[string]loggedRequest)
		}
		l.first[p] = loggedRequest{origin: origin, outcome: o}
	}
}

// redirectOrigin returns the request that started req's redirect chain, or
// req itself. http.Client sets Response on each redirect hop to the response
// that caused it, and boundedTransport points that response back at its hop.
func redirectOrigin(req *http.Request) *http.Request {
	origin := req
	for origin.Response != nil && origin.Response.Request != nil {
		origin = origin.Response.Request
	}
	return origin
}

// outcome returns the outcome of the first request to path, which is
// featuresPath, registerPath or metricsPath, or the zero value if there was
// none.
func (l *requestLog) outcome(path string) requestOutcome {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.first[path].outcome
}

// boundedTransport sends requests through base, cancels each one when run
// ends, cancels a register request, with every redirect hop it takes,
// registerTimeout after it started, and records outcomes in log.
// http.Client.Timeout bounds each request on its own, so without run the
// register request and the metrics flush in Close could each take a full
// timeout.
type boundedTransport struct {
	base            http.RoundTripper
	run             context.Context
	registerTimeout time.Duration
	log             *requestLog

	mu sync.Mutex
	// registerDeadlines holds the deadline of each register request, keyed
	// by the request that started its redirect chain.
	registerDeadlines map[*http.Request]time.Time
}

// registerDeadline returns the deadline for origin, a register request,
// starting its clock on the first call.
func (t *boundedTransport) registerDeadline(origin *http.Request) time.Time {
	t.mu.Lock()
	defer t.mu.Unlock()
	if d, ok := t.registerDeadlines[origin]; ok {
		return d
	}
	if t.registerDeadlines == nil {
		t.registerDeadlines = make(map[*http.Request]time.Time)
	}
	d := time.Now().Add(t.registerTimeout)
	t.registerDeadlines[origin] = d
	return d
}

// RoundTrip sends req through base within the limits described on
// boundedTransport and records its outcome in log.
func (t *boundedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var ctx context.Context
	var cancel context.CancelFunc
	if origin := redirectOrigin(req); strings.HasSuffix(origin.URL.Path, registerPath) {
		ctx, cancel = context.WithDeadline(req.Context(), t.registerDeadline(origin))
	} else {
		ctx, cancel = context.WithCancel(req.Context())
	}
	stop := context.AfterFunc(t.run, cancel)
	release := func() {
		stop()
		cancel()
	}

	resp, err := t.base.RoundTrip(req.WithContext(ctx))
	if err != nil {
		release()
		t.log.record(req, requestOutcome{err: err})
		return nil, err
	}
	// base points Request at the copy that carries ctx. requestLog follows
	// redirect chains through it, so it must be the request this hop was given.
	resp.Request = req
	t.log.record(req, requestOutcome{status: resp.StatusCode})
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
