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
**Status:** 📋 Not Started (3/8 fully implemented)
**Complexity:** Medium

### Messages to Upgrade/Implement

#### Task 1.1: StatusNotification - Add Persistence
**Status:** ⚠️ Partial → ✅
**Complexity:** Low

**Current:** Traces connector status but doesn't store it.

- [ ] Add `store.Engine` dependency to handler (convert from function to struct)
- [ ] Create/use store method to persist EVSE/connector status
- [ ] Update `manager/handlers/ocpp201/status_notification.go`
- [ ] Update `manager/handlers/ocpp201/status_notification_test.go`
- [ ] Update routing in `manager/handlers/ocpp201/routing.go`

**Store Requirements:**
- **Interface:** `UpdateConnectorStatus(ctx, chargeStationId string, evseId int, connectorId int, status string) error`
- **PostgreSQL:** `manager/store/postgres/` - new query/method
- **Firestore:** `manager/store/firestore/` - new method
- **In-Memory:** `manager/store/inmemory/` - new method

---

#### Task 1.2: NotifyReport - Add Persistence
**Status:** ⚠️ Partial → ✅
**Complexity:** Medium

- [ ] Add store dependency
- [ ] Store reported variable/component data
- [ ] Update `manager/handlers/ocpp201/notify_report.go`
- [ ] Update `manager/handlers/ocpp201/notify_report_test.go`

**Store Requirements:**
- **Interface:** `StoreChargeStationReport(ctx, chargeStationId string, requestId int, reportData []ReportDataType) error`
- **PostgreSQL/Firestore/In-Memory:** New methods in each backend

---

#### Task 1.3: GetBaseReport - Meaningful CallResult Processing
**Status:** ⚠️ Partial → ✅
**Complexity:** Low

- [ ] Track pending report requests
- [ ] Update `manager/handlers/ocpp201/get_base_report_result.go`
- [ ] Update test

---

#### Task 1.4: GetVariables - Store Retrieved Values
**Status:** ⚠️ Partial → ✅
**Complexity:** Low

- [ ] Store retrieved variable values
- [ ] Update `manager/handlers/ocpp201/get_variables_result.go`
- [ ] Update test

---

#### Task 1.5: Reset - Track Reset Status
**Status:** ⚠️ Partial → ✅
**Complexity:** Low

- [ ] Log reset acceptance/rejection meaningfully
- [ ] Update `manager/handlers/ocpp201/reset_result.go`

---

### Module 1 Completion Checklist
- [ ] All Provisioning handlers store meaningful data
- [ ] Unit tests updated
- [ ] Create PR: `feature/ocpp201-provisioning` → `main`
- [ ] Merge to main

---

## Module 2: MeterValues (Critical Gap) 🔥

**Branch:** `feature/ocpp201-meter-values`
**Priority:** Critical
**Status:** 📋 Not Started
**Complexity:** Medium

### Task 2.1: MeterValues - Add Storage
**Status:** ⚠️ Partial → ✅
**Complexity:** Medium

**Current:** Only traces EVSE ID. Meter data is discarded.

- [ ] Add `store.Engine` dependency
- [ ] Parse and store `MeterValue` data (sampled values, measurands, phases, units)
- [ ] Associate meter values with active transactions
- [ ] Update `manager/handlers/ocpp201/meter_values.go`
- [ ] Update `manager/handlers/ocpp201/meter_values_test.go`
- [ ] Update routing in `routing.go`

**Store Requirements:**
- **Interface:** `StoreMeterValues(ctx, chargeStationId string, evseId int, meterValues []MeterValueType) error`
- **PostgreSQL:** Migration for meter_values table, sqlc queries
- **Firestore:** New subcollection under charge station
- **In-Memory:** New map in store

---

### Module 2 Completion Checklist
- [ ] MeterValues stored with full fidelity
- [ ] Unit tests
- [ ] Create PR → Merge

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
**Status:** 📋 Not Started
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
**Status:** ✅ Complete
**Complexity:** Low
**Completed:** 2026-02-15
- [x] Store firmware update status via store.FirmwareStore
- [x] Update handler to struct with Store dependency
- [x] Update routing.go to inject engine
- [x] 5 unit tests: basic, request_id, installed state, status progression, multiple stations

