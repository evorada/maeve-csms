// SPDX-License-Identifier: Apache-2.0

package firestore

import (
	"context"
	"errors"
	"testing"
	"time"

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
		"AppendOutboxEvents": func() error {
			return s.AppendOutboxEvents(ctx, &store.OutboxEvent{})
		},
		"RelayOutboxEvents": func() error {
			_, err := s.RelayOutboxEvents(ctx, 10, func(context.Context, []*store.OutboxEvent) (int, error) {
				panic("publish must not be called")
			})
			return err
		},
		"ListOutboxEvents": func() error {
			_, err := s.ListOutboxEvents(ctx, store.OutboxEventFilter{})
			return err
		},
		"PurgePublishedOutboxEvents": func() error {
			_, err := s.PurgePublishedOutboxEvents(ctx, time.Now())
			return err
		},
		"RecordStationConnected": func() error {
			_, err := s.RecordStationConnected(ctx, "cs001", "conn-a", time.Now())
			return err
		},
		"RecordStationDisconnected": func() error {
			_, err := s.RecordStationDisconnected(ctx, "cs001", "conn-a", time.Now())
			return err
		},
		"TouchStationLastSeen": func() error {
			return s.TouchStationLastSeen(ctx, "cs001", time.Now())
		},
		"DisconnectStaleStations": func() error {
			_, err := s.DisconnectStaleStations(ctx, time.Now(), time.Now())
			return err
		},
		"CreateCommand": func() error {
			return s.CreateCommand(ctx, &store.Command{})
		},
		"GetCommand": func() error {
			_, err := s.GetCommand(ctx, "01936b2e-0000-7000-8000-000000000001")
			return err
		},
		"ListCommands": func() error {
			_, _, err := s.ListCommands(ctx, store.CommandFilter{})
			return err
		},
		"ClaimNextCommand": func() error {
			_, err := s.ClaimNextCommand(ctx, "cs001", time.Now())
			return err
		},
		"TransitionCommand": func() error {
			_, err := s.TransitionCommand(ctx, "01936b2e-0000-7000-8000-000000000001",
				[]store.CommandStatus{store.CommandStatusSent}, store.CommandUpdate{Status: store.CommandStatusAccepted})
			return err
		},
		"SetCommandProgress": func() error {
			_, err := s.SetCommandProgress(ctx, "01936b2e-0000-7000-8000-000000000001", []byte(`{}`), time.Now(), nil)
			return err
		},
		"RedactCommandPayload": func() error {
			return s.RedactCommandPayload(ctx, "01936b2e-0000-7000-8000-000000000001", []byte(`{}`))
		},
		"ListQueuedStationKeys": func() error {
			_, err := s.ListQueuedStationKeys(ctx, time.Now(), "", 10)
			return err
		},
		"ListExpiredQueuedCommands": func() error {
			_, err := s.ListExpiredQueuedCommands(ctx, time.Now(), 10)
			return err
		},
		"ListOverdueSentCommands": func() error {
			_, err := s.ListOverdueSentCommands(ctx, time.Now(), 10)
			return err
		},
		"DeleteCompletedCommandsBefore": func() error {
			_, err := s.DeleteCompletedCommandsBefore(ctx, time.Now(), 10)
			return err
		},
		"CreateAssetLocation": func() error {
			return s.CreateAssetLocation(ctx, &store.AssetLocation{})
		},
		"UpdateAssetLocation": func() error {
			return s.UpdateAssetLocation(ctx, &store.AssetLocation{})
		},
		"LookupAssetLocation": func() error {
			_, err := s.LookupAssetLocation(ctx, "loc001")
			return err
		},
		"LookupAssetLocationByOcpiId": func() error {
			_, err := s.LookupAssetLocationByOcpiId(ctx, "party001", "loc001")
			return err
		},
		"ListAssetLocations": func() error {
			_, _, err := s.ListAssetLocations(ctx, store.AssetLocationFilter{})
			return err
		},
		"DeleteAssetLocation": func() error {
			return s.DeleteAssetLocation(ctx, "loc001")
		},
		"CreateAssetEvse": func() error {
			return s.CreateAssetEvse(ctx, &store.AssetEvse{})
		},
		"UpdateAssetEvse": func() error {
			return s.UpdateAssetEvse(ctx, &store.AssetEvse{})
		},
		"LookupAssetEvse": func() error {
			_, err := s.LookupAssetEvse(ctx, "evse001")
			return err
		},
		"LookupAssetEvseByOcpp": func() error {
			_, err := s.LookupAssetEvseByOcpp(ctx, "cs001", 1)
			return err
		},
		"ListAssetEvsesForLocation": func() error {
			_, err := s.ListAssetEvsesForLocation(ctx, "loc001")
			return err
		},
		"ListAssetEvsesForStation": func() error {
			_, err := s.ListAssetEvsesForStation(ctx, "cs001")
			return err
		},
		"DeleteAssetEvse": func() error {
			return s.DeleteAssetEvse(ctx, "evse001")
		},
		"CreateAssetConnector": func() error {
			return s.CreateAssetConnector(ctx, &store.AssetConnector{})
		},
		"UpdateAssetConnector": func() error {
			return s.UpdateAssetConnector(ctx, &store.AssetConnector{})
		},
		"LookupAssetConnector": func() error {
			_, err := s.LookupAssetConnector(ctx, "connector001")
			return err
		},
		"LookupAssetConnectorByOcpp": func() error {
			_, err := s.LookupAssetConnectorByOcpp(ctx, "cs001", 1, 1)
			return err
		},
		"DeleteAssetConnector": func() error {
			return s.DeleteAssetConnector(ctx, "connector001")
		},
		"MoveChargeStationToLocation": func() error {
			return s.MoveChargeStationToLocation(ctx, "cs001", "location001")
		},
		"DecommissionChargeStation": func() error {
			return s.DecommissionChargeStation(ctx, "cs001", nil)
		},
		"TransferChargeStation": func() error {
			_, err := s.TransferChargeStation(ctx, store.TransferParams{StationKey: "cs001"})
			return err
		},
		"DeleteToken": func() error {
			return s.DeleteToken(ctx, "org001", "token001")
		},
		"LookupChargeStation": func() error {
			_, err := s.LookupChargeStation(ctx, "cs001")
			return err
		},
		"UpdateChargeStation": func() error {
			return s.UpdateChargeStation(ctx, &store.ChargeStation{StationKey: "cs001"})
		},
		"SetChargeStationState": func() error {
			return s.SetChargeStationState(ctx, "cs001", store.StationStateDecommissioned, nil)
		},
		"ListChargeStations": func() error {
			_, _, err := s.ListChargeStations(ctx, store.ChargeStationFilter{})
			return err
		},
		"CountChargeStations": func() error {
			_, err := s.CountChargeStations(ctx, "org001")
			return err
		},
		"RecordOnboardingConnection": func() error {
			_, err := s.RecordOnboardingConnection(ctx, "cs001", "org001", "ws", "open", 50, time.Now())
			return err
		},
		"RecordOnboardingBoot": func() error {
			_, _, err := s.RecordOnboardingBoot(ctx, "cs001", time.Now())
			return err
		},
		"SetOnboarding": func() error {
			return s.SetOnboarding(ctx, "cs001", &store.StationOnboarding{}, nil)
		},
		"CompleteApproval": func() error {
			return s.CompleteApproval(ctx, "cs001", &store.PendingApproval{}, &store.ChargeStationAuth{}, false)
		},
		"RejectChargeStation": func() error {
			return s.RejectChargeStation(ctx, "cs001", "")
		},
		"ListInventory": func() error {
			_, _, err := s.ListInventory(ctx, "org001", store.InventoryStation, false, store.PageRequest{})
			return err
		},
		"CountInventory": func() error {
			_, err := s.CountInventory(ctx, "org001", store.InventoryStation, false)
			return err
		},
		"CreateTariff": func() error {
			return s.CreateTariff(ctx, &store.Tariff{}, &store.TariffVersion{})
		},
		"AddTariffVersion": func() error {
			return s.AddTariffVersion(ctx, "tariff001", 1, &store.TariffVersion{})
		},
		"RenameTariff": func() error {
			return s.RenameTariff(ctx, "tariff001", "name")
		},
		"LookupTariff": func() error {
			_, err := s.LookupTariff(ctx, "tariff001")
			return err
		},
		"LookupTariffByOcpiId": func() error {
			_, err := s.LookupTariffByOcpiId(ctx, "party001", "tariff001")
			return err
		},
		"LookupTariffVersion": func() error {
			_, err := s.LookupTariffVersion(ctx, "tariff001", 1)
			return err
		},
		"ListTariffs": func() error {
			_, _, err := s.ListTariffs(ctx, store.TariffFilter{})
			return err
		},
		"ListTariffVersions": func() error {
			_, err := s.ListTariffVersions(ctx, "tariff001")
			return err
		},
		"DeleteTariff": func() error {
			return s.DeleteTariff(ctx, "tariff001")
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

// The Firestore backend has no transaction that spans store calls: RunInTx only calls
// fn, with the context it was given, and returns what fn returns.
func TestRunInTxCallsFn(t *testing.T) {
	type ctxKey struct{}
	ctx := context.WithValue(context.Background(), ctxKey{}, "caller")
	s := &Store{}

	var transactor store.Transactor = s

	calls := 0
	err := transactor.RunInTx(ctx, func(fnCtx context.Context) error {
		calls++
		assert.Equal(t, "caller", fnCtx.Value(ctxKey{}))
		return nil
	})
	assert.NoError(t, err)
	assert.Equal(t, 1, calls)

	errFn := errors.New("fn failed")
	err = transactor.RunInTx(ctx, func(context.Context) error {
		return errFn
	})
	assert.ErrorIs(t, err, errFn)
}
