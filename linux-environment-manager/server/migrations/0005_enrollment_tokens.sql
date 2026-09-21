-- Enrollment tokens for the new agent installer flow.
-- Tokens are short-lived, single-use, and bound to an owner.
CREATE TABLE IF NOT EXISTS enrollment_tokens (
    id         TEXT PRIMARY KEY,
    owner_id   TEXT NOT NULL,
    token      TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    used_at    TEXT NOT NULL DEFAULT '',
    device_id  TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_enrollment_tokens_token ON enrollment_tokens(token);
CREATE INDEX IF NOT EXISTS idx_enrollment_tokens_owner ON enrollment_tokens(owner_id);
