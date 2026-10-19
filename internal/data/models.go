package data

import (
	"context"
	"database/sql"
	"time"
)

// Models aggregates every data-access model used by the application. Each
// model wraps the shared *sql.DB pool.
type Models struct {
	Images   ImageModel
	Jobs     JobModel
	Variants VariantModel
}

// NewModels builds a Models value with each model wired to the given pool.
func NewModels(db *sql.DB) Models {
	return Models{
		Images:   ImageModel{DB: db},
		Jobs:     JobModel{DB: db},
		Variants: VariantModel{DB: db},
	}
}

// AcceptOriginal is the durable acceptance boundary. It records the uploaded
// original and its queued job inside one transaction so both exist before the
// upload handler returns 202 Accepted; neither record may exist alone.
func (m Models) AcceptOriginal(image *Image, job *Job) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	tx, err := m.Images.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	err = tx.QueryRowContext(ctx, `
		INSERT INTO imagelab.images (original_filename, stored_filename, media_type, size_bytes)
		VALUES ($1, $2, $3, $4) RETURNING id, created_at`,
		image.OriginalFilename, image.StoredFilename, image.MediaType, image.SizeBytes,
	).Scan(&image.ID, &image.CreatedAt)
	if err != nil {
		return err
	}

	err = tx.QueryRowContext(ctx, `
		INSERT INTO imagelab.jobs (image_id, status, queued_at)
		VALUES ($1, 'queued', now()) RETURNING id, public_id, image_id, status, version, queued_at`,
		image.ID,
	).Scan(&job.ID, &job.PublicID, &job.ImageID, &job.Status, &job.Version, &job.QueuedAt)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// RecordVariantProgress stores one generated variant and the resulting job
// progress (stage + variants_completed) in a single transaction (PROG-01).
// When finish is true it also completes the job in the same transaction, so
// the final variant milestone and the completed status share one version
// increment (PROG-02). Either the variant row and the progress update both
// commit, or neither does, so progress always reflects durable output.
func (m Models) RecordVariantProgress(ctx context.Context, jobID string, variant *Variant, stage string, count int, finish bool) (int, error) {
	tx, err := m.Jobs.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	err = tx.QueryRowContext(ctx, `
		INSERT INTO imagelab.variants (image_id, name, stored_filename, width, height, size_bytes)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, created_at`,
		variant.ImageID, variant.Name, variant.StoredFilename,
		variant.Width, variant.Height, variant.SizeBytes,
	).Scan(&variant.ID, &variant.CreatedAt)
	if err != nil {
		return 0, err
	}

	var version int
	if finish {
		err = tx.QueryRowContext(ctx, `
			UPDATE imagelab.jobs
			SET status = 'completed', completed_at = now(), stage = $2, variants_completed = $3, version = version + 1
			WHERE id = $1 RETURNING version`,
			jobID, stage, count).Scan(&version)
	} else {
		err = tx.QueryRowContext(ctx, `
			UPDATE imagelab.jobs
			SET stage = $2, variants_completed = $3, version = version + 1
			WHERE id = $1 RETURNING version`,
			jobID, stage, count).Scan(&version)
	}
	if err != nil {
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return version, nil
}
