# ocpp-go Integration Plan for MaEVe CSMS

> Architectural Decision Document  
> Created: 2026-02-12  
> Status: Draft

## Table of Contents

1. [Current State Analysis](#1-current-state-analysis)
2. [Overlap Map](#2-overlap-map)
3. [Migration Plan](#3-migration-plan)
4. [Risk Assessment](#4-risk-assessment)
5. [Code Reduction Estimate](#5-code-reduction-estimate)
6. [OCPP 1.6 Specifics](#6-ocpp-16-specifics)
7. [OCPP 2.0.1 Specifics](#7-ocpp-201-specifics)

---

## 1. Current State Analysis

### MaEVe Architecture Overview

MaEVe uses a **two-process architecture** with MQTT as the message bus between them:

```
Charge Station ←WebSocket→ Gateway ←MQTT→ Manager
```

- **Gateway** (`gateway/`): Accepts WebSocket connections, handles OCPP-J framing (marshal/unmarshal the `[messageType, messageId, ...]` array format), authenticates charge stations via device registry, and bridges messages to/from MQTT.
- **Manager** (`manager/`): Subscribes to MQTT topics, routes messages to handlers, contains all business logic.

### What MaEVe Implements Manually

| Component | Location | Lines | Description |
|---|---|---|---|
| OCPP 1.6 message types | `manager/ocpp/ocpp16/` | ~2,200 (73 files) | Hand-written Go structs for all 1.6 request/response types |
| OCPP 2.0.1 message types | `manager/ocpp/ocpp201/` | ~2,300 (74 files) | Hand-written Go structs for all 2.0.1 request/response types |
| has2be extension types | `manager/ocpp/has2be/` | ~340 (11 files) | ISO 15118 extensions for OCPP 1.6 |
| OCPP-J framing | `gateway/ocpp/ocppj.go` | Part of gateway | Manual `[type, id, action, payload]` array marshaling/unmarshaling |
| WebSocket server | `gateway/server/` | ~4,395 total (26 files) | nhooyr.io/websocket, manual subprotocol negotiation, security profiles 1-3 |
| MQTT transport layer | `manager/transport/` | ~1,200 (14 files) | Custom Listener/Emitter/Message abstractions over MQTT |
| Message routing | `manager/handlers/router.go` | ~120 | Action→handler dispatch with JSON schema validation |
| JSON schema validation | `manager/schemas/` | ~100 Go + 224 JSON files | jsonschema-based validation of all request/response payloads |
| Handler framework | `manager/handlers/types.go` | ~65 | CallHandler/CallResultHandler interfaces, CallRoute/CallResultRoute types |
| Request/Response interfaces | `manager/ocpp/types.go` | ~10 | Marker interfaces `Request{IsRequest()}`, `Response{IsResponse()}` |
| Message correlation | Gateway pipe system | ~200 | `gateway/pipe/` manages request/response correlation via channels |

### What ocpp-go Provides

ocpp-go is a complete OCPP implementation library that provides:

1. **All OCPP 1.6 and 2.0.1 message types** as Go structs with validation tags
2. **WebSocket server** (Central System / CSMS) with connection management
3. **WebSocket client** (Charge Point / Charging Station) 
4. **OCPP-J protocol layer** — message framing, serialization, deserialization
5. **Message routing** — automatic dispatch to profile-based handler interfaces
6. **Request/response correlation** — built-in message ID tracking and async callbacks
7. **Struct-based validation** using `go-playground/validator` tags (not JSON Schema)
8. **Ping/pong** and connection timeout management
9. **TLS support**

### Key Architectural Difference

MaEVe's Gateway↔MQTT↔Manager split exists to allow horizontal scaling of the manager. ocpp-go assumes a single-process model where WebSocket connections and message handling are in the same process.

**This is the fundamental tension of the integration.**

---

## 2. Overlap Map

### Side-by-Side Comparison

| MaEVe Component | ocpp-go Equivalent | Compatibility |
|---|---|---|
| `manager/ocpp/ocpp16/*.go` — 73 files of request/response structs | `ocpp1.6/core/`, `ocpp1.6/firmware/`, `ocpp1.6/localauth/`, `ocpp1.6/remotetrigger/`, `ocpp1.6/reservation/`, `ocpp1.6/smartcharging/`, `ocpp1.6/types/` | ⚠️ Similar but different field names, no `IsRequest()`/`IsResponse()` markers. ocpp-go uses `GetFeatureName()` method. |
| `manager/ocpp/ocpp201/*.go` — 74 files of request/response structs | `ocpp2.0.1/` sub-packages (authorization, availability, provisioning, transactions, etc.) | ⚠️ Same caveat as 1.6 |
| `manager/ocpp/has2be/*.go` — ISO 15118 extensions | **No equivalent** | ❌ ocpp-go does not have has2be/ISO 15118 PnC extensions for OCPP 1.6 |
| `manager/ocpp/types.go` — `Request`/`Response` interfaces | `ocpp.Request`/`ocpp.Response` in ocpp-go (with `GetFeatureName()`) | ⚠️ Different interface; ocpp-go's is richer |
| `gateway/server/` — WebSocket server + auth | `ws.Server` + `ocpp16.CentralSystem` / `ocpp2.NewCSMS()` | ⚠️ ocpp-go has built-in WS server but MaEVe needs custom auth (security profiles 1-3, device registry, TLS client certs) |
| `gateway/ocpp/ocppj.go` — OCPP-J framing | `ocppj` package in ocpp-go | ✅ Full replacement possible |
| `gateway/pipe/` — message correlation | Built into ocpp-go's `ocppj.Client`/`ocppj.Server` | ✅ Full replacement possible |
| `manager/transport/` — MQTT transport | **No equivalent** | ❌ ocpp-go has no MQTT transport; it's direct WebSocket |
| `manager/handlers/router.go` — action→handler routing | Built into `CentralSystem`/`CSMS` via profile handlers | ⚠️ Different pattern: ocpp-go uses typed callback interfaces per profile, MaEVe uses a generic `map[string]CallRoute` |
| `manager/handlers/types.go` — `CallHandler`/`CallResultHandler` | Profile handler interfaces (e.g., `core.CentralSystemHandler`) | ⚠️ ocpp-go has per-profile interfaces, not a generic handler |
| `manager/schemas/` — JSON Schema validation | `go-playground/validator` struct tags | ⚠️ Different validation approach; ocpp-go validates structs, MaEVe validates raw JSON |

### Handler Interface Comparison

**MaEVe pattern:**
```go
type CallHandler interface {
    HandleCall(ctx context.Context, chargeStationId string, request ocpp.Request) (ocpp.Response, error)
}
```

**ocpp-go 1.6 pattern:**
```go
type CentralSystemHandler interface {
    OnAuthorize(chargePointId string, request *core.AuthorizeRequest) (*core.AuthorizeConfirmation, error)
    OnBootNotification(chargePointId string, request *core.BootNotificationRequest) (*core.BootNotificationConfirmation, error)
    // ... one method per message type
}
```

**ocpp-go 2.0.1 pattern:**
```go
// Multiple handler interfaces, one per functional block:
type ProvisioningCSMSHandler interface { ... }
type AuthorizationCSMSHandler interface { ... }
type TransactionsCSMSHandler interface { ... }
// etc.
```

---

## 3. Migration Plan

### Phase 1: Message Types (Replace hand-written structs with ocpp-go types)

**Goal:** Eliminate `manager/ocpp/ocpp16/`, `manager/ocpp/ocpp201/` by using ocpp-go's types.

**Complexity:** HIGH — Touches every handler file.

#### Steps:

- [ ] **1.1** Add `github.com/lorenzodonini/ocpp-go` as a dependency
- [ ] **1.2** Create adapter layer: thin wrapper types or type aliases mapping ocpp-go types to satisfy MaEVe's `ocpp.Request`/`ocpp.Response` interfaces
  ```go
  // Option A: Wrapper
  type BootNotificationRequest struct {
      *core.BootNotificationRequest
  }
  func (*BootNotificationRequest) IsRequest() {}
  
  // Option B: Change MaEVe's interface to match ocpp-go's
  // This is more invasive but cleaner long-term
  ```
- [ ] **1.3** For OCPP 1.6: Map all 36+ message pairs from MaEVe structs to ocpp-go equivalents
  - `ocpp16.BootNotificationJson` → `core.BootNotificationRequest`
  - `ocpp16.AuthorizeJson` → `core.AuthorizeRequest`
  - `ocpp16.StartTransactionJson` → `core.StartTransactionRequest`
  - ... (all message types)
- [ ] **1.4** For OCPP 2.0.1: Map all 37+ message pairs
  - `ocpp201.BootNotificationRequestJson` → `provisioning.BootNotificationRequest`
  - `ocpp201.AuthorizeRequestJson` → `authorization.AuthorizeRequest`
  - `ocpp201.TransactionEventRequestJson` → `transactions.TransactionEventRequest`
  - ... (all message types)
- [ ] **1.5** Update every handler to use new types (field name changes likely)
- [ ] **1.6** Update routing tables in `handlers/ocpp16/routing.go` and `handlers/ocpp201/routing.go`
- [ ] **1.7** Handle JSON serialization differences (field names, enum values)
- [ ] **1.8** Keep `manager/ocpp/has2be/` as-is (no ocpp-go equivalent)
- [ ] **1.9** Run full test suite and fix breakage

**⚠️ Key Risk:** ocpp-go struct field names and JSON tags may differ from MaEVe's JSON-Schema-generated structs. Thorough comparison needed per type.

### Phase 2: Transport Layer (Replace custom WebSocket/MQTT bridge)

**Goal:** Evaluate whether to replace the Gateway+MQTT architecture with ocpp-go's built-in WebSocket server.

**Complexity:** VERY HIGH — Fundamental architectural change.

#### Option A: Replace Gateway entirely with ocpp-go's CSMS server

- [ ] **2A.1** Implement ocpp-go's handler interfaces (see Phase 3) with your business logic
- [ ] **2A.2** Use `ocpp2.NewCSMS(nil, nil)` or `ocpp16.NewCentralSystem(nil, nil)` as the server
- [ ] **2A.3** Implement custom `ws.WsServer` if needed for auth customization
- [ ] **2A.4** Remove Gateway entirely, remove MQTT dependency
- [ ] **2A.5** Handle dual-protocol support (both OCPP 1.6 and 2.0.1)

**Impact:** Removes the Gateway process, MQTT broker, and the entire `gateway/` directory. But loses horizontal scaling of the manager.

#### Option B: Use ocpp-go only for types + OCPP-J layer, keep MQTT architecture

- [ ] **2B.1** Use ocpp-go's `ocppj` package in the Gateway for OCPP-J framing (replace `gateway/ocpp/ocppj.go`)
- [ ] **2B.2** Keep MQTT transport as-is
- [ ] **2B.3** Use ocpp-go types in the Manager (from Phase 1)

**Impact:** Smaller change, keeps the distributed architecture, but less code reduction.

#### Option C: Hybrid — Use ocpp-go's WS server with custom transport adapter

- [ ] **2C.1** Implement a custom `ws.WsServer` that bridges to MQTT internally
- [ ] **2C.2** Use ocpp-go's OCPP-J and routing layer
- [ ] **2C.3** Keep Manager as a separate process consuming MQTT

**Impact:** Gets ocpp-go's protocol handling while preserving the distributed architecture. Most complex to implement.

**Recommendation:** Start with **Option B** for minimal risk, evolve to **Option A** if single-process is acceptable.

### Phase 3: Handler Adaptation

**Goal:** Adapt MaEVe's handler pattern to work with ocpp-go's callback interface.

**Complexity:** MEDIUM-HIGH

#### If using Option A (full ocpp-go server):

- [ ] **3A.1** Create a `CSMSHandler` struct implementing all ocpp-go handler interfaces
- [ ] **3A.2** For each handler method, delegate to existing MaEVe handler logic:
  ```go
  func (h *CSMSHandler) OnBootNotification(chargingStationId string, request *provisioning.BootNotificationRequest) (*provisioning.BootNotificationResponse, error) {
      // Reuse existing MaEVe handler logic
      return h.bootHandler.Handle(ctx, chargingStationId, request)
  }
  ```
- [ ] **3A.3** Handle `context.Context` — ocpp-go callbacks don't receive a context; you'd need to create one
- [ ] **3A.4** Handle CSMS-initiated calls (CallResult pattern) — ocpp-go uses async callbacks:
  ```go
  csms.RequestStartTransaction(csId, callback, request)
  ```
  MaEVe's `CallResultHandler` pattern would map to these callbacks
- [ ] **3A.5** Adapt the `CallMaker` / `OcppCallMaker` to use ocpp-go's send API
- [ ] **3A.6** Handle the DataTransfer-based ISO 15118 PnC routing (MaEVe's special DataTransfer handler)

#### If using Option B (keep MQTT):

- [ ] **3B.1** Keep existing handler framework mostly unchanged
- [ ] **3B.2** Only update type references (from Phase 1)
- [ ] **3B.3** Router dispatch logic remains the same

### Phase 4: Schema Validation

**Goal:** Determine if ocpp-go's built-in validation can replace MaEVe's JSON Schema validation.

**Complexity:** LOW-MEDIUM

#### Analysis:

- MaEVe uses `santhosh-tekuri/jsonschema` with 224 JSON Schema files to validate raw `json.RawMessage` payloads **before** unmarshaling
- ocpp-go uses `go-playground/validator` struct tags to validate **after** unmarshaling
- Both approaches catch the same issues but at different stages

#### Steps:

- [ ] **4.1** If using Option A: ocpp-go's validation replaces MaEVe's entirely. Remove `manager/schemas/` directory and all 224 JSON files.
  - ocpp-go validates automatically; can be disabled with `ocppj.SetMessageValidation(false)`
- [ ] **4.2** If using Option B: Evaluate replacing JSON Schema validation with ocpp-go struct validation post-unmarshal
  - Remove the `schemas.Validate()` call from `router.go`
  - Use `validator.New().Struct(req)` on the ocpp-go types instead
  - Remove `RequestSchema`/`ResponseSchema` fields from `CallRoute`/`CallResultRoute`
- [ ] **4.3** Run compliance tests to ensure validation coverage parity
- [ ] **4.4** Remove `manager/schemas/` directory (224 JSON files + Go loader code)

---

## 4. Risk Assessment

### Critical Risks

| Risk | Severity | Mitigation |
|---|---|---|
| **MQTT architecture incompatibility**: ocpp-go assumes single-process; MaEVe's Gateway/Manager split over MQTT is fundamental to its scaling model | 🔴 HIGH | Choose Option B initially (types only); evaluate full replacement later |
| **has2be / ISO 15118 PnC extensions**: ocpp-go has no support for the has2be vendor-specific DataTransfer extensions that MaEVe uses for OCPP 1.6 ISO 15118 | 🔴 HIGH | Keep `manager/ocpp/has2be/` and related handlers; cannot use ocpp-go for this |
| **Dual-protocol support**: MaEVe supports both OCPP 1.6 and 2.0.1 simultaneously on the same WebSocket endpoint (via subprotocol negotiation). ocpp-go's `CentralSystem` and `CSMS` are separate objects | 🟡 MEDIUM | Would need a custom WS handler that creates the right ocpp-go instance per connection |
| **No `context.Context` in ocpp-go callbacks**: MaEVe passes `context.Context` through the entire handler chain (for tracing, cancellation). ocpp-go callbacks don't receive a context | 🟡 MEDIUM | Would need to synthesize contexts or contribute context support upstream |
| **DataTransfer routing complexity**: MaEVe's OCPP 1.6 DataTransfer handler has nested routing for ISO 15118 PnC messages. This is a custom pattern not supported by ocpp-go | 🟡 MEDIUM | Keep custom DataTransfer handler regardless of ocpp-go adoption |
| **Security profile handling**: MaEVe implements OCPP security profiles 1-3 with custom device registry lookup. ocpp-go's built-in WS server may not support all of these | 🟡 MEDIUM | Would need custom `ws.WsServer` implementation |
| **OpenTelemetry tracing**: MaEVe has deep OTel integration in both Gateway and Manager. ocpp-go has no tracing support | 🟡 MEDIUM | Would need to add tracing hooks, likely via middleware or custom transport |

### What MaEVe Does That ocpp-go Doesn't Support

1. **MQTT-based distributed architecture** (Gateway ↔ Manager)
2. **has2be ISO 15118 vendor extensions** via DataTransfer
3. **JSON Schema validation** (pre-unmarshal; ocpp-go does post-unmarshal struct validation)
4. **OpenTelemetry tracing** throughout the message pipeline
5. **Device registry** with security profile-based authentication
6. **Nested DataTransfer routing** (vendor-specific sub-actions within DataTransfer)
7. **State passing** in CallResult messages (the `State` field on `transport.Message`)

---

## 5. Code Reduction Estimate

### If Option A (Full replacement — remove Gateway, use ocpp-go server):

| Component | Lines | Files | Removable? |
|---|---|---|---|
| `manager/ocpp/ocpp16/` | ~2,200 | 73 | ✅ Replace with ocpp-go types |
| `manager/ocpp/ocpp201/` | ~2,300 | 74 | ✅ Replace with ocpp-go types |
| `gateway/` (entire directory) | ~4,395 | 26 | ✅ Replace with ocpp-go server |
| `manager/transport/` | ~1,200 | 14 | ✅ No longer needed (no MQTT) |
| `manager/schemas/` | ~100 Go + 224 JSON | 227 | ✅ Replace with ocpp-go validation |
| `manager/handlers/router.go` | ~120 | 1 | ✅ Replace with ocpp-go routing |
| `manager/handlers/types.go` | ~65 | 1 | ⚠️ Partially replaceable |
| **Total removable** | **~10,380 lines** | **~416 files** | |

**New code needed:** ~500-1000 lines for adapter layer, custom WS auth handler, tracing hooks.

**Net reduction: ~9,000-10,000 lines, ~400 files**

### If Option B (Types only — keep MQTT architecture):

| Component | Lines | Files | Removable? |
|---|---|---|---|
| `manager/ocpp/ocpp16/` | ~2,200 | 73 | ✅ Replace with ocpp-go types |
| `manager/ocpp/ocpp201/` | ~2,300 | 74 | ✅ Replace with ocpp-go types |
| `manager/schemas/` | ~100 Go + 224 JSON | 227 | ✅ Replace with ocpp-go validation |
| `gateway/ocpp/ocppj.go` | ~100 | 1 | ✅ Replace with ocpp-go OCPP-J |
| **Total removable** | **~4,700 lines** | **~375 files** | |

**New code needed:** ~200-400 lines for adapter/wrapper types.

**Net reduction: ~4,300-4,500 lines, ~370 files**

---

## 6. OCPP 1.6 Specifics

### Message Type Mapping (MaEVe → ocpp-go)

| MaEVe Type | ocpp-go Package | ocpp-go Type |
|---|---|---|
| `ocpp16.BootNotificationJson` | `ocpp1.6/core` | `core.BootNotificationRequest` |
| `ocpp16.BootNotificationResponseJson` | `ocpp1.6/core` | `core.BootNotificationConfirmation` |
| `ocpp16.AuthorizeJson` | `ocpp1.6/core` | `core.AuthorizeRequest` |
| `ocpp16.HeartbeatJson` | `ocpp1.6/core` | `core.HeartbeatRequest` |
| `ocpp16.StartTransactionJson` | `ocpp1.6/core` | `core.StartTransactionRequest` |
| `ocpp16.StopTransactionJson` | `ocpp1.6/core` | `core.StopTransactionRequest` |
| `ocpp16.StatusNotificationJson` | `ocpp1.6/core` | `core.StatusNotificationRequest` |
| `ocpp16.MeterValuesJson` | `ocpp1.6/core` | `core.MeterValuesRequest` |
| `ocpp16.DataTransferJson` | `ocpp1.6/core` | `core.DataTransferRequest` |
| `ocpp16.ChangeAvailabilityJson` | `ocpp1.6/core` | `core.ChangeAvailabilityRequest` |
| `ocpp16.ChangeConfigurationJson` | `ocpp1.6/core` | `core.ChangeConfigurationRequest` |
| `ocpp16.GetConfigurationJson` | `ocpp1.6/core` | `core.GetConfigurationRequest` |
| `ocpp16.ResetJson` | `ocpp1.6/core` | `core.ResetRequest` |
| `ocpp16.UnlockConnectorJson` | `ocpp1.6/core` | `core.UnlockConnectorRequest` |
| `ocpp16.ClearCacheJson` | `ocpp1.6/core` | `core.ClearCacheRequest` |
| `ocpp16.RemoteStartTransactionJson` | `ocpp1.6/core` | `core.RemoteStartTransactionRequest` |
| `ocpp16.RemoteStopTransactionJson` | `ocpp1.6/core` | `core.RemoteStopTransactionRequest` |
| `ocpp16.TriggerMessageJson` | `ocpp1.6/remotetrigger` | `remotetrigger.TriggerMessageRequest` |
| `ocpp16.SetChargingProfileJson` | `ocpp1.6/smartcharging` | `smartcharging.SetChargingProfileRequest` |
| `ocpp16.GetCompositeScheduleJson` | `ocpp1.6/smartcharging` | `smartcharging.GetCompositeScheduleRequest` |
| `ocpp16.ClearChargingProfileJson` | `ocpp1.6/smartcharging` | `smartcharging.ClearChargingProfileRequest` |
| `ocpp16.UpdateFirmwareJson` | `ocpp1.6/firmware` | `firmware.UpdateFirmwareRequest` |
| `ocpp16.GetDiagnosticsJson` | `ocpp1.6/firmware` | `firmware.GetDiagnosticsRequest` |
| `ocpp16.FirmwareStatusNotificationJson` | `ocpp1.6/firmware` | `firmware.FirmwareStatusNotificationRequest` |
| `ocpp16.DiagnosticsStatusNotificationJson` | `ocpp1.6/firmware` | `firmware.DiagnosticsStatusNotificationRequest` |
| `ocpp16.GetLocalListVersionJson` | `ocpp1.6/localauth` | `localauth.GetLocalListVersionRequest` |
| `ocpp16.SendLocalListJson` | `ocpp1.6/localauth` | `localauth.SendLocalListRequest` |
| `ocpp16.ReserveNowJson` | `ocpp1.6/reservation` | `reservation.ReserveNowRequest` |
| `ocpp16.CancelReservationJson` | `ocpp1.6/reservation` | `reservation.CancelReservationRequest` |

#### OCPP 1.6 Security Extension Messages

MaEVe supports the OCPP 1.6 security extension. ocpp-go also supports this via `ocpp1.6/securefirmware` and `ocpp1.6/security` packages:

| MaEVe Type | ocpp-go Equivalent |
|---|---|
| `ocpp16.SecurityEventNotificationJson` | `security.SecurityEventNotificationRequest` |
| `ocpp16.SignedUpdateFirmwareJson` | `securefirmware.SignedUpdateFirmwareRequest` |
| `ocpp16.SignedFirmwareStatusNotificationJson` | `securefirmware.SignedFirmwareStatusNotificationRequest` |
| `ocpp16.ExtendedTriggerMessageJson` | `securefirmware.ExtendedTriggerMessageRequest` |
| `ocpp16.DeleteCertificateJson` | `security.DeleteCertificateRequest` |
| `ocpp16.GetInstalledCertificateIdsJson` | `security.GetInstalledCertificateIdsRequest` |
| `ocpp16.GetLogJson` | `security.GetLogRequest` |
| `ocpp16.LogStatusNotificationJson` | `security.LogStatusNotificationRequest` |

### OCPP 1.6 has2be Extensions — NOT in ocpp-go

These must remain as MaEVe custom types:
- `has2be.AuthorizeRequestJson` / `AuthorizeResponseJson`
- `has2be.CertificateSignedRequestJson` / `CertificateSignedResponseJson`
- `has2be.Get15118EVCertificateRequestJson` / `Get15118EVCertificateResponseJson`
- `has2be.GetCertificateStatusRequestJson` / `GetCertificateStatusResponseJson`
- `has2be.SignCertificateRequestJson` / `SignCertificateResponseJson`

### Handler Mapping (OCPP 1.6)

ocpp-go 1.6 uses profile-based handler interfaces. MaEVe's handlers map as follows:

| MaEVe Handler | ocpp-go Interface Method |
|---|---|
| `BootNotificationHandler` | `core.CentralSystemHandler.OnBootNotification()` |
| `HeartbeatHandler` | `core.CentralSystemHandler.OnHeartbeat()` |
| `AuthorizeHandler` | `core.CentralSystemHandler.OnAuthorize()` |
| `StartTransactionHandler` | `core.CentralSystemHandler.OnStartTransaction()` |
| `StopTransactionHandler` | `core.CentralSystemHandler.OnStopTransaction()` |
| `StatusNotificationHandler` | `core.CentralSystemHandler.OnStatusNotification()` |
| `MeterValuesHandler` | `core.CentralSystemHandler.OnMeterValues()` |
| `DataTransferHandler` | `core.CentralSystemHandler.OnDataTransfer()` |
| `FirmwareStatusNotificationHandler` | `firmware.CentralSystemHandler.OnFirmwareStatusNotification()` |
| `DiagnosticsStatusNotificationHandler` | `firmware.CentralSystemHandler.OnDiagnosticsStatusNotification()` |

---

## 7. OCPP 2.0.1 Specifics

### Message Type Mapping (MaEVe → ocpp-go)

| MaEVe Type | ocpp-go Package | ocpp-go Type |
|---|---|---|
| `ocpp201.BootNotificationRequestJson` | `ocpp2.0.1/provisioning` | `provisioning.BootNotificationRequest` |
| `ocpp201.AuthorizeRequestJson` | `ocpp2.0.1/authorization` | `authorization.AuthorizeRequest` |
| `ocpp201.HeartbeatRequestJson` | `ocpp2.0.1/availability` | `availability.HeartbeatRequest` |
| `ocpp201.TransactionEventRequestJson` | `ocpp2.0.1/transactions` | `transactions.TransactionEventRequest` |
| `ocpp201.StatusNotificationRequestJson` | `ocpp2.0.1/availability` | `availability.StatusNotificationRequest` |
| `ocpp201.MeterValuesRequestJson` | `ocpp2.0.1/meter` | `meter.MeterValuesRequest` |
| `ocpp201.SecurityEventNotificationRequestJson` | `ocpp2.0.1/security` | `security.SecurityEventNotificationRequest` |
| `ocpp201.SignCertificateRequestJson` | `ocpp2.0.1/security` | `security.SignCertificateRequest` |
| `ocpp201.Get15118EVCertificateRequestJson` | `ocpp2.0.1/iso15118` | `iso15118.Get15118EVCertificateRequest` |
| `ocpp201.GetCertificateStatusRequestJson` | `ocpp2.0.1/iso15118` | `iso15118.GetCertificateStatusRequest` |
| `ocpp201.FirmwareStatusNotificationRequestJson` | `ocpp2.0.1/firmware` | `firmware.FirmwareStatusNotificationRequest` |
| `ocpp201.NotifyReportRequestJson` | `ocpp2.0.1/provisioning` | `provisioning.NotifyReportRequest` |
| `ocpp201.LogStatusNotificationRequestJson` | `ocpp2.0.1/diagnostics` | `diagnostics.LogStatusNotificationRequest` |
| `ocpp201.CertificateSignedRequestJson` | `ocpp2.0.1/security` | `security.CertificateSignedRequest` |
| `ocpp201.ChangeAvailabilityRequestJson` | `ocpp2.0.1/availability` | `availability.ChangeAvailabilityRequest` |
| `ocpp201.ClearCacheRequestJson` | `ocpp2.0.1/authorization` | `authorization.ClearCacheRequest` |
| `ocpp201.DeleteCertificateRequestJson` | `ocpp2.0.1/security` | `security.DeleteCertificateRequest` |
| `ocpp201.GetBaseReportRequestJson` | `ocpp2.0.1/provisioning` | `provisioning.GetBaseReportRequest` |
| `ocpp201.GetInstalledCertificateIdsRequestJson` | `ocpp2.0.1/security` | `security.GetInstalledCertificateIdsRequest` |
| `ocpp201.GetLocalListVersionRequestJson` | `ocpp2.0.1/localauth` | `localauth.GetLocalListVersionRequest` |
| `ocpp201.GetReportRequestJson` | `ocpp2.0.1/provisioning` | `provisioning.GetReportRequest` |
| `ocpp201.GetTransactionStatusRequestJson` | `ocpp2.0.1/transactions` | `transactions.GetTransactionStatusRequest` |
| `ocpp201.GetVariablesRequestJson` | `ocpp2.0.1/provisioning` | `provisioning.GetVariablesRequest` |
| `ocpp201.InstallCertificateRequestJson` | `ocpp2.0.1/security` | `security.InstallCertificateRequest` |
| `ocpp201.RequestStartTransactionRequestJson` | `ocpp2.0.1/remotecontrol` | `remotecontrol.RequestStartTransactionRequest` |
| `ocpp201.RequestStopTransactionRequestJson` | `ocpp2.0.1/remotecontrol` | `remotecontrol.RequestStopTransactionRequest` |
| `ocpp201.ResetRequestJson` | `ocpp2.0.1/provisioning` | `provisioning.ResetRequest` |
| `ocpp201.SendLocalListRequestJson` | `ocpp2.0.1/localauth` | `localauth.SendLocalListRequest` |
| `ocpp201.SetNetworkProfileRequestJson` | `ocpp2.0.1/provisioning` | `provisioning.SetNetworkProfileRequest` |
| `ocpp201.SetVariablesRequestJson` | `ocpp2.0.1/provisioning` | `provisioning.SetVariablesRequest` |
| `ocpp201.TriggerMessageRequestJson` | `ocpp2.0.1/remotecontrol` | `remotecontrol.TriggerMessageRequest` |
| `ocpp201.UnlockConnectorRequestJson` | `ocpp2.0.1/remotecontrol` | `remotecontrol.UnlockConnectorRequest` |

### Handler Mapping (OCPP 2.0.1)

ocpp-go 2.0.1 uses multiple handler interfaces per functional block:

| MaEVe CallRoute Handler | ocpp-go Interface |
|---|---|
| `AuthorizeHandler` | `AuthorizationCSMSHandler.OnAuthorize()` |
| `BootNotificationHandler` | `ProvisioningCSMSHandler.OnBootNotification()` |
| `HeartbeatHandler` | `AvailabilityCSMSHandler.OnHeartbeat()` |
| `StatusNotificationHandler` | `AvailabilityCSMSHandler.OnStatusNotification()` |
| `TransactionEventHandler` | `TransactionsCSMSHandler.OnTransactionEvent()` |
| `MeterValuesHandler` | `MeterCSMSHandler.OnMeterValues()` |
| `SignCertificateHandler` | `SecurityCSMSHandler.OnSignCertificate()` |
| `Get15118EvCertificateHandler` | `ISO15118CSMSHandler.OnGet15118EVCertificate()` |
| `GetCertificateStatusHandler` | `ISO15118CSMSHandler.OnGetCertificateStatus()` |
| `SecurityEventNotificationHandler` | `SecurityCSMSHandler.OnSecurityEventNotification()` |
| `FirmwareStatusNotificationHandler` | `FirmwareCSMSHandler.OnFirmwareStatusNotification()` |
| `NotifyReportHandler` | `ProvisioningCSMSHandler.OnNotifyReport()` |
| `LogStatusNotificationHandler` | `DiagnosticsCSMSHandler.OnLogStatusNotification()` |

---

## Appendix: Recommended Migration Strategy

### TL;DR

1. **Start with Phase 1 (types only)** — Lowest risk, highest value. Eliminates ~4,500 lines and 370 files of hand-maintained message structs and JSON schemas.
2. **Use Option B for Phase 2** — Keep the MQTT architecture. Use ocpp-go only for types and optionally for OCPP-J framing in the gateway.
3. **Defer full ocpp-go server adoption** until you decide whether the MQTT-based distributed architecture is still needed.
4. **Never migrate has2be extensions** — These are custom to MaEVe and have no ocpp-go support.

### Estimated Effort

| Phase | Effort | Value |
|---|---|---|
| Phase 1 (Message types) | 2-3 weeks | High — eliminates maintenance burden of 158 struct files |
| Phase 2B (OCPP-J only) | 1 week | Medium — cleaner gateway code |
| Phase 2A (Full server replacement) | 4-6 weeks | High — but high risk |
| Phase 3 (Handler adaptation) | 2-3 weeks (with Option A) | Tied to Phase 2 choice |
| Phase 4 (Validation) | 2-3 days | Medium — removes 224 JSON schema files |
