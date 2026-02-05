# OCPP 1.6 Implementation Plan

**Project:** MaEVe CSMS OCPP 1.6 Completion  
**Created:** 2026-02-05  
**Status:** 🚧 In Progress  
**Target:** Complete OCPP 1.6 Core Profile (44% → 100%)

---

## Overview

Based on the [OCPP 1.6 Implementation Audit](ocpp16-implementation-audit.md), this plan implements the missing 27 OCPP 1.6 messages across all profiles. The audit identified:

- **Core Profile:** 44% complete (7/16 messages) - **Priority 1**
- **Smart Charging:** 0% complete (0/3 messages) - **Priority 2**
- **Remote Trigger:** 25% complete (0.5/2 messages) - **Priority 1**
- **Other Profiles:** 0% complete - **Priority 3**

**Current Overall Coverage:** 36% (13/42 messages fully implemented, 2 partial)

---

## Implementation Strategy

### Approach
- **Incremental:** One message handler at a time
- **Test-Driven:** Unit tests for each handler
- **Commit Early:** Commit after each working handler
- **Follow Patterns:** Match existing handler structure

### Branch Strategy
- **Branch:** `feature/ocpp16-completion`
- **Base:** Current main branch
- **Merge:** After Core Profile completion + testing

---

## Phase 1: Core Profile Completion (Priority 1)

**Goal:** Complete OCPP 1.6 Core Profile to 100% (16/16 messages)  
**Timeline:** ~2-3 weeks  
**Impact:** Essential CSMS functionality

### Quick Wins (Week 1) - Simple Handlers

#### Task 1.1: Reset Handler ✅
- [x] Create `manager/handlers/ocpp16/reset.go`
- [x] Implement `ResetHandler` struct
- [x] Add `HandleCallResult` method
- [x] Add routing in `routing.go` (CallResultRoutes)
- [x] Add action mapping in `router.go` (CallMaker Actions)
- [x] Write unit test `reset_test.go`
- [x] Manual integration test
- [x] Commit: "Add Reset handler for OCPP 1.6"

**Handler Template:**
```go
type ResetHandler struct{}

func (r ResetHandler) HandleCallResult(ctx context.Context, chargeStationId string, 
    request ocpp.Request, response ocpp.Response, state any) error {
    req := request.(*types.ResetJson)
    resp := response.(*types.ResetResponseJson)
    
    if resp.Status == types.ResetResponseJsonStatusAccepted {
        slog.Info("reset accepted", "chargeStationId", chargeStationId, "type", req.Type)
    } else {
        slog.Warn("reset rejected", "chargeStationId", chargeStationId, "type", req.Type)
    }
    return nil
}
```

---

#### Task 1.2: UnlockConnector Handler ✅
- [x] Create `manager/handlers/ocpp16/unlock_connector.go`
- [x] Implement `UnlockConnectorHandler` struct
- [x] Add `HandleCallResult` method
- [x] Add routing in `routing.go`
- [x] Add action mapping in `router.go`
- [x] Write unit test `unlock_connector_test.go`
- [x] Manual integration test
- [x] Commit: "Add UnlockConnector handler for OCPP 1.6"

---

#### Task 1.3: ClearCache Handler ✅
- [x] Create `manager/handlers/ocpp16/clear_cache.go`
- [x] Implement `ClearCacheHandler` struct
- [x] Add `HandleCallResult` method
- [x] Add routing in `routing.go`
- [x] Add action mapping in `router.go`
- [x] Write unit test `clear_cache_test.go`
- [x] Manual integration test
- [x] Commit: "Add ClearCache handler for OCPP 1.6"

---

#### Task 1.4: ChangeAvailability Handler ✅
- [x] Create `manager/handlers/ocpp16/change_availability.go`
- [x] Implement `ChangeAvailabilityHandler` struct
- [x] Add `HandleCallResult` method
- [x] Add routing in `routing.go`
- [x] Add action mapping in `router.go`
- [x] Write unit test `change_availability_test.go`
- [x] Manual integration test
- [x] Commit: "Add ChangeAvailability handler for OCPP 1.6"

**Note:** Consider storing availability state in future enhancement

---

#### Task 1.5: ChangeConfiguration Call Handler ✅
- [x] Open `manager/handlers/ocpp16/change_configuration.go`
- [x] Note: CallResult handler already exists
- [x] Verify routing in `routing.go` (CallResultRoutes section)
- [x] Verify action already in `router.go`
- [x] Write unit test for Call path
- [x] Manual integration test
- [x] Commit: "test: Add comprehensive unit tests for ChangeConfiguration handler"

