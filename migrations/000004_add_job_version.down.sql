-- Filename: 000004_add_job_version.down.sql

BEGIN;

ALTER TABLE imagelab.jobs DROP CONSTRAINT IF EXISTS jobs_version_positive;
ALTER TABLE imagelab.jobs DROP COLUMN IF EXISTS version;

COMMIT;
