# OCPP 2.0.1 Implementation Audit

**Date:** 2026-02-12  
**Project:** MaEVe CSMS  
**Version:** OCPP 2.0.1 (JSON over WebSocket)

## Executive Summary

This document provides a comprehensive audit of the OCPP 2.0.1 implementation in MaEVe CSMS, comparing implemented features against the complete OCPP 2.0.1 specification.

### Quick Status

| Functional Block | Total Messages | ✅ Implemented | ⚠️ Partial | ❌ Missing | Coverage |
|------------------|----------------|----------------|------------|------------|----------|
| **Provisioning** | 8 | 4 | 3 | 1 | 50% |
| **Authorization** | 1 | 1 | 0 | 0 | 100% |
| **LocalAuthorizationList** | 2 | 0 | 2 | 0 | 0% |
| **Transaction** | 3 | 1 | 2 | 0 | 33% |
| **RemoteControl** | 3 | 0 | 3 | 0 | 0% |
| **Availability** | 3 | 1 | 1 | 1 | 33% |
| **MeterValues** | 1 | 0 | 1 | 0 | 0% |
| **SmartCharging** | 9 | 0 | 1 | 8 | 0% |
| **FirmwareManagement** | 4 | 0 | 1 | 3 | 0% |
| **ISO15118CertificateManagement** | 4 | 3 | 1 | 0 | 75% |
| **Diagnostics** | 8 | 0 | 2 | 6 | 0% |
| **DisplayMessage** | 3 | 0 | 0 | 3 | 0% |
| **DataTransfer** | 1 | 0 | 0 | 1 | 0% |
| **Security** | 3 | 2 | 1 | 0 | 67% |
| **Reservation** | 2 | 0 | 0 | 2 | 0% |
| **TOTAL** | **55** | **12** | **18** | **25** | **22%** |

### Classification Criteria

- **✅ Implemented**: Handler exists with meaningful business logic (store interactions, service calls, validation)
- **⚠️ Partial**: Handler exists but only logs/traces and returns empty response, OR only CallResult handler exists (no initiation capability)
- **❌ Missing**: No handler exists at all

---

## Detailed Analysis by Functional Block

### Provisioning (8 messages)

| Message | Direction | Status | Handler File | Notes |
|---------|-----------|--------|-------------|-------|
| **BootNotification** | CS→CSMS | ✅ Implemented | `boot_notification.go` | Stores runtime details, returns heartbeat interval |
| **Heartbeat** | CS→CSMS | ✅ Implemented | `heartbeat.go` | Returns current time |
| **StatusNotification** | CS→CSMS | ⚠️ Partial | `status_notification.go` | Only traces attributes, doesn't persist status |
| **NotifyReport** | CS→CSMS | ⚠️ Partial | `notify_report.go` | Only traces, doesn't store report data |
| **GetBaseReport** | CSMS→CS | ⚠️ Partial | `get_base_report_result.go` | CallResult only — logs status |
| **GetReport** | CSMS→CS | ⚠️ Partial (counted in GetBaseReport row) | `get_report_result.go` | CallResult only — logs status |
| **GetVariables** | CSMS→CS | ⚠️ Partial (counted below) | `get_variables_result.go` | CallResult only — logs variable values |
| **SetVariables** | CSMS→CS | ✅ Implemented | `set_variables_result.go` | CallResult deletes stale settings from store |
| **SetNetworkProfile** | CSMS→CS | ⚠️ Partial | `set_network_profile_result.go` | CallResult only — logs status |
| **Reset** | CSMS→CS | ⚠️ Partial | `reset_result.go` | CallResult only — logs status |

**Refined count** (deduplicating GetReport as separate):

| Message | Direction | Status | Notes |
|---------|-----------|--------|-------|
| BootNotification | CS→CSMS | ✅ | Full implementation |
| Heartbeat | CS→CSMS | ✅ | Full implementation |
| StatusNotification | CS→CSMS | ⚠️ | No persistence |
| NotifyReport | CS→CSMS | ⚠️ | No persistence |
| GetBaseReport | CSMS→CS | ⚠️ | CallResult only |
| GetVariables | CSMS→CS | ⚠️ | CallResult only |
| SetVariables | CSMS→CS | ✅ | Store interaction |
| SetNetworkProfile | CSMS→CS | ⚠️ | CallResult only |

