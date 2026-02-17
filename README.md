[![Manager](https://github.com/evorada/maeve-csms/workflows/Manager/badge.svg)](https://github.com/evorada/maeve-csms/actions/workflows/manager.yml)
[![Gateway](https://github.com/evorada/maeve-csms/workflows/Gateway/badge.svg)](https://github.com/evorada/maeve-csms/actions/workflows/gateway.yml)

![](https://github.com/evorada/maeve-csms/raw/refs/heads/main/docs/assets/maeve_logo.svg)

MaEVe is an EV charge station management system (CSMS). It began life as a simple proof of concept for
implementing ISO-15118-2 Plug and Charge (PnC) functionality and remains a work in progress. It is hoped that over
time it will become more complete, but already provides a useful basis for experimentation.

Originally developed by [Thoughtworks](https://github.com/thoughtworks/maeve-csms), the project was archived on Jun 2, 2025 and has been revived and maintained by [EVorada](https://github.com/evorada/maeve-csms) since then. 

The system currently integrates with [Hubject](https://hubject.stoplight.io/) for PnC functionality and fully supports OCPP 1.6 and 2.0.1.

## Table of Contents
- [OCPP Support](#ocpp-support)
- [Storage Backends](#storage-backends)
- [Documentation](#documentation)
- [Getting Started](#getting-started)
- [Configuration](#configuration)
- [Contributing](#contributing)
- [License](#license)

## OCPP Support

MaEVe fully supports **OCPP 1.6j** and **OCPP 2.0.1**. Charge stations negotiate the protocol version via the WebSocket `Sec-WebSocket-Protocol` header; the gateway defaults to OCPP 2.0.1 when no preference is indicated.

### Message Handler Coverage

The table below lists every implemented action:

| Action | 1.6 Call | 1.6 CallResult | 2.0.1 Call | 2.0.1 CallResult |
|---|:---:|:---:|:---:|:---:|
| Authorize | ✅ | | ✅ | |
| BootNotification | ✅ | | ✅ | |
| CancelReservation | | ✅ | | |
| CertificateSigned | | ✅ | | ✅ |
| ChangeAvailability | | ✅ | | ✅ |
| ChangeConfiguration | | ✅ | | |
| ClearCache | | ✅ | | ✅ |
| ClearChargingProfile | | ✅ | | ✅ |
| ClearedChargingLimit | | | ✅ | |
| CostUpdated | | | | ✅ |
| DataTransfer (PnC) | ✅ | ✅ | | |
| DeleteCertificate | | ✅ | | ✅ |
| DiagnosticsStatusNotification | ✅ | | | |
| ExtendedTriggerMessage | | ✅ | | |
| FirmwareStatusNotification | ✅ | | ✅ | |
| Get15118EVCertificate | ✅ | | ✅ | |
| GetBaseReport | | | | ✅ |
| GetCertificateStatus | ✅ | | ✅ | |
| GetChargingProfiles | | | | ✅ |
| GetCompositeSchedule | | ✅ | | ✅ |
| GetConfiguration | | ✅ | | |
| GetDiagnostics | | ✅ | | |
| GetInstalledCertificateIds | | ✅ | | ✅ |
| GetLocalListVersion | | ✅ | | ✅ |
| GetLog | | ✅ | | |
| GetReport | | | | ✅ |
| GetTransactionStatus | | | | ✅ |
| GetVariables | | | | ✅ |
| Heartbeat | ✅ | | ✅ | |
| InstallCertificate | | ✅ | | ✅ |
| LogStatusNotification | ✅ | | ✅ | |
| MeterValues | ✅ | | ✅ | |
| NotifyChargingLimit | | | ✅ | |
| NotifyEVChargingNeeds | | | ✅ | |
| NotifyEVChargingSchedule | | | ✅ | |
| NotifyReport | | | ✅ | |
| RemoteStartTransaction | | ✅ | | |
| RemoteStopTransaction | | ✅ | | |
| ReportChargingProfiles | | | ✅ | |
| RequestStartTransaction | | | | ✅ |
| RequestStopTransaction | | | | ✅ |
| ReserveNow | | ✅ | | |
| Reset | | ✅ | | ✅ |
| SecurityEventNotification | ✅ | | ✅ | |
| SendLocalList | | ✅ | | ✅ |
| SetChargingProfile | | ✅ | | ✅ |
| SetNetworkProfile | | | | ✅ |
| SetVariables | | | | ✅ |
| SignCertificate | ✅ | | ✅ | |
| SignedFirmwareStatusNotification | ✅ | | | |
| SignedUpdateFirmware | | ✅ | | |
| StartTransaction | ✅ | | | |
| StatusNotification | ✅ | | ✅ | |
| StopTransaction | ✅ | | | |
| TransactionEvent | | | ✅ | |
| TriggerMessage | | ✅ | | ✅ |
| UnlockConnector | | ✅ | | ✅ |
| UpdateFirmware | | ✅ | | |

## Storage Backends

MaEVe supports three pluggable storage backends, selected via the `type` field in the `[storage]` section of the manager configuration.

| Feature | PostgreSQL | Firestore | In-Memory |
|---|:---:|:---:|:---:|
| **Config type key** | `postgres` | `firestore` | `in_memory` |
| **Persistent storage** | ✅ | ✅ | |
| **Self-hosted** | ✅ | | ✅ |
| **Open source** | ✅ | | ✅ |
| **ACID transactions** | ✅ | | |
| **Multi-instance support** | ✅ | ✅ | |
| **Auto-migrations** | ✅ | | |
| **Recommended for production** | ✅ | ✅ | |

### PostgreSQL

A self-hosted, open-source option backed by [pgx/v5](https://github.com/jackc/pgx) with connection pooling and type-safe queries via [sqlc](https://sqlc.dev/). Schema migrations run automatically on startup or via the `manager migrate` command. A ready-to-use Docker Compose file is provided at `docker-compose-postgres.yml`.

```toml
[storage]
type = "postgres"

[storage.postgres]
host = "localhost"
port = 5432
database = "maeve_csms"
user = "maeve"
password = "your_secure_password"
ssl_mode = "disable"  # use "require" or "verify-full" in production
run_migrations = true
```

See [manager/store/postgres/README.md](./manager/store/postgres/README.md) for full setup instructions, migration commands, and performance tuning.

### Firestore

Google Cloud Firestore — a managed, serverless document database. Requires a GCP project ID. This is the default backend in the example configuration.

```toml
[storage]
type = "firestore"

[storage.firestore]
project_id = "your-gcp-project-id"
```

### In-Memory

A volatile, non-persistent store held entirely in process memory. All data is lost on restart. Does not support running more than one manager instance simultaneously. Intended for unit testing and local development only — no configuration parameters required.

```toml
[storage]
type = "in_memory"
```

## Documentation
MaEVe is implemented in Go 1.20. Learn more about MaEVe and its existing components through this [High-level design document](./docs/design.md).

## Pre-requisites

MaEVe runs in a set of Docker containers. This means you need to have `docker`, `docker-compose` and a docker daemon (e.g. docker desktop, `colima` or `rancher`) installed and running.
Scripts that fetch various tokens use `jq`. Make sure you have it installed.

## Getting started

To get the system up and running:

1. `(cd config/certificates && make)`
2. Run the [./scripts/run.sh](./scripts/run.sh) script

Charge stations can connect to the CSMS using:
* `ws://localhost/ws/<cs-id>`
* `wss://localhost/ws/<cs-id>`

If the charge station is also running in a Docker container then the charge
station docker container can connect to the `maeve-csms` network and the
charge station can connect to the CSMS using:
* `ws://gateway:9310/ws/<cs-id>`
* `wss://gateway:9311/ws/<cs-id>`

Charge stations can use either OCPP 1.6j or OCPP 2.0.1.

For TLS, the charge station should use a certificate provisioned using the
[Hubject CPO EST service](https://hubject.stoplight.io/).

A charge station must first be registered with the CSMS before it can be used. This can be done using the
[manager API](./manager/api/API.md). e.g. for TLS with client certificate, use:

```shell
$ curl http://localhost:9410/api/v0/cs/<cs-id> -H 'content-type: application/json' -d '{"securityProfile":2}'
```

Tokens, which identify a payment method for a non-contract charge, must also be registered with the CSMS before they can be used. This can also be done using the
[manager API](./manager/api/API.md). e.g.:

```shell
$ curl http://localhost:9410/api/v0/token -H 'content-type: application/json' -d '{
  "countryCode": "GB",
  "partyId": "TWK",
  "type": "RFID",
  "uid": "DEADBEEF",
  "contractId": "GBTWK012345678V",
  "issuer": "Thoughtworks",
  "valid": true,
  "cacheMode": "ALWAYS"
}'
```

## Troubleshooting

Docker compose doesn't always rebuild the docker images which can cause all kinds of errors. If in doubt, force a rebuild by `docker-compose build` before launching containers.

`java.io.IOException: keystore password was incorrect`
This error results from incompatibility between java version and openssl; try upgrading your java version.

## Configuration

The gateway is configured through command-line flags. The available flags can be viewed using the `-h` flag. 

The manager is configured through a TOML configuration file. An example configuration file can be found in 
[./config/manager/config.toml](./config/manager/config.toml). Details of the available configuration options
can be found in [./manager/config/README.md](./manager/config/README.md).

## Contributing

Learn more about how to contribute on this project through [Contributing](./CONTRIBUTING.md)

## License
MaEVe is [Apache licensed](./LICENSE).


