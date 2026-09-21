-- Add encoding column to files for binary save support.
ALTER TABLE files ADD COLUMN encoding TEXT NOT NULL DEFAULT '';
