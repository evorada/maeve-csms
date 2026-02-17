# OCPP 2.0.1 Implementation Plan

**Project:** MaEVe CSMS OCPP 2.0.1 Completion
**Created:** 2026-02-12
**Status:** 📋 Planning

---

## Overview

Based on the [OCPP 2.0.1 Implementation Audit](ocpp201-implementation-audit.md), this plan implements the missing and incomplete OCPP 2.0.1 messages organized by functional block. Each block will be implemented in its own feature branch.

**Current Overall Coverage:** ~22% (12/55 fully implemented)
**Target Coverage:** 100% of mandatory blocks, 80%+ optional blocks

---

## Implementation Strategy

### Approach
- **Block-Based:** One OCPP 2.0.1 functional block at a time
- **Independent Branches:** Each block gets its own feature branch
- **Upgrade Existing:** Many handlers exist as stubs - upgrade them with real logic
- **Test-Driven:** Unit tests for each handler
- **Follow Patterns:** Match existing handler structure (OpenTelemetry tracing, etc.)

### Branch Naming Convention
```
feature/ocpp201-provisioning
feature/ocpp201-meter-values
feature/ocpp201-remote-control
feature/ocpp201-smart-charging
feature/ocpp201-availability
feature/ocpp201-firmware-management
feature/ocpp201-diagnostics
feature/ocpp201-display-message
feature/ocpp201-local-auth-list
feature/ocpp201-data-transfer
feature/ocpp201-reservation
feature/ocpp201-security
```

---

## Module 1: Provisioning (Upgrade Existing) 🔥

**Branch:** `feature/ocpp201-provisioning`
**Priority:** Critical
**Status:** ✅ Complete - Ready for PR (5/5 tasks complete)
**Complexity:** Medium

### Messages to Upgrade/Implement

#### Task 1.1: StatusNotification - Add Persistence
**Status:** ✅ Complete
**Complexity:** Low
**Completed:** 2026-02-14
**Commit:** 08357fd
**Follow-up:** 2026-02-16 test hardening for timestamp fallback and store error handling

**Current:** Traces connector status but doesn't store it.

- [x] Add `store.Engine` dependency to handler (convert from function to struct)
- [x] Create/use store method to persist EVSE/connector status
- [x] Update `manager/handlers/ocpp201/status_notification.go`
- [x] Update `manager/handlers/ocpp201/status_notification_test.go`
- [x] Update routing in `manager/handlers/ocpp201/routing.go`

**Store Requirements:**
- **Interface:** `UpdateConnectorStatus(ctx, chargeStationId string, evseId int, connectorId int, status string, timestamp time.Time) error`
- **PostgreSQL:** `manager/store/postgres/` - new query/method + migration 000011
- **Firestore:** `manager/store/firestore/` - new method
- **In-Memory:** `manager/store/inmemory/` - new method

---

#### Task 1.2: NotifyReport - Add Persistence
**Status:** ✅ Complete
**Complexity:** Medium
**Completed:** 2026-02-14
**Commit:** 466de1f

- [x] Add store dependency
- [x] Store reported variable/component data
- [x] Update `manager/handlers/ocpp201/notify_report.go`
- [x] Update `manager/handlers/ocpp201/notify_report_test.go`

**Store Requirements:**
- **Interface:** `StoreChargeStationReport(ctx, chargeStationId string, requestId int, reportData []ReportDataType) error`
- **PostgreSQL/Firestore/In-Memory:** New methods in each backend

---

#### Task 1.3: GetBaseReport - Meaningful CallResult Processing
**Status:** ✅ Complete
**Complexity:** Low
**Completed:** 2026-02-14
**Commit:** 3e2b4fb

- [x] Track pending report requests
- [x] Update `manager/handlers/ocpp201/get_base_report_result.go`
- [x] Update test

**Implementation:**
- Added `ReportRequestStatus` enum and `ChargeStationReportRequest` type to store
- Added `UpdateReportRequestStatus` method to `ChargeStationReportStore` interface
- Implemented in PostgreSQL (migration 000013), Firestore, and In-Memory stores
- Handler now persists request status (Accepted/Rejected/NotSupported/EmptyResultSet)
- Updated tests to verify status persistence

---

