package data

import (
	"database/sql"
	"time"
)

// Job models one unit of asynchronous image processing work. It is created as
// queued in the upload handler and later claimed by the background worker.
type Job struct {
	ID          int64
	ImageID     int64
	Status      string
	QueuedAt    time.Time
	StartedAt   *time.Time
	CompletedAt *time.Time
	FailedAt    *time.Time
}

// JobModel wraps the database pool for jobs-table operations. The worker
// claim/complete/fail methods and the status lookup are added in Week 2.
type JobModel struct {
	DB *sql.DB
}
