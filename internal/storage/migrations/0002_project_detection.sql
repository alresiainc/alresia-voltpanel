-- Phase 3 addition (§17): persist the framework-detection engine's result
-- per project so the UI doesn't need to re-run detection on every page
-- load. Purely additive -- new nullable-with-default columns on the
-- existing `projects` table, not a reshape of 0001_init.sql's schema, per
-- that migration's own comment about later phases adding columns rather
-- than churning the schema.
ALTER TABLE projects ADD COLUMN detected_kind TEXT NOT NULL DEFAULT 'unknown';
ALTER TABLE projects ADD COLUMN run_command TEXT NOT NULL DEFAULT '';
