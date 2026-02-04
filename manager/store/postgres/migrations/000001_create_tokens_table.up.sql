CREATE TABLE tokens (
    id BIGSERIAL PRIMARY KEY,
    country_code VARCHAR(2) NOT NULL,
    party_id VARCHAR(3) NOT NULL,
    type VARCHAR(50) NOT NULL,
    uid VARCHAR(36) NOT NULL,
    contract_id VARCHAR(255) NOT NULL,
    visual_number VARCHAR(64),
    issuer VARCHAR(255) NOT NULL,
    group_id VARCHAR(36),
    valid BOOLEAN NOT NULL DEFAULT true,
    language_code VARCHAR(2),
    cache_mode VARCHAR(20) NOT NULL,
    last_updated TIMESTAMP NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    
    CONSTRAINT tokens_uid_unique UNIQUE (uid)
);

CREATE INDEX idx_tokens_contract_id ON tokens(contract_id);
CREATE INDEX idx_tokens_valid ON tokens(valid);
CREATE INDEX idx_tokens_cache_mode ON tokens(cache_mode);
CREATE INDEX idx_tokens_last_updated ON tokens(last_updated);
