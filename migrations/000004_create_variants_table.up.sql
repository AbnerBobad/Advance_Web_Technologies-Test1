-- Filename: 000004_create_variants_table.up.sql

BEGIN;

CREATE TABLE IF NOT EXISTS imagelab.variants (
    id              BIGSERIAL PRIMARY KEY,
    image_id        BIGINT NOT NULL REFERENCES imagelab.images(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    stored_filename TEXT NOT NULL,
    width           INTEGER NOT NULL,
    height          INTEGER NOT NULL,
    size_bytes      BIGINT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT variants_name_check
        CHECK (name IN ('thumbnail', 'preview', 'display')),
    CONSTRAINT variants_unique_per_image
        UNIQUE (image_id, name)
);

CREATE INDEX IF NOT EXISTS variants_image_id_idx ON imagelab.variants (image_id);

COMMIT;