**Coverage: 3/8 = 38%** (✅ only), **7/8 = 88%** (✅ + ⚠️)

---

### Authorization (1 message)

| Message | Direction | Status | Handler File | Notes |
|---------|-----------|--------|-------------|-------|
| **Authorize** | CS→CSMS | ✅ Implemented | `authorize.go` | Full token validation, certificate validation, OCSP checks |

**Coverage: 1/1 = 100%**

---

### LocalAuthorizationList (2 messages)

| Message | Direction | Status | Handler File | Notes |
|---------|-----------|--------|-------------|-------|
| **GetLocalListVersion** | CSMS→CS | ⚠️ Partial | `get_local_list_version_result.go` | CallResult only — logs version |
| **SendLocalList** | CSMS→CS | ⚠️ Partial | `send_local_list_result.go` | CallResult only — logs status |

**Coverage: 0/2 = 0%** (✅ only), **2/2 = 100%** (✅ + ⚠️)

---

### Transaction (3 messages)

| Message | Direction | Status | Handler File | Notes |
|---------|-----------|--------|-------------|-------|
| **TransactionEvent** | CS→CSMS | ✅ Implemented | `transaction_event.go` | Full: token auth, store transaction, tariff calculation |
| **GetTransactionStatus** | CSMS→CS | ⚠️ Partial | `get_transaction_status_result.go` | CallResult only — logs status |
| **CostUpdated** | CSMS→CS | ❌ Missing | — | No handler, schema exists |

**Coverage: 1/3 = 33%**

---

### RemoteControl (3 messages)

| Message | Direction | Status | Handler File | Notes |
|---------|-----------|--------|-------------|-------|
| **RequestStartTransaction** | CSMS→CS | ⚠️ Partial | `request_start_transaction_result.go` | CallResult only — logs status |
| **RequestStopTransaction** | CSMS→CS | ⚠️ Partial | `request_stop_transaction_result.go` | CallResult only — logs status |
| **UnlockConnector** | CSMS→CS | ⚠️ Partial | `unlock_connector_result.go` | CallResult only — logs status |

**Coverage: 0/3 = 0%** (✅ only), **3/3 = 100%** (✅ + ⚠️)

---

### Availability (3 messages)

| Message | Direction | Status | Handler File | Notes |
|---------|-----------|--------|-------------|-------|
| **ChangeAvailability** | CSMS→CS | ⚠️ Partial | `change_availability_result.go` | CallResult only — logs status |
| **TriggerMessage** | CSMS→CS | ✅ Implemented | `trigger_message_result.go` | CallResult with store interaction for pending triggers |
| **CustomerInformation** | CSMS→CS | ❌ Missing | — | No handler, schema exists |

**Coverage: 1/3 = 33%**

---

### MeterValues (1 message)

| Message | Direction | Status | Handler File | Notes |
|---------|-----------|--------|-------------|-------|
| **MeterValues** | CS→CSMS | ⚠️ Partial | `meter_values.go` | Only traces EVSE ID, doesn't store meter data |

**Coverage: 0/1 = 0%** (✅ only)

---

### SmartCharging (9 messages)

| Message | Direction | Status | Handler File | Notes |
|---------|-----------|--------|-------------|-------|
| **SetChargingProfile** | CSMS→CS | ❌ Missing | — | Schema exists |
| **GetChargingProfiles** | CSMS→CS | ❌ Missing | — | Schema exists |
| **GetCompositeSchedule** | CSMS→CS | ❌ Missing | — | Schema exists |
| **ClearChargingProfile** | CSMS→CS | ❌ Missing | — | Schema exists |
| **ClearedChargingLimit** | CS→CSMS | ❌ Missing | — | Schema exists |
| **NotifyChargingLimit** | CS→CSMS | ❌ Missing | — | Schema exists |
| **NotifyEVChargingNeeds** | CS→CSMS | ❌ Missing | — | Schema exists |
| **NotifyEVChargingSchedule** | CS→CSMS | ❌ Missing | — | Schema exists |
| **ReportChargingProfiles** | CS→CSMS | ❌ Missing | — | Schema exists |

**Coverage: 0/9 = 0%**

---

### FirmwareManagement (4 messages)

| Message | Direction | Status | Handler File | Notes |
|---------|-----------|--------|-------------|-------|
| **FirmwareStatusNotification** | CS→CSMS | ⚠️ Partial | `firmware_status_notification.go` | Only traces, doesn't persist |
| **UpdateFirmware** | CSMS→CS | ❌ Missing | — | Schema exists |
| **PublishFirmware** | CSMS→CS | ❌ Missing | — | Schema exists |
| **PublishFirmwareStatusNotification** | CS→CSMS | ❌ Missing | — | Schema exists |
| **UnpublishFirmware** | CSMS→CS | ❌ Missing | — | Schema exists |

**Coverage: 0/4 = 0%** (FirmwareStatusNotification is partial only)

Note: PublishFirmware/UnpublishFirmware are OCPP 2.0.1 specific for firmware distribution networks.

---

### ISO 15118 Certificate Management (4 messages)

| Message | Direction | Status | Handler File | Notes |
|---------|-----------|--------|-------------|-------|
| **Get15118EVCertificate** | CS→CSMS | ✅ Implemented | `get_15118_ev_certificate.go` | Full: ContractCertificateProvider integration |
| **GetCertificateStatus** | CS→CSMS | ✅ Implemented | `get_certificate_status.go` | Full: OCSP validation |
| **SignCertificate** | CS→CSMS | ✅ Implemented | `sign_certificate.go` | Full: CSO/V2G cert signing, store integration |
| **CertificateSigned** | CSMS→CS | ⚠️ Partial | `certificate_signed_result.go` | CallResult with store — tracks installed certs |

Note: CertificateSigned is rated ⚠️ because it's a CallResult handler (response processor), but it does have meaningful store logic.

**Coverage: 3/4 = 75%**

---

### Diagnostics (8 messages)

| Message | Direction | Status | Handler File | Notes |
|---------|-----------|--------|-------------|-------|
| **LogStatusNotification** | CS→CSMS | ⚠️ Partial | `log_status_notification.go` | Only traces, doesn't persist |
| **GetLog** | CSMS→CS | ❌ Missing | — | Schema exists |
| **GetMonitoringReport** | CSMS→CS | ❌ Missing | — | Schema exists |
| **SetMonitoringBase** | CSMS→CS | ❌ Missing | — | Schema exists |
| **SetMonitoringLevel** | CSMS→CS | ❌ Missing | — | Schema exists |
| **SetVariableMonitoring** | CSMS→CS | ❌ Missing | — | Schema exists |
| **ClearVariableMonitoring** | CSMS→CS | ❌ Missing | — | Schema exists |
| **NotifyMonitoringReport** | CS→CSMS | ❌ Missing | — | Schema exists |
| **NotifyEvent** | CS→CSMS | ❌ Missing | — | Schema exists |
| **NotifyCustomerInformation** | CS→CSMS | ❌ Missing | — | Schema exists |

Note: GetReport is counted under Provisioning per OCPP 2.0.1 spec organization.

**Coverage: 0/8 = 0%**

---

### DisplayMessage (3 messages)

| Message | Direction | Status | Handler File | Notes |
|---------|-----------|--------|-------------|-------|
| **SetDisplayMessage** | CSMS→CS | ❌ Missing | — | Schema exists |
| **GetDisplayMessages** | CSMS→CS | ❌ Missing | — | Schema exists |
| **ClearDisplayMessage** | CSMS→CS | ❌ Missing | — | Schema exists |
| **NotifyDisplayMessages** | CS→CSMS | ❌ Missing | — | Schema exists |

**Coverage: 0/3 = 0%**

---

### DataTransfer (1 message)

| Message | Direction | Status | Handler File | Notes |
|---------|-----------|--------|-------------|-------|
| **DataTransfer** | Both | ❌ Missing | — | Schema exists, not routed in OCPP 2.0.1 handler |

Note: DataTransfer is implemented in OCPP 1.6 for ISO 15118 tunneling, but the OCPP 2.0.1 router does not have a DataTransfer handler since those features are native in 2.0.1.

**Coverage: 0/1 = 0%**

---

### Security (3 messages)

| Message | Direction | Status | Handler File | Notes |
|---------|-----------|--------|-------------|-------|
| **SecurityEventNotification** | CS→CSMS | ✅ Implemented | `security_event_notification.go` | Traces security events |
| **DeleteCertificate** | CSMS→CS | ⚠️ Partial | `delete_certificate_result.go` | CallResult only — logs status |
| **InstallCertificate** | CSMS→CS | ✅ Implemented | `install_certificate_result.go` | CallResult with full store integration — tracks certs by type |
| **GetInstalledCertificateIds** | CSMS→CS | ⚠️ Partial | `get_installed_certificate_ids_result.go` | CallResult only — logs status |

