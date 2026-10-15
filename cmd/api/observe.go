package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"imagelab/internal/data"
)

const (
	// maxHoldingDeadline bounds how long a long-poll request may be held,
	// matching the largest value the wait parameter accepts (API-02).
	maxHoldingDeadline = 20 * time.Second
	// defaultHoldingDeadline is used when after is supplied without wait.
	defaultHoldingDeadline = 20 * time.Second
)

// errVersionAhead reports a client cursor positioned beyond the job's current
// version. It maps to 400; the handler must not wait for an impossible
// position (API-02).
var errVersionAhead = errors.New("after is ahead of the current version")

// observationRequest describes a parsed GET /v1/jobs/{id} request. When
// waitMode is false the caller wants a prompt snapshot (API-01).
type observationRequest struct {
	waitMode bool
	after    int
	wait     time.Duration
}

// parseObservationRequest validates the optional after and wait query
// parameters. Omitting both selects the prompt snapshot; after alone holds
// for the default deadline; wait without after, malformed integers, a
// negative after, and an out-of-range wait are all rejected with 400.
func parseObservationRequest(r *http.Request) (observationRequest, error) {
	query := r.URL.Query()
	afterRaw := query.Get("after")
	waitRaw := query.Get("wait")

	if afterRaw == "" {
		if waitRaw != "" {
			return observationRequest{}, errors.New("wait requires after")
		}
		return observationRequest{waitMode: false}, nil
	}

	after, err := strconv.Atoi(afterRaw)
	if err != nil || after < 0 {
		return observationRequest{}, errors.New("after must be a nonnegative integer")
	}

	req := observationRequest{
		waitMode: true,
		after:    after,
		wait:     defaultHoldingDeadline,
	}

	if waitRaw != "" {
		seconds, err := strconv.Atoi(waitRaw)
		if err != nil {
			return observationRequest{}, errors.New("wait must be an integer number of seconds")
		}
		if seconds < 1 || seconds > int(maxHoldingDeadline/time.Second) {
			return observationRequest{}, fmt.Errorf("wait must be between 1 and %d seconds", int(maxHoldingDeadline/time.Second))
		}
		req.wait = time.Duration(seconds) * time.Second
	}

	return req, nil
}

// isTerminal reports whether a job has reached a final state, after which no
// worker progress updates are allowed.
func isTerminal(status string) bool {
	return status == "completed" || status == "failed"
}

// waitForJobChange blocks until the job's persisted version is newer than
// after, the job becomes terminal, the holding deadline elapses, or the
// request context is cancelled. It always returns the authoritative
// representation it last read. changed is false only for a genuine holding
// deadline with no newer version, which the handler turns into 204.
func (app *application) waitForJobChange(ctx context.Context, publicID string, after int, hold time.Duration) (*data.Job, bool, error) {
	deadline := time.NewTimer(hold)
	defer deadline.Stop()

	for {
		// Subscribe before reading the version. Any change committed after
		// this read closes sig.ch, so a change cannot be missed between the
		// check and the wait (WAIT-01). Re-subscribing on each wake keeps the
		// wait event-driven rather than a polling loop (WAIT-02).
		sig := app.broadcaster.subscribe(publicID)

		job, err := app.models.Jobs.GetByPublicID(publicID)
		if err != nil {
			app.broadcaster.release(publicID, sig)
			return nil, false, err
		}
		if after > job.Version {
			app.broadcaster.release(publicID, sig)
			return job, false, errVersionAhead
		}
		if job.Version > after || isTerminal(job.Status) {
			app.broadcaster.release(publicID, sig)
			return job, true, nil
		}

		select {
		case <-sig.ch:
			// A committed change woke us: re-read with a fresh subscription.
			app.broadcaster.release(publicID, sig)
			continue
		case <-deadline.C:
			app.broadcaster.release(publicID, sig)
			// Recheck authoritative state before choosing 204 so a change
			// that arrived right at the deadline is still reported.
			latest, err := app.models.Jobs.GetByPublicID(publicID)
			if err != nil {
				return nil, false, err
			}
			if latest.Version > after || isTerminal(latest.Status) {
				return latest, true, nil
			}
			return latest, false, nil
		case <-ctx.Done():
			app.broadcaster.release(publicID, sig)
			return nil, false, ctx.Err()
		}
	}
}

// observeJobHandler serves the long-poll branch of GET /v1/jobs/{id}. It
// returns promptly when the job is already newer or terminal, otherwise holds
// the request until a committed change, the holding deadline (204), or
// cancellation (API-02).
func (app *application) observeJobHandler(w http.ResponseWriter, r *http.Request, req observationRequest) {
	publicID := r.PathValue("id")
	started := time.Now()

	job, changed, err := app.waitForJobChange(r.Context(), publicID, req.after, req.wait)
	if err != nil {
		switch {
		case errors.Is(err, errVersionAhead):
			app.logLongPoll(publicID, "version_ahead", started)
			app.badRequestResponse(w, r, fmt.Errorf("%s (current version %d)", err, job.Version))
		case errors.Is(err, data.ErrRecordNotFound):
			app.logLongPoll(publicID, "not_found", started)
			app.notFoundResponse(w, r)
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			// The client went away or the server is shutting down. There is
			// no one to answer; the worker's job is unaffected (WAIT-02).
			app.logLongPoll(publicID, "cancelled", started)
			return
		default:
			app.logLongPoll(publicID, "error", started)
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	if !changed {
		app.logLongPoll(publicID, "holding_deadline", started)
		app.noContentResponse(w)
		return
	}

	app.logLongPoll(publicID, "changed", started)
	if err := app.writeJobSnapshot(w, r, job); err != nil {
		app.serverErrorResponse(w, r, err)
	}
}

// logLongPoll records how a long-poll request ended together with its wait
// time, which is useful for the observation experiment and for diagnosing
// whether a client or the server ended the hold.
func (app *application) logLongPoll(publicID, reason string, started time.Time) {
	app.logger.Info("long poll ended",
		"job_id", publicID,
		"end_reason", reason,
		"wait_ms", time.Since(started).Milliseconds())
}
