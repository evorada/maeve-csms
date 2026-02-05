# OCPP Version Architecture Analysis

**Date:** 2026-02-05  
**Project:** MaEVe CSMS  
**Versions Analyzed:** OCPP 1.6j, OCPP 2.0.1

## Overview

MaEVe CSMS is designed with a clean, extensible architecture that supports multiple OCPP versions simultaneously. This document analyzes how different OCPP client versions are handled and provides guidance for adding future versions.

---

## Table of Contents

- [Architecture Overview](#architecture-overview)
- [Version Detection and Negotiation](#version-detection-and-negotiation)
- [Message Routing](#message-routing)
- [Handler Organization](#handler-organization)
- [Type System](#type-system)
- [Adding New OCPP Versions](#adding-new-ocpp-versions)
- [Best Practices](#best-practices)
- [Current Limitations](#current-limitations)

---

## Architecture Overview

### High-Level Flow

```
Charge Station (OCPP 1.6 or 2.0.1)
    |
    | WebSocket Connection
    | (protocol negotiation)
    v
Gateway (server/ws.go)
    |
    | MQTT Message (with version tag)
    v
MQTT Broker
    |
    | Version-specific topics:
    | - cs/in/ocpp1.6/#
    | - cs/in/ocpp2.0.1/#
    v
Manager (Routers)
    |
    +---> OCPP 1.6 Router (handlers/ocpp16/)
    |
    +---> OCPP 2.0.1 Router (handlers/ocpp201/)
```

### Key Components

1. **Gateway** - WebSocket server, version negotiation, MQTT bridge
2. **Transport Layer** - Version-tagged messaging via MQTT
3. **Manager Routers** - Version-specific message routers
4. **Handlers** - Version-specific business logic

---

## Version Detection and Negotiation

### Gateway WebSocket Protocol Negotiation

**Location:** `gateway/server/ws.go:245`

```go
wsConn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
    Subprotocols: []string{"ocpp2.0.1", "ocpp1.6"}, 
    InsecureSkipVerify: true
})

protocol := wsConn.Subprotocol()
if protocol == "" {
    protocol = "ocpp2.0.1" // Default to 2.0.1
}
```

**How it works:**

1. **Client sends** `Sec-WebSocket-Protocol` header with supported versions:
   ```
   Sec-WebSocket-Protocol: ocpp1.6
   ```
   or
   ```
   Sec-WebSocket-Protocol: ocpp2.0.1, ocpp1.6
   ```

2. **Gateway responds** with selected protocol:
   ```
   Sec-WebSocket-Protocol: ocpp1.6
   ```

3. **Protocol string** is used to:
   - Route MQTT messages to correct topic (`cs/in/ocpp1.6/` vs `cs/in/ocpp2.0.1/`)
   - Tag responses with correct version

**Default Behavior:**
- If no protocol specified → defaults to `ocpp2.0.1`
- This ensures backwards compatibility

---

## Message Routing

### Transport Layer Version System

**Location:** `manager/transport/emitter.go`

```go
type OcppVersion string

const (
    OcppVersion16  OcppVersion = "ocpp1.6"   // OCPP 1.6
    OcppVersion201 OcppVersion = "ocpp2.0.1" // OCPP 2.0.1
)
```

**Key Interfaces:**

```go
// Emitter - sends messages to gateway
type Emitter interface {
    Emit(ctx context.Context, 
         ocppVersion OcppVersion, 
         chargeStationId string, 
         message *Message) error
}

// Listener - receives messages from gateway
type Listener interface {
    Connect(ctx context.Context, 
            ocppVersion OcppVersion, 
            chargeStationId *string, 
            handler MessageHandler) (Connection, error)
}
```

**MQTT Topic Structure:**

- **Incoming (from charge stations):**
  - `cs/in/ocpp1.6/{chargeStationId}`
  - `cs/in/ocpp2.0.1/{chargeStationId}`

- **Outgoing (to charge stations):**
  - `cs/out/ocpp1.6/{chargeStationId}`
  - `cs/out/ocpp2.0.1/{chargeStationId}`

**Shared Groups:**
- Manager uses `$share/manager/cs/in/{version}/#` for load balancing across multiple manager instances

---

## Handler Organization

### Directory Structure

```
manager/
├── ocpp/                    # Protocol type definitions
│   ├── ocpp16/             # OCPP 1.6 message types
│   │   ├── authorize.go
│   │   ├── boot_notification.go
│   │   └── ... (24 more)
│   └── ocpp201/            # OCPP 2.0.1 message types
│       ├── authorize_request.go
│       ├── boot_notification_request.go
│       └── ... (73 more)
│
├── handlers/               # Business logic
│   ├── ocpp16/            # OCPP 1.6 handlers
│   │   ├── routing.go     # Router setup
│   │   ├── authorize.go
│   │   ├── boot_notification.go
│   │   └── ... (25 more)
│   └── ocpp201/           # OCPP 2.0.1 handlers
│       ├── routing.go     # Router setup
│       ├── authorize.go
│       ├── boot_notification.go
│       └── ... (66 more)
│
└── schemas/               # JSON Schema validation
    ├── ocpp16/           # OCPP 1.6 schemas
    │   ├── Authorize.json
    │   ├── BootNotification.json
    │   └── ... (40+ schemas)
    └── ocpp201/          # OCPP 2.0.1 schemas
        ├── AuthorizeRequest.json
        ├── BootNotificationRequest.json
        └── ... (120+ schemas)
```

### Router Pattern

**OCPP 1.6 Router** (`handlers/ocpp16/routing.go`):

```go
func NewRouter(
    emitter transport.Emitter,
    clk clock.PassiveClock,
    engine store.Engine,
    // ... other dependencies
    schemaFS fs.FS,
) transport.MessageHandler {
    
    return &handlers.Router{
        Emitter:     emitter,
        SchemaFS:    schemaFS,
        OcppVersion: transport.OcppVersion16,
        CallRoutes:  map[string]handlers.CallRoute{
            "BootNotification": {
                NewRequest:     func() ocpp.Request { return new(ocpp16.BootNotificationJson) },
                RequestSchema:  "ocpp16/BootNotification.json",
                ResponseSchema: "ocpp16/BootNotificationResponse.json",
                Handler: BootNotificationHandler{
                    Clock:               clk,
                    RuntimeDetailsStore: engine,
                    SettingsStore:       engine,
                    HeartbeatInterval:   int(heartbeatInterval.Seconds()),
                },
            },
            "Authorize": { /* ... */ },
            "StartTransaction": { /* ... */ },
            // ... more routes
        },
        CallResultRoutes: map[string]handlers.CallResultRoute{
            "ChangeConfiguration": { /* ... */ },
            "TriggerMessage": { /* ... */ },
            // ... more routes
        },
    }
}
```

**OCPP 2.0.1 Router** (`handlers/ocpp201/routing.go`) - follows identical pattern

---

## Type System

### Base Interfaces

**Location:** `manager/ocpp/types.go`

```go
type Request interface {
    IsRequest()
}

type Response interface {
    IsResponse()
}
```

**All OCPP messages implement these interfaces:**

```go
// OCPP 1.6 example
type BootNotificationJson struct {
    ChargePointVendor       string  `json:"chargePointVendor" validate:"required,max=20"`
    ChargePointModel        string  `json:"chargePointModel" validate:"required,max=20"`
    ChargePointSerialNumber *string `json:"chargePointSerialNumber,omitempty"`
    // ...
}

func (b BootNotificationJson) IsRequest() {}

// OCPP 2.0.1 example
type BootNotificationRequestJson struct {
    ChargingStation ChargingStationType `json:"chargingStation" validate:"required"`
    Reason          BootReasonEnumType  `json:"reason" validate:"required"`
}

func (b BootNotificationRequestJson) IsRequest() {}
```

### Handler Interface

**Location:** `manager/handlers/types.go`

```go
// CallHandler processes incoming OCPP Call messages
type CallHandler interface {
    HandleCall(ctx context.Context, 
               chargeStationId string, 
               request ocpp.Request) (response ocpp.Response, err error)
}

// CallResultHandler processes incoming CallResult messages (responses to CSMS-initiated calls)
type CallResultHandler interface {
    HandleCallResult(ctx context.Context, 
                     chargeStationId string, 
                     request ocpp.Request, 
                     response ocpp.Response, 
                     state any) error
}
```

**Benefits of this design:**

✅ **Type safety** - Go compiler enforces correct types  
✅ **Version isolation** - Each version is self-contained  
✅ **Testability** - Easy to mock and test individual handlers  
✅ **Extensibility** - New versions don't affect existing ones  

---

## Adding New OCPP Versions

### Step-by-Step Guide for Adding OCPP 2.1

#### Step 1: Add Version Constant

**File:** `manager/transport/emitter.go`

```go
const (
    OcppVersion16  OcppVersion = "ocpp1.6"
    OcppVersion201 OcppVersion = "ocpp2.0.1"
    OcppVersion21  OcppVersion = "ocpp2.1"  // NEW
)
```

#### Step 2: Create Message Type Definitions

**Directory:** `manager/ocpp/ocpp21/`

```bash
mkdir -p manager/ocpp/ocpp21
```

**Create message types** (one file per message):
- `authorize_request.go`
- `boot_notification_request.go`
- etc.

**Example:** `manager/ocpp/ocpp21/authorize_request.go`

```go
// SPDX-License-Identifier: Apache-2.0

package ocpp21

type AuthorizeRequestJson struct {
    IdToken      IdTokenType       `json:"idToken" validate:"required"`
    Certificate  *string           `json:"certificate,omitempty"`
    Iso15118CertificateHashData []OCSPRequestDataType `json:"iso15118CertificateHashData,omitempty"`
}

func (a AuthorizeRequestJson) IsRequest() {}

type AuthorizeResponseJson struct {
    IdTokenInfo  IdTokenInfoType   `json:"idTokenInfo" validate:"required"`
    CertificateStatus *AuthorizeCertificateStatusEnumType `json:"certificateStatus,omitempty"`
}

func (a AuthorizeResponseJson) IsResponse() {}
```

#### Step 3: Add JSON Schemas

**Directory:** `manager/schemas/ocpp21/`

```bash
mkdir -p manager/schemas/ocpp21
```

Add JSON schema files for validation:
- `AuthorizeRequest.json`
- `AuthorizeResponse.json`
- etc.

#### Step 4: Create Handlers

**Directory:** `manager/handlers/ocpp21/`

```bash
mkdir -p manager/handlers/ocpp21
```

**Create handler files:**

**Example:** `manager/handlers/ocpp21/authorize.go`

```go
// SPDX-License-Identifier: Apache-2.0

package ocpp21

import (
    "context"
    "github.com/thoughtworks/maeve-csms/manager/ocpp"
    types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp21"
    "github.com/thoughtworks/maeve-csms/manager/services"
)

type AuthorizeHandler struct {
    TokenAuthService services.TokenAuthService
}

func (a AuthorizeHandler) HandleCall(ctx context.Context, chargeStationId string, request ocpp.Request) (ocpp.Response, error) {
    req := request.(*types.AuthorizeRequestJson)
    
    tokenInfo := a.TokenAuthService.Authorize(ctx, req.IdToken)
    
    return &types.AuthorizeResponseJson{
        IdTokenInfo: tokenInfo,
    }, nil
}
```

#### Step 5: Create Router

**File:** `manager/handlers/ocpp21/routing.go`

```go
// SPDX-License-Identifier: Apache-2.0

package ocpp21

import (
    "github.com/thoughtworks/maeve-csms/manager/handlers"
    "github.com/thoughtworks/maeve-csms/manager/ocpp"
    types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp21"
    "github.com/thoughtworks/maeve-csms/manager/store"
    "github.com/thoughtworks/maeve-csms/manager/transport"
    "io/fs"
    "k8s.io/utils/clock"
)

func NewRouter(
    emitter transport.Emitter,
    clk clock.PassiveClock,
    engine store.Engine,
    // ... other dependencies
    schemaFS fs.FS,
) transport.MessageHandler {
    
    return &handlers.Router{
        Emitter:     emitter,
        SchemaFS:    schemaFS,
        OcppVersion: transport.OcppVersion21,  // Use new version constant
        CallRoutes:  map[string]handlers.CallRoute{
            "Authorize": {
                NewRequest:     func() ocpp.Request { return new(types.AuthorizeRequestJson) },
                RequestSchema:  "ocpp21/AuthorizeRequest.json",
                ResponseSchema: "ocpp21/AuthorizeResponse.json",
                Handler: AuthorizeHandler{
                    TokenAuthService: &services.OcppTokenAuthService{
                        Clock:      clk,
                        TokenStore: engine,
                    },
                },
            },
            "BootNotification": { /* ... */ },
            // ... more routes
        },
        CallResultRoutes: map[string]handlers.CallResultRoute{
            // ... routes for CSMS-initiated calls
        },
    }
}
```

#### Step 6: Wire Up in Configuration

**File:** `manager/config/config.go`

Add to Settings struct:

```go
type Settings struct {
    // ... existing fields
    Ocpp16Handler  transport.MessageHandler
    Ocpp201Handler transport.MessageHandler
    Ocpp21Handler  transport.MessageHandler  // NEW
    // ...
}
```

Create router in `Configure()` function:

```go
func Configure(ctx context.Context, cfg *Config) (*Settings, error) {
    // ... existing setup
    
    // Add OCPP 2.1 router
    c.Ocpp21Handler = ocpp21.NewRouter(
        c.MsgEmitter,
        clock.RealClock{},
        c.Storage,
        // ... dependencies
        c.SchemaFS,
    )
    
    return c, nil
}
```

#### Step 7: Connect in Serve Command

**File:** `manager/cmd/serve.go`

```go
var ocpp21Connection transport.Connection
if settings.Ocpp21Handler != nil {
    ocpp21Connection, err = settings.MsgListener.Connect(
        context.Background(), 
        transport.OcppVersion21, 
        nil, 
        settings.Ocpp21Handler,
    )
    if err != nil {
        errCh <- err
    }
}

// Don't forget to disconnect on shutdown
if ocpp21Connection != nil {
    err := ocpp21Connection.Disconnect(context.Background())
    if err != nil {
        slog.Warn("disconnecting from broker", "err", err)
    }
}
```

#### Step 8: Update Gateway WebSocket Negotiation

**File:** `gateway/server/ws.go`

```go
wsConn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
    Subprotocols: []string{"ocpp2.1", "ocpp2.0.1", "ocpp1.6"},  // Add ocpp2.1
    InsecureSkipVerify: true,
})
```

Update default protocol logic if needed:

```go
protocol := wsConn.Subprotocol()
if protocol == "" {
    protocol = "ocpp2.1"  // Update default to latest
}
```

#### Step 9: Add Tests

**Directory:** `manager/handlers/ocpp21/`

Create test files:
- `authorize_test.go`
- `boot_notification_test.go`
- etc.

**Example:** `manager/handlers/ocpp21/authorize_test.go`

```go
package ocpp21_test

import (
    "context"
    "testing"
    "github.com/stretchr/testify/assert"
    "github.com/thoughtworks/maeve-csms/manager/handlers/ocpp21"
    types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp21"
    // ...
)

func TestAuthorizeHandler(t *testing.T) {
    // Test implementation
}
```

---

## Best Practices

### 1. Version Isolation

✅ **DO:** Keep version-specific code in separate packages
```
handlers/ocpp16/
handlers/ocpp201/
handlers/ocpp21/
```

❌ **DON'T:** Mix version-specific code in shared handlers
```
handlers/authorize.go  // contains if/else for different versions
```

### 2. Shared Business Logic

When logic is truly version-agnostic, extract to services:

```go
// services/token_auth.go
type TokenAuthService interface {
    Authorize(ctx context.Context, token ocpp201.IdTokenType) ocpp201.IdTokenInfoType
}

// Used by both OCPP 2.0.1 and 2.1 handlers
type OcppTokenAuthService struct {
    TokenStore store.TokenStore
    Clock      clock.PassiveClock
}
```

### 3. Backward Compatibility

When adding new fields to shared types (store models), make them optional:

```go
type Token struct {
    Uid       string
    Valid     bool
    // New field added in OCPP 2.1
    GroupId2  *string  // Use pointer for optional field
}
```

### 4. Protocol Extensions

For vendor-specific extensions (e.g., Has2Be), use nested routing:

```go
// Inside OCPP 1.6 DataTransfer handler
CallRoutes: map[string]map[string]handlers.CallRoute{
    "org.openchargealliance.iso15118pnc": {
        "Authorize": { /* OCPP 2.0.1 Authorize via DataTransfer */ },
    },
    "iso15118": { // Has2Be extensions
        "Authorize": { /* Has2Be variant */ },
    },
},
```

### 5. Schema Validation

Always validate messages against JSON schemas:

```go
err := schemas.Validate(message.RequestPayload, r.SchemaFS, route.RequestSchema)
if err != nil {
    return transport.NewError(transport.ErrorFormatViolation, err)
}
```

### 6. Testing Strategy

**Unit Tests:** Test each handler in isolation
```go
func TestBootNotificationHandler(t *testing.T) {
    handler := BootNotificationHandler{
        Clock: clockTest.NewFakePassiveClock(now),
        // ... mocked dependencies
    }
    
    resp, err := handler.HandleCall(ctx, "cs001", request)
    
    assert.NoError(t, err)
    assert.Equal(t, expectedStatus, resp.Status)
}
```

**Integration Tests:** Test full message flow with testcontainers

**E2E Tests:** Test with real charge station simulators (see `e2e_tests/`)

---

## Current Limitations

### 1. MQTT Topic Structure

**Current:** Fixed topic structure in code

**Limitation:** Adding a new version requires code changes in multiple places

**Improvement Opportunity:** Make topic structure configurable or use a registry pattern

### 2. Gateway Default Protocol

**Current:** Hardcoded default to `ocpp2.0.1`

**Limitation:** May not be appropriate for all deployments

**Improvement Opportunity:** Make default protocol configurable via command-line flag or environment variable

### 3. Version-Specific Storage

**Current:** Single shared `store.Engine` interface

**Limitation:** Version-specific features must be optional in storage layer

**Improvement Opportunity:** Consider version-specific storage extensions for features unique to newer versions

### 4. Schema Location Convention

**Current:** Schema path convention (`ocpp16/`, `ocpp201/`) is assumed by routing code

**Limitation:** Requires consistent naming across versions

**Improvement Opportunity:** Use a schema registry or make paths explicit in route configuration

---

## Code Generation Opportunities

For future versions (especially OCPP 2.1 with 200+ messages), consider code generation:

### Option 1: Generate from JSON Schema

```bash
# Generate Go types from JSON schema
json-schema-to-go -schema schemas/ocpp21/*.json -out ocpp/ocpp21/
```

### Option 2: Generate from WSDL/XSD

```bash
# For versions with formal XML schemas
xsd-to-go -schema ocpp21.xsd -out ocpp/ocpp21/
```

### Option 3: Custom Code Generator

Create a generator that produces:
- Message types
- Handler stubs
- Router configuration
- Test stubs

**Example:**

```bash
# From OCPP spec (JSON or YAML)
ocpp-gen -version 2.1 -spec ocpp21-spec.json -out manager/
```

This could generate 80% of the boilerplate, leaving only business logic to implement.

---

## Checklist for Adding New Version

When adding support for a new OCPP version, use this checklist:

### Protocol Definition
- [ ] Add version constant in `transport/emitter.go`
- [ ] Update gateway WebSocket subprotocols in `gateway/server/ws.go`
- [ ] Update default protocol if needed

### Message Types
- [ ] Create `manager/ocpp/ocppXX/` package
- [ ] Define request/response types
- [ ] Implement `IsRequest()` and `IsResponse()` methods
- [ ] Add validation tags

### Schemas
- [ ] Create `manager/schemas/ocppXX/` directory
- [ ] Add JSON schemas for all message types
- [ ] Verify schema completeness

### Handlers
- [ ] Create `manager/handlers/ocppXX/` package
- [ ] Implement handler for each message type
- [ ] Create `routing.go` with router setup
- [ ] Handle both Call and CallResult routes

### Configuration
- [ ] Add handler field to `config.Settings`
- [ ] Create router in `config.Configure()`
- [ ] Update `cmd/serve.go` to connect listener
- [ ] Add disconnect logic in shutdown

### Testing
- [ ] Add unit tests for each handler
- [ ] Add integration tests with MQTT
- [ ] Add end-to-end tests with simulator
- [ ] Verify schema validation works

### Documentation
- [ ] Update README.md with supported versions
- [ ] Update this architecture document
- [ ] Add migration guide if breaking changes
- [ ] Document version-specific features

---

## Architecture Strengths

### ✅ Advantages of Current Design

1. **Clean Separation**
   - Each version is completely isolated
   - No version-specific conditionals in shared code
   - Easy to add/remove versions

2. **Type Safety**
   - Go compiler catches type errors at compile time
   - No reflection or runtime type assertions needed
   - IDE autocomplete works perfectly

3. **Scalability**
   - Multiple manager instances can run in parallel
   - MQTT shared subscriptions for load balancing
   - Each version can scale independently

4. **Testability**
   - Mock dependencies easily
   - Test versions independently
   - Clear boundaries for unit tests

5. **Maintainability**
   - Changes to one version don't affect others
   - Clear ownership of version-specific code
   - Easy to deprecate old versions

---

## Recommendations

### Short-Term

1. **Add Version Metrics**
   ```go
   // Add Prometheus metrics
   ocppConnectionsGauge.WithLabelValues("ocpp1.6").Inc()
   ocppMessagesCounter.WithLabelValues("ocpp2.0.1", "Authorize").Inc()
   ```

2. **Make Default Protocol Configurable**
   ```bash
   gateway --default-protocol ocpp2.0.1
   ```

3. **Add Version Detection Logging**
   ```go
   slog.Info("charge station connected", 
       "id", chargeStationId, 
       "protocol", protocol,
       "ip", remoteAddr)
   ```

### Medium-Term

1. **Version Registry Pattern**
   ```go
   type VersionRegistry struct {
       versions map[OcppVersion]*VersionConfig
   }
   
   func (r *VersionRegistry) Register(version OcppVersion, config *VersionConfig) {
       r.versions[version] = config
   }
   ```

2. **Automated Testing with Version Matrix**
   ```bash
   # Test all versions in CI
   for version in 1.6 2.0.1; do
       test-suite --ocpp-version $version
   done
   ```

3. **Version Migration Tools**
   ```go
   // Helper to convert OCPP 2.0.1 message to 2.1
   ocpp21.FromOcpp201(ocpp201Message)
   ```

### Long-Term

1. **Code Generation Pipeline**
   - Generate 80% of boilerplate from spec
   - Focus development on business logic only

2. **Dynamic Version Loading**
   - Load version handlers as plugins
   - Add new versions without recompilation

3. **Version Analytics Dashboard**
   - Track version adoption
   - Identify deprecated version usage
   - Plan migration timelines

---

## Conclusion

MaEVe's OCPP version architecture is **well-designed for extensibility**. The clear separation of concerns, type-safe routing, and version isolation make it straightforward to add new OCPP versions while maintaining existing ones.

**Key Takeaways:**

✅ **Version isolation** prevents breaking changes  
✅ **Type safety** catches errors at compile time  
✅ **Scalable design** supports horizontal scaling  
✅ **Clear patterns** make adding versions straightforward  

**Adding OCPP 2.1 or future versions requires:**
1. Creating new packages (types, handlers, schemas)
2. Registering the version in transport and config
3. Updating gateway protocol negotiation
4. Following established patterns from OCPP 2.0.1

The architecture supports both **incremental adoption** (new versions alongside old) and **eventual deprecation** (removing old versions cleanly).

---

## References

- [OCPP 1.6 Specification](https://www.openchargealliance.org/protocols/ocpp-16/)
- [OCPP 2.0.1 Specification](https://www.openchargealliance.org/protocols/ocpp-201/)
- [MaEVe Design Document](./design.md)
- [MaEVe Handler Documentation](../manager/handlers/)
