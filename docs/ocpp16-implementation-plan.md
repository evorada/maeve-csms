# OCPP 1.6 Implementation Plan

**Project:** MaEVe CSMS OCPP 1.6 Completion  
**Created:** 2026-02-05  
**Updated:** 2026-02-05  
**Status:** 🚧 In Progress  

---

## Overview

Based on the [OCPP 1.6 Implementation Audit](ocpp16-implementation-audit.md), this plan implements the missing OCPP 1.6 messages organized by OCPP profile/module. Each module will be implemented in its own feature branch and merged independently.

**Current Overall Coverage:** 36% (15/42 messages implemented)

---

## Implementation Strategy

### Approach
- **Module-Based:** One OCPP profile/module at a time
- **Independent Branches:** Each module gets its own feature branch
- **Incremental Merges:** Merge each module to main when complete
- **Test-Driven:** Unit tests for each handler
- **Follow Patterns:** Match existing handler structure

### Branch Naming Convention
```
feature/ocpp16-core-profile
feature/ocpp16-smart-charging
feature/ocpp16-remote-trigger
feature/ocpp16-firmware-management
feature/ocpp16-local-auth-list
feature/ocpp16-reservation
feature/ocpp16-security-extensions
```

### Merge Strategy
1. Complete all handlers in a module
2. Write comprehensive tests
3. Create PR from module branch → main
4. Code review
5. Merge to main
6. Start next module branch from updated main

---

## Module 1: Core Profile (Mandatory) 🔥

**Branch:** `feature/ocpp16-core-profile`  
**Priority:** Critical  
**Status:** 🚧 In Progress (9/16 complete - 56%)  
**Timeline:** 2-3 weeks  
**Impact:** Essential CSMS functionality  

### Current Status

✅ **Implemented (9):**
1. Authorize
2. BootNotification
3. DataTransfer
4. Heartbeat
5. MeterValues
6. StartTransaction
7. StatusNotification
8. StopTransaction
9. SecurityEventNotification (from Security Extensions)

🚧 **In Progress (0):**
- None currently

❌ **Missing (7):**
10. ChangeAvailability
11. ChangeConfiguration (Call handler needed, CallResult exists)
12. ClearCache
13. GetConfiguration
14. RemoteStartTransaction
15. RemoteStopTransaction
16. Reset
17. TriggerMessage (Call handler needed, CallResult exists)
18. UnlockConnector

### Implementation Tasks

#### Task 1.1: Reset Handler ✅
**Status:** Complete  
**Files:**
- `manager/handlers/ocpp16/reset.go`
- `manager/handlers/ocpp16/reset_test.go`
- Updated `routing.go`

**Commit:** `feat(ocpp16): Add Reset handler`

---

#### Task 1.2: UnlockConnector Handler ✅
**Status:** Complete  
**Files:**
- `manager/handlers/ocpp16/unlock_connector.go`
- `manager/handlers/ocpp16/unlock_connector_test.go`
- Updated `routing.go`

**Commit:** `feat(ocpp16): Add UnlockConnector handler`

---

#### Task 1.3: ClearCache Handler ✅
**Status:** Complete  
**Files:**
- `manager/handlers/ocpp16/clear_cache.go`
- `manager/handlers/ocpp16/clear_cache_test.go`
- Updated `routing.go`

**Commit:** `feat(ocpp16): Add ClearCache handler`

---

#### Task 1.4: ChangeAvailability Handler ✅
**Status:** Complete  
**Files:**
- `manager/handlers/ocpp16/change_availability.go`
- `manager/handlers/ocpp16/change_availability_test.go`
- Updated `routing.go`

**Commit:** `feat(ocpp16): Add ChangeAvailability handler`

---

#### Task 1.5: ChangeConfiguration Call Handler ✅
**Status:** Complete  
**Note:** CallResult handler already exists, added Call routing

**Files:**
- `manager/handlers/ocpp16/change_configuration_result_test.go` (enhanced)
- Updated `routing.go`

**Commit:** `feat(ocpp16): Add ChangeConfiguration Call handler`

---

#### Task 1.6: TriggerMessage Call Handler ✅
**Status:** Complete  
**Note:** CallResult handler already exists, added Call routing

**Files:**
- Updated `routing.go`

**Commit:** `feat(ocpp16): Add TriggerMessage Call handler`

---

#### Task 1.7: GetConfiguration Handler ✅
**Status:** Complete  
**Dependencies:** `ChargeStationSettingsStore`

