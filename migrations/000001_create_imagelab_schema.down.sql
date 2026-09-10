-- Filename: 000001_create_imagelab_schema.down.sql

BEGIN;

-- CASCADE drops the images table along with the schema itself, so this
-- single statement fully undoes the merged up migration.
DROP SCHEMA IF EXISTS imagelab CASCADE;

COMMIT;