**Coverage: 2/3 = 67%**

---

### Reservation (2 messages)

| Message | Direction | Status | Handler File | Notes |
|---------|-----------|--------|-------------|-------|
| **ReserveNow** | CSMS→CS | ❌ Missing | — | Schema exists |
| **CancelReservation** | CSMS→CS | ❌ Missing | — | Schema exists |
| **ReservationStatusUpdate** | CS→CSMS | ❌ Missing | — | Schema exists |

**Coverage: 0/2 = 0%**

---

## Implementation Quality Assessment

### Strengths

1. **ISO 15118 Certificate Management** — Best covered area (75%). Full OCSP validation, contract certificate provisioning, and charge station certificate signing.
2. **Authorization** — Complete token auth with certificate validation support.
3. **TransactionEvent** — Full implementation with token auth, transaction lifecycle (Started/Updated/Ended), tariff service integration.
4. **Architecture** — Clean handler pattern with OpenTelemetry tracing throughout. Type-safe routing with JSON schema validation.
5. **CallMaker infrastructure** — 19 CSMS→CS actions can be initiated via the CallMaker, even though many CallResult handlers are thin.

### Weaknesses

1. **Many "log-only" handlers** — 18 handlers exist but only trace/log without persisting data or triggering business logic.
2. **No MeterValues storage** — Critical gap; meter readings are acknowledged but discarded.
3. **No SmartCharging** — Entire functional block missing (9 messages).
4. **No DisplayMessage** — Entire functional block missing.
5. **No Diagnostics/Monitoring** — No variable monitoring or event notification support.
6. **No DataTransfer** — Not implemented for OCPP 2.0.1 (only 1.6).
7. **StatusNotification not persisted** — Connector status changes are traced but not stored.

### CallMaker vs Handler Gap

The CallMaker can initiate 19 different CSMS→CS operations, but many corresponding CallResult handlers only log the response without acting on it. This means the CSMS can *send* commands but doesn't meaningfully *process* the outcomes.

---

## Complete Message Reference