**Files:**
- `manager/handlers/ocpp16/get_configuration.go`
- `manager/handlers/ocpp16/get_configuration_test.go`
- Updated `routing.go`

**Commit:** `feat(ocpp16): Add GetConfiguration handler`

---

#### Task 1.8: RemoteStartTransaction Handler ✅
**Status:** Complete  
**Dependencies:** `TokenStore`

**Files:**
- `manager/handlers/ocpp16/remote_start_transaction.go`
- `manager/handlers/ocpp16/remote_start_transaction_test.go`
- Updated `routing.go`

**Commit:** `feat(ocpp16): Add RemoteStartTransaction handler`

---

#### Task 1.9: RemoteStopTransaction Handler ✅
**Status:** Complete  
**Dependencies:** `TransactionStore`

**Files:**
- `manager/handlers/ocpp16/remote_stop_transaction.go`
- `manager/handlers/ocpp16/remote_stop_transaction_test.go`
- Updated `routing.go`

**Commit:** `feat(ocpp16): Add RemoteStopTransaction handler`

---

### Module 1 Completion Checklist

- [x] All 9 Core Profile handlers implemented
- [x] Unit tests for all handlers
- [ ] Integration tests with charge station simulator
- [ ] Update README.md with Core Profile features
- [ ] Create PR: `feature/ocpp16-core-profile` → `main`
- [ ] Code review
- [ ] Merge to main

---

## Module 2: Remote Trigger Profile

**Branch:** `feature/ocpp16-remote-trigger`  
**Priority:** High  
**Status:** ✅ Complete (2/2 complete - 100%)  
**Timeline:** 1 week  
**Base:** main (after Core Profile merge)  

### Messages to Implement

#### Task 2.1: TriggerMessage (Complete Call Handler) ✅
**Status:** Complete  
**Note:** CallResult handler exists, Call routing added in Core Profile

**Already Done:**
- [x] CallResult handler exists
- [x] Call routing added in Core Profile

**Additional Work:**
- [x] Comprehensive integration tests
- [x] Support all trigger message types

---

#### Task 2.2: ExtendedTriggerMessage Handler ✅
**Status:** Complete

**Implementation:**
- [x] Create `manager/handlers/ocpp16/extended_trigger_message_result.go`
- [x] Implement `ExtendedTriggerMessageResultHandler` struct
- [x] Add `HandleCallResult` method
- [x] Support extended message types (BootNotification, LogStatusNotification, FirmwareStatusNotification, Heartbeat, MeterValues, SignChargePointCertificate, StatusNotification)
- [x] Add routing in `routing.go`
- [x] Add action mapping in `router.go`
- [x] Write unit test `extended_trigger_message_result_test.go`

**Types Created:**
- `manager/ocpp/ocpp16/extended_trigger_message.go`
- `manager/ocpp/ocpp16/extended_trigger_message_response.go`

**Commit:** `feat(ocpp16): Add ExtendedTriggerMessage handler`

---

### Module 2 Completion Checklist

- [x] All 2 Remote Trigger handlers implemented
- [x] Unit tests for all handlers
- [x] Integration tests
- [ ] Update README.md
- [ ] Create PR: `feature/ocpp16-remote-trigger` → `main`
- [ ] Merge to main

---

## Module 3: Smart Charging Profile

**Branch:** `feature/ocpp16-smart-charging`  
**Priority:** Medium  
**Status:** 📋 Not Started (0/3 complete - 0%)  
**Timeline:** 4-6 weeks  
**Base:** main (after Remote Trigger merge)  
**Complexity:** High (requires ChargingProfileStore)  

### Messages to Implement

#### Task 3.0: ChargingProfileStore — Data Store Implementation ✅
**Status:** Complete  
**Complexity:** High  
**Priority:** Must be done before any Module 3 handlers

**Store Interface** (`manager/store/charging_profile.go`):
- [x] Define `ChargingProfile` struct: `ProfileId` (int), `StackLevel` (int), `ChargingProfilePurpose` (enum: TxProfile/TxDefaultProfile/ChargePointMaxProfile), `ChargingProfileKind` (enum: Absolute/Relative/Recurring), `RecurrencyKind` (enum: Daily/Weekly, optional), `ValidFrom`/`ValidTo` (time), `ChargingSchedule` (struct with `ChargingRateUnit`, `Duration`, `StartSchedule`, `MinChargingRate`, `ChargingSchedulePeriod[]` with `StartPeriod`, `Limit`, `NumberPhases`)
- [x] Define `ChargingProfileStore` interface:
  - `SetChargingProfile(ctx, profile *ChargingProfile) error`
  - `GetChargingProfiles(ctx, chargeStationId string, connectorId *int, purpose *ChargingProfilePurpose, stackLevel *int) ([]*ChargingProfile, error)`
  - `ClearChargingProfile(ctx, chargeStationId string, profileId *int, connectorId *int, purpose *ChargingProfilePurpose, stackLevel *int) (int, error)`
  - `GetCompositeSchedule(ctx, chargeStationId string, connectorId int, duration int, chargingRateUnit *ChargingRateUnit) (*ChargingSchedule, error)`
- [x] Add `ChargingProfileStore` to `Engine` interface in `manager/store/engine.go`

**PostgreSQL** (`manager/store/postgres/`):
- [x] Create migration `000007_create_charging_profiles.up.sql` / `.down.sql`
- [x] Create SQL queries in `queries/charging_profiles.sql`
- [x] Generate sqlc code
- [x] Implement `ChargingProfileStore` methods in `charging_profiles.go`
- [ ] Write tests in `charging_profiles_test.go` (requires PostgreSQL instance)

**Firestore** (`manager/store/firestore/`):
- [x] Implement `ChargingProfileStore` methods in `charging_profile.go`
- [x] Write tests in `charging_profile_test.go`

**In-Memory** (`manager/store/inmemory/`):
- [x] Implement `ChargingProfileStore` methods in `charging_profile.go`
- [x] Write tests in `charging_profile_test.go`

**Commit:** `feat(store): Add ChargingProfileStore for smart charging`

---

#### Task 3.1: SetChargingProfile Handler ✅
**Status:** Complete  
**Complexity:** High  
**Dependencies:** Task 3.0 (ChargingProfileStore)

**Implementation:**
- [x] Create `manager/handlers/ocpp16/set_charging_profile.go`
- [x] Create OCPP 1.6 types: `manager/ocpp/ocpp16/set_charging_profile.go`, `set_charging_profile_response.go`
- [x] Implement profile conversion from OCPP types to store types
- [x] Handle ChargingProfilePurpose (TxProfile, TxDefaultProfile, ChargePointMaxProfile)
- [x] Store profile on Accepted response, skip on Rejected/NotSupported
- [x] Handle optional fields (ValidFrom, ValidTo, RecurrencyKind, TransactionId, StartSchedule, Duration, MinChargingRate, NumberPhases)
- [x] Add routing in `routing.go` (CallResult route + CallMaker action)
- [x] Write unit tests (`set_charging_profile_test.go`): accepted/rejected/not-supported, full profile conversion, invalid date handling, profile replacement
- [ ] Write integration tests

**Commit:** `feat(ocpp16): Add SetChargingProfile handler`

---

#### Task 3.2: GetCompositeSchedule Handler ✅
**Status:** Complete  
**Complexity:** High  
**Dependencies:** `ChargingProfileStore` (from Task 3.1)

**Implementation:**
- [x] Create `manager/handlers/ocpp16/get_composite_schedule.go`
- [x] Create OCPP 1.6 types: `manager/ocpp/ocpp16/get_composite_schedule.go`, `get_composite_schedule_response.go`
- [x] Handle accepted/rejected responses with tracing and logging
- [x] Support optional ChargingRateUnit in request
- [x] Log composite schedule details (rate unit, period count)
- [x] Add routing in `routing.go` (CallResult route + CallMaker action)
- [x] Write unit tests (`get_composite_schedule_test.go`): accepted with W/A, rejected, connector 0, with/without schedule body, schedule start/duration/min charging rate
- [ ] Write integration tests

**Commit:** `feat(ocpp16): Add GetCompositeSchedule handler`

---

#### Task 3.3: ClearChargingProfile Handler ✅
**Status:** Complete  
**Complexity:** Medium  
**Dependencies:** `ChargingProfileStore` (from Task 3.1)

**Implementation:**
- [x] Create `manager/handlers/ocpp16/clear_charging_profile.go`
- [x] Handle optional filters (profileId, connectorId, purpose, stack level)
- [x] Clear matching profiles
- [x] Return cleared count
- [x] Write unit tests
- [ ] Write integration tests

**Commit:** `feat(ocpp16): Add ClearChargingProfile handler`

---

### Module 3 Completion Checklist

- [ ] ChargingProfileStore interface designed and implemented
- [ ] All 3 Smart Charging handlers implemented
- [ ] Unit tests for all handlers
- [ ] Integration tests with profile stacking scenarios
- [ ] Load balancing examples/documentation
- [ ] Update README.md with Smart Charging features
- [ ] Create PR: `feature/ocpp16-smart-charging` → `main`
- [ ] Merge to main

