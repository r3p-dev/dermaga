-- Migration 0004_m6_apps_deploy.sql
-- Adds app health consecutive threshold, resource/capability limits (APP-5, APP-6),
-- and deployment error_message / image_ref columns (DEP-3, DEP-9).

ALTER TABLE apps ADD COLUMN health_consecutive INTEGER NOT NULL DEFAULT 3;
ALTER TABLE apps ADD COLUMN memory_limit TEXT NOT NULL DEFAULT '512m';
ALTER TABLE apps ADD COLUMN cpu_limit TEXT NOT NULL DEFAULT '1.0';
ALTER TABLE apps ADD COLUMN pids_limit INTEGER NOT NULL DEFAULT 128;
ALTER TABLE apps ADD COLUMN read_only_root INTEGER NOT NULL DEFAULT 0;
ALTER TABLE apps ADD COLUMN add_capabilities TEXT NOT NULL DEFAULT '';

ALTER TABLE deployments ADD COLUMN error_message TEXT NOT NULL DEFAULT '';
ALTER TABLE deployments ADD COLUMN image_ref TEXT NOT NULL DEFAULT '';
