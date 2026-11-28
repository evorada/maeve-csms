// SPDX-License-Identifier: Apache-2.0

package ocpp16_test

import (
	"math"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
)

func TestConvertToUUID(t *testing.T) {
	assert.Equal(t, "00000000-0000-0000-0000-00000000007b", ocpp16.ConvertToUUID(123))
	assert.Equal(t, "00000000-0000-0000-0000-000000000000", ocpp16.ConvertToUUID(0))
	assert.Equal(t, "00000000-0000-0000-0000-00007fffffff", ocpp16.ConvertToUUID(math.MaxInt32))
}

func TestConvertFromUUIDReversesConvertToUUID(t *testing.T) {
	for _, n := range []int{0, 1, 123, math.MaxInt32, -1, math.MinInt32} {
		got, ok := ocpp16.ConvertFromUUID(ocpp16.ConvertToUUID(n))
		assert.True(t, ok, "%d", n)
		assert.Equal(t, n, got)
	}
}

func TestConvertFromUUIDRefusesOtherIds(t *testing.T) {
	for _, id := range []string{
		uuid.NewString(),
		"00000000-0000-0000-0001-00000000007b", // a byte of the first 12 set
		"10000000-0000-0000-0000-00000000007b",
		"123",
		"",
		"not-a-uuid",
	} {
		_, ok := ocpp16.ConvertFromUUID(id)
		assert.False(t, ok, id)
	}
}
