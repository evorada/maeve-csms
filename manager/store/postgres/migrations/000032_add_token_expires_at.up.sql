-- Tokens can expire: from expires_at on, authorisation returns Expired.
-- NULL means that the token does not expire.
ALTER TABLE tokens ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ;