---

### Task 7.2: UpdateFirmware (CSMS→CS, New)
**Status:** ✅ Complete
**Complexity:** Medium
**Completed:** 2026-02-15
- [x] Create UpdateFirmwareRequestJson/ResponseJson types + FirmwareType in ocpp/ocpp201/
- [x] Create UpdateFirmwareResultHandler with store persistence on Accepted
- [x] Register in routing.go CallResultRoutes + CallMaker Actions
- [x] 5 unit tests: Accepted, Rejected, AcceptedWithRetries, InvalidCertificate, AcceptedInvalidDate

---

### Task 7.3: PublishFirmware (CSMS→CS, New)
**Status:** ✅ Complete
**Complexity:** Medium
**Completed:** 2026-02-15
- [x] Create PublishFirmwareRequestJson/ResponseJson types in ocpp/ocpp201/
- [x] Add PublishFirmwareStatus store types + interface methods (FirmwareStore)
- [x] Implement SetPublishFirmwareStatus/GetPublishFirmwareStatus in inmemory, firestore, postgres stores
- [x] Add postgres migration 000011: publish_firmware_status table + sqlc-generated queries
- [x] Create PublishFirmwareResultHandler with store persistence on Accepted
- [x] Register in routing.go CallResultRoutes + CallMaker Actions
- [x] 5 unit tests: Accepted, Rejected, AcceptedWithRetries, MultipleStations, SpanAttributes
- [x] Fix MockFirmwareStore in ocpp16 tests to implement new interface

---

### Task 7.4: PublishFirmwareStatusNotification (CS→CSMS, New)
**Status:** ✅ Complete
**Complexity:** Low
**Completed:** 2026-02-15
- [x] Create PublishFirmwareStatusNotificationRequestJson with full PublishFirmwareStatusEnumType enum
- [x] Create PublishFirmwareStatusNotificationResponseJson
- [x] Expand store.PublishFirmwareStatusType vars to cover all OCPP 2.0.1 states
- [x] Implement PublishFirmwareStatusNotificationHandler (CS→CSMS Call):
      persists status, preserves existing location/checksum/requestId metadata
- [x] Register in routing.go CallRoutes
- [x] 5 unit tests: Basic, WithRequestId, PublishedWithLocations, PreservesExistingMetadata, MultipleLocalControllers

---

### Task 7.5: UnpublishFirmware (CSMS→CS, New)
**Status:** ✅ Complete
**Complexity:** Low
**Completed:** 2026-02-15
- [x] Create UnpublishFirmwareRequestJson type (checksum field per OCPP 2.0.1 schema)
- [x] Create UnpublishFirmwareResponseJson with UnpublishFirmwareStatusEnumType enum
- [x] Implement UnpublishFirmwareResultHandler with FirmwareStore:
      Unpublished → sets store to Idle; DownloadOngoing/NoFirmware → log only
- [x] Register in routing.go CallResultRoutes + CallMaker Actions
- [x] 5 unit tests: Unpublished, DownloadOngoing, NoFirmware, MultipleStations, SpanAttributes

---

**Store Requirements:**
- **Interface:** `FirmwareStore` for tracking firmware update/publish status
- **PostgreSQL/Firestore/In-Memory:** New methods

---

### Module 7 Completion Checklist
- [x] All Firmware handlers (Tasks 7.1-7.5) ✅
- [ ] Create PR → Merge

---

## Module 8: Diagnostics & Monitoring

**Branch:** `feature/ocpp201-diagnostics`
**Priority:** Medium
**Status:** 🔄 In Progress (4/10)
**Complexity:** High

### Task 8.1: LogStatusNotification - Add Persistence
**Status:** ✅ Complete
**Complexity:** Low
**Completed:** 2026-02-15
- [x] Convert handler to struct with FirmwareStore dependency
- [x] Add mapLogUploadStatus() mapping all 8 UploadLogStatusEnumType values to DiagnosticsStatusType
- [x] Persist status via existing SetDiagnosticsStatus store method
- [x] Update routing.go to inject engine
- [x] 5 unit tests: basic, request_id, upload failure, status progression, multiple stations

---

