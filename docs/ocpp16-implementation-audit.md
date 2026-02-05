# OCPP 1.6 Implementation Audit

**Date:** 2026-02-05  
**Project:** MaEVe CSMS  
**Version:** OCPP 1.6j (JSON over WebSocket)

## Executive Summary

This document provides a comprehensive audit of the OCPP 1.6 implementation in MaEVe CSMS, comparing implemented features against the complete OCPP 1.6 specification.

### Quick Status

| Category | Total Messages | Implemented | Partial | Missing | Coverage |
|----------|----------------|-------------|---------|---------|----------|
| **Core Profile** | 16 | 7 | 2 | 7 | 44% |
| **Smart Charging** | 3 | 0 | 0 | 3 | 0% |
| **Remote Trigger** | 2 | 1 | 0 | 1 | 50% |
| **Firmware Management** | 6 | 0 | 0 | 6 | 0% |
| **Local Auth List** | 2 | 0 | 0 | 2 | 0% |
| **Reservation** | 2 | 0 | 0 | 2 | 0% |
| **Security Extensions** | 7 | 1 | 0 | 6 | 14% |
| **ISO 15118 (via DataTransfer)** | 4 | 4 | 0 | 0 | 100% |
| **TOTAL** | 42 | 13 | 2 | 27 | **36%** |

---

## Table of Contents

