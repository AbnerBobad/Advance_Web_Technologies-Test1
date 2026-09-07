-- Filename: 000002_create_images_table.up.sql

BEGIN;

CREATE TABLE IF NOT EXISTS imagelab.images (
    id                BIGSERIAL PRIMARY KEY,
    original_filename TEXT NOT NULL,
    stored_filename   TEXT NOT NULL,
    media_type        TEXT NOT NULL,
    size_bytes        BIGINT NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMIT;