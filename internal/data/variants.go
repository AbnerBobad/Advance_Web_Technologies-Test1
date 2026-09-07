package data

import (
	"database/sql"
	"time"
)

// Variant records one generated output (thumbnail, preview, display) owned by
// an accepted image. The worker populates these in Week 2.
type Variant struct {
	ID             int64
	ImageID        int64
	Name           string
	StoredFilename string
	Width          int
	Height         int
	SizeBytes      int64
	CreatedAt      time.Time
}

// VariantModel wraps the database pool for variants-table operations.
type VariantModel struct {
	DB *sql.DB
}
