-- Filename: 000004_add_job_version.up.sql
-- Version 2 (VER-01): every job carries a monotonically increasing integer
-- version. It starts at 1, is bumped in the same transaction as each
-- observable status/progress change (VER-02), and is returned in every
-- successful job representation. The DEFAULT backfills existing rows.

BEGIN;

ALTER TABLE imagelab.jobs
    ADD COLUMN IF NOT EXISTS version INTEGER NOT NULL DEFAULT 1;

ALTER TABLE imagelab.jobs
    ADD CONSTRAINT jobs_version_positive CHECK (version >= 1);

COMMIT;