---

## Module 4: Firmware Management Profile

**Branch:** `feature/ocpp16-firmware-management`  
**Priority:** Medium  
**Status:** ✅ Complete (6/6 complete - 100%)  
**Timeline:** 3-4 weeks  
**Base:** main (after Smart Charging merge)  
**Complexity:** High (requires file transfer infrastructure)  

### Messages to Implement

#### Task 4.0: FirmwareStore — Data Store Implementation ✅
**Status:** Complete  
**Priority:** Must be done before Module 4 handlers

**Store Interface** (`manager/store/firmware.go`):
- [x] Define `FirmwareUpdateStatus` struct: `ChargeStationId`, `Status` (enum: Downloading/Downloaded/InstallationFailed/Installing/Installed/Idle), `Location` (URL string), `RetrieveDate` (time), `RetryCount` (int), `UpdatedAt` (time)
- [x] Define `DiagnosticsStatus` struct: `ChargeStationId`, `Status` (enum: Idle/Uploaded/UploadFailed/Uploading), `Location` (URL string), `UpdatedAt` (time)
- [x] Define `FirmwareStore` interface:
  - `SetFirmwareUpdateStatus(ctx, chargeStationId string, status *FirmwareUpdateStatus) error`
  - `GetFirmwareUpdateStatus(ctx, chargeStationId string) (*FirmwareUpdateStatus, error)`
  - `SetDiagnosticsStatus(ctx, chargeStationId string, status *DiagnosticsStatus) error`
  - `GetDiagnosticsStatus(ctx, chargeStationId string) (*DiagnosticsStatus, error)`
- [x] Add `FirmwareStore` to `Engine` interface in `manager/store/engine.go`

**PostgreSQL** (`manager/store/postgres/`):
- [x] Create migration `000007_create_firmware_status.up.sql` / `.down.sql`
- [x] Create SQL queries in `queries/firmware.sql`
- [x] Generate sqlc code
- [x] Implement `FirmwareStore` methods in `firmware.go`
- [x] Write tests

**Firestore** (`manager/store/firestore/`):
- [x] Implement `FirmwareStore` methods in `firmware.go`
- [x] Write tests

**In-Memory** (`manager/store/inmemory/`):
- [x] Implement `FirmwareStore` methods in `store.go`
- [x] Write tests

**Commit:** `feat(store): Add FirmwareStore for firmware and diagnostics tracking`

---

#### Task 4.1: UpdateFirmware Handler ✅
**Status:** Complete

**Implementation:**
- [x] Create `manager/handlers/ocpp16/update_firmware.go`
- [x] Create OCPP types `manager/ocpp/ocpp16/update_firmware.go` and `update_firmware_response.go`
- [x] Implement firmware status tracking via FirmwareStore
- [x] Add retry count handling
- [x] Add routing in `routing.go` and action mapping in CallMaker
- [x] Write unit tests (`update_firmware_test.go`) — 5 test cases

**Commit:** `feat(ocpp16): Add UpdateFirmware handler`

---

#### Task 4.2: FirmwareStatusNotification Handler ✅
**Status:** Complete

**Implementation:**
- [x] Create `manager/handlers/ocpp16/firmware_status_notification.go`
- [x] Track firmware update status
- [x] Store status in database
- [x] Write unit tests (`firmware_status_notification_test.go`) — 4 test cases covering all statuses, no existing status, store errors, unknown status
- [x] Create OCPP types `manager/ocpp/ocpp16/firmware_status_notification.go` and `firmware_status_notification_response.go`
- [x] Add `DownloadFailed` status to `FirmwareUpdateStatusType` in store
- [x] Add routing in `routing.go`

**Commit:** `feat(ocpp16): Add FirmwareStatusNotification handler`

---

#### Task 4.3: GetDiagnostics Handler ✅
**Status:** Complete

**Implementation:**
- [x] Create `manager/handlers/ocpp16/get_diagnostics.go`
- [x] Create OCPP types `manager/ocpp/ocpp16/get_diagnostics.go` and `get_diagnostics_response.go`
- [x] Implement diagnostics status tracking via FirmwareStore
- [x] Add routing in `routing.go` and action mapping in CallMaker
- [x] Write unit tests (`get_diagnostics_test.go`) — 4 test cases covering success, time range params, no filename, store errors

**Commit:** `feat(ocpp16): Add GetDiagnostics handler`

---

