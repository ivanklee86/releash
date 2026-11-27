package releash

import (
	"context"
	"fmt"
	"time"

	unleash "github.com/Unleash/unleash-go-sdk/v6"
)

// metricsInterval is how often the SDK sends buffered usage counts. A check
// evaluates one flag once, so it bounds how long the check waits for the send
// to start. Ticks with nothing counted send nothing.
const metricsInterval = 100 * time.Millisecond

// awaitMetrics waits until Unleash has accepted the usage metrics of an
// evaluated flag, the metrics request fails, the SDK reports that it could not
// evaluate the flag, or runCtx ends. It returns "" for an accepted send and
// otherwise the --loud note, without the "releash: " prefix. If the SDK
// reports an evaluation error first, it returns that error and no note: the
// answer then falls back, and a fallback does not wait for metrics. events and
// log must belong to the client that evaluated the flag.
func awaitMetrics(runCtx context.Context, events *sdkListener, log *requestLog, timeout time.Duration) (string, error) {
	for {
		select {
		case err := <-events.failed:
			return "", err
		case <-events.sent:
			return "", nil
		case <-events.metricsProblem:
			// The signal may be about another request, such as a warning
			// about the register request; the log says whether it was ours.
			if seen := log.outcome(metricsPath); seen.err != nil || (seen.status != 0 && !sdkAccepted(seen.status)) {
				return metricsFailure(seen, timeout), nil
			}
		case <-runCtx.Done():
			select {
			case <-events.sent:
				return "", nil
			default:
			}
			seen := log.outcome(metricsPath)
			// Unleash accepted the counts, but the deadline came before the
			// SDK passed that on.
			if sdkAccepted(seen.status) {
				return "", nil
			}
			return metricsFailure(seen, timeout), nil
		}
	}
}

// metricsFailure returns the --loud note for a metrics send that ended with
// seen, or that had not finished, in a check bounded by timeout.
func metricsFailure(seen requestOutcome, timeout time.Duration) string {
	switch {
	case seen.err == nil && seen.status == 0, isTimeout(seen.err):
		return fmt.Sprintf("sending usage metrics to unleash did not finish within %s; the answer is unaffected", timeout)
	case seen.err != nil:
		return fmt.Sprintf("sending usage metrics to unleash failed: %v; the answer is unaffected", seen.err)
	default:
		return fmt.Sprintf("sending usage metrics to unleash failed: HTTP %d; the answer is unaffected", seen.status)
	}
}

// signalMetricsProblem wakes awaitMetrics without blocking the SDK's goroutine.
func (l *sdkListener) signalMetricsProblem() {
	select {
	case l.metricsProblem <- struct{}{}:
	default:
	}
}

// OnCount is called for every evaluation the SDK counts.
func (l *sdkListener) OnCount(string, bool) {}

// OnSent is called after Unleash answered a metrics send with a 2xx or 300
// status, which the SDK counts as accepted.
func (l *sdkListener) OnSent(unleash.MetricsData) { l.sentOnce.Do(func() { close(l.sent) }) }

// OnRegistered is called after the register request was answered, whatever
// the status.
func (l *sdkListener) OnRegistered(unleash.ClientData) {}
