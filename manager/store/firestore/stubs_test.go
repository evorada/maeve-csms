// SPDX-License-Identifier: Apache-2.0

package firestore

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// The Firestore backend does not implement every store method. The methods that it does
// not implement return an error that wraps store.ErrNotImplemented, so that callers can
// detect it with errors.Is. The stubs never touch the Firestore client, so a zero Store
// is enough to call them.
func TestStubsReturnErrNotImplemented(t *testing.T) {
	ctx := context.Background()
	s := &Store{}

	stubs := map[string]func() error{
		"SetChargeStationDataTransfer": func() error {
			return s.SetChargeStationDataTransfer(ctx, "cs001", &store.ChargeStationDataTransfer{})
		},
		"LookupChargeStationDataTransfer": func() error {
			_, err := s.LookupChargeStationDataTransfer(ctx, "cs001")
			return err
		},
		"ListChargeStationDataTransfers": func() error {
			_, err := s.ListChargeStationDataTransfers(ctx, 10, "")
			return err
		},
		"DeleteChargeStationDataTransfer": func() error {
			return s.DeleteChargeStationDataTransfer(ctx, "cs001")
		},
		"SetChargeStationClearCache": func() error {
			return s.SetChargeStationClearCache(ctx, "cs001", &store.ChargeStationClearCache{})
		},
		"LookupChargeStationClearCache": func() error {
			_, err := s.LookupChargeStationClearCache(ctx, "cs001")
			return err
		},
		"ListChargeStationClearCaches": func() error {
			_, err := s.ListChargeStationClearCaches(ctx, 10, "")
			return err
		},
		"DeleteChargeStationClearCache": func() error {
			return s.DeleteChargeStationClearCache(ctx, "cs001")
		},
		"SetChargeStationChangeAvailability": func() error {
			return s.SetChargeStationChangeAvailability(ctx, "cs001", &store.ChargeStationChangeAvailability{})
		},
		"LookupChargeStationChangeAvailability": func() error {
			_, err := s.LookupChargeStationChangeAvailability(ctx, "cs001")
			return err
		},
		"ListChargeStationChangeAvailabilities": func() error {
			_, err := s.ListChargeStationChangeAvailabilities(ctx, 10, "")
			return err
		},
		"DeleteChargeStationChangeAvailability": func() error {
			return s.DeleteChargeStationChangeAvailability(ctx, "cs001")
		},
		"QueryMeterValues": func() error {
			_, err := s.QueryMeterValues(ctx, store.MeterValuesFilter{})
			return err
		},
		"UpdateTransactionDetails": func() error {
			return s.UpdateTransactionDetails(ctx, "cs001", "tx001", &store.TransactionDetails{})
		},
		"NextOcpp16TransactionId": func() error {
			_, err := s.NextOcpp16TransactionId(ctx)
			return err
		},
		"PutTenant": func() error {
			return s.PutTenant(ctx, &store.Tenant{})
		},
		"GetTenant": func() error {
			_, err := s.GetTenant(ctx, store.LegacyOrgId)
			return err
		},
		"GetTenantByHost": func() error {
			_, err := s.GetTenantByHost(ctx, "chargeco.ocpp.example.com")
			return err
		},
		"GetTenantByOrgKey": func() error {
			_, err := s.GetTenantByOrgKey(ctx, "chrg01")
			return err
		},
		"ListTenants": func() error {
			_, err := s.ListTenants(ctx, "", 10)
			return err
		},
		"ListSubtreeOrgIds": func() error {
			_, err := s.ListSubtreeOrgIds(ctx, store.LegacyOrgId)
			return err
		},
		"SetTenantStatus": func() error {
			_, err := s.SetTenantStatus(ctx, store.LegacyOrgId, store.TenantStatusSuspended)
			return err
		},
		"CountConnectorsForOrgs": func() error {
			_, err := s.CountConnectorsForOrgs(ctx, []string{store.LegacyOrgId})
			return err
		},
	}

	for name, stub := range stubs {
		t.Run(name, func(t *testing.T) {
			err := stub()
			assert.ErrorIs(t, err, store.ErrNotImplemented)
			assert.ErrorContains(t, err, name)
		})
	}
}
