-- Captured content is stored once per distinct byte string: Claude Code resends
-- the whole conversation on every request.
CREATE TABLE content_blobs (hash TEXT PRIMARY KEY, data BLOB NOT NULL, size INTEGER NOT NULL);

CREATE TABLE request_content (
  request_id INTEGER PRIMARY KEY REFERENCES requests(id) ON DELETE CASCADE,
  envelope_hash TEXT NOT NULL,
  message_count INTEGER NOT NULL,
  created_at INTEGER NOT NULL
);

CREATE INDEX request_content_created ON request_content (created_at);

CREATE TABLE request_messages (
  request_id INTEGER NOT NULL REFERENCES requests(id) ON DELETE CASCADE,
  position INTEGER NOT NULL,
  hash TEXT NOT NULL,
  PRIMARY KEY (request_id, position)
);

CREATE INDEX request_messages_hash ON request_messages (hash);

CREATE TABLE leg_content (
  request_id INTEGER NOT NULL REFERENCES requests(id) ON DELETE CASCADE,
  seq INTEGER NOT NULL,
  input TEXT NOT NULL DEFAULT '',
  output_hash TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (request_id, seq)
);
