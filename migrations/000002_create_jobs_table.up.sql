-- Filename: 000002_create_jobs_table.up.sql
-- public_id is a second, fully-random identifier (uuidv4()),
    -- distinct from the internal, time-ordered id. Only public_id
    -- should ever be returned to a client or accepted in a URL never
    -- the internal id.

BEGIN;

CREATE TABLE IF NOT EXISTS imagelab.jobs (
    id            UUID PRIMARY KEY DEFAULT uuidv7(),
    public_id     UUID NOT NULL UNIQUE DEFAULT uuidv4(),
    image_id      UUID NOT NULL REFERENCES imagelab.images(id) ON DELETE CASCADE,
    status        TEXT NOT NULL DEFAULT 'queued',
    safe_error    TEXT,
    queued_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at    TIMESTAMPTZ,
    completed_at  TIMESTAMPTZ,
    failed_at     TIMESTAMPTZ,
    CONSTRAINT jobs_status_check
        CHECK (status IN ('queued', 'processing', 'completed', 'failed')),
    CONSTRAINT jobs_status_times CHECK (
        (status = 'queued'     AND started_at IS NULL     AND completed_at IS NULL AND failed_at IS NULL) OR
        (status = 'processing' AND started_at IS NOT NULL AND completed_at IS NULL AND failed_at IS NULL) OR
        (status = 'completed'  AND started_at IS NOT NULL AND completed_at IS NOT NULL AND failed_at IS NULL) OR
        (status = 'failed'     AND started_at IS NOT NULL AND completed_at IS NULL AND failed_at IS NOT NULL)
    )
);

CREATE INDEX IF NOT EXISTS jobs_status_idx ON imagelab.jobs (status);
CREATE INDEX IF NOT EXISTS jobs_image_id_idx ON imagelab.jobs (image_id);

COMMIT;
