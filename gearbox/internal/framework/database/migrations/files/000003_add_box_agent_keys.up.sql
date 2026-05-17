-- Issue #72 Phase 1: per-box keyring for zero-downtime rotation.
--
-- Adds a one-to-many `box_agent_keys` table where each row is one
-- accepted API key for that box. Replaces the single
-- `boxes.api_key_encrypted` field (which stays in place for one release
-- as a read-only fallback). New writes go to this table; Phase 2 wires
-- the rotation endpoints + UI on top.
--
-- Roles:
--   primary   = the key the dashboard signs outbound requests with
--   secondary = still accepted by the agent during overlap, but the
--               dashboard has demoted it; will be removed after
--               retired_at + the overlap window.
--
-- The migration backfills one `kid='legacy'` row per existing box so
-- existing fleets keep working before any rotation happens.

CREATE TABLE IF NOT EXISTS box_agent_keys (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    box_id INTEGER NOT NULL,
    kid TEXT NOT NULL,
    secret_encrypted BLOB NOT NULL,
    role TEXT NOT NULL CHECK(role IN ('primary', 'secondary')),
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    retired_at DATETIME,
    last_used_at DATETIME,
    FOREIGN KEY (box_id) REFERENCES boxes(id) ON DELETE CASCADE,
    UNIQUE(box_id, kid)
);

CREATE INDEX IF NOT EXISTS idx_box_agent_keys_box
    ON box_agent_keys(box_id);

CREATE INDEX IF NOT EXISTS idx_box_agent_keys_role
    ON box_agent_keys(box_id, role);

-- Enforce the "at most one primary per box" invariant the rotator
-- relies on. Without this constraint a buggy SetBoxPrimaryKey path
-- could leave two primaries on the same box and GetBoxPrimaryKey
-- would return an arbitrary one. Partial-unique-index is the
-- SQLite-supported way to express "unique only when role='primary'".
CREATE UNIQUE INDEX IF NOT EXISTS idx_box_agent_keys_one_primary
    ON box_agent_keys(box_id) WHERE role = 'primary';

-- Backfill: every existing box gets a legacy entry that's a copy of its
-- current `api_key_encrypted` value, marked primary. Idempotent — the
-- WHERE NOT EXISTS guard means re-running the migration on a partially-
-- migrated DB is a no-op.
INSERT INTO box_agent_keys (box_id, kid, secret_encrypted, role, created_at)
SELECT id, 'legacy', api_key_encrypted, 'primary', created_at
FROM boxes
WHERE NOT EXISTS (
    SELECT 1 FROM box_agent_keys
    WHERE box_id = boxes.id AND kid = 'legacy'
);
