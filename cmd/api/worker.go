package main

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"imagelab/internal/data"
	"imagelab/internal/imageproc"
)

// startImageWorker launches a single background goroutine that repeatedly
// polls for queued image jobs and processes them one at a time. It exits
// cleanly when ctx is cancelled (during graceful shutdown). There is exactly
// one worker in Version 1; later submissions queue behind earlier ones.
func (app *application) startImageWorker(ctx context.Context) {
	app.wg.Add(1)
	go func() {
		defer app.wg.Done()

		ticker := time.NewTicker(app.config.workerPollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				app.logger.Info("image worker stopped")
				return
			case <-ticker.C:
				// sql.ErrNoRows just means there is nothing queued right now;
				// context.Canceled is expected during shutdown. Everything
				// else is a genuine error worth logging.
				err := app.processNextJob(ctx)
				if err != nil && !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, context.Canceled) {
					app.logger.Error("image worker failed", "error", err)
				}
			}
		}
	}()
}

// processNextJob claims one queued job, reads the original, generates the
// variant set, stores every output, and marks the job completed or failed.
// All expensive image work lives here, never in the upload handler.
func (app *application) processNextJob(ctx context.Context) error {
	// Atomically claim the oldest queued job and mark it processing. The
	// commit also sets stage 'starting', so the first snapshot after the
	// claim already carries progress.
	job, err := app.models.Jobs.ClaimNext(ctx)
	if err != nil {
		return err
	}
	// queued -> processing is an observable change already committed by
	// ClaimNext, so announce it to any long-poll observers.
	app.recordChange(job.PublicID, "processing", job.Version)
	app.logger.Info("image job started", "job_id", job.PublicID,
		"image_id", job.ImageID)

	// Section 8: the simulated update phase runs before any real
	// transformation (EXP-03) and is disabled unless configured.
	if err := app.runSimulatedPhase(ctx, job); err != nil {
		return err
	}

	// Optional quiet delay: processing time with no intermediate updates
	// (condition A). Disabled by default.
	if app.config.quietDelay > 0 {
		if err := sleepFor(ctx, app.config.quietDelay); err != nil {
			return err
		}
	}

	// Optional artificial delay that stands in for expensive transformation
	// work. It watches ctx too, so cancellation during shutdown aborts the
	// sleep immediately. Real costs dominate; this only makes queueing visible
	// for the five-image burst measurement.
	if app.config.processingDelay > 0 {
		if err := sleepFor(ctx, app.config.processingDelay); err != nil {
			return err
		}
	}

	original, err := app.images.Read(job.StoredFilename)
	if err != nil {
		return app.markFailed(ctx, job.ID, job.PublicID, "could not read the original image")
	}

	// Generate every variant up front. A failure here means none exist yet.
	results, err := imageproc.Generate(original)
	if err != nil {
		// Failures are recorded on the job so clients can observe them.
		return app.markFailed(ctx, job.ID, job.PublicID, "could not process the image")
	}

	// Store each variant file and, in the same transaction, advance the job's
	// durable progress (stage + variants_completed) with a version bump, then
	// announce it (PROG-01). The final variant and the completed status share
	// one transaction and one version increment (PROG-02). If any step fails,
	// clean up the files saved so far and mark the job failed: partial work is
	// never reported as completed, but the last known progress is kept
	// (PROG-03).
	var saved []string
	for i, res := range results {
		name, err := app.images.Save(res.Data, extensionFor(job.MediaType))
		if err != nil {
			removeAll(app.images, saved)
			return app.markFailed(ctx, job.ID, job.PublicID, "could not store the generated variants")
		}
		saved = append(saved, name)

		variant := &data.Variant{
			ImageID:        job.ImageID,
			Name:           res.Name,
			StoredFilename: name,
			Width:          res.Width,
			Height:         res.Height,
			SizeBytes:      int64(len(res.Data)),
		}
		finish := i == len(results)-1
		version, err := app.models.RecordVariantProgress(ctx, job.ID, variant, stageFor(res.Name), i+1, finish)
		if err != nil {
			_ = app.images.Remove(name)
			removeAll(app.images, saved)
			return app.markFailed(ctx, job.ID, job.PublicID, "could not store the generated variants")
		}
		status := "processing"
		if finish {
			status = "completed"
		}
		app.recordChange(job.PublicID, status, version)
	}

	app.logger.Info("image job completed", "job_id", job.PublicID)
	return nil
}

// runSimulatedPhase advances a separate simulation_step counter on a fixed
// cadence from the experimental controls (EXP-02). It is disabled unless both
// the duration and interval are positive, and it finishes before any real
// transformation begins (EXP-03). Each step bumps the persisted version and is
// logged with its actual commit time rather than an assumed schedule.
func (app *application) runSimulatedPhase(ctx context.Context, job *data.PendingJob) error {
	duration := app.config.simulateDuration
	interval := app.config.simulateInterval
	if duration <= 0 || interval <= 0 {
		return nil
	}

	app.logger.Info("simulated phase started", "job_id", job.PublicID,
		"duration", duration.String(), "interval", interval.String())

	start := time.Now()
	steps := int(duration / interval)
	for step := 1; step <= steps; step++ {
		if err := sleepUntil(ctx, start.Add(time.Duration(step)*interval)); err != nil {
			return err
		}
		version, err := app.models.Jobs.AdvanceSimulation(ctx, job.ID, step)
		if err != nil {
			return err
		}
		app.logger.Info("simulated progress committed",
			"job_id", job.PublicID,
			"simulation_step", step,
			"version", version,
			"committed_at", time.Now().UTC().Format(time.RFC3339Nano))
		app.broadcaster.broadcast(job.PublicID)
	}

	app.logger.Info("simulated phase finished", "job_id", job.PublicID, "steps", steps)
	return nil
}

// stageFor maps a variant name to its progress stage. The last milestone and
// the completed status share one commit (PROG-02), so a completed job keeps
// stage 'display_ready'.
func stageFor(variant string) string {
	switch variant {
	case "thumbnail":
		return "thumbnail_ready"
	case "preview":
		return "preview_ready"
	case "display":
		return "display_ready"
	default:
		return "starting"
	}
}

// sleepFor waits for d, returning early with ctx.Err() if the context is
// cancelled (during shutdown, for example).
func sleepFor(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// sleepUntil waits until the wall-clock instant t. A deadline already in the
// past returns immediately.
func sleepUntil(ctx context.Context, t time.Time) error {
	if d := time.Until(t); d > 0 {
		return sleepFor(ctx, d)
	}
	return nil
}

// markFailed commits the safe failure and announces the change, returning the
// error from the failed commit (nil on success).
func (app *application) markFailed(ctx context.Context, jobID, publicID, message string) error {
	version, err := app.models.Jobs.MarkFailed(ctx, jobID, message)
	if err != nil {
		return err
	}
	app.recordChange(publicID, "failed", version)
	return nil
}

// recordChange logs a committed job change with its version and a post-commit
// wall-clock timestamp, then wakes every long-poll observer of that job. The
// notification happens strictly after the commit so waiters re-read durable
// state (VER-03).
func (app *application) recordChange(publicID, status string, version int) {
	app.logger.Info("job change committed",
		"job_id", publicID,
		"status", status,
		"version", version,
		"committed_at", time.Now().UTC().Format(time.RFC3339Nano))
	app.broadcaster.broadcast(publicID)
}

// removeAll best-effort deletes every stored file in names.
func removeAll(store interface {
	Remove(name string) error
}, names []string) {
	for _, name := range names {
		_ = store.Remove(name)
	}
}
