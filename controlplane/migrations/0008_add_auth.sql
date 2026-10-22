-- Authentication users and opaque server-side sessions.

CREATE TABLE IF NOT EXISTS auth_users (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  username      text NOT NULL UNIQUE,
  display_name  text NOT NULL DEFAULT '',
  password_hash text NOT NULL,
  role          text NOT NULL CHECK (role IN ('superadmin', 'admin')),
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS auth_sessions (
  token_hash text PRIMARY KEY,
  user_id    uuid NOT NULL REFERENCES auth_users(id) ON DELETE CASCADE,
  expires_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS auth_sessions_user_id_idx
  ON auth_sessions(user_id);

CREATE INDEX IF NOT EXISTS auth_sessions_expires_at_idx
  ON auth_sessions(expires_at);