#### Task 1.4: GetVariables - Store Retrieved Values
**Status:** ✅ Complete
**Complexity:** Low
**Completed:** 2026-02-14
**Commit:** 49b845f

- [x] Store retrieved variable values
- [x] Update `manager/handlers/ocpp201/get_variables_result.go`
- [x] Update test

**Store Requirements:**
- **Interface:** `ChargeStationVariableStore.StoreVariableValues(ctx, values []VariableValue) error`
- **PostgreSQL:** Migration 000014 + InsertVariableValue query
- **Firestore:** Subcollection ChargeStation/{id}/Variables
- **In-Memory:** New map in store

**Implementation:**
- Added ChargeStationVariableStore interface to store.Engine
- Handler now stores component/variable metadata with attribute type, value, and status
- Supports EVSE and connector scoping
- Updated tests to verify persistence

---

#### Task 1.5: Reset - Track Reset Status
**Status:** ✅ Complete
**Complexity:** Low
**Completed:** 2026-02-14
**Commit:** b3d3dcd

- [x] Log reset acceptance/rejection meaningfully
- [x] Update `manager/handlers/ocpp201/reset_result.go`

**Implementation:**
- Added structured logging with slog
- Info level for Accepted/Scheduled, Warn level for Rejected
- Logs StatusInfo details (reason_code, additional_info) when present
- Added comprehensive test coverage for all reset statuses

---

