-- SQLite does not support DROP COLUMN in older versions; recreate table without skip_tls_verify
CREATE TABLE boxes_backup AS SELECT id, box_id, name, location, notes, agent_url, api_key_encrypted, enabled, auto_discovery, created_at, updated_at, created_by FROM boxes;
DROP TABLE boxes;
ALTER TABLE boxes_backup RENAME TO boxes;
