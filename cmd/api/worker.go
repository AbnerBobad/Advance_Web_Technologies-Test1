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
	// Atomically claim the oldest queued job and mark it processing.
	job, err := app.models.Jobs.ClaimNext(ctx)
	if err != nil {
		return err
	}
	app.logger.Info("image job started", "job_id", job.PublicID,
		"image_id", job.ImageID)

	// Optional artificial delay that stands in for expensive transformation
	// work. It watches ctx too, so cancellation during shutdown aborts the
	// sleep immediately. Real costs dominate; this only makes queueing visible
	// for the five-image burst measurement.
	if app.config.processingDelay > 0 {
		timer := time.NewTimer(app.config.processingDelay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}

	original, err := app.images.Read(job.StoredFilename)
	if err != nil {
		return app.models.Jobs.MarkFailed(ctx, job.ID, "could not read the original image")
	}

	// Generate every variant up front. A failure here means none exist yet.
	results, err := imageproc.Generate(original)
	if err != nil {
		// Failures are recorded on the job so clients can observe them.
		return app.models.Jobs.MarkFailed(ctx, job.ID, "could not process the image")
	}

	// Store each variant file and its metadata. If any step fails, clean up
	// the files saved so far and mark the job failed: partial work is never
	// reported as completed.
	var saved []string
	for _, res := range results {
		name, err := app.images.Save(res.Data, extensionFor(job.MediaType))
		if err != nil {
			removeAll(app.images, saved)
			return app.models.Jobs.MarkFailed(ctx, job.ID, "could not store the generated variants")
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
		if err := app.models.Variants.Insert(ctx, variant); err != nil {
			_ = app.images.Remove(name)
			removeAll(app.images, saved)
			return app.models.Jobs.MarkFailed(ctx, job.ID, "could not store the generated variants")
		}
	}

	// Only after every variant and its metadata exist may the job complete.
	if err := app.models.Jobs.MarkCompleted(ctx, job.ID); err != nil {
		return err
	}
	app.logger.Info("image job completed", "job_id", job.PublicID)
	return nil
}

// removeAll best-effort deletes every stored file in names.
func removeAll(store interface {
	Remove(name string) error
}, names []string) {
	for _, name := range names {
		_ = store.Remove(name)
	}
}