**Existing CallResult Handler:** `change_configuration_result.go` ✅
**Routing:** Already configured in CallResultRoutes and CallMaker Actions ✅
**Tests:** Comprehensive test coverage added ✅

---

#### Task 1.6: TriggerMessage Call Handler ✅
- [x] Open `manager/handlers/ocpp16/trigger_message.go`
- [x] Note: CallResult handler already exists
- [x] Add routing in `routing.go` (CallResultRoutes section)
- [x] Add action mapping in `router.go`
- [x] Write unit test for Call path
- [x] Verified implementation complete
- [x] Commit: Already complete (no new commit needed)

**Status:** COMPLETE - CallResult handler exists in `trigger_message_result.go`, routing configured in `routing.go` (CallResultRoutes), action mapping in `router.go` (CallMaker Actions), comprehensive unit tests in `trigger_message_result_test.go` ✅

---

### Core Transactions (Week 2) - Complex Handlers

#### Task 1.7: GetConfiguration Handler ✅
- [x] Create `manager/handlers/ocpp16/get_configuration.go`
- [x] Implement `GetConfigurationHandler` struct with `SettingsStore`
- [x] Add `HandleCallResult` method
- [x] Parse optional key filter
- [x] Store configuration values received from charge station
- [x] Handle unknown keys with logging
- [x] Add routing in `routing.go` (CallResultRoutes)
- [x] Add action mapping in `router.go` (CallMaker Actions)
- [x] Write unit test `get_configuration_test.go`
- [x] Comprehensive test coverage (all scenarios)
- [x] Commit: "feat: Add GetConfiguration handler for OCPP 1.6"

**Status:** COMPLETE - Retrieves and stores charge station configuration settings. Handles unknown keys, empty responses, and updates existing settings. Full unit test coverage. ✅

**Dependencies:**
- `store.ChargeStationSettingsStore` ✅

---

#### Task 1.8: RemoteStartTransaction Handler ✅❌
- [ ] Create `manager/handlers/ocpp16/remote_start_transaction.go`
- [ ] Implement `RemoteStartTransactionHandler` struct
- [ ] Add dependencies: `TokenStore`, optional `TransactionStore`
- [ ] Add `HandleCallResult` method
- [ ] Validate token authorization
- [ ] Log start requests (accepted/rejected)
- [ ] Add routing in `routing.go`
- [ ] Add action mapping in `router.go`
- [ ] Write unit test `remote_start_transaction_test.go`
- [ ] Manual integration test
- [ ] Commit: "Add RemoteStartTransaction handler for OCPP 1.6"

**Dependencies:**
- `store.TokenStore`

**Critical for:** Mobile apps, OCPI roaming, fleet management

---

#### Task 1.9: RemoteStopTransaction Handler ✅❌
- [ ] Create `manager/handlers/ocpp16/remote_stop_transaction.go`
- [ ] Implement `RemoteStopTransactionHandler` struct
- [ ] Add dependency: `TransactionStore`
- [ ] Add `HandleCallResult` method
- [ ] Validate transaction exists
- [ ] Log stop requests (accepted/rejected)
- [ ] Add routing in `routing.go`
- [ ] Add action mapping in `router.go`
- [ ] Write unit test `remote_stop_transaction_test.go`
- [ ] Manual integration test
- [ ] Commit: "Add RemoteStopTransaction handler for OCPP 1.6"

**Dependencies:**
- `store.TransactionStore`

**Critical for:** Emergency stop, payment failures, session management

---

### Milestone: Core Profile Complete
- [ ] All 16 Core Profile messages implemented
- [ ] All unit tests passing
- [ ] Integration tests passing
- [ ] Documentation updated
- [ ] Commit: "Complete OCPP 1.6 Core Profile implementation"

**Coverage:** 44% → **100%** (Core Profile)

---

## Phase 2: Smart Charging Profile (Priority 2)

**Goal:** Add load management capabilities  
**Timeline:** ~4-6 weeks  
**Impact:** Dynamic pricing, load balancing, peak shaving

