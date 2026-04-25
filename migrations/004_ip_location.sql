-- Add IP internal and location fields to devices table

ALTER TABLE devices ADD COLUMN ip_internal TEXT NOT NULL DEFAULT '';
ALTER TABLE devices ADD COLUMN ip_location TEXT NOT NULL DEFAULT '';
