// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// SetCertificate stores a PEM certificate in the database
func (s *Store) SetCertificate(ctx context.Context, pemCertificate string) error {
	// Calculate certificate hash
	hash := sha256.Sum256([]byte(pemCertificate))
	certificateHash := hex.EncodeToString(hash[:])

	params := SetCertificateParams{
		CertificateHash: certificateHash,
		CertificateType: "PEM", // Default type
		CertificateData: pemCertificate,
	}

	_, err := s.q.SetCertificate(ctx, params)
	if err != nil {
		return fmt.Errorf("failed to set certificate: %w", err)
	}

	return nil
}

// LookupCertificate retrieves a certificate by its hash
func (s *Store) LookupCertificate(ctx context.Context, certificateHash string) (string, error) {
	cert, err := s.q.GetCertificate(ctx, certificateHash)
	if err != nil {
		if err == pgx.ErrNoRows {
			return "", nil
		}
		return "", fmt.Errorf("failed to lookup certificate: %w", err)
	}

	return cert.CertificateData, nil
}

// DeleteCertificate removes a certificate from the database
func (s *Store) DeleteCertificate(ctx context.Context, certificateHash string) error {
	err := s.q.DeleteCertificate(ctx, certificateHash)
	if err != nil {
		return fmt.Errorf("failed to delete certificate: %w", err)
	}

	return nil
}
