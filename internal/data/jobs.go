package data

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Job models one unit of asynchronous image-processing work. It is created as
// queued in the upload handler and later claimed by the background worker.
// The internal DB ID is hidden from clients; PublicID is the externally
// visible opaque identifier used in the status URL.
type Job struct {
	ID          string
	PublicID    string
	ImageID     string
	Status      string
	Version     int
	SafeError   *string
	QueuedAt    time.Time
	StartedAt   *time.Time
	CompletedAt *time.Time
	FailedAt    *time.Time
}

// PendingJob is a claimed job together with the original-image file the worker
// must read to generate the variants. It is only produced by ClaimNext.
type PendingJob struct {
	Job
	StoredFilename string
	MediaType      string
}

// JobModel wraps the database pool for jobs-table operations. Inserting the
// queued job belongs to the acceptance path (Models.AcceptOriginal); the
// status lookup and the worker's claim/complete/fail methods live here.
type JobModel struct {
	DB *sql.DB
}

// GetByPublicID returns the current state of a job for the status endpoint.
func (m JobModel) GetByPublicID(publicID string) (*Job, error) {
	query := `SELECT id, public_id, image_id, status, version, safe_error,
		queued_at, started_at, completed_at, failed_at
		FROM imagelab.jobs WHERE public_id = $1`
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var job Job
	err := m.DB.QueryRowContext(ctx, query, publicID).Scan(
		&job.ID, &job.PublicID, &job.ImageID, &job.Status, &job.Version, &job.SafeError,
		&job.QueuedAt, &job.StartedAt, &job.CompletedAt, &job.FailedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRecordNotFound
		}
		return nil, err
	}
	return &job, nil
}

// ClaimNext atomically grabs the oldest queued job and marks it processing.
// The transaction combines a SELECT ... FOR UPDATE SKIP LOCKED join with the
// status update, so exactly one worker claims any given job and concurrent
// workers never block on rows another worker already owns. sql.ErrNoRows is
// returned when the queue is empty.
func (m JobModel) ClaimNext(ctx context.Context) (*PendingJob, error) {
	tx, err := m.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	query := `SELECT j.id, j.public_id, j.image_id, j.status,
		i.stored_filename, i.media_type
		FROM imagelab.jobs j
		JOIN imagelab.images i ON i.id = j.image_id
		WHERE j.status = 'queued'
		ORDER BY j.queued_at
		FOR UPDATE SKIP LOCKED
		LIMIT 1`

	var job PendingJob
	err = tx.QueryRowContext(ctx, query).Scan(&job.ID, &job.PublicID, &job.ImageID,
		&job.Status, &job.StoredFilename, &job.MediaType)
	if err != nil {
		return nil, err
	}

	// Transition the claimed job from queued to processing. Bumping version
	// in the same statement keeps the status change and the version that
	// describes it atomic (VER-02).
	if err := tx.QueryRowContext(ctx,
		`UPDATE imagelab.jobs SET status = 'processing', started_at = now(), version = version + 1
		 WHERE id = $1 RETURNING version`,
		job.ID).Scan(&job.Version); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	job.Status = "processing"
	return &job, nil
}

// MarkCompleted stamps completion time and bumps the version atomically.
// Called only after every variant and its metadata are available, so completed
// never means partial work. It returns the committed version.
func (m JobModel) MarkCompleted(ctx context.Context, id string) (int, error) {
	var version int
	err := m.DB.QueryRowContext(ctx,
		`UPDATE imagelab.jobs SET status = 'completed', completed_at = now(), version = version + 1
		 WHERE id = $1 RETURNING version`,
		id).Scan(&version)
	return version, err
}

// MarkFailed records the client-safe reason, stamps failed_at, and bumps the
// version atomically. The queued-to-processing transition already set
// started_at, satisfying the jobs_status_times constraint. It returns the
// committed version.
func (m JobModel) MarkFailed(ctx context.Context, id, message string) (int, error) {
	var version int
	err := m.DB.QueryRowContext(ctx,
		`UPDATE imagelab.jobs SET status = 'failed', safe_error = $2, failed_at = now(), version = version + 1
		 WHERE id = $1 RETURNING version`,
		id, message).Scan(&version)
	return version, err
}