### Module 1 Completion Checklist
- [x] All Provisioning handlers store meaningful data
- [x] Unit tests updated
- [x] Create PR: `feature/ocpp201-provisioning` → `main` (MR !1: https://gitlab.com/evorada/maeve-csms/-/merge_requests/1)
- [ ] Merge to main

---

## Module 2: MeterValues (Critical Gap) 🔥

**Branch:** `feature/ocpp201-meter-values`
**Priority:** Critical
**Status:** ✅ Complete (1/1)
**Complexity:** Medium

### Task 2.1: MeterValues - Add Storage
**Status:** ✅ Complete
**Complexity:** Medium
**Completed:** 2026-02-17

**Current:** Only traces EVSE ID. Meter data is discarded.

- [x] Add store dependency (`StoreMeterValues`)
- [x] Parse and store `MeterValue` data (sampled values, measurands, phases, units)
- [x] Persist meter values per charge station/EVSE with backend support (PostgreSQL, Firestore, In-Memory)
- [x] Resolve and attach active transaction IDs when present
- [x] Update `manager/handlers/ocpp201/meter_values.go`
- [x] Update `manager/handlers/ocpp201/meter_values_test.go`
- [x] Update routing in `routing.go`

**Store Requirements:**
- **Interface:** `StoreMeterValues(ctx, chargeStationId string, evseId int, transactionId string, meterValues []MeterValueType) error`
- **PostgreSQL:** Migration for meter_values table, sqlc queries
- **Firestore:** New subcollection under charge station
- **In-Memory:** New map in store

---

### Module 2 Completion Checklist
- [x] MeterValues stored with full fidelity
- [x] Unit tests
- [x] Create MR: `feature/ocpp201-meter-values` → `main` (MR !4: https://gitlab.com/evorada/maeve-csms/-/merge_requests/4)
- [ ] Merge to main

---

## Module 3: Remote Control 🔥

**Branch:** `feature/ocpp201-remote-control`
**Priority:** Critical
**Status:** 📋 Not Started (0/3 fully implemented)
**Complexity:** Low

All three handlers exist as CallResult-only. The CallMaker can already initiate these. Just need meaningful result processing.

### Task 3.1: RequestStartTransaction - Track Result
**Status:** ⚠️ Partial → ✅
**Complexity:** Low

- [ ] Store remote start result (transaction ID mapping)
- [ ] Update `manager/handlers/ocpp201/request_start_transaction_result.go`
- [ ] Update test

---

### Task 3.2: RequestStopTransaction - Track Result
**Status:** ⚠️ Partial → ✅
**Complexity:** Low

- [ ] Store remote stop result
- [ ] Update `manager/handlers/ocpp201/request_stop_transaction_result.go`
- [ ] Update test

---

### Task 3.3: UnlockConnector - Track Result
**Status:** ⚠️ Partial → ✅
**Complexity:** Low

- [ ] Already functional as trace-only; optionally persist
- [ ] Update `manager/handlers/ocpp201/unlock_connector_result.go`

---

### Module 3 Completion Checklist
- [ ] All 3 Remote Control handlers upgraded
- [ ] Create PR → Merge

---

## Module 4: Transaction Completion

**Branch:** `feature/ocpp201-transaction`
**Priority:** High
**Status:** ✅ Complete (1/1)
**Complexity:** Medium

### Task 4.1: CostUpdated Handler (New)
**Status:** ❌ Missing
**Complexity:** Medium

- [ ] Create `manager/handlers/ocpp201/cost_updated_result.go`
- [ ] Create OCPP types: `manager/ocpp/ocpp201/cost_updated_request.go`, `cost_updated_response.go`
- [ ] Add CallResult route in `routing.go`
- [ ] Add to CallMaker actions
- [ ] Write unit tests

**Store Requirements:** Track cost updates per transaction

---

### Module 4 Completion Checklist
- [ ] CostUpdated handler implemented
- [ ] Create PR → Merge

---

## Module 5: Smart Charging

**Branch:** `feature/ocpp201-smart-charging`
**Priority:** High
**Status:** 📋 Not Started (0/9)
**Complexity:** High

### Task 5.0: ChargingProfileStore
**Status:** Not Started
**Complexity:** High

- [ ] Define `ChargingProfileStore` interface in `manager/store/`
- [ ] Implement for PostgreSQL, Firestore, In-Memory
- [ ] Add to `Engine` interface

---

### Task 5.1: SetChargingProfile (CSMS→CS)
**Complexity:** High
- [ ] Create `manager/handlers/ocpp201/set_charging_profile_result.go`
- [ ] Add types if missing
- [ ] Add to routing + CallMaker
- [ ] Write tests

---

### Task 5.2: GetChargingProfiles (CSMS→CS)
**Complexity:** Medium
- [ ] Create handler
- [ ] Add to routing + CallMaker
- [ ] Write tests

---

### Task 5.3: GetCompositeSchedule (CSMS→CS)
**Complexity:** High
- [ ] Create handler
- [ ] Implement composite schedule calculation
- [ ] Write tests

---

### Task 5.4: ClearChargingProfile (CSMS→CS)
**Complexity:** Low
- [ ] Create handler
- [ ] Write tests

---

### Task 5.5: ClearedChargingLimit (CS→CSMS)
**Complexity:** Low
- [ ] Create `manager/handlers/ocpp201/cleared_charging_limit.go`
- [ ] Add Call route in routing
- [ ] Write tests

---

### Task 5.6: NotifyChargingLimit (CS→CSMS)
**Complexity:** Low
- [ ] Create handler + Call route
- [ ] Write tests

---

### Task 5.7: NotifyEVChargingNeeds (CS→CSMS)
**Complexity:** Medium
- [ ] Create handler + Call route
- [ ] Write tests

---

### Task 5.8: NotifyEVChargingSchedule (CS→CSMS)
**Complexity:** Low
- [ ] Create handler + Call route
- [ ] Write tests

---

### Task 5.9: ReportChargingProfiles (CS→CSMS)
**Complexity:** Medium
- [ ] Create handler + Call route
- [ ] Store reported profiles
- [ ] Write tests

---

### Module 5 Completion Checklist
- [ ] ChargingProfileStore implemented for all 3 backends
- [ ] All 9 Smart Charging handlers
- [ ] Create PR → Merge

---

## Module 6: Availability

**Branch:** `feature/ocpp201-availability`
**Priority:** Medium
**Status:** 📋 Not Started (1/3)
**Complexity:** Low-Medium

### Task 6.1: ChangeAvailability - Upgrade
**Status:** ⚠️ Partial → ✅
**Complexity:** Low
- [ ] Optionally persist availability state
- [ ] Update `change_availability_result.go`

---

### Task 6.2: CustomerInformation (CSMS→CS, New)
**Status:** ❌ Missing
**Complexity:** Medium
- [ ] Create types + handler + routing
- [ ] Write tests

---

### Module 6 Completion Checklist
- [ ] All Availability handlers complete
- [ ] Create PR → Merge

---

## Module 7: Firmware Management

**Branch:** `feature/ocpp201-firmware-management`
**Priority:** Medium
**Status:** 📋 Not Started (0/4)
**Complexity:** High

### Task 7.1: FirmwareStatusNotification - Add Persistence
**Complexity:** Low
- [ ] Store firmware update status
- [ ] Update existing handler

---

### Task 7.2: UpdateFirmware (CSMS→CS, New)
**Complexity:** Medium
- [ ] Create handler + types + routing + CallMaker
- [ ] Write tests

---

### Task 7.3: PublishFirmware (CSMS→CS, New)
**Complexity:** Medium
- [ ] Create handler + types + routing
- [ ] Write tests

---

### Task 7.4: PublishFirmwareStatusNotification (CS→CSMS, New)
**Complexity:** Low
- [ ] Create handler + Call route
- [ ] Write tests

---

### Task 7.5: UnpublishFirmware (CSMS→CS, New)
**Complexity:** Low
- [ ] Create handler + routing
- [ ] Write tests

---

**Store Requirements:**
- **Interface:** `FirmwareStore` for tracking firmware update/publish status
- **PostgreSQL/Firestore/In-Memory:** New methods

---

### Module 7 Completion Checklist
- [ ] All Firmware handlers
- [ ] Create PR → Merge

---

## Module 8: Diagnostics & Monitoring

**Branch:** `feature/ocpp201-diagnostics`
**Priority:** Medium
**Status:** 📋 Not Started (0/8)
**Complexity:** High

### Task 8.1: LogStatusNotification - Add Persistence
**Complexity:** Low
- [ ] Store log upload status

---

### Task 8.2: GetLog (CSMS→CS, New)
**Complexity:** Medium
- [ ] Create handler + types + routing + CallMaker

---

### Task 8.3: GetMonitoringReport (CSMS→CS, New)
**Complexity:** Medium
- [ ] Create handler + types + routing + CallMaker

---

### Task 8.4: SetMonitoringBase (CSMS→CS, New)
**Complexity:** Low
- [ ] Create handler + routing + CallMaker

---

### Task 8.5: SetMonitoringLevel (CSMS→CS, New)
**Complexity:** Low
- [ ] Create handler + routing + CallMaker

---

### Task 8.6: SetVariableMonitoring (CSMS→CS, New)
**Complexity:** Medium
- [ ] Create handler + routing + CallMaker

---

### Task 8.7: ClearVariableMonitoring (CSMS→CS, New)
**Complexity:** Low
- [ ] Create handler + routing + CallMaker

---

### Task 8.8: NotifyMonitoringReport (CS→CSMS, New)
**Complexity:** Medium
- [ ] Create handler + Call route
- [ ] Store monitoring report data

---

### Task 8.9: NotifyEvent (CS→CSMS, New)
**Complexity:** Medium
- [ ] Create handler + Call route
- [ ] Store event data

---

### Task 8.10: NotifyCustomerInformation (CS→CSMS, New)
**Complexity:** Low
- [ ] Create handler + Call route

---

**Store Requirements:**
- **Interface:** `MonitoringStore` for variable monitoring configs and reports
- **PostgreSQL/Firestore/In-Memory:** New methods

---

### Module 8 Completion Checklist
- [ ] All Diagnostics handlers
- [ ] Create PR → Merge

---

## Module 9: Display Message

**Branch:** `feature/ocpp201-display-message`
**Priority:** Low
**Status:** ✅ Complete (4/4)
**Complexity:** Medium

### Task 9.1: SetDisplayMessage (CSMS→CS, New)
**Status:** ✅ Complete
**Complexity:** Medium
**Completed:** 2026-02-17
- [x] Create `SetDisplayMessageRequestJson` / `SetDisplayMessageResponseJson` types (+ enums)
- [x] Implement `SetDisplayMessageResultHandler`
- [x] Register in `routing.go` CallResultRoutes + CallMaker Actions
- [x] Add unit tests (`set_display_message_result_test.go`) + routing coverage updates

---

### Task 9.2: GetDisplayMessages (CSMS→CS, New)
**Status:** ✅ Complete
**Complexity:** Medium
**Completed:** 2026-02-16
- [x] Create `GetDisplayMessagesRequestJson` / `GetDisplayMessagesResponseJson` types
- [x] Implement `GetDisplayMessagesResultHandler` with trace attributes for request criteria/status
- [x] Register in `routing.go` CallResultRoutes + CallMaker Actions
- [x] Add unit tests (`get_display_messages_result_test.go`)

---

### Task 9.3: ClearDisplayMessage (CSMS→CS, New)
**Status:** ✅ Complete
**Complexity:** Low
**Completed:** 2026-02-16
- [x] Create `ClearDisplayMessageRequestJson` / `ClearDisplayMessageResponseJson` types (+ enums)
- [x] Implement `ClearDisplayMessageResultHandler`
- [x] Register in `routing.go` CallResultRoutes + CallMaker Actions
- [x] Add unit tests (`clear_display_message_result_test.go`) + routing coverage updates

---

### Task 9.4: NotifyDisplayMessages (CS→CSMS, New)
**Status:** ✅ Complete
**Complexity:** Low
**Completed:** 2026-02-17
- [x] Create `NotifyDisplayMessagesRequestJson` / `NotifyDisplayMessagesResponseJson` types
- [x] Implement `NotifyDisplayMessagesHandler` with trace attributes for request fragments
- [x] Register in `routing.go` CallRoutes
- [x] Add unit tests (`notify_display_messages_test.go`)

---

**Store Requirements:**
- **Interface:** `DisplayMessageStore` for message management
- **PostgreSQL/Firestore/In-Memory:** New methods

---

### Module 9 Completion Checklist
- [x] All DisplayMessage handlers
- [ ] Create PR → Merge

---

## Module 10: Local Auth List

**Branch:** `feature/ocpp201-local-auth-list`
**Priority:** Low
**Status:** 🚧 In Progress (1/2 fully implemented)
**Complexity:** Medium

### Task 10.1: GetLocalListVersion - Upgrade
**Status:** ✅ Complete
**Complexity:** Low
**Completed:** 2026-02-17
- [x] Store/track list version per charge station
- [x] Update `get_local_list_version_result.go`
- [x] Update `get_local_list_version_result_test.go`
- [x] Update routing to inject LocalAuthListStore

**Implementation:**
- Handler now persists reported local auth list version per charge station via `LocalAuthListStore`
- Uses `UpdateLocalAuthList(..., Differential, nil)` to safely update version without mutating entries
- Added test coverage to verify version persistence and trace attributes

---

### Task 10.2: SendLocalList - Upgrade
**Status:** ✅ Complete
**Complexity:** Medium
**Completed:** 2026-02-17
- [x] Track list sync status
- [x] Update `send_local_list_result.go`
- [x] Update `send_local_list_result_test.go`
- [x] Update routing to inject LocalAuthListStore

**Implementation:**
- Handler now persists full local auth list payload when `SendLocalList` is accepted
- Maps OCPP id token / id token info into `store.LocalAuthListEntry` records
- Keeps trace attributes and ignores non-accepted statuses without mutating store
- Added tests covering accepted persistence and rejected/no-op behavior

---

**Store Requirements:**
- **Interface:** `LocalAuthListStore` - version tracking per charge station
- **PostgreSQL/Firestore/In-Memory:** New methods

---

### Module 10 Completion Checklist
- [x] Both LocalAuthList handlers upgraded
- [ ] Create PR → Merge

---

## Module 11: DataTransfer

**Branch:** `feature/ocpp201-data-transfer`
**Priority:** Low
**Status:** 📋 Not Started (0/1)
**Complexity:** Medium

### Task 11.1: DataTransfer Handler (New)
**Complexity:** Medium

- [ ] Create `manager/handlers/ocpp201/data_transfer.go`
- [ ] Support bidirectional DataTransfer
- [ ] Create extensible vendor routing (similar to OCPP 1.6 pattern)
- [ ] Add Call route + CallResult route in `routing.go`
- [ ] Write tests

**Implementation:**
- Added OCPP 2.0.1 `DataTransferRequestJson` / `DataTransferResponseJson` types with `DataTransferStatusEnumType`
- Implemented `DataTransferHandler` for CS→CSMS Call routing by `vendorId + messageId`
- Implemented `DataTransferResultHandler` for CSMS→CS CallResult routing by `vendorId + messageId`
- Wired both routes into `routing.go` and added `DataTransfer` to CallMaker actions
- Added focused tests in `data_transfer_test.go` covering call routing, unknown vendor/message handling, and CallResult routing
- 2026-02-17 follow-up: added missing `DataTransfer` CallResult route registration and routing coverage assertions

---

### Module 11 Completion Checklist
- [ ] DataTransfer handler implemented
- [ ] Create PR → Merge

---

## Module 12: Reservation

**Branch:** `feature/ocpp201-reservation`
**Priority:** Low
**Status:** 📋 Not Started (0/2)
**Complexity:** Medium

### Task 12.1: ReserveNow (CSMS→CS, New)
**Complexity:** Medium
- [ ] Create types + handler + routing + CallMaker
- [ ] Track reservation state

---

### Task 12.2: CancelReservation (CSMS→CS, New)
**Complexity:** Low
- [ ] Create handler + routing + CallMaker

---

### Task 12.3: ReservationStatusUpdate (CS→CSMS, New)
**Complexity:** Low
- [ ] Create handler + Call route

---

**Store Requirements:**
- **Interface:** `ReservationStore` - reservation state management with expiry
- **PostgreSQL/Firestore/In-Memory:** New methods

---

### Module 12 Completion Checklist
- [ ] All Reservation handlers
- [ ] Create PR → Merge

---

## Module 13: Security (Upgrade Existing)

**Branch:** `feature/ocpp201-security`
**Priority:** Medium
**Status:** 📋 Not Started (2/3 fully implemented)
**Complexity:** Low

### Task 13.1: DeleteCertificate - Upgrade
**Complexity:** Low
- [ ] Add store interaction to remove certificate record
- [ ] Update `delete_certificate_result.go`

---

### Task 13.2: GetInstalledCertificateIds - Upgrade
**Complexity:** Low
- [ ] Store returned certificate list
- [ ] Update `get_installed_certificate_ids_result.go`

---

### Module 13 Completion Checklist
- [ ] All Security handlers with store logic
- [ ] Create PR → Merge

---

## Overall Progress Tracking

| Module | Branch | Priority | Messages | Status |
|--------|--------|----------|----------|--------|
| Provisioning | `feature/ocpp201-provisioning` | Critical | 5 to upgrade | 📋 |
| MeterValues | `feature/ocpp201-meter-values` | Critical | 1 to upgrade | ✅ (1/1) |
| Remote Control | `feature/ocpp201-remote-control` | Critical | 3 to upgrade | 📋 |
| Transaction | `feature/ocpp201-transaction` | High | 1 new | 📋 |
| Smart Charging | `feature/ocpp201-smart-charging` | High | 9 new | 📋 |
| Availability | `feature/ocpp201-availability` | Medium | 2 to handle | 📋 |
| Firmware Management | `feature/ocpp201-firmware-management` | Medium | 5 new | 📋 |
| Diagnostics | `feature/ocpp201-diagnostics` | Medium | 10 new | 📋 |
| Display Message | `feature/ocpp201-display-message` | Low | 4 new | 📋 |
| Local Auth List | `feature/ocpp201-local-auth-list` | Low | 2 to upgrade | ✅ (2/2) |
| DataTransfer | `feature/ocpp201-data-transfer` | Low | 1 new | 📋 |
| Reservation | `feature/ocpp201-reservation` | Low | 3 new | 📋 |
| Security | `feature/ocpp201-security` | Medium | 2 to upgrade | 📋 |

---

## Timeline Estimate

| Module | Duration | Priority |
|--------|----------|----------|
| Provisioning + MeterValues + Remote Control | 2-3 weeks | Critical |
| Transaction + Availability | 1 week | High |
| Smart Charging | 4-6 weeks | High |
| Firmware + Diagnostics | 3-4 weeks | Medium |
| Security | 1 week | Medium |
| DisplayMessage + LocalAuthList + DataTransfer + Reservation | 3-4 weeks | Low |
| **TOTAL** | **14-19 weeks** | |

---

## References

- [OCPP 2.0.1 Implementation Audit](ocpp201-implementation-audit.md)
- [OCPP 1.6 Implementation Plan](ocpp16-implementation-plan.md) (format template)
- [OCPP Version Architecture](ocpp-version-architecture.md)
- [Existing OCPP 2.0.1 Handlers](../manager/handlers/ocpp201/)
- [Store Interfaces](../manager/store/)

---

**Created by:** Patricio (AI Assistant)
**Last Updated:** 2026-02-12