#### Task 4.4: DiagnosticsStatusNotification Handler ✅
**Status:** Complete

**Implementation:**
- [x] Create `manager/handlers/ocpp16/diagnostics_status_notification.go`
- [x] Create OCPP types `manager/ocpp/ocpp16/diagnostics_status_notification.go` and `diagnostics_status_notification_response.go`
- [x] Track diagnostic upload status (Idle/Uploaded/UploadFailed/Uploading) via FirmwareStore
- [x] Preserve existing location info from previous status entries
- [x] Add routing in `routing.go`
- [x] Write unit tests (`diagnostics_status_notification_test.go`) — 4 test cases covering all statuses, no existing status, store errors, unknown status

**Commit:** `feat(ocpp16): Add DiagnosticsStatusNotification handler`

---

#### Task 4.5: SignedUpdateFirmware Handler ✅
**Status:** Complete  
**Note:** Security extension

**Implementation:**
- [x] Create `manager/handlers/ocpp16/signed_update_firmware.go`
- [x] Create OCPP types `manager/ocpp/ocpp16/signed_update_firmware.go` and `signed_update_firmware_response.go`
- [x] Handle all response statuses (Accepted, Rejected, AcceptedCanceled, InvalidCertificate, RevokedCertificate)
- [x] Track firmware update status via FirmwareStore on acceptance
- [x] Add routing in `routing.go` and action mapping in CallMaker
- [x] Write unit tests (`signed_update_firmware_test.go`) — 5 test cases covering accepted, rejected, invalid certificate, store error, no retries

**Commit:** `feat(ocpp16): Add SignedUpdateFirmware handler`

---

#### Task 4.6: SignedFirmwareStatusNotification Handler ✅
**Status:** Complete  
**Note:** Security extension

**Implementation:**
- [x] Create `manager/handlers/ocpp16/signed_firmware_status_notification.go`
- [x] Create OCPP types `manager/ocpp/ocpp16/signed_firmware_status_notification.go` and `signed_firmware_status_notification_response.go`
- [x] Track signed firmware update status via FirmwareStore (all 14 status types)
- [x] Add new store status types for security extension firmware statuses
- [x] Add routing in `routing.go`
- [x] Write unit tests (`signed_firmware_status_notification_test.go`) — 5 test cases covering all statuses, no existing status, store errors, unknown status, no requestId

**Commit:** `feat(ocpp16): Add SignedFirmwareStatusNotification handler`

---

### Module 4 Completion Checklist

- [ ] File transfer infrastructure implemented
- [ ] All 6 Firmware Management handlers implemented
- [ ] Unit tests for all handlers
- [ ] Integration tests with firmware update flow
- [ ] Update README.md
- [ ] Create PR: `feature/ocpp16-firmware-management` → `main`
- [ ] Merge to main

---

## Module 5: Local Auth List Profile

**Branch:** `feature/ocpp16-local-auth-list`  
**Priority:** Low  
**Status:** 📋 Not Started (0/2 complete - 0%)  
**Timeline:** 2 weeks  
**Base:** main (after Firmware Management merge)  

### Messages to Implement

#### Task 5.0: LocalAuthListStore — Data Store Implementation ✅
**Status:** Complete  
**Priority:** Must be done before Module 5 handlers

**Store Interface** (`manager/store/local_auth_list.go`):
- [x] Define `LocalAuthListEntry` struct with `IdTag`, `IdTagInfo` (Status, ExpiryDate, ParentIdTag)
- [x] Define `LocalAuthListStore` interface: `GetLocalListVersion`, `UpdateLocalAuthList` (Full/Differential), `GetLocalAuthList`
- [x] Add `LocalAuthListStore` to `Engine` interface in `manager/store/engine.go`

**PostgreSQL** (`manager/store/postgres/`):
- [x] Create migration `000007_create_local_auth_list.up.sql` / `.down.sql`
- [x] Create SQL queries in `queries/local_auth_list.sql`
- [x] Generate sqlc code
- [x] Implement `LocalAuthListStore` methods in `local_auth_list.go`

**Firestore** (`manager/store/firestore/`):
- [x] Implement `LocalAuthListStore` methods in `local_auth_list.go`

**In-Memory** (`manager/store/inmemory/`):
- [x] Implement `LocalAuthListStore` methods in `store.go`
- [x] Write tests (10 test cases in `local_auth_list_test.go`)

**Commit:** `feat(store): Add LocalAuthListStore for local auth list management`

---

#### Task 5.1: GetLocalListVersion Handler ✅
**Status:** Complete

