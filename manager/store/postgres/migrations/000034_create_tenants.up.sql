CREATE EXTENSION IF NOT EXISTS ltree;

CREATE TABLE tenants (
    org_id               UUID PRIMARY KEY,
    parent_id            UUID,                      -- no FK: the parent may not be provisioned here
    path                 LTREE NOT NULL,
    slug                 TEXT NOT NULL,
    org_key              TEXT NOT NULL,
    parties              JSONB NOT NULL DEFAULT '[]',
    entitlements_version BIGINT NOT NULL DEFAULT 0,
    entitlements         JSONB NOT NULL DEFAULT '{}',
    config_version       BIGINT NOT NULL DEFAULT 0,
    config               JSONB NOT NULL DEFAULT '{}',
    status               TEXT NOT NULL DEFAULT 'active'
                         CHECK (status IN ('active', 'suspended', 'cancelled')),
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT tenants_slug_unique UNIQUE (slug),
    CONSTRAINT tenants_org_key_unique UNIQUE (org_key)
);
CREATE INDEX idx_tenants_path_gist ON tenants USING GIST (path);

CREATE TABLE tenant_hostnames (
    hostname TEXT PRIMARY KEY,
    org_id   UUID NOT NULL REFERENCES tenants(org_id) ON DELETE CASCADE
);
CREATE INDEX idx_tenant_hostnames_org ON tenant_hostnames(org_id);
