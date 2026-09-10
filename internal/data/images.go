package data

import (
	"database/sql"
	"time"
)

// Image records one accepted original upload. The stored filename is
// server-controlled and never derived from the submitted filename.
type Image struct {
	ID               string
	OriginalFilename string
	StoredFilename   string
	MediaType        string
	SizeBytes        int64
	CreatedAt        time.Time
}

// ImageModel wraps the database pool for images-table operations. Inserts for
// the acceptance path live in Models.AcceptOriginal so the image and its
// queued job are recorded atomically.
type ImageModel struct {
	DB *sql.DB
}