**Implementation:**
- [x] Create `manager/handlers/ocpp16/get_local_list_version.go`
- [x] Create OCPP types: `manager/ocpp/ocpp16/get_local_list_version.go`, `get_local_list_version_response.go`
- [x] Implement version tracking via tracing and logging
- [x] Add routing in `routing.go` (CallResult route)
- [x] Add action mapping in CallMaker
- [x] Write unit tests (`get_local_list_version_test.go` — 4 test cases)

**Commit:** `feat(ocpp16): Add GetLocalListVersion handler`

---

#### Task 5.2: SendLocalList Handler ✅
**Status:** Complete

**Implementation:**
- [x] Create `manager/handlers/ocpp16/send_local_list.go`
- [x] Create OCPP types: `manager/ocpp/ocpp16/send_local_list.go`, `send_local_list_response.go`
- [x] Implement list synchronization logic (Full and Differential updates)
- [x] Handle differential updates (entries without IdTagInfo are removals)
- [x] Persist accepted updates to LocalAuthListStore
- [x] Add routing in `routing.go` (CallResult route + CallMaker action)
- [x] Write unit tests (`send_local_list_test.go` — 8 test cases)

**Commit:** `feat(ocpp16): Add SendLocalList handler`

---

### Module 5 Completion Checklist

- [ ] All 2 Local Auth List handlers implemented
- [ ] Unit tests for all handlers
- [ ] Integration tests with offline authorization scenarios
- [ ] Update README.md
- [ ] Create PR: `feature/ocpp16-local-auth-list` → `main`
- [ ] Merge to main

---

## Module 6: Reservation Profile

**Branch:** `feature/ocpp16-reservation`  
**Priority:** Low  
**Status:** 📋 Not Started (0/2 complete - 0%)  
**Timeline:** 2 weeks  
**Base:** main (after Local Auth List merge)  

### Messages to Implement

#### Task 6.0: ReservationStore — Data Store Implementation ✅
**Status:** Complete  
**Priority:** Must be done before Module 6 handlers

**Store Interface** (`manager/store/reservation.go`):
- [x] Define `Reservation` struct: `ReservationId` (int), `ChargeStationId` (string), `ConnectorId` (int), `IdTag` (string), `ParentIdTag` (*string), `ExpiryDate` (time), `Status` (enum: Accepted/Faulted/Occupied/Rejected/Unavailable/Cancelled/Expired), `CreatedAt` (time)
- [x] Define `ReservationStore` interface:
  - `CreateReservation(ctx, reservation *Reservation) error`
  - `GetReservation(ctx, reservationId int) (*Reservation, error)`
  - `CancelReservation(ctx, reservationId int) error`
  - `GetActiveReservations(ctx, chargeStationId string) ([]*Reservation, error)`
  - `GetReservationByConnector(ctx, chargeStationId string, connectorId int) (*Reservation, error)`
  - `ExpireReservations(ctx) (int, error)` — expire all past-due reservations
- [x] Add `ReservationStore` to `Engine` interface in `manager/store/engine.go`

**PostgreSQL** (`manager/store/postgres/`):
- [x] Create migration `000007_create_reservations.up.sql` / `.down.sql`
- [x] Create SQL queries in `queries/reservations.sql`
- [x] Generate sqlc code
- [x] Implement `ReservationStore` methods in `reservations.go`

**Firestore** (`manager/store/firestore/`):
- [x] Implement `ReservationStore` methods in `reservation.go`

**In-Memory** (`manager/store/inmemory/`):
- [x] Implement `ReservationStore` methods in `store.go`
- [x] Write tests (10 test cases in `reservation_test.go`)

**Commit:** `feat(store): Add ReservationStore for connector reservation management`

---

#### Task 6.1: ReserveNow Handler ✅
**Status:** Complete  
**Dependencies:** Task 6.0 (ReservationStore)

**Implementation:**
- [x] Create `manager/handlers/ocpp16/reserve_now.go`
- [x] Create OCPP types: `manager/ocpp/ocpp16/reserve_now.go`, `reserve_now_response.go`
- [x] Implement reservation state management (create reservation on Accepted, skip on rejection)
- [x] Handle expiry date parsing (RFC3339)
- [x] Handle optional parentIdTag
- [x] Add routing in `routing.go` (CallResult route + CallMaker action)
- [x] Write unit tests (`reserve_now_test.go` — 10 test cases: accepted, accepted with parentIdTag, faulted, occupied, rejected, unavailable, invalid expiry date, all rejection statuses, connector 0)

