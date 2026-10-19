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
	// Stage names the current step of processing (PROG-01/02), e.g.
	// "thumbnail_ready". It is null before the worker claims the job and is
	// held at its last value if the job later fails (PROG-03). It is not a
	// job status.
	Stage *string
	// VariantsCompleted counts variant files actually stored (0..3). It only
	// advances after a variant and its metadata are durable.
	VariantsCompleted int
	// SimulationStep counts simulated progress updates from the experimental
	// controls (EXP-02). It is null unless a simulated phase ran, and is kept
	// separate from VariantsCompleted so simulated progress is never confused
	// with real output.
	SimulationStep *int
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
		queued_at, started_at, completed_at, failed_at,
		stage, variants_completed, simulation_step
		FROM imagelab.jobs WHERE public_id = $1`
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var job Job
	var stage sql.NullString
	var simulationStep sql.NullInt64
	err := m.DB.QueryRowContext(ctx, query, publicID).Scan(
		&job.ID, &job.PublicID, &job.ImageID, &job.Status, &job.Version, &job.SafeError,
		&job.QueuedAt, &job.StartedAt, &job.CompletedAt, &job.FailedAt,
		&stage, &job.VariantsCompleted, &simulationStep,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRecordNotFound
		}
		return nil, err
	}
	job.Stage = nullString(stage)
	job.SimulationStep = nullInt(simulationStep)
	return &job, nil
}

// nullString converts a nullable TEXT column into a *string, so an unset stage
// is reported as null rather than an empty string.
func nullString(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	s := ns.String
	return &s
}

// nullInt converts a nullable INTEGER column into a *int.
func nullInt(ni sql.NullInt64) *int {
	if !ni.Valid {
		return nil
	}
	n := int(ni.Int64)
	return &n
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

	// Transition the claimed job from queued to processing and name the first
	// stage. Bumping version in the same statement keeps the status change,
	// the stage, and the version that describes them atomic (VER-02, PROG-01).
	var stage string
	if err := tx.QueryRowContext(ctx,
		`UPDATE imagelab.jobs
		 SET status = 'processing', started_at = now(), stage = 'starting', version = version + 1
		 WHERE id = $1 RETURNING version, stage`,
		job.ID).Scan(&job.Version, &stage); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	job.Status = "processing"
	job.Stage = &stage
	return &job, nil
}

// AdvanceSimulation records the next simulated progress step and bumps the
// version atomically (EXP-02). It touches only simulation_step, so a simulated
// update never changes variants_completed or the job status. The caller logs
// the actual commit time after this returns.
func (m JobModel) AdvanceSimulation(ctx context.Context, id string, step int) (int, error) {
	var version int
	err := m.DB.QueryRowContext(ctx,
		`UPDATE imagelab.jobs SET simulation_step = $2, version = version + 1
		 WHERE id = $1 RETURNING version`,
		id, step).Scan(&version)
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
