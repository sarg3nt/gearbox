-- 2026-05 security audit P2-4: track per-account timestamp of the most
-- recent failed login attempt so RecordLoginAttempt can implement a
-- sliding window over failures (rather than the previous "increment
-- forever until success" semantic). Used purely server-side; never
-- exposed in API responses or UI — surfacing lockout state to the
-- caller is itself an enumeration aid for attackers.
ALTER TABLE users ADD COLUMN last_failed_attempt DATETIME;