**Commit:** `feat(ocpp16): Add ReserveNow handler`

---

#### Task 6.2: CancelReservation Handler ✅
**Status:** Complete

**Implementation:**
- [x] Create `manager/handlers/ocpp16/cancel_reservation.go`
- [x] Implement reservation cancellation
- [x] Write unit tests (`cancel_reservation_test.go` — 4 test cases: accepted, rejected, non-existent reservation, multiple reservations)

**Types Created:**
- `manager/ocpp/ocpp16/cancel_reservation.go`
- `manager/ocpp/ocpp16/cancel_reservation_response.go`

**Commit:** `feat(ocpp16): Add CancelReservation handler`

---

### Module 6 Completion Checklist

- [ ] Reservation data model and storage implemented
- [ ] All 2 Reservation handlers implemented
- [ ] Unit tests for all handlers
- [ ] Integration tests with reservation flow
- [ ] Update README.md
- [ ] Create PR: `feature/ocpp16-reservation` → `main`
- [ ] Merge to main

---

## Module 7: Security Extensions

**Branch:** `feature/ocpp16-security-extensions`  
**Priority:** Medium  
**Status:** 📋 Not Started (4/8 complete - 50%)  
**Timeline:** 3-4 weeks  
**Base:** main (after Reservation merge)  
**Note:** Some security features already implemented for ISO 15118  

### Current Status

✅ **Implemented (4):**
1. CertificateSigned (via DataTransfer for ISO 15118)
2. InstallCertificate (via DataTransfer for ISO 15118)
3. SecurityEventNotification
4. SignCertificate (via DataTransfer for ISO 15118)

❌ **Missing (4):**
5. DeleteCertificate
6. GetInstalledCertificateIds
7. GetLog
8. LogStatusNotification

### Messages to Implement

#### Task 7.1: DeleteCertificate Handler ✅
**Status:** Complete

**Implementation:**
- [x] Create OCPP 1.6 types: `manager/ocpp/ocpp16/delete_certificate.go`, `delete_certificate_response.go`
- [x] Define `CertificateHashDataType` struct with `HashAlgorithm`, `IssuerNameHash`, `IssuerKeyHash`, `SerialNumber`
- [x] Define `HashAlgorithmEnumType` (SHA256/SHA384/SHA512)
- [x] Define `DeleteCertificateResponseJsonStatus` (Accepted/Failed/NotFound)
- [x] Create `manager/handlers/ocpp16/delete_certificate_result.go`
- [x] Implement `DeleteCertificateResultHandler` with tracing and logging
- [x] Add routing in `routing.go` (CallResult route)
- [x] Add action mapping in `NewCallMaker`
- [x] Write unit tests (`delete_certificate_result_test.go` — 11 test cases: all hash algorithms, all statuses, accepted/failed/not found scenarios)

**Commit:** `feat(ocpp16): Add DeleteCertificate handler`

---

#### Task 7.2: GetInstalledCertificateIds Handler ✅
**Status:** Complete

**Implementation:**
- [x] Create OCPP 1.6 types: `manager/ocpp/ocpp16/get_installed_certificate_ids.go`, `get_installed_certificate_ids_response.go`
- [x] Define `CertificateUseEnumType` (CentralSystemRootCertificate/ManufacturerRootCertificate)
- [x] Define `GetInstalledCertificateIdsResponseJsonStatus` (Accepted/NotFound)
- [x] Create `manager/handlers/ocpp16/get_installed_certificate_ids_result.go`
- [x] Implement `GetInstalledCertificateIdsResultHandler` with tracing and logging
- [x] Add routing in `routing.go` (CallResult route)
- [x] Add action mapping in `NewCallMaker`
- [x] Write unit tests (`get_installed_certificate_ids_result_test.go` — 10 test cases: all certificate types, all statuses, multiple certificates, not found scenarios)

**Commit:** `feat(ocpp16): Add GetInstalledCertificateIds handler`

---

#### Task 7.3: GetLog Handler
**Status:** Not Started

**Implementation:**
- [ ] Create `manager/handlers/ocpp16/get_log.go`
- [ ] Implement log retrieval
- [ ] Write unit tests

**Commit:** `feat(ocpp16): Add GetLog handler`

---

#### Task 7.4: LogStatusNotification Handler
**Status:** Not Started

**Implementation:**
- [ ] Create `manager/handlers/ocpp16/log_status_notification.go`
- [ ] Track log upload status
- [ ] Write unit tests

**Commit:** `feat(ocpp16): Add LogStatusNotification handler`

---

