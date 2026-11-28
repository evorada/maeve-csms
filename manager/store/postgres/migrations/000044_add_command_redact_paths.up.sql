-- The JSON paths of the payload of a command whose values are secrets (a password that
-- is sent to a station). The dispatcher replaces them with "***" in the stored payload
-- once the command is sent or ends without being sent, and the API never shows them.
ALTER TABLE commands
    ADD COLUMN IF NOT EXISTS redact_paths TEXT[] NOT NULL DEFAULT '{}';