### Task 2.1: SetChargingProfile Handler ✅❌
- [ ] Create `manager/handlers/ocpp16/set_charging_profile.go`
- [ ] Implement `SetChargingProfileHandler` struct
- [ ] Add dependency: `ChargingProfileStore` (new interface needed)
- [ ] Validate charging profile structure
- [ ] Implement profile stacking logic
- [ ] Handle ChargingProfilePurpose (TxProfile, TxDefaultProfile, ChargePointMaxProfile)
- [ ] Validate ChargingSchedule
- [ ] Store profile
- [ ] Add routing in `routing.go`
- [ ] Add action mapping in `router.go`
- [ ] Write unit test `set_charging_profile_test.go`
- [ ] Manual integration test
- [ ] Commit: "Add SetChargingProfile handler for OCPP 1.6"

**New Store Interface Needed:**
```go
type ChargingProfileStore interface {
    SetChargingProfile(ctx context.Context, chargeStationId string, profile *ChargingProfile) error
    GetChargingProfiles(ctx context.Context, chargeStationId string, connectorId int) ([]*ChargingProfile, error)
    ClearChargingProfile(ctx context.Context, chargeStationId string, profileId *int) error
}
```

---

#### Task 2.2: GetCompositeSchedule Handler ✅❌
- [ ] Create `manager/handlers/ocpp16/get_composite_schedule.go`
- [ ] Implement `GetCompositeScheduleHandler` struct
- [ ] Add dependency: `ChargingProfileStore`
- [ ] Implement composite schedule calculation
- [ ] Stack profiles by priority
- [ ] Apply ChargingRateUnit
- [ ] Return calculated schedule
- [ ] Add routing in `routing.go`
- [ ] Add action mapping in `router.go`
- [ ] Write unit test `get_composite_schedule_test.go`
- [ ] Manual integration test
- [ ] Commit: "Add GetCompositeSchedule handler for OCPP 1.6"

**Complex Logic:** Profile stacking, time-based calculation

---

#### Task 2.3: ClearChargingProfile Handler ✅❌
- [ ] Create `manager/handlers/ocpp16/clear_charging_profile.go`
- [ ] Implement `ClearChargingProfileHandler` struct
- [ ] Add dependency: `ChargingProfileStore`
- [ ] Handle optional filters (profileId, connectorId, purpose, stack level)
- [ ] Clear matching profiles
- [ ] Return cleared count
- [ ] Add routing in `routing.go`
- [ ] Add action mapping in `router.go`
- [ ] Write unit test `clear_charging_profile_test.go`
- [ ] Manual integration test
- [ ] Commit: "Add ClearChargingProfile handler for OCPP 1.6"

---

### Task 2.4: ChargingProfileStore Implementation ✅❌
- [ ] Create `manager/store/charging_profile.go` (interface)
- [ ] Create `manager/store/postgres/charging_profile.go` (PostgreSQL impl)
- [ ] Design schema for charging profiles
- [ ] Create migration `000007_create_charging_profiles_table.up.sql`
- [ ] Write SQL queries `queries/charging_profiles.sql`
- [ ] Generate sqlc code
- [ ] Implement store methods
- [ ] Write tests `charging_profile_test.go`
- [ ] Commit: "Add ChargingProfileStore with PostgreSQL implementation"

**Database Schema:**
```sql
CREATE TABLE charging_profiles (
    id SERIAL PRIMARY KEY,
    charge_station_id VARCHAR(255) NOT NULL,
    connector_id INT NOT NULL,
    stack_level INT NOT NULL,
    charging_profile_purpose VARCHAR(50) NOT NULL,
    charging_profile_kind VARCHAR(50) NOT NULL,
    recurrency_kind VARCHAR(50),
    valid_from TIMESTAMP,
    valid_to TIMESTAMP,
    charging_schedule JSONB NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);
```

---

### Milestone: Smart Charging Complete
- [ ] All 3 Smart Charging messages implemented
- [ ] ChargingProfileStore implemented
- [ ] All unit tests passing
- [ ] Integration tests passing
- [ ] Documentation updated
- [ ] Commit: "Complete OCPP 1.6 Smart Charging Profile implementation"

**Coverage:** 36% → **~50%** (Overall)

---

## Phase 3: Remote Trigger Profile (Priority 2)

**Goal:** Complete Remote Trigger capabilities  
**Timeline:** ~1 week

