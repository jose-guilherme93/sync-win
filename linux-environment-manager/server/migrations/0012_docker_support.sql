-- Add payload column to commands table for Docker command results and exec data.
ALTER TABLE commands ADD COLUMN payload TEXT NOT NULL DEFAULT '';
