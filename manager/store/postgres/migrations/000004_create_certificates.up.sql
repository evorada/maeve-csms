CREATE TABLE certificates (
    certificate_hash VARCHAR(255) PRIMARY KEY,
    certificate_type VARCHAR(50) NOT NULL,
    certificate_data TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_certificates_type ON certificates(certificate_type);
