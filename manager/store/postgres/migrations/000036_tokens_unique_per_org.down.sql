-- Back to one token per UID. This fails ("could not create unique index") while two
-- tokens share a UID (in two orgs, or for two parties of one org), and then changes
-- nothing: such tokens have to be removed by hand first, the migration does not
-- choose which of them to delete.
DROP INDEX IF EXISTS idx_tokens_uid;
DROP INDEX IF EXISTS idx_tokens_org_uid;

ALTER TABLE tokens DROP CONSTRAINT IF EXISTS tokens_org_party_uid_unique;
ALTER TABLE tokens ADD CONSTRAINT tokens_uid_unique UNIQUE (uid);