### Module 7 Completion Checklist

- [ ] All 4 remaining Security Extension handlers implemented
- [ ] Unit tests for all handlers
- [ ] Integration tests with certificate management flow
- [ ] Update README.md with security features
- [ ] Create PR: `feature/ocpp16-security-extensions` → `main`
- [ ] Merge to main

---

## Overall Progress Tracking

### Completion Summary

| Module | Branch | Status | Messages | Completion |
|--------|--------|--------|----------|------------|
| Core Profile | `feature/ocpp16-core-profile` | 🚧 In Progress | 9/16 | 56% |
| Remote Trigger | `feature/ocpp16-remote-trigger` | 📋 Not Started | 0/2 | 0% |
| Smart Charging | `feature/ocpp16-smart-charging` | 📋 Not Started | 0/3 | 0% |
| Firmware Management | `feature/ocpp16-firmware-management` | 📋 Not Started | 0/6 | 0% |
| Local Auth List | `feature/ocpp16-local-auth-list` | 📋 Not Started | 0/2 | 0% |
| Reservation | `feature/ocpp16-reservation` | 📋 Not Started | 0/2 | 0% |
| Security Extensions | `feature/ocpp16-security-extensions` | 📋 Not Started | 4/8 | 50% |
| **TOTAL** | | | **13/39** | **33%** |

**Note:** ISO 15118 messages (4) via DataTransfer are already at 100%

---

## Testing Strategy

### Unit Tests

Each handler must have comprehensive unit tests covering:
- Success scenarios (Accepted)
- Failure scenarios (Rejected)
- Edge cases (empty fields, unknown values)
- Error handling

### Integration Tests

Each module should have integration tests:
- MQTT message flow
- CallMaker integration
- Store interaction
- End-to-end message flow with simulator

### OCTT Compliance

Run OCPP Compliance Testing Tool (OCTT) after each module completion to verify:
- Message format compliance
- Protocol behavior
- Error handling

---

## Merge Strategy

### Per-Module Merge Workflow

1. **Complete Module Branch**
   - All handlers implemented
   - All tests passing
   - Documentation updated

2. **Pre-Merge Checklist**
   - [ ] Rebase on latest main
   - [ ] All tests passing
   - [ ] No merge conflicts
   - [ ] README.md updated
   - [ ] CHANGELOG.md entry added

3. **Create Pull Request**
   - Title: `feat(ocpp16): Implement [Module Name] Profile`
   - Description: Link to audit, list implemented messages
   - Request review

4. **Code Review**
   - Address feedback
   - Update tests if needed

5. **Merge to Main**
   - Squash commits or merge (team preference)
   - Delete feature branch after merge

6. **Start Next Module**
   - Create new branch from updated main
   - Begin next module implementation

---

## Success Criteria

### Per Module

✅ **Implementation Complete**
- All messages in module implemented
- All handlers have unit tests
- Integration tests pass

✅ **Documentation Updated**
- README.md reflects new features
- API documentation updated
- Examples provided

✅ **Quality Gates**
- No failing tests
- Code review approved
- OCTT tests pass (if applicable)

### Overall Project

✅ **OCPP 1.6 Compliance**
- All mandatory messages implemented (Core Profile)
- Optional profiles implemented as planned
- OCTT Core Profile tests pass

✅ **Production Ready**
- Comprehensive test coverage
- Error handling in place
- Monitoring/logging added
- Documentation complete

---

## Timeline Estimate

| Module | Duration | Start After |
|--------|----------|-------------|
| Core Profile | 2-3 weeks | Now |
| Remote Trigger | 1 week | Core Profile merge |
| Smart Charging | 4-6 weeks | Remote Trigger merge |
| Firmware Management | 3-4 weeks | Smart Charging merge |
| Local Auth List | 2 weeks | Firmware Management merge |
| Reservation | 2 weeks | Local Auth List merge |
| Security Extensions | 3-4 weeks | Reservation merge |
| **TOTAL** | **17-22 weeks** | **~4-5 months** |

---

## References

- [OCPP 1.6 Implementation Audit](ocpp16-implementation-audit.md)
- [OCPP Specifications](https://openchargealliance.org/protocols/open-charge-point-protocol/)
- [OCPP Version Architecture](ocpp-version-architecture.md)
- [Existing Handler Patterns](../manager/handlers/ocpp16/)
- [Store Interfaces](../manager/store/)

---

**Created by:** Patricio (AI Assistant)  
**Last Updated:** 2026-02-05  
**Current Module:** Core Profile (56% complete)
