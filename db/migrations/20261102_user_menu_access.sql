BEGIN;
ALTER TABLE users ADD COLUMN IF NOT EXISTS menu_access TEXT[] NOT NULL DEFAULT '{}';
CREATE TABLE IF NOT EXISTS user_access_audits (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  actor_id TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  actor_name TEXT NOT NULL,
  action TEXT NOT NULL,
  detail TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS user_access_audits_user_created_idx
  ON user_access_audits (user_id, created_at DESC);
COMMIT;
