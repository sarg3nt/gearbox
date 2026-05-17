-- SQLite < 3.35 can't ALTER TABLE ... DROP COLUMN. Most modern
-- distros have 3.35+ but to stay portable across older deployments
-- this down migration is a no-op; rollback leaves the column in
-- place with all rows defaulting to 0 (= console disabled), which is
-- functionally equivalent to "not migrated yet" for any code path
-- that uses the value.
SELECT 1;