### Task 3.1: ExtendedTriggerMessage Handler ✅❌
- [ ] Create `manager/handlers/ocpp16/extended_trigger_message.go`
- [ ] Implement `ExtendedTriggerMessageHandler` struct
- [ ] Add `HandleCallResult` method
- [ ] Support extended message types
- [ ] Add routing in `routing.go`
- [ ] Add action mapping in `router.go`
- [ ] Write unit test `extended_trigger_message_test.go`
- [ ] Manual integration test
- [ ] Commit: "Add ExtendedTriggerMessage handler for OCPP 1.6"

---

## Phase 4: Optional Profiles (Priority 3)

**Goal:** Complete optional profile support  
**Timeline:** ~6-8 weeks  
**Impact:** Production-grade feature completeness

### Firmware Management Profile (6 messages)

#### Task 4.1: GetDiagnostics Handler ✅❌
- [ ] Create `manager/handlers/ocpp16/get_diagnostics.go`
- [ ] Implement file upload infrastructure
- [ ] Add routing
- [ ] Write tests
- [ ] Commit: "Add GetDiagnostics handler for OCPP 1.6"

#### Task 4.2: DiagnosticsStatusNotification Handler ✅❌
- [ ] Create `manager/handlers/ocpp16/diagnostics_status_notification.go`
- [ ] Track diagnostic upload status
- [ ] Add routing
- [ ] Write tests
- [ ] Commit: "Add DiagnosticsStatusNotification handler for OCPP 1.6"

#### Task 4.3: UpdateFirmware Handler ✅❌
- [ ] Create `manager/handlers/ocpp16/update_firmware.go`
- [ ] Implement firmware URL validation
- [ ] Add routing
- [ ] Write tests
- [ ] Commit: "Add UpdateFirmware handler for OCPP 1.6"

#### Task 4.4: FirmwareStatusNotification Handler ✅❌
- [ ] Create `manager/handlers/ocpp16/firmware_status_notification.go`
- [ ] Track firmware update status
- [ ] Add routing
- [ ] Write tests
- [ ] Commit: "Add FirmwareStatusNotification handler for OCPP 1.6"

#### Task 4.5: SignedUpdateFirmware Handler ✅❌
- [ ] Create `manager/handlers/ocpp16/signed_update_firmware.go`
- [ ] Verify firmware signatures
- [ ] Add routing
- [ ] Write tests
- [ ] Commit: "Add SignedUpdateFirmware handler for OCPP 1.6"

#### Task 4.6: SignedFirmwareStatusNotification Handler ✅❌
- [ ] Create `manager/handlers/ocpp16/signed_firmware_status_notification.go`
- [ ] Track signed firmware status
- [ ] Add routing
- [ ] Write tests
- [ ] Commit: "Add SignedFirmwareStatusNotification handler for OCPP 1.6"

---

### Local Auth List Profile (2 messages)

#### Task 4.7: GetLocalListVersion Handler ✅❌
- [ ] Create `manager/handlers/ocpp16/get_local_list_version.go`
- [ ] Implement version tracking
- [ ] Add routing
- [ ] Write tests
- [ ] Commit: "Add GetLocalListVersion handler for OCPP 1.6"

#### Task 4.8: SendLocalList Handler ✅❌
- [ ] Create `manager/handlers/ocpp16/send_local_list.go`
- [ ] Implement list synchronization
- [ ] Add routing
- [ ] Write tests
- [ ] Commit: "Add SendLocalList handler for OCPP 1.6"

---

### Reservation Profile (2 messages)

#### Task 4.9: ReserveNow Handler ✅❌
- [ ] Create `manager/handlers/ocpp16/reserve_now.go`
- [ ] Implement reservation state management
- [ ] Add routing
- [ ] Write tests
- [ ] Commit: "Add ReserveNow handler for OCPP 1.6"

#### Task 4.10: CancelReservation Handler ✅❌
- [ ] Create `manager/handlers/ocpp16/cancel_reservation.go`
- [ ] Implement reservation cancellation
- [ ] Add routing
- [ ] Write tests
- [ ] Commit: "Add CancelReservation handler for OCPP 1.6"

---

### Security Extensions (4 remaining messages)

#### Task 4.11: DeleteCertificate Handler ✅❌
- [ ] Create `manager/handlers/ocpp16/delete_certificate.go`
- [ ] Implement certificate deletion
- [ ] Add routing
- [ ] Write tests
- [ ] Commit: "Add DeleteCertificate handler for OCPP 1.6"

#### Task 4.12: GetInstalledCertificateIds Handler ✅❌
- [ ] Create `manager/handlers/ocpp16/get_installed_certificate_ids.go`
- [ ] Query installed certificates
- [ ] Add routing
- [ ] Write tests
- [ ] Commit: "Add GetInstalledCertificateIds handler for OCPP 1.6"

