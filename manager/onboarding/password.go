// SPDX-License-Identifier: Apache-2.0

// Package onboarding approves the charge stations that connected without being
// registered: it gives each one a password of its own, through the command queue or by
// hand, or lets it keep the onboarding password of its tenant, and makes it active once
// it has its password.
package onboarding

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// passwordBytes is how many random bytes a new password has. Hex encoded they are 40
// characters: the longest OCPP 1.6 AuthorizationKey and OCPP 2.0.1 BasicAuthPassword
// (16 to 40 characters).
const passwordBytes = 20

// NewPassword returns a new password for a charge station, from crypto/rand, and its
// hash as PasswordHash gives it.
func NewPassword() (password, hash string, err error) {
	random := make([]byte, passwordBytes)
	if _, err := rand.Read(random); err != nil {
		return "", "", fmt.Errorf("new password of a charge station: %w", err)
	}
	password = hex.EncodeToString(random)
	return password, PasswordHash(password), nil
}

// PasswordHash returns the base64 encoded SHA-256 hash of a password, which is how the
// gateway compares the password of a charge station.
func PasswordHash(password string) string {
	sum := sha256.Sum256([]byte(password))
	return base64.StdEncoding.EncodeToString(sum[:])
}
