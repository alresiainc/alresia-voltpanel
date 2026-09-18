-- Phase 10: webhook triggers need a per-pipeline shared secret to verify
-- an incoming request is genuinely from the configured git host (an
-- HMAC-SHA256 signature, the same scheme GitHub/GitLab webhooks use) --
-- 0001_init.sql's pipelines table had no place to store it.
ALTER TABLE pipelines ADD COLUMN webhook_secret TEXT;
