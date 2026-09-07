-- Filename: 000001_create_imagelab_schema.up.sql

BEGIN;

-- The pooling role owns no privileges on the shared public schema (a
-- PostgreSQL 15+ default), so all ImageLab objects live in a dedicated
-- schema owned by the application role.
CREATE SCHEMA IF NOT EXISTS imagelab;

COMMIT;