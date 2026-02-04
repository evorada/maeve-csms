# MaEVe CSMS Code Style Guide

## Table of Contents
- [Overview](#overview)
- [Project Structure](#project-structure)
- [Go Language Conventions](#go-language-conventions)
- [Package Organization](#package-organization)
- [Naming Conventions](#naming-conventions)
- [Error Handling](#error-handling)
- [Testing](#testing)
- [Documentation](#documentation)
- [Dependencies](#dependencies)
- [Observability](#observability)
- [Interfaces and Abstractions](#interfaces-and-abstractions)

## Overview

MaEVe is an EV Charge Station Management System (CSMS) written in Go 1.20. This document describes the code style and patterns used throughout the codebase to maintain consistency and quality.

## Project Structure

The project follows a modular structure with clear separation of concerns:

```
maeve-csms/
├── gateway/           # Stateful service handling OCPP websocket connections
│   ├── cmd/          # Command-line interface
│   ├── ocpp/         # OCPP protocol handling
│   ├── pipe/         # Message pipeline
│   ├── registry/     # Charge station registry
│   └── server/       # HTTP/WebSocket server
├── manager/          # Stateless service handling OCPP message logic
│   ├── api/          # REST API definitions
│   ├── cmd/          # Command-line interface
│   ├── config/       # Configuration management
│   ├── handlers/     # OCPP message handlers
│   ├── ocpp/         # OCPP protocol types
│   ├── ocpi/         # OCPI protocol support
│   ├── schemas/      # JSON schemas
│   ├── server/       # HTTP server
│   ├── services/     # Business logic services
│   ├── store/        # Data access layer
│   ├── sync/         # Synchronization utilities
│   └── transport/    # Message transport (MQTT)
├── config/           # Configuration files
├── docs/            # Documentation
├── e2e_tests/       # End-to-end tests
└── scripts/         # Build and deployment scripts
```

### Key Architectural Principles

1. **Separation of Concerns**: Gateway handles connections, Manager handles business logic
2. **Stateless Services**: Manager is designed to be horizontally scalable
3. **Event-Driven**: Components communicate via MQTT for loose coupling
4. **Storage Abstraction**: Database access is abstracted through store interfaces

## Go Language Conventions

### General Rules

1. **Follow Standard Go Formatting**: Use `gofmt` or `goimports` for automatic formatting
2. **Go Version**: Target Go 1.20 or higher
3. **Effective Go**: Follow the guidelines in [Effective Go](https://go.dev/doc/effective_go)

### License Headers

All source files must include the Apache 2.0 license header:

```go
// SPDX-License-Identifier: Apache-2.0
```

## Package Organization

### Package Naming

- Use **lowercase** package names
- Use **short, descriptive** names (prefer `store` over `datastore`)
- Package names should be **singular** unless plurality makes sense (e.g., `handlers`, `services`)
- Avoid package names that conflict with standard library

### Package Structure

Each package should have:

1. **Interface definitions** in the main package files
2. **Implementations** in separate files or subpackages
3. **Tests** alongside the implementation (`*_test.go`)
4. **Documentation** in `doc.go` (optional but recommended for complex packages)

Example from `transport` package:

```go
// transport/listener.go - Interface definition
package transport

type Listener interface {
    Connect(ctx context.Context, ocppVersion OcppVersion, 
            chargeStationId *string, handler MessageHandler) (Connection, error)
}

// transport/mqtt/listener.go - Implementation
package mqtt

type Listener struct {
    connectionDetails
    mqttGroup string
    tracer    trace.Tracer
}
```

## Naming Conventions

### Types and Structs

- Use **PascalCase** for exported types
- Use **camelCase** for unexported types
- Struct names should be **nouns** or **noun phrases**

```go
// Exported
type BootNotificationHandler struct { ... }
type MessageHandler interface { ... }

// Unexported
type connectionDetails struct { ... }
```

### Functions and Methods

- Use **PascalCase** for exported functions
- Use **camelCase** for unexported functions
- Function names should be **verbs** or **verb phrases**
- Use **Get/Set** prefix sparingly (Go convention prefers direct names)

```go
// Preferred
func (s *Store) LookupToken(ctx context.Context, tokenUid string) (*Token, error)
func (h *Handler) HandleCall(ctx context.Context, chargeStationId string, request Request)

// Avoid (unless getter/setter pattern is necessary)
func (s *Store) GetToken(ctx context.Context, tokenUid string) (*Token, error)
```

### Variables and Constants

- Use **camelCase** for variables
- Use **PascalCase** or **SCREAMING_SNAKE_CASE** for constants (depending on context)
- Constants used as enum values follow PascalCase

```go
const (
    CacheModeAlways         = "ALWAYS"
    CacheModeNever          = "NEVER"
)

var (
    defaultTimeout = 10 * time.Second
    maxRetries     = 3
)
```

### Interfaces

- Interface names should describe **behavior** or **capability**
- Single-method interfaces often end in **-er** suffix
- Multi-method interfaces use descriptive names

```go
type MessageHandler interface { ... }
type Listener interface { ... }
type TokenStore interface { ... }
type Engine interface { ... }  // Composite interface
```

## Error Handling

### Error Returns

- **Always** return errors as the last return value
- Use named error variables for clarity in complex functions
- Return `error` not `*error`

```go
func (h *Handler) HandleCall(ctx context.Context, chargeStationId string, 
                              request Request) (Response, error) {
    // implementation
}
```

### Error Checking

- **Check errors immediately** after they occur
- **Don't** ignore errors (use `_` only when intentional)
- Propagate errors up the stack when appropriate

```go
err := store.SetToken(ctx, token)
if err != nil {
    return nil, err
}
```

### Error Context

- Add context to errors when propagating up
- Use `fmt.Errorf` with `%w` verb for error wrapping (Go 1.13+)

```go
if err != nil {
    return nil, fmt.Errorf("failed to lookup token %s: %w", tokenUid, err)
}
```

### Error Handling in Tests

- Use `require.NoError()` for setup errors that should abort the test
- Use `assert.Error()` or `assert.NoError()` for test assertions

```go
err := engine.SetToken(context.Background(), token)
require.NoError(t, err)  // If this fails, stop the test

result, err := handler.Handle(ctx, request)
assert.NoError(t, err)  // If this fails, continue with other tests
```

## Testing

### Test File Naming

- Test files use `_test.go` suffix
- Place tests in the same package or `<package>_test` package

```go
// Same package (white-box testing)
package ocpp16

// External package (black-box testing)
package ocpp16_test
```

### Test Function Naming

- Use `Test<FunctionName>` pattern
- Use descriptive names that explain what is being tested

```go
func TestStopTransactionHandler(t *testing.T) { ... }
func TestBootNotificationWithMissingFields(t *testing.T) { ... }
```

### Test Structure

Follow the **Arrange-Act-Assert** pattern:

```go
func TestStopTransactionHandler(t *testing.T) {
    // Arrange
    chargingStationId := "cs001"
    engine := inmemory.NewStore(clock.RealClock{})
    handler := handlers.StopTransactionHandler{
        Clock:            clock.RealClock{},
        TokenStore:       engine,
        TransactionStore: engine,
    }
    
    // Act
    response, err := handler.HandleCall(ctx, chargingStationId, request)
    
    // Assert
    require.NoError(t, err)
    assert.Equal(t, expectedStatus, response.Status)
}
```

### Test Dependencies

- Use **testify/assert** and **testify/require** for assertions
- Use **testcontainers** for integration tests requiring external services
- Use **k8s.io/utils/clock** for time-dependent tests

### Mocking and Test Doubles

- Prefer **in-memory implementations** over mocking frameworks
- Example: `inmemory.NewStore()` for testing handlers

## Documentation

### Package Documentation

- Include a `doc.go` file for packages with non-obvious purposes
- Start with `// Package <name>` comment

```go
// Package transport provides message transport abstractions for the CSMS manager.
//
// The transport layer handles sending and receiving OCPP messages between
// the gateway and manager components via MQTT.
package transport
```

### Function/Method Documentation

- Document **all exported** functions, types, and constants
- Start with the name of the thing being documented
- Be concise but clear about purpose and behavior

```go
// HandleCall processes a BootNotification request from a charge station.
// It updates the charge station runtime details and clears any reboot-required settings.
func (b BootNotificationHandler) HandleCall(ctx context.Context, 
                                             chargeStationId string, 
                                             request ocpp.Request) (ocpp.Response, error) {
    // implementation
}
```

### TODO Comments

- Use `TODO` comments for future improvements
- Include your name or issue number for tracking

```go
// TODO(username): Implement retry logic for failed connections
// TODO: Issue #123 - Add support for OCPP 2.0.1 SecurityEventNotification
```

## Dependencies

### Dependency Management

- Use **go.mod** for dependency management
- Keep dependencies **minimal** and **up-to-date**
- Prefer standard library over external dependencies when appropriate

### Key Dependencies

The project uses these major dependencies:

- **cobra**: CLI framework
- **chi**: HTTP router
- **paho**: MQTT client
- **testify**: Testing framework
- **opentelemetry**: Observability
- **firestore**: Database (one implementation of store interface)

### Importing

- Use **goimports** for automatic import management
- Group imports in the following order:
  1. Standard library
  2. External packages
  3. Internal packages

```go
import (
    "context"
    "fmt"
    "time"
    
    "github.com/spf13/cobra"
    "go.opentelemetry.io/otel/trace"
    
    "github.com/thoughtworks/maeve-csms/manager/ocpp"
    "github.com/thoughtworks/maeve-csms/manager/store"
)
```

## Observability

### Tracing

Use **OpenTelemetry** for distributed tracing:

```go
func (h *Handler) HandleCall(ctx context.Context, chargeStationId string, 
                              request ocpp.Request) (ocpp.Response, error) {
    span := trace.SpanFromContext(ctx)
    
    span.SetAttributes(
        attribute.String("charge_station_id", chargeStationId),
        attribute.String("request_type", request.MessageType()))
    
    // ... implementation
    
    if err != nil {
        span.RecordError(err)
        return nil, err
    }
    
    return response, nil
}
```

### Logging

- Use **slog** (structured logging) for logging
- Log at appropriate levels (Debug, Info, Warn, Error)
- Include relevant context in log messages

```go
slog.Error("failed to subscribe to topic", "topic", topic, "error", err)
slog.Info("charge station connected", "id", chargeStationId, "version", ocppVersion)
```

### Metrics

Use **Prometheus** client for metrics (when applicable)

## Interfaces and Abstractions

### Store Pattern

All data access goes through **store interfaces**:

```go
type TokenStore interface {
    SetToken(ctx context.Context, token *Token) error
    LookupToken(ctx context.Context, tokenUid string) (*Token, error)
    ListTokens(ctx context.Context, offset int, limit int) ([]*Token, error)
}
```

**Benefits:**
- Enables multiple implementations (in-memory, Firestore, etc.)
- Facilitates testing with mock/in-memory stores
- Decouples business logic from storage details

### Service Pattern

Business logic is encapsulated in **service interfaces**:

```go
type TokenAuthService interface {
    Authorize(ctx context.Context, token ocpp201.IdTokenType) ocpp201.IdTokenInfoType
}
```

### Handler Pattern

OCPP message handling follows a consistent pattern:

```go
type Handler interface {
    HandleCall(ctx context.Context, chargeStationId string, 
               request ocpp.Request) (ocpp.Response, error)
}
```

Each OCPP message type has a dedicated handler struct:

```go
type BootNotificationHandler struct {
    Clock               clock.PassiveClock
    RuntimeDetailsStore store.ChargeStationRuntimeDetailsStore
    SettingsStore       store.ChargeStationSettingsStore
    HeartbeatInterval   int
}
```

### Functional Options Pattern

Use **functional options** for configuring complex types:

```go
type Opt[T any] func(*T)

func WithBrokerUrl(url *url.URL) Opt[Listener] {
    return func(l *Listener) {
        l.mqttBrokerUrls = []*url.URL{url}
    }
}

// Usage
listener := NewListener(
    WithBrokerUrl(brokerUrl),
    WithPrefix("cs"),
    WithGroup("manager"),
)
```

### Dependency Injection

- Use **constructor injection** for dependencies
- Keep constructors simple (delegate complex setup to factory functions)

```go
type Handler struct {
    store   store.TokenStore
    service services.TokenAuthService
}

func NewHandler(store store.TokenStore, service services.TokenAuthService) *Handler {
    return &Handler{
        store:   store,
        service: service,
    }
}
```

## Context Usage

### Context Parameter

- **Always** use `context.Context` as the first parameter
- **Never** store context in a struct
- Pass context through the call chain

```go
func (s *Store) LookupToken(ctx context.Context, tokenUid string) (*Token, error) {
    // Use ctx for cancellation, deadlines, and values
}
```

### Context Values

Use context for:
- Request-scoped values (trace IDs, user IDs)
- Cancellation signals
- Deadlines and timeouts

**Don't** use context for optional parameters.

## Concurrency

### Goroutines

- Use goroutines sparingly and with clear lifecycle management
- Always consider how goroutines will be terminated
- Use context for cancellation

### Synchronization

- Prefer channels over shared memory
- Use `sync.Mutex` when shared state is necessary
- Document any thread-safety guarantees or requirements

## Configuration

### Configuration Files

- Use **TOML** for configuration (manager)
- Use **command-line flags** for simple configuration (gateway)
- Provide sensible defaults for optional configuration

### Configuration Validation

- Validate configuration at startup
- Fail fast on invalid configuration
- Provide clear error messages

```go
func ensureListenerDefaults(l *Listener) {
    if l.mqttBrokerUrls == nil {
        u, err := url.Parse("mqtt://127.0.0.1:1883/")
        if err != nil {
            panic(err)  // Configuration error at startup
        }
        l.mqttBrokerUrls = []*url.URL{u}
    }
}
```

## Code Review Guidelines

When reviewing code, check for:

1. **Correctness**: Does the code do what it's supposed to do?
2. **Style**: Does it follow this guide?
3. **Tests**: Are there adequate tests?
4. **Documentation**: Are exported items documented?
5. **Error Handling**: Are errors handled appropriately?
6. **Observability**: Are traces/logs added for debugging?
7. **Performance**: Are there obvious performance issues?
8. **Security**: Are there security concerns?

## Resources

- [Effective Go](https://go.dev/doc/effective_go)
- [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments)
- [Uber Go Style Guide](https://github.com/uber-go/guide/blob/master/style.md)
- [MaEVe Design Document](./docs/design.md)
