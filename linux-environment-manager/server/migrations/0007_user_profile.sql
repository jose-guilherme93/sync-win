-- Add updated_at column to users table for profile change tracking.
ALTER TABLE users ADD COLUMN updated_at TEXT;
