-- Phase 5: formalizes the Service domain entity's restart/graceful-stop
-- configuration. 0001_init.sql already has kind/autostart/depends_on on
-- `services`, but nothing yet for graceful-stop timeout or crash-restart
-- policy -- add those here (migrations are append-only, never edit 0001).

ALTER TABLE services ADD COLUMN restart_policy TEXT NOT NULL DEFAULT 'on-failure';
ALTER TABLE services ADD COLUMN graceful_timeout_seconds INTEGER NOT NULL DEFAULT 5;
ALTER TABLE services ADD COLUMN max_restarts INTEGER NOT NULL DEFAULT 5;
ALTER TABLE services ADD COLUMN restart_window_seconds INTEGER NOT NULL DEFAULT 60;
