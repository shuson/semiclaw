CREATE TABLE IF NOT EXISTS agents (
  name TEXT PRIMARY KEY,
  system_prompt TEXT NOT NULL,
  model TEXT NOT NULL,
  base_url TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
