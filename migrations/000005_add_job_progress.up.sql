-- Filename: 000005_add_job_progress.up.sql
-- Version 2 progress (PROG-01..03) and experimental controls (EXP-02).
-- Real progress is a stage plus the number of variant files actually stored;
-- the simulated phase uses a separate simulation_step counter so a simulated
-- update is never mistaken for a real variant. None of this adds a fifth job
-- status: status stays one of queued/processing/completed/failed.

BEGIN;

ALTER TABLE imagelab.jobs
    ADD COLUMN IF NOT EXISTS stage TEXT,
    ADD COLUMN IF NOT EXISTS variants_completed INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS simulation_step INTEGER;

ALTER TABLE imagelab.jobs
    ADD CONSTRAINT jobs_stage_check CHECK (
        stage IS NULL OR stage IN ('starting', 'thumbnail_ready', 'preview_ready', 'display_ready')
    );

ALTER TABLE imagelab.jobs
    ADD CONSTRAINT jobs_variants_completed_check CHECK (variants_completed BETWEEN 0 AND 3);

ALTER TABLE imagelab.jobs
    ADD CONSTRAINT jobs_simulation_step_check CHECK (simulation_step IS NULL OR simulation_step >= 0);

COMMIT;