#### Task 4.13: GetLog Handler ✅❌
- [ ] Create `manager/handlers/ocpp16/get_log.go`
- [ ] Implement log retrieval
- [ ] Add routing
- [ ] Write tests
- [ ] Commit: "Add GetLog handler for OCPP 1.6"

#### Task 4.14: LogStatusNotification Handler ✅❌
- [ ] Create `manager/handlers/ocpp16/log_status_notification.go`
- [ ] Track log upload status
- [ ] Add routing
- [ ] Write tests
- [ ] Commit: "Add LogStatusNotification handler for OCPP 1.6"

---

## Phase 5: Production Hardening (Priority 1)

**Goal:** Polish and production readiness  
**Timeline:** ~2-4 weeks

### Task 5.1: StatusNotification Enhancement ✅❌
- [ ] Update `manager/handlers/ocpp16/status_notification.go`
- [ ] Add `ConnectorStatusStore` interface
- [ ] Persist connector status updates
- [ ] Add status history tracking
- [ ] Write additional tests
- [ ] Commit: "Enhance StatusNotification to persist connector state"

### Task 5.2: Error Handling Improvements ✅❌
- [ ] Audit all handlers for error cases
- [ ] Add consistent error logging
- [ ] Add metrics for failures
- [ ] Improve error messages
- [ ] Commit: "Improve error handling across OCPP 1.6 handlers"

### Task 5.3: Integration Testing ✅❌
- [ ] Create comprehensive integration test suite
- [ ] Test MQTT message flow end-to-end
- [ ] Test CallMaker with real charge station simulator
- [ ] Document test scenarios
- [ ] Commit: "Add comprehensive OCPP 1.6 integration tests"

### Task 5.4: Documentation Updates ✅❌
- [ ] Update README.md with OCPP 1.6 feature list
- [ ] Document all handler configurations
- [ ] Add API examples for each message
- [ ] Update deployment guide
- [ ] Commit: "Update documentation for OCPP 1.6 completion"

### Task 5.5: OCTT Compliance Testing ✅❌
- [ ] Set up OCPP Compliance Testing Tool
- [ ] Run Core Profile tests
- [ ] Run Smart Charging tests
- [ ] Fix any compliance issues
- [ ] Document compliance status
- [ ] Commit: "Achieve OCTT compliance for OCPP 1.6 Core Profile"

---

## Testing Strategy

### Unit Tests

Each handler must have comprehensive unit tests:

**Test Template:**
```go
func TestResetHandler(t *testing.T) {
    tests := []struct {
        name          string
        request       *ocpp16.ResetJson
        response      *ocpp16.ResetResponseJson
        expectedError error
    }{
        {
            name: "successful soft reset",
            request: &ocpp16.ResetJson{
                Type: ocpp16.ResetJsonTypeSoft,
            },
            response: &ocpp16.ResetResponseJson{
                Status: ocpp16.ResetResponseJsonStatusAccepted,
            },
            expectedError: nil,
        },
        {
            name: "rejected hard reset",
            request: &ocpp16.ResetJson{
                Type: ocpp16.ResetJsonTypeHard,
            },
            response: &ocpp16.ResetResponseJson{
                Status: ocpp16.ResetResponseJsonStatusRejected,
            },
            expectedError: nil,
        },
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            handler := ResetHandler{}
            err := handler.HandleCallResult(context.Background(), "cs001", tt.request, tt.response, nil)
            assert.Equal(t, tt.expectedError, err)
        })
    }
}
```

### Integration Tests

Test with MQTT and full message flow:

```go
func TestResetIntegration(t *testing.T) {
    // Setup MQTT broker
    broker := setupTestMQTTBroker(t)
    defer broker.Close()
    
    // Setup manager with handler
    manager := setupTestManager(t, broker)
    
    // Send Reset via CallMaker
    request := &ocpp16.ResetJson{Type: ocpp16.ResetJsonTypeSoft}
    err := manager.SendCall(context.Background(), "cs001", request)
    require.NoError(t, err)
    
    // Verify message on MQTT topic
    msg := receiveFromMQTT(t, broker, "cs/out/cs001")
    assert.Contains(t, string(msg), "Reset")
    
    // Simulate charge station response
    response := &ocpp16.ResetResponseJson{Status: ocpp16.ResetResponseJsonStatusAccepted}
    sendToMQTT(t, broker, "cs/in/cs001", createCallResultMessage(response))
    
    // Verify handler processes response
    time.Sleep(100 * time.Millisecond)
    // Assert logs or metrics
}
```