- [Detailed Analysis by Profile](#detailed-analysis-by-profile)
- [Implemented Messages](#implemented-messages)
- [Partially Implemented](#partially-implemented)
- [Missing Messages](#missing-messages)
- [Schema vs Handler Gap](#schema-vs-handler-gap)
- [Priority Recommendations](#priority-recommendations)
- [Implementation Roadmap](#implementation-roadmap)

---

## Detailed Analysis by Profile

### Core Profile (Mandatory)

The Core Profile contains the essential functionality for basic charging operations.

| Message | Direction | Status | Handler | Notes |
|---------|-----------|--------|---------|-------|
| **Authorize** | CS → CSMS | ✅ Implemented | `authorize.go` | Token validation working |
| **BootNotification** | CS → CSMS | ✅ Implemented | `boot_notification.go` | Registration & heartbeat interval |
| **ChangeAvailability** | CSMS → CS | ⚠️ Partial | ❌ No handler | Schema exists, no handler |
| **ChangeConfiguration** | CSMS → CS | ⚠️ Partial | `change_configuration_result.go` | Only CallResult handler (response processing) |
| **ClearCache** | CSMS → CS | ❌ Missing | ❌ No handler | Schema exists, no handler |
| **DataTransfer** | Both | ✅ Implemented | `data_transfer.go` | Bidirectional, with vendor extensions |
| **GetConfiguration** | CSMS → CS | ❌ Missing | ❌ No handler | Schema exists, no handler |
| **Heartbeat** | CS → CSMS | ✅ Implemented | `heartbeat.go` | Keep-alive mechanism |
| **MeterValues** | CS → CSMS | ✅ Implemented | `meter_values.go` | Energy consumption tracking |
| **RemoteStartTransaction** | CSMS → CS | ❌ Missing | ❌ No handler | Schema + types exist, no handler |
| **RemoteStopTransaction** | CSMS → CS | ❌ Missing | ❌ No handler | Schema exists, no handler |
| **Reset** | CSMS → CS | ❌ Missing | ❌ No handler | Schema exists, no handler |
| **StartTransaction** | CS → CSMS | ✅ Implemented | `start_transaction.go` | Transaction initiation |
| **StatusNotification** | CS → CSMS | ✅ Implemented | `status_notification.go` | Connector status updates |
| **StopTransaction** | CS → CSMS | ✅ Implemented | `stop_transaction.go` | Transaction completion |
| **UnlockConnector** | CSMS → CS | ❌ Missing | ❌ No handler | Schema exists, no handler |

**Core Profile Coverage:** 7/16 = **44%** fully implemented

**Critical Missing Features:**
- 🚨 **RemoteStartTransaction** - Cannot start charging remotely (major feature gap)
- 🚨 **RemoteStopTransaction** - Cannot stop charging remotely (major feature gap)
- 🚨 **GetConfiguration** - Cannot query charge station settings
- ⚠️ **ChangeConfiguration** - Can only receive responses, cannot initiate changes
- ⚠️ **Reset** - Cannot remotely reset charge stations
- ⚠️ **UnlockConnector** - Cannot unlock stuck connectors

---

### Smart Charging Profile (Optional)

Advanced load management and charging schedule features.

| Message | Direction | Status | Handler | Notes |
|---------|-----------|--------|---------|-------|
| **ClearChargingProfile** | CSMS → CS | ❌ Missing | ❌ No handler | Schema exists |
| **GetCompositeSchedule** | CSMS → CS | ❌ Missing | ❌ No handler | Schema exists |
| **SetChargingProfile** | CSMS → CS | ❌ Missing | ❌ No handler | Schema exists |

**Smart Charging Coverage:** 0/3 = **0%**

**Impact:** No smart charging, load balancing, or dynamic pricing features.

---

### Remote Trigger Profile (Optional)

Ability to trigger charge station actions on demand.

| Message | Direction | Status | Handler | Notes |
|---------|-----------|--------|---------|-------|
| **TriggerMessage** | CSMS → CS | ⚠️ Partial | `trigger_message_result.go` | Only CallResult handler (response processing) |
| **ExtendedTriggerMessage** | CSMS → CS | ❌ Missing | ❌ No handler | Schema exists |

**Remote Trigger Coverage:** 0.5/2 = **25%**

**Note:** Can process TriggerMessage responses but cannot initiate triggers.

---

### Firmware Management Profile (Optional)

Over-the-air firmware update capabilities.

| Message | Direction | Status | Handler | Notes |
|---------|-----------|--------|---------|-------|
| **GetDiagnostics** | CSMS → CS | ❌ Missing | ❌ No handler | Schema exists |
| **UpdateFirmware** | CSMS → CS | ❌ Missing | ❌ No handler | Schema exists |
| **DiagnosticsStatusNotification** | CS → CSMS | ❌ Missing | ❌ No handler | Schema exists |
| **FirmwareStatusNotification** | CS → CSMS | ❌ Missing | ❌ No handler | Schema exists |
| **SignedUpdateFirmware** | CSMS → CS | ❌ Missing | ❌ No handler | Schema exists (security extension) |
| **SignedFirmwareStatusNotification** | CS → CSMS | ❌ Missing | ❌ No handler | Schema exists (security extension) |

**Firmware Management Coverage:** 0/6 = **0%**

**Impact:** No remote firmware update capability.

---

### Local Auth List Management Profile (Optional)

Offline authorization list synchronization.

| Message | Direction | Status | Handler | Notes |
|---------|-----------|--------|---------|-------|
| **GetLocalListVersion** | CSMS → CS | ❌ Missing | ❌ No handler | Schema exists |
| **SendLocalList** | CSMS → CS | ❌ Missing | ❌ No handler | Schema exists |

**Local Auth List Coverage:** 0/2 = **0%**

**Impact:** Cannot sync authorization lists for offline operation.

---

### Reservation Profile (Optional)

Charge point reservation for future sessions.

| Message | Direction | Status | Handler | Notes |
|---------|-----------|--------|---------|-------|
| **ReserveNow** | CSMS → CS | ❌ Missing | ❌ No handler | Schema exists |
| **CancelReservation** | CSMS → CS | ❌ Missing | ❌ No handler | Schema exists |

**Reservation Coverage:** 0/2 = **0%**

**Impact:** Cannot reserve charge points in advance.

---

### Security Extensions (OCPP 1.6 Security Whitepaper)

Enhanced security features added in OCPP 1.6 Edition 2.

| Message | Direction | Status | Handler | Notes |
|---------|-----------|--------|---------|-------|
| **CertificateSigned** | CSMS → CS | ✅ Implemented | Via `data_transfer_result.go` | ISO 15118 support |
| **DeleteCertificate** | CSMS → CS | ❌ Missing | ❌ No handler | Schema exists |
| **GetInstalledCertificateIds** | CSMS → CS | ❌ Missing | ❌ No handler | Schema exists |
| **GetLog** | CSMS → CS | ❌ Missing | ❌ No handler | Schema exists |
| **InstallCertificate** | CSMS → CS | ✅ Implemented | Via `data_transfer_result.go` | ISO 15118 support |
| **LogStatusNotification** | CS → CSMS | ❌ Missing | ❌ No handler | Schema exists |
| **SecurityEventNotification** | CS → CSMS | ✅ Implemented | `security_event_notification.go` | Security event logging |
| **SignCertificate** | CSMS → CS | ✅ Implemented | Via `data_transfer.go` | Certificate signing request |

**Security Extensions Coverage:** 4/8 = **50%**

**Note:** ISO 15118 Plug & Charge certificates are supported, but general security management features are missing.

---

### ISO 15118 Plug & Charge (via DataTransfer)

**Vendor ID:** `org.openchargealliance.iso15118pnc`

| Message | MessageId | Status | Handler | Notes |
|---------|-----------|--------|---------|-------|
| **Authorize** | `Authorize` | ✅ Implemented | Via `data_transfer.go` | OCPP 2.0.1 message tunneled through DataTransfer |
| **GetCertificateStatus** | `GetCertificateStatus` | ✅ Implemented | Via `data_transfer.go` | Certificate validation |
| **SignCertificate** | `SignCertificate` | ✅ Implemented | Via `data_transfer.go` | Certificate signing |
| **Get15118EVCertificate** | `Get15118EVCertificate` | ✅ Implemented | Via `data_transfer.go` | EV certificate provisioning |

**Also supports Has2Be variant** (`vendorId: "iso15118"`) for each message.

**ISO 15118 Coverage:** 4/4 = **100%** ✅

**Note:** This is a strength of the implementation - full Plug & Charge support.

---

## Schema vs Handler Gap

### Schemas Present, Handlers Missing

The following messages have **complete JSON schemas** but **no handler implementation**:

#### Core Profile (7 missing)
1. ❌ **ChangeAvailability** - Cannot change connector availability
2. ❌ **ClearCache** - Cannot clear authorization cache
3. ❌ **GetConfiguration** - Cannot query settings
4. ❌ **RemoteStartTransaction** - Cannot start remotely
5. ❌ **RemoteStopTransaction** - Cannot stop remotely
6. ❌ **Reset** - Cannot reset charge stations
7. ❌ **UnlockConnector** - Cannot unlock connectors

#### Smart Charging (3 missing)
8. ❌ **ClearChargingProfile**
9. ❌ **GetCompositeSchedule**
10. ❌ **SetChargingProfile**

#### Remote Trigger (1 missing)
11. ❌ **ExtendedTriggerMessage**

#### Firmware Management (6 missing)
12. ❌ **GetDiagnostics**
13. ❌ **UpdateFirmware**
14. ❌ **DiagnosticsStatusNotification**
15. ❌ **FirmwareStatusNotification**
16. ❌ **SignedUpdateFirmware**
17. ❌ **SignedFirmwareStatusNotification**

#### Local Auth List (2 missing)
18. ❌ **GetLocalListVersion**
19. ❌ **SendLocalList**

#### Reservation (2 missing)
20. ❌ **ReserveNow**
21. ❌ **CancelReservation**

#### Security Extensions (4 missing)
22. ❌ **DeleteCertificate**
23. ❌ **GetInstalledCertificateIds**
24. ❌ **GetLog**
25. ❌ **LogStatusNotification**

**Total: 25 messages have schemas but no handlers**

This represents a **significant implementation gap** - the infrastructure (schemas, types) exists but business logic is missing.

---

## Implemented Messages (Detail)

### Fully Implemented ✅

These messages have complete handler implementations and are production-ready:

#### 1. **Authorize** (`authorize.go`)
```go
type AuthorizeHandler struct {
    TokenStore store.TokenStore
}
```
- ✅ Validates RFID/token against TokenStore
- ✅ Returns authorization status
- ✅ Handles cache modes (ALWAYS, NEVER, etc.)

#### 2. **BootNotification** (`boot_notification.go`)
```go
type BootNotificationHandler struct {
    Clock               clock.PassiveClock
    RuntimeDetailsStore store.ChargeStationRuntimeDetailsStore
    SettingsStore       store.ChargeStationSettingsStore
    HeartbeatInterval   int
}
```
- ✅ Registers charge station
- ✅ Stores runtime details (vendor, model, serial, firmware)
- ✅ Clears reboot-required settings
- ✅ Returns heartbeat interval

#### 3. **DataTransfer** (`data_transfer.go`)
```go
type DataTransferHandler struct {
    SchemaFS   fs.FS
    CallRoutes map[string]map[string]handlers.CallRoute
}
```
- ✅ Bidirectional data transfer
- ✅ Vendor-specific extensions
- ✅ **ISO 15118 Plug & Charge** support
- ✅ **Has2Be** variant support
- ✅ Nested routing for vendor messages

**Supported Vendors:**
- `org.openchargealliance.iso15118pnc`
  - Authorize (OCPP 2.0.1 via DataTransfer)
  - GetCertificateStatus
  - SignCertificate
  - Get15118EVCertificate
- `iso15118` (Has2Be extensions)
  - Same messages as above with Has2Be variants

#### 4. **Heartbeat** (`heartbeat.go`)
```go
type HeartbeatHandler struct {
    Clock clock.PassiveClock
}
```
- ✅ Returns current server time
- ✅ Keep-alive mechanism

#### 5. **MeterValues** (`meter_values.go`)
```go
type MeterValuesHandler struct {
    TransactionStore store.TransactionStore
}
```
- ✅ Stores meter values for active transactions
- ✅ Updates transaction with new readings

#### 6. **SecurityEventNotification** (`security_event_notification.go`)
```go
type SecurityEventNotificationHandler struct{}
```
- ✅ Logs security events
- ✅ Returns empty response (ack)

#### 7. **StartTransaction** (`start_transaction.go`)
```go
type StartTransactionHandler struct {
    Clock            clock.PassiveClock
    TokenStore       store.TokenStore
    TransactionStore store.TransactionStore
}
```
- ✅ Validates token authorization
- ✅ Creates transaction record
- ✅ Stores initial meter values
- ✅ Returns transaction ID

#### 8. **StatusNotification** (`status_notification.go`)
```go
func StatusNotificationHandler(ctx context.Context, chargeStationId string, 
                               request ocpp.Request) (ocpp.Response, error)
```
- ✅ Receives connector status updates
- ✅ Returns empty response (ack)
- ⚠️ **Does not persist status** (just acknowledges)

#### 9. **StopTransaction** (`stop_transaction.go`)
```go
type StopTransactionHandler struct {
    Clock            clock.PassiveClock
    TokenStore       store.TokenStore
    TransactionStore store.TransactionStore
}
```
- ✅ Validates token (optional)
- ✅ Updates transaction with stop details
- ✅ Stores final meter values
- ✅ Records stop reason

---

### Partially Implemented ⚠️

These messages have CallResult handlers (response processing) but cannot be initiated by the CSMS:

#### 1. **ChangeConfiguration** (`change_configuration_result.go`)
```go
type ChangeConfigurationResultHandler struct {
    SettingsStore store.ChargeStationSettingsStore
    CallMaker     handlers.CallMaker
}
```
- ✅ Processes responses from charge station
- ✅ Updates setting status (Accepted, Rejected, RebootRequired)
- ✅ Can trigger reboot if needed
- ❌ **Cannot initiate** ChangeConfiguration requests

**Gap:** No Call handler to send configuration changes to charge stations.

#### 2. **TriggerMessage** (`trigger_message_result.go`)
```go
type TriggerMessageResultHandler struct{}
```
- ✅ Processes TriggerMessage responses
- ❌ **Cannot initiate** TriggerMessage requests

**Gap:** No Call handler to trigger messages from charge stations.

---

## Missing Messages (Priority Assessment)

### 🔴 **Critical Priority** (Core functionality gaps)

These are essential for basic CSMS operations:

#### 1. **RemoteStartTransaction** ⭐⭐⭐⭐⭐
**Impact:** Cannot start charging sessions remotely (e.g., from mobile app, OCPI roaming)

**Use Cases:**
- Mobile app: "Start charging on connector 1"
- Roaming: eMSP triggers charging for authorized user
- Fleet management: Pre-authorize vehicles

**Difficulty:** ⭐⭐ (Medium)
- CallMaker already exists in routing.go
- Just needs handler implementation

#### 2. **RemoteStopTransaction** ⭐⭐⭐⭐⭐
**Impact:** Cannot stop charging sessions remotely (safety/billing issue)

**Use Cases:**
- Emergency stop
- Payment failures
- Session timeouts
- Roaming session end

**Difficulty:** ⭐ (Easy)
- Similar to RemoteStartTransaction

#### 3. **GetConfiguration** ⭐⭐⭐⭐
**Impact:** Cannot query charge station settings (blind configuration management)

**Use Cases:**
- Verify settings before making changes
- Troubleshooting
- Configuration audit
- Display settings in admin UI

**Difficulty:** ⭐⭐ (Medium)
- Need to handle variable-length key arrays

#### 4. **ChangeConfiguration** (Call handler) ⭐⭐⭐⭐
**Impact:** Cannot configure charge stations remotely

**Use Cases:**
- Change heartbeat interval
- Update connection settings
- Enable/disable features
- Set pricing parameters

**Difficulty:** ⭐ (Easy)
- CallResult handler exists
- Just need Call handler

#### 5. **Reset** ⭐⭐⭐
**Impact:** Cannot remotely reboot charge stations

**Use Cases:**
- Fix stuck charge stations
- Apply configuration changes
- Troubleshooting

**Difficulty:** ⭐ (Easy)

#### 6. **UnlockConnector** ⭐⭐⭐
**Impact:** Cannot help users with stuck connectors

**Use Cases:**
- User calls support: "Connector is locked"
- Emergency unlock
- Maintenance

**Difficulty:** ⭐ (Easy)

---

### 🟡 **High Priority** (Important features)

#### 7. **ChangeAvailability** ⭐⭐⭐⭐
**Impact:** Cannot take connectors online/offline

**Use Cases:**
- Maintenance mode
- Scheduled downtime
- Connector malfunction

**Difficulty:** ⭐ (Easy)

#### 8. **ClearCache** ⭐⭐⭐
**Impact:** Cannot clear authorization cache

**Use Cases:**
- Token revoked - need immediate effect
- Testing
- Security incident response

**Difficulty:** ⭐ (Easy)

#### 9. **TriggerMessage** (Call handler) ⭐⭐⭐
**Impact:** Cannot request charge station to send specific messages

**Use Cases:**
- Force status update
- Request meter values
- Request boot notification
- Troubleshooting

**Difficulty:** ⭐ (Easy)

---

### 🟠 **Medium Priority** (Optional profiles)

#### Smart Charging Profile

#### 10. **SetChargingProfile** ⭐⭐⭐
**Impact:** No load management or dynamic pricing

**Difficulty:** ⭐⭐⭐⭐ (Complex)
- Requires understanding of composite schedules
- Stack-based profile management

#### 11. **GetCompositeSchedule** ⭐⭐
**Impact:** Cannot verify active charging schedule

**Difficulty:** ⭐⭐⭐ (Moderate)

#### 12. **ClearChargingProfile** ⭐⭐
**Impact:** Cannot remove charging profiles

**Difficulty:** ⭐⭐ (Medium)

---

#### Firmware Management Profile

#### 13-18. **Firmware Management** (6 messages) ⭐⭐
**Impact:** No OTA firmware updates

**Difficulty:** ⭐⭐⭐⭐ (Complex)
- Requires file transfer infrastructure
- Status tracking
- Rollback handling

---

#### Local Auth List Profile

#### 19-20. **Local Auth List** (2 messages) ⭐⭐
**Impact:** No offline authorization capability

**Difficulty:** ⭐⭐⭐ (Moderate)
- Requires list synchronization logic
- Version management

---

#### Reservation Profile

#### 21-22. **Reservation** (2 messages) ⭐⭐
**Impact:** Cannot reserve charge points

**Difficulty:** ⭐⭐⭐ (Moderate)
- Requires reservation state management
- Expiry handling

---

### 🟢 **Low Priority** (Advanced features)

#### Security Extensions

#### 23-26. **Security Management** (4 messages) ⭐
**Impact:** Limited security operations

**Note:** Core security (certificates for ISO 15118) is already implemented.

**Difficulty:** ⭐⭐⭐ (Moderate to Complex)

---

## Priority Recommendations

### Phase 1: Core Functionality Completion (2-4 weeks)

**Goal:** Complete OCPP 1.6 Core Profile to production quality

**Tasks:**
1. ✅ **RemoteStartTransaction** - Enable remote start capability
2. ✅ **RemoteStopTransaction** - Enable remote stop capability
3. ✅ **GetConfiguration** - Add configuration query
4. ✅ **ChangeConfiguration** Call handler - Enable configuration changes
5. ✅ **Reset** - Add reboot capability
6. ✅ **UnlockConnector** - Add connector unlock
7. ✅ **ChangeAvailability** - Add availability management
8. ✅ **ClearCache** - Add cache clearing
9. ✅ **TriggerMessage** Call handler - Add message triggering

**Expected Outcome:** Core Profile at **100%** (16/16 messages)

---

### Phase 2: Smart Charging & Management (4-6 weeks)

**Goal:** Add load management and advanced features

**Tasks:**
1. ✅ **SetChargingProfile** - Enable load management
2. ✅ **GetCompositeSchedule** - Query active schedules
3. ✅ **ClearChargingProfile** - Remove profiles
4. ✅ **TriggerMessage** extended support
5. ⚠️ **ExtendedTriggerMessage** (if needed)

**Expected Outcome:** Smart Charging at **100%**

---

### Phase 3: Optional Profiles (6-8 weeks)

**Goal:** Complete optional profile support

**Tasks:**
1. **Firmware Management Profile**
   - GetDiagnostics
   - UpdateFirmware
   - Status notifications (2 messages)
2. **Local Auth List Profile**
   - GetLocalListVersion
   - SendLocalList
3. **Reservation Profile**
   - ReserveNow
   - CancelReservation
4. **Security Extensions**
   - Additional certificate management
   - Log retrieval

**Expected Outcome:** Optional profiles functional

---

### Phase 4: Production Hardening (2-4 weeks)

**Goal:** Polish and production readiness

**Tasks:**
1. **StatusNotification** enhancement - Persist connector states
2. **Error handling** improvements
3. **Monitoring and metrics**
4. **End-to-end testing** with real charge stations
5. **OCTT** (OCPP Compliance Testing Tool) validation
6. **Load testing**
7. **Documentation** updates

---

## Implementation Roadmap

### Quick Wins (Week 1-2)

These are **easy implementations** with **high impact**:

1. **Reset** (1 day)
   ```go
   type ResetHandler struct {
       // No dependencies needed
   }
   ```

2. **UnlockConnector** (1 day)
   ```go
   type UnlockConnectorHandler struct {
       // No dependencies needed
   }
   ```

3. **ClearCache** (1 day)
   ```go
   type ClearCacheHandler struct {
       // No dependencies needed
   }
   ```

4. **ChangeAvailability** (1 day)
   ```go
   type ChangeAvailabilityHandler struct {
       // Might want to store availability status
   }
   ```

5. **ChangeConfiguration** Call handler (1 day)
   - CallResult handler already exists
   - Just need to add Call handler to routing

6. **TriggerMessage** Call handler (1 day)
   - CallResult handler already exists
   - Just need to add Call handler

**Total:** ~1 week, adds **6 critical messages**

---

### Core Transactions (Week 3-4)

**High complexity, high impact:**

1. **RemoteStartTransaction** (3-5 days)
   ```go
   type RemoteStartTransactionHandler struct {
       TokenStore       store.TokenStore
       TransactionStore store.TransactionStore
   }
   ```
   - Validate token
   - Check connector availability
   - Initiate charging session

2. **RemoteStopTransaction** (2-3 days)
   ```go
   type RemoteStopTransactionHandler struct {
       TransactionStore store.TransactionStore
   }
   ```
   - Find active transaction
   - Request stop

3. **GetConfiguration** (2-3 days)
   ```go
   type GetConfigurationHandler struct {
       ConfigStore store.ChargeStationSettingsStore
   }
   ```
   - Query settings
   - Return current values

**Total:** ~2 weeks, completes **critical transaction features**

---

### Smart Charging (Week 5-8)

**Complex but valuable:**

1. **SetChargingProfile** (1-2 weeks)
   - Profile validation
   - Stack management
   - Schedule calculation

2. **GetCompositeSchedule** (3-5 days)
   - Composite schedule generation
   - Profile stacking logic

3. **ClearChargingProfile** (2-3 days)
   - Profile removal
   - Stack updates

---

## Code Examples

### Example 1: RemoteStartTransaction Handler

```go
// SPDX-License-Identifier: Apache-2.0

package ocpp16

import (
    "context"
    "github.com/thoughtworks/maeve-csms/manager/ocpp"
    types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
    "github.com/thoughtworks/maeve-csms/manager/store"
)

type RemoteStartTransactionHandler struct {
    TokenStore store.TokenStore
}

func (r RemoteStartTransactionHandler) HandleCallResult(
    ctx context.Context, 
    chargeStationId string, 
    request ocpp.Request, 
    response ocpp.Response, 
    state any,
) error {
    req := request.(*types.RemoteStartTransactionJson)
    resp := response.(*types.RemoteStartTransactionResponseJson)
    
    // Optional: Log remote start result
    if resp.Status == types.RemoteStartTransactionResponseJsonStatusAccepted {
        // Successfully initiated remote start
        slog.Info("remote start accepted", 
            "chargeStationId", chargeStationId,
            "idTag", req.IdTag)
    } else {
        // Remote start rejected
        slog.Warn("remote start rejected", 
            "chargeStationId", chargeStationId,
            "idTag", req.IdTag)
    }
    
    return nil
}
```

**Add to routing.go:**

```go
CallResultRoutes: map[string]handlers.CallResultRoute{
    // ... existing routes
    "RemoteStartTransaction": {
        NewRequest:     func() ocpp.Request { return new(ocpp16.RemoteStartTransactionJson) },
        NewResponse:    func() ocpp.Response { return new(ocpp16.RemoteStartTransactionResponseJson) },
        RequestSchema:  "ocpp16/RemoteStartTransaction.json",
        ResponseSchema: "ocpp16/RemoteStartTransactionResponse.json",
        Handler: RemoteStartTransactionHandler{
            TokenStore: engine,
        },
    },
}
```

**Add to CallMaker Actions:**

```go
Actions: map[reflect.Type]string{
    reflect.TypeOf(&ocpp16.ChangeConfigurationJson{}):       "ChangeConfiguration",
    reflect.TypeOf(&ocpp16.TriggerMessageJson{}):            "TriggerMessage",
    reflect.TypeOf(&ocpp16.RemoteStartTransactionJson{}):    "RemoteStartTransaction",  // ADD THIS
    reflect.TypeOf(&ocpp16.RemoteStopTransactionJson{}):     "RemoteStopTransaction",   // ADD THIS
},
```

---

### Example 2: Reset Handler

```go
// SPDX-License-Identifier: Apache-2.0

package ocpp16

import (
    "context"
    "github.com/thoughtworks/maeve-csms/manager/ocpp"
    types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
)

type ResetHandler struct{}

func (r ResetHandler) HandleCallResult(
    ctx context.Context, 
    chargeStationId string, 
    request ocpp.Request, 
    response ocpp.Response, 
    state any,
) error {
    req := request.(*types.ResetJson)
    resp := response.(*types.ResetResponseJson)
    
    if resp.Status == types.ResetResponseJsonStatusAccepted {
        slog.Info("reset accepted", 
            "chargeStationId", chargeStationId,
            "type", req.Type)
    } else {
        slog.Warn("reset rejected", 
            "chargeStationId", chargeStationId,
            "type", req.Type)
    }
    
    return nil
}
```

---

## Testing Strategy

### Unit Tests

Each handler needs comprehensive unit tests:

```go
func TestRemoteStartTransactionHandler(t *testing.T) {
    tests := []struct {
        name          string
        request       *ocpp16.RemoteStartTransactionJson
        response      *ocpp16.RemoteStartTransactionResponseJson
        expectedError error
    }{
        {
            name: "successful remote start",
            request: &ocpp16.RemoteStartTransactionJson{
                IdTag:        "RFID123",
                ConnectorId:  1,
            },
            response: &ocpp16.RemoteStartTransactionResponseJson{
                Status: ocpp16.RemoteStartTransactionResponseJsonStatusAccepted,
            },
            expectedError: nil,
        },
        {
            name: "rejected remote start",
            request: &ocpp16.RemoteStartTransactionJson{
                IdTag:        "RFID123",
                ConnectorId:  1,
            },
            response: &ocpp16.RemoteStartTransactionResponseJson{
                Status: ocpp16.RemoteStartTransactionResponseJsonStatusRejected,
            },
            expectedError: nil,
        },
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            handler := RemoteStartTransactionHandler{
                TokenStore: mockTokenStore,
            }
            
            err := handler.HandleCallResult(ctx, "cs001", tt.request, tt.response, nil)
            
            assert.Equal(t, tt.expectedError, err)
        })
    }
}
```

### Integration Tests

Test with MQTT and full message flow:

```go
func TestRemoteStartTransactionIntegration(t *testing.T) {
    // Setup MQTT broker
    // Setup manager with handler
    // Send RemoteStartTransaction via CallMaker
    // Verify message on MQTT topic
    // Simulate charge station response
    // Verify handler processes response
}
```

### E2E Tests

Test with real charge station simulators (see `e2e_tests/`).

---

## Compliance and Certification

### OCPP Compliance Testing Tool (OCTT)

To verify full compliance, use OCTT:

1. **Core Profile Tests**
   - All Core messages must pass
   - Current: 7/16 would pass
   - After Phase 1: 16/16 should pass

2. **Optional Profile Tests**
   - Smart Charging
   - Firmware Management
   - Local Auth List
   - Reservation

### Certification Process

1. Complete Core Profile (Phase 1)
2. Run OCTT Core tests
3. Fix any failures
4. Document compliance
5. Optional: Certify optional profiles

---

## Conclusion

### Current State

✅ **Strengths:**
- Solid foundation (36% implemented)
- Full ISO 15118 Plug & Charge support (100%)
- Good transaction management
- Clean architecture

⚠️ **Weaknesses:**
- Core Profile incomplete (44%)
- No remote control capabilities (RemoteStart/Stop)
- No configuration management (Get/Change Configuration)
- No smart charging (0%)
- No firmware management (0%)

### Path Forward

**8-12 weeks to production-quality OCPP 1.6 implementation:**

- **Weeks 1-2:** Quick wins (6 easy messages)
- **Weeks 3-4:** Core transactions (RemoteStart/Stop, GetConfiguration)
- **Weeks 5-8:** Smart Charging
- **Weeks 9-12:** Optional profiles + hardening

### Priority Actions

**This week:**
1. Implement Reset handler
2. Implement UnlockConnector handler
3. Implement ClearCache handler
4. Add ChangeConfiguration Call handler
5. Add TriggerMessage Call handler

**Next week:**
1. Implement RemoteStartTransaction
2. Implement RemoteStopTransaction
3. Implement GetConfiguration

This would bring **Core Profile to 88%** (14/16) in just 2 weeks!

---

## Appendix: Message Reference

### Full OCPP 1.6 Message List

| # | Message | Profile | Direction | Status |
|---|---------|---------|-----------|--------|
| 1 | Authorize | Core | CS→CSMS | ✅ |
| 2 | BootNotification | Core | CS→CSMS | ✅ |
| 3 | CancelReservation | Reservation | CSMS→CS | ❌ |
| 4 | CertificateSigned | Security | CSMS→CS | ✅ |
| 5 | ChangeAvailability | Core | CSMS→CS | ❌ |
| 6 | ChangeConfiguration | Core | CSMS→CS | ⚠️ |
| 7 | ClearCache | Core | CSMS→CS | ❌ |
| 8 | ClearChargingProfile | Smart Charging | CSMS→CS | ❌ |
| 9 | DataTransfer | Core | Both | ✅ |
| 10 | DeleteCertificate | Security | CSMS→CS | ❌ |
| 11 | DiagnosticsStatusNotification | Firmware | CS→CSMS | ❌ |
| 12 | ExtendedTriggerMessage | Remote Trigger | CSMS→CS | ❌ |
| 13 | FirmwareStatusNotification | Firmware | CS→CSMS | ❌ |
| 14 | GetCompositeSchedule | Smart Charging | CSMS→CS | ❌ |
| 15 | GetConfiguration | Core | CSMS→CS | ❌ |
| 16 | GetDiagnostics | Firmware | CSMS→CS | ❌ |
| 17 | GetInstalledCertificateIds | Security | CSMS→CS | ❌ |
| 18 | GetLocalListVersion | Local Auth | CSMS→CS | ❌ |
| 19 | GetLog | Security | CSMS→CS | ❌ |
| 20 | Heartbeat | Core | CS→CSMS | ✅ |
| 21 | InstallCertificate | Security | CSMS→CS | ✅ |
| 22 | LogStatusNotification | Security | CS→CSMS | ❌ |
| 23 | MeterValues | Core | CS→CSMS | ✅ |
| 24 | RemoteStartTransaction | Core | CSMS→CS | ❌ |
| 25 | RemoteStopTransaction | Core | CSMS→CS | ❌ |
| 26 | ReserveNow | Reservation | CSMS→CS | ❌ |
| 27 | Reset | Core | CSMS→CS | ❌ |
| 28 | SecurityEventNotification | Security | CS→CSMS | ✅ |
| 29 | SendLocalList | Local Auth | CSMS→CS | ❌ |
| 30 | SetChargingProfile | Smart Charging | CSMS→CS | ❌ |
| 31 | SignCertificate | Security | CS→CSMS | ✅ |
| 32 | SignedFirmwareStatusNotification | Security | CS→CSMS | ❌ |
| 33 | SignedUpdateFirmware | Security | CSMS→CS | ❌ |
| 34 | StartTransaction | Core | CS→CSMS | ✅ |
| 35 | StatusNotification | Core | CS→CSMS | ✅ |
| 36 | StopTransaction | Core | CS→CSMS | ✅ |
| 37 | TriggerMessage | Remote Trigger | CSMS→CS | ⚠️ |
| 38 | UnlockConnector | Core | CSMS→CS | ❌ |
| 39 | UpdateFirmware | Firmware | CSMS→CS | ❌ |

**Legend:**
- ✅ Fully implemented
- ⚠️ Partially implemented (CallResult only)
- ❌ Missing

---

**End of Audit Report**
