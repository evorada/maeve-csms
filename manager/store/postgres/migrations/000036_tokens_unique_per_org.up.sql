-- Tokens are unique per org and party: two orgs, or two parties of one org, can hold
-- a token with the same UID. Until now a UID could be stored only once, so the rows
-- that are there cannot violate the new constraint.
ALTER TABLE tokens DROP CONSTRAINT tokens_uid_unique;
ALTER TABLE tokens ADD CONSTRAINT tokens_org_party_uid_unique UNIQUE (org_id, country_code, party_id, uid);

-- lookups by UID: inside one org (OCPP authorisation) and across the orgs
CREATE INDEX idx_tokens_org_uid ON tokens(org_id, uid);
CREATE INDEX idx_tokens_uid     ON tokens(uid);
