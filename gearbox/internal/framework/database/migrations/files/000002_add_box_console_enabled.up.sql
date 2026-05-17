-- #89 Phase 2c: per-box opt-in for the remote console feature.
-- Defaults to 0 so the column rollout doesn't accidentally enable
-- console on every box; operators flip it on per box via the box
-- settings UI. The agent-side HAPROXY_AGENT_CONSOLE_ENABLED flag is
-- still required — both must be true for a session to open.
ALTER TABLE boxes ADD COLUMN console_enabled INTEGER NOT NULL DEFAULT 0;