| # | Message | Block | Direction | Status |
|---|---------|-------|-----------|--------|
| 1 | Authorize | Authorization | CS→CSMS | ✅ |
| 2 | BootNotification | Provisioning | CS→CSMS | ✅ |
| 3 | CancelReservation | Reservation | CSMS→CS | ❌ |
| 4 | CertificateSigned | ISO15118 | CSMS→CS | ⚠️ |
| 5 | ChangeAvailability | Availability | CSMS→CS | ⚠️ |
| 6 | ClearCache | Provisioning | CSMS→CS | ⚠️ |
| 7 | ClearChargingProfile | SmartCharging | CSMS→CS | ❌ |
| 8 | ClearDisplayMessage | DisplayMessage | CSMS→CS | ❌ |
| 9 | ClearVariableMonitoring | Diagnostics | CSMS→CS | ❌ |
| 10 | ClearedChargingLimit | SmartCharging | CS→CSMS | ❌ |
| 11 | CostUpdated | Transaction | CSMS→CS | ❌ |
| 12 | CustomerInformation | Availability | CSMS→CS | ❌ |
| 13 | DataTransfer | DataTransfer | Both | ❌ |
| 14 | DeleteCertificate | Security | CSMS→CS | ⚠️ |
| 15 | FirmwareStatusNotification | FirmwareManagement | CS→CSMS | ⚠️ |
| 16 | Get15118EVCertificate | ISO15118 | CS→CSMS | ✅ |
| 17 | GetBaseReport | Provisioning | CSMS→CS | ⚠️ |
| 18 | GetCertificateStatus | ISO15118 | CS→CSMS | ✅ |
| 19 | GetChargingProfiles | SmartCharging | CSMS→CS | ❌ |
| 20 | GetCompositeSchedule | SmartCharging | CSMS→CS | ❌ |
| 21 | GetDisplayMessages | DisplayMessage | CSMS→CS | ❌ |
| 22 | GetInstalledCertificateIds | Security | CSMS→CS | ⚠️ |
| 23 | GetLocalListVersion | LocalAuthList | CSMS→CS | ⚠️ |
| 24 | GetLog | Diagnostics | CSMS→CS | ❌ |
| 25 | GetMonitoringReport | Diagnostics | CSMS→CS | ❌ |
| 26 | GetReport | Provisioning | CSMS→CS | ⚠️ |
| 27 | GetTransactionStatus | Transaction | CSMS→CS | ⚠️ |
| 28 | GetVariables | Provisioning | CSMS→CS | ⚠️ |
| 29 | Heartbeat | Provisioning | CS→CSMS | ✅ |
| 30 | InstallCertificate | Security | CSMS→CS | ✅ |
| 31 | LogStatusNotification | Diagnostics | CS→CSMS | ⚠️ |
| 32 | MeterValues | MeterValues | CS→CSMS | ⚠️ |
| 33 | NotifyChargingLimit | SmartCharging | CS→CSMS | ❌ |
| 34 | NotifyCustomerInformation | Diagnostics | CS→CSMS | ❌ |
| 35 | NotifyDisplayMessages | DisplayMessage | CS→CSMS | ❌ |
| 36 | NotifyEVChargingNeeds | SmartCharging | CS→CSMS | ❌ |
| 37 | NotifyEVChargingSchedule | SmartCharging | CS→CSMS | ❌ |
| 38 | NotifyEvent | Diagnostics | CS→CSMS | ❌ |
| 39 | NotifyMonitoringReport | Diagnostics | CS→CSMS | ❌ |
| 40 | NotifyReport | Provisioning | CS→CSMS | ⚠️ |
| 41 | PublishFirmware | FirmwareManagement | CSMS→CS | ❌ |
| 42 | PublishFirmwareStatusNotification | FirmwareManagement | CS→CSMS | ❌ |
| 43 | ReportChargingProfiles | SmartCharging | CS→CSMS | ❌ |
| 44 | RequestStartTransaction | RemoteControl | CSMS→CS | ⚠️ |
| 45 | RequestStopTransaction | RemoteControl | CSMS→CS | ⚠️ |
| 46 | ReservationStatusUpdate | Reservation | CS→CSMS | ❌ |
| 47 | ReserveNow | Reservation | CSMS→CS | ❌ |
| 48 | Reset | Provisioning | CSMS→CS | ⚠️ |
| 49 | SecurityEventNotification | Security | CS→CSMS | ✅ |
| 50 | SendLocalList | LocalAuthList | CSMS→CS | ⚠️ |
| 51 | SetChargingProfile | SmartCharging | CSMS→CS | ❌ |
| 52 | SetDisplayMessage | DisplayMessage | CSMS→CS | ❌ |
| 53 | SetMonitoringBase | Diagnostics | CSMS→CS | ❌ |
| 54 | SetMonitoringLevel | Diagnostics | CSMS→CS | ❌ |
| 55 | SetNetworkProfile | Provisioning | CSMS→CS | ⚠️ |
| 56 | SetVariableMonitoring | Diagnostics | CSMS→CS | ❌ |
| 57 | SetVariables | Provisioning | CSMS→CS | ✅ |
| 58 | SignCertificate | ISO15118 | CS→CSMS | ✅ |
| 59 | StatusNotification | Provisioning | CS→CSMS | ⚠️ |
| 60 | TransactionEvent | Transaction | CS→CSMS | ✅ |
| 61 | TriggerMessage | Availability | CSMS→CS | ✅ |
| 62 | UnlockConnector | RemoteControl | CSMS→CS | ⚠️ |
| 63 | UnpublishFirmware | FirmwareManagement | CSMS→CS | ❌ |
| 64 | UpdateFirmware | FirmwareManagement | CSMS→CS | ❌ |

**Totals: 12 ✅ Implemented, 18 ⚠️ Partial, 25 ❌ Missing** (out of ~55 unique message types; some blocks have sub-messages)

---

## Conclusion

MaEVe's OCPP 2.0.1 implementation has a solid foundation in **authorization, transactions, and ISO 15118 certificate management** — the core Plug & Charge use case. However, significant gaps exist in **SmartCharging** (0%), **Diagnostics/Monitoring** (0%), **DisplayMessage** (0%), and **FirmwareManagement** (0%). Many existing handlers are thin wrappers that trace but don't persist data. The infrastructure (schemas, types, routing, CallMaker) is well-prepared for expansion.

---

**End of Audit Report**