### Task 8.2: GetLog (CSMS→CS, New)
**Status:** ✅ Complete
**Complexity:** Medium
**Completed:** 2026-02-16
- [x] Create `GetLogRequestJson` / `GetLogResponseJson` types (+ enums)
- [x] Implement `GetLogResultHandler` with diagnostics status persistence on Accepted/AcceptedCanceled
- [x] Register in `routing.go` CallResultRoutes + CallMaker Actions
- [x] Add unit tests (`get_log_result_test.go`) + routing coverage updates

---

### Task 8.3: GetMonitoringReport (CSMS→CS, New)
**Status:** ✅ Complete
**Complexity:** Medium
**Completed:** 2026-02-16
- [x] Create `GetMonitoringReportRequestJson` / `GetMonitoringReportResponseJson` types
- [x] Implement `GetMonitoringReportResultHandler`
- [x] Register in `routing.go` CallResultRoutes + CallMaker Actions
- [x] Add unit tests (`get_monitoring_report_result_test.go`) + routing coverage updates

---

### Task 8.4: SetMonitoringBase (CSMS→CS, New)
**Status:** ✅ Complete
**Complexity:** Low
**Completed:** 2026-02-16
- [x] Create `SetMonitoringBaseRequestJson` / `SetMonitoringBaseResponseJson` types
- [x] Implement `SetMonitoringBaseResultHandler`
- [x] Register in `routing.go` CallResultRoutes + CallMaker Actions
- [x] Add unit tests (`set_monitoring_base_result_test.go`) + routing coverage updates

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
**Status:** 📋 Not Started (0/3)
**Complexity:** Medium

### Task 9.1: SetDisplayMessage (CSMS→CS, New)
**Complexity:** Medium
- [ ] Create types + handler + routing + CallMaker

---

### Task 9.2: GetDisplayMessages (CSMS→CS, New)
**Complexity:** Medium
- [ ] Create handler + routing + CallMaker

---

### Task 9.3: ClearDisplayMessage (CSMS→CS, New)
**Complexity:** Low
- [ ] Create handler + routing + CallMaker

---

### Task 9.4: NotifyDisplayMessages (CS→CSMS, New)
**Complexity:** Low
- [ ] Create handler + Call route

---

**Store Requirements:**
- **Interface:** `DisplayMessageStore` for message management
- **PostgreSQL/Firestore/In-Memory:** New methods

---

### Module 9 Completion Checklist
- [ ] All DisplayMessage handlers
- [ ] Create PR → Merge

---

## Module 10: Local Auth List

**Branch:** `feature/ocpp201-local-auth-list`
**Priority:** Low
**Status:** 📋 Not Started (0/2 fully implemented)
**Complexity:** Medium

### Task 10.1: GetLocalListVersion - Upgrade
**Complexity:** Low
- [ ] Store/track list version per charge station
- [ ] Update `get_local_list_version_result.go`

---

### Task 10.2: SendLocalList - Upgrade
**Complexity:** Medium
- [ ] Track list sync status
- [ ] Update `send_local_list_result.go`

---

**Store Requirements:**
- **Interface:** `LocalAuthListStore` - version tracking per charge station
- **PostgreSQL/Firestore/In-Memory:** New methods

---

### Module 10 Completion Checklist
- [ ] Both LocalAuthList handlers upgraded
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
| MeterValues | `feature/ocpp201-meter-values` | Critical | 1 to upgrade | 📋 |
| Remote Control | `feature/ocpp201-remote-control` | Critical | 3 to upgrade | 📋 |
| Transaction | `feature/ocpp201-transaction` | High | 1 new | 📋 |
| Smart Charging | `feature/ocpp201-smart-charging` | High | 9 new | 📋 |
| Availability | `feature/ocpp201-availability` | Medium | 2 to handle | 📋 |
| Firmware Management | `feature/ocpp201-firmware-management` | Medium | 5 new | 📋 |
| Diagnostics | `feature/ocpp201-diagnostics` | Medium | 10 new | 🔄 (4/10) |
| Display Message | `feature/ocpp201-display-message` | Low | 4 new | 📋 |
| Local Auth List | `feature/ocpp201-local-auth-list` | Low | 2 to upgrade | 📋 |
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
