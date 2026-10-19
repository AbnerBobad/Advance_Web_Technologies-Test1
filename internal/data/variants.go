package data

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Variant records one generated output (thumbnail, preview, display) owned by
// an accepted image. The worker populates these rows and their files.
type Variant struct {
	ID             string
	ImageID        string
	Name           string
	StoredFilename string
	Width          int
	Height         int
	SizeBytes      int64
	CreatedAt      time.Time
}

// VariantModel wraps the database pool for variants-table read operations.
// Rows are written only by the background worker through
// Models.RecordVariantProgress, which stores the variant and the resulting job
// progress in one transaction.
type VariantModel struct {
	DB *sql.DB
}

// GetByImageID returns every variant owned by an image, used to render the
// completed-job response.
func (m VariantModel) GetByImageID(ctx context.Context, imageID string) ([]Variant, error) {
	query := `SELECT id, image_id, name, stored_filename, width, height, size_bytes, created_at
		FROM imagelab.variants WHERE image_id = $1 ORDER BY created_at`
	rows, err := m.DB.QueryContext(ctx, query, imageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	variants := []Variant{}
	for rows.Next() {
		var v Variant
		if err := rows.Scan(&v.ID, &v.ImageID, &v.Name, &v.StoredFilename,
			&v.Width, &v.Height, &v.SizeBytes, &v.CreatedAt); err != nil {
			return nil, err
		}
		variants = append(variants, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return variants, nil
}

// Get returns a single named variant, used by the file-serving endpoint. The
// variants_name_check constraint means an unknown name simply matches no row.
func (m VariantModel) Get(ctx context.Context, imageID, name string) (*Variant, error) {
	query := `SELECT id, image_id, name, stored_filename, width, height, size_bytes, created_at
		FROM imagelab.variants WHERE image_id = $1 AND name = $2`
	var v Variant
	err := m.DB.QueryRowContext(ctx, query, imageID, name).Scan(
		&v.ID, &v.ImageID, &v.Name, &v.StoredFilename, &v.Width, &v.Height, &v.SizeBytes, &v.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRecordNotFound
		}
		return nil, err
	}
	return &v, nil
}