### Manual Testing

For each handler:
1. Start manager locally
2. Use OCPP charge station simulator
3. Trigger message via CallMaker or simulator
4. Verify logs and behavior
5. Test error cases

---

## Commit Strategy

### Commit Message Format

```
<type>: <subject>

<body>

<footer>
```

**Types:**
- `feat`: New handler implementation
- `test`: Test additions
- `refactor`: Code improvements
- `docs`: Documentation updates
- `fix`: Bug fixes

**Examples:**
```
feat: Add Reset handler for OCPP 1.6

Implements Reset message handler with support for both Soft and Hard reset types.
Adds routing and action mapping for CallMaker integration.

Closes #123
```

```
test: Add unit tests for RemoteStartTransaction handler

Covers successful and rejected remote start scenarios.
Tests token validation and connector availability checks.
```

---

## Progress Tracking

### Current Status

**Phase 1: Core Profile Completion**
- [x] Task 1.1: Reset Handler
- [x] Task 1.2: UnlockConnector Handler
- [x] Task 1.3: ClearCache Handler
- [x] Task 1.4: ChangeAvailability Handler
- [x] Task 1.5: ChangeConfiguration Call Handler
- [ ] Task 1.6: TriggerMessage Call Handler
- [ ] Task 1.7: GetConfiguration Handler
- [ ] Task 1.8: RemoteStartTransaction Handler
- [ ] Task 1.9: RemoteStopTransaction Handler

**Overall Progress:** 7/9 tasks (78%)

---

## Automation Plan

### Cron Job Configuration

The implementation will be driven by a cron job that:
1. Reads this plan
2. Identifies next unchecked task
3. Implements handler following patterns
4. Runs tests
5. Commits changes
6. Updates this plan
7. Repeats every 15 minutes

**Cron Job Payload:**
```json
{
  "kind": "agentTurn",
  "message": "Continue OCPP 1.6 implementation. Read /Users/suda/Projects/Personal/Go/maeve-csms/docs/ocpp16-implementation-plan.md and implement the next unchecked task. Follow existing handler patterns from the codebase. Write unit tests. Commit with descriptive message. Update the plan with ✅. If blocked, document why and move to next task.",
  "model": "anthropic/claude-sonnet-4-5",
  "thinking": "low",
  "timeoutSeconds": 600
}
```

---

## Success Criteria

### Phase 1 Complete When:
- ✅ All 9 Core Profile tasks checked
- ✅ All unit tests passing
- ✅ Integration tests passing
- ✅ Documentation updated
- ✅ Core Profile at 100% (16/16 messages)

### Phase 2 Complete When:
- ✅ All 4 Smart Charging tasks checked
- ✅ ChargingProfileStore implemented
- ✅ All tests passing
- ✅ Smart Charging at 100% (3/3 messages)

### Overall Success:
- ✅ All OCPP 1.6 messages implemented (42/42)
- ✅ OCTT compliance tests passing
- ✅ Production deployment ready
- ✅ Documentation complete

---

## Risk Management

### Known Risks

1. **Complex Logic in Smart Charging**
   - Mitigation: Start with simple profiles, iterate
   - Reference: OCPP 1.6 Specification section on charging profiles

2. **Store Interface Changes**
   - Mitigation: Design interfaces carefully, use PostgreSQL from start
   - Reference: Existing store patterns in codebase

3. **MQTT Message Flow**
   - Mitigation: Extensive integration testing
   - Reference: Existing DataTransfer handler as example

4. **Testcontainers Issues**
   - Mitigation: Use manual PostgreSQL instance for testing if needed
   - Workaround: Already used successfully in PostgreSQL implementation

---

## References

- [OCPP 1.6 Implementation Audit](ocpp16-implementation-audit.md)
- [OCPP 1.6 Specification](https://www.openchargealliance.org/protocols/ocpp-16/)
- [Existing Handler Patterns](../manager/handlers/ocpp16/)
- [Store Interfaces](../manager/store/)
- [PostgreSQL Implementation](postgres-implementation.md)

---

**Created by:** Patricio (AI Assistant)  
**Last Updated:** 2026-02-05  
**Next Review:** After Phase 1 completion
