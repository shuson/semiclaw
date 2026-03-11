CREATE TABLE IF NOT EXISTS owners (
  owner_id TEXT PRIMARY KEY,
  password_hash TEXT NOT NULL,
  session_token_hash TEXT,
  claimed_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS config (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL,
  updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS secrets (
  key TEXT PRIMARY KEY,
  ciphertext BLOB NOT NULL,
  nonce BLOB NOT NULL,
  updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS messages (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id TEXT NOT NULL,
  role TEXT NOT NULL,
  content TEXT NOT NULL,
  created_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_messages_user_created_at ON messages(user_id, created_at);
