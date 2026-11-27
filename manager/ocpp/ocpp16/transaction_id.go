// SPDX-License-Identifier: Apache-2.0

package ocpp16

import (
	"encoding/binary"

	"github.com/google/uuid"
)

// ConvertToUUID returns the id under which the manager stores the transaction that an
// OCPP 1.6 charge station numbered transactionId: a UUID whose first 12 bytes are zero
// and whose last 4 bytes hold the number.
func ConvertToUUID(transactionId int) string {
	uuidBytes := []byte{
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
		(byte)(transactionId >> 24),
		(byte)(transactionId >> 16),
		(byte)(transactionId >> 8),
		(byte)(transactionId),
	}
	return uuid.Must(uuid.FromBytes(uuidBytes)).String()
}

// ConvertFromUUID returns the OCPP 1.6 transaction id of a stored transaction id made by
// ConvertToUUID. It reports false for any other id: one that is not a UUID, or a UUID
// whose first 12 bytes are not all zero (the transactions of OCPP 2.0.1 stations).
func ConvertFromUUID(id string) (int, bool) {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return 0, false
	}
	for _, b := range parsed[:12] {
		if b != 0 {
			return 0, false
		}
	}
	// the 32 bits of the integer that ConvertToUUID kept, read back with their sign
	return int(int32(binary.BigEndian.Uint32(parsed[12:]))), true // #nosec G115 -- the 32 bits that ConvertToUUID stored
}
