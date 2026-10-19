-- Filename: 000005_add_job_progress.down.sql

BEGIN;

ALTER TABLE imagelab.jobs DROP CONSTRAINT IF EXISTS jobs_stage_check;
ALTER TABLE imagelab.jobs DROP CONSTRAINT IF EXISTS jobs_variants_completed_check;
ALTER TABLE imagelab.jobs DROP CONSTRAINT IF EXISTS jobs_simulation_step_check;

ALTER TABLE imagelab.jobs
    DROP COLUMN IF EXISTS stage,
    DROP COLUMN IF EXISTS variants_completed,
    DROP COLUMN IF EXISTS simulation_step;

COMMIT;
