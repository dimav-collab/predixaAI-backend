CREATE TABLE IF NOT EXISTS db_connections (
  id uuid PRIMARY KEY,
  name text NOT NULL,
  type text NOT NULL,
  host text NOT NULL,
  port int NOT NULL,
  user_name text NOT NULL,
  password_enc text NOT NULL,
  database text NOT NULL,
  created_at timestamptz NOT NULL
);
