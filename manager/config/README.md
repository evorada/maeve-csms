# Configuration

Configuration is defined in TOML.

## Table of Contents

* [General settings](#general-settings)
* [Service settings](#service-settings)
* [Transport](#transport-settings)
  * [NATS connection](#nats-connection)
* [Storage](#storage)
* [Contract certificate validator](#contract-certificate-validator)
* [Contract certificate provider](#contract-certificate-provider)
* [Charge station certificate provider](#charge-station-certificate-provider)
* [Tariff service](#tariff-service)
* [Root certificate provider](#root-certificate-provider)
* [Http auth service](#http-auth-service)
* [Example configuration](#example-configuration)

## General settings

| Section       | Key                         | Type   | Description                                                          |
|---------------|-----------------------------|--------|----------------------------------------------------------------------|
| api           | addr                        | string | Address that API server will listen on, e.g. localhost:9410. It serves only `/api/v0`, to callers with a token (see [API authentication](#api-authentication)) |
| api           | internal_addr               | string | Address that the internal listener will listen on. Default "localhost:9412". It serves the control plane of the platform (`/_control/v1`, only for tokens of a platform service with the permission `cpms.tenant.manage`; closed in "dev" auth mode), the lookups of the gateway (`/_internal/v1`, see [the internal API](../internalapi/README.md)), the health check (`/health`), the metrics (`/metrics`), the OpenAPI document of the API (`/api/openapi.json`) and the legacy pages that are enabled. Everything but the control plane is served without authentication, so the listener must only be reachable from inside the deployment (the platform, the gateway, health probes, the metrics scraper) |
| api           | enable_transactions_page    | bool   | Serve the legacy page `/transactions`, which lists the transactions of every org, on the internal listener. Default "false". For local development only: the page has no authentication |
| api           | enable_admin_ui             | bool   | Serve the legacy admin pages `/adminui`, which register charge stations and tokens and issue client certificates, on the internal listener. Default "false". For local development only: the pages have no authentication |
| api           | external_addr               | string | The Externally visible URL that the server is available on           |
| api.auth      | mode                        | string | How the callers of `/api/v0` are authenticated: "jwt" (default) or "dev". See [API authentication](#api-authentication) |
| api.auth      | jwks_url                    | string | URL of the key set (JWKS) with the public keys that sign the tokens of the platform. Required in "jwt" mode; an http or https URL without credentials |
| api.auth      | issuer                      | string | The only accepted issuer (`iss`) of a token. Default "platform"      |
| api.auth      | audience                    | string | The audience (`aud`) that a token must have. Default "maeve"         |
| api.auth      | min_refresh_interval        | string | Shortest time between two requests for the key set, e.g. "5m" (the default) |
| api.auth      | clock_skew                  | string | Leeway for the time claims of a token (`exp`, `iat`, `nbf`), e.g. "5s" (the default) |
| api.auth      | max_token_lifetime          | string | Longest accepted time between `iat` and `exp` of a token, e.g. "120s" (the default) |
| api.auth.dev  | org_id                      | string | "dev" mode: org (UUID) of the caller. Default: `default_org_id` of `[tenancy]` |
| api.auth.dev  | org_scope                   | string | "dev" mode: "self" or "subtree" (default)                             |
| api.auth.dev  | perms                       | array of strings | "dev" mode: permissions of the caller. Default: all permissions |
| api           | org_name                    | string | Deprecated, single-tenant mode only: the organization name in the client certificates that the admin pages issue. With `[tenancy] mode = "multi"` the first `accepted_cert_org_names` of the tenant config is used, and this value only for a charge station whose tenant has none |
| ocpp          | heartbeat_interval          | string | Frequency to request charge station heartbeat messages at, e.g. "5m". A tenant can set its own with `heartbeat_interval_s` of its config (see below) |
| ocpp          | ocpp16_enabled              | bool   | Is OCPP 1.6 support enabled, e.g. "true"?                            |
| ocpp          | ocpp201_enabled             | bool   | Is OCPP 2.0.1 support enabled, e.g. "true"?                          |
| jobs          | reservation_expiry_interval | string | How often reservations past their expiry date are expired, e.g. "1m" |
| tenancy       | default_org_id              | string | Org (UUID) that owns the data from before orgs and everything created without a tenant. Default "00000000-0000-0000-0000-000000000000" (the legacy org). With PostgreSQL storage, the rows of the legacy org are moved to this org at startup; a token of the legacy org stays there when this org already has a token with the same country code, party id and UID (a warning in the log names it) |
| tenancy       | mode                        | string | Either "single" (default) or "multi". In single mode a charge station is known by the id it connects with and the host it connects to is ignored. In multi mode the host names the tenant, a charge station is known as `{org_key}.{stationId}` and a host that no tenant has is refused. Multi mode needs PostgreSQL or in-memory storage |
| tenancy       | cache_ttl                   | string | How long a tenant that was looked up by host, org key or org id is kept in memory, e.g. "30s" (the default). It is also how long another instance of the manager takes to see that the platform suspended or resumed a tenant |
| tenancy       | bootstrap_tenants           | array of tables | Tenants that are created or updated at startup, for deployments without a platform that provisions them. Only allowed in multi mode. See [Bootstrap tenants](#bootstrap-tenants) |
| events        | enabled                     | bool   | Store domain events in the transactional outbox of the storage, in the same transaction as the change they describe. Default "true". Always off with Firestore storage, which has no outbox (a warning in the log at startup) |
| observability | log_format                  | string | Either "json" or "text"                                              |
| observability | otel_collector_addr         | string | Address of the OpenTelemetry collector, e.g. "localhost:4317"        |
| observability | tls_keylog_file             | string | File where TLS session keys will be written for use with Wireshark   |

### API authentication

"The platform" in this document is the system that the manager is deployed under: it signs the tokens
of the callers of the API and provisions the tenants through the control plane (`/_control/v1`). The manager
knows it only by its key set, its issuer name and the tokens it signs.

Every request to `/api/v0` must carry a token of the platform: `Authorization: Bearer <JWT>`. In "jwt" mode
the manager only accepts a compact JWS that is signed with EdDSA by an Ed25519 key of the key set at `jwks_url`,
with the configured issuer and audience, the claims `sub`, `jti`, `iat` and `exp` (not expired, not issued in the
future, at most `max_token_lifetime` between `iat` and `exp`), `org` (a UUID) and `org_scope` ("self" or
"subtree"); `perms` (a list of strings) and `act` are optional. Anything else is answered with `401`. The key set
is loaded at startup and refreshed in the background; a token with an unknown key id makes the manager load it
again, at most once per `min_refresh_interval`. While no key set could be loaded (the platform may start after the
manager) requests are answered with `503`.

In "dev" mode nothing is verified: every request to `/api/v0`, with any token or with none, is accepted as the
caller `user:dev` of `[api.auth.dev]`. The manager refuses to start in this mode unless the `ENVIRONMENT`
environment variable is `dev`, and logs the warning `API authentication disabled (dev mode)` at every startup.
Never use it on a deployment that others can reach. The control plane (`/_control/v1` on the internal listener)
stays closed in this mode: it only serves services of the platform, and `user:dev` is not one, so every request to
it is answered with `403`. Tenants of a deployment without a platform come from `[[tenancy.bootstrap_tenants]]`.

A caller needs the permission of the operation (`x-permission` in the API spec), otherwise the answer is `403`,
and only reaches the data of the orgs in its scope. With `[tenancy] mode = "single"` the only org is
`default_org_id`: a caller of any other org reaches nothing. With `mode = "multi"` a caller with `org_scope`
"self" reaches its own org and a caller with "subtree" the tenants at and below its org (its own org only when
it is a tenant here). A charge station, a token or an org outside the scope is answered like one that does not
exist (`404`). In "dev" mode with `mode = "multi"` set `[api.auth.dev] org_id` to the org of a tenant: the
default org is not a tenant, so the dev caller would reach nothing.

While the platform has suspended a tenant (`mode = "multi"`), the operations that create or change what the
tenant owns (the ones with the permission `cpms.station.write`, `cpms.token.write` or `cpms.tariff.write`
that do not only read) are answered with `403` and the error `tenant_suspended`. Reading and commands to
charge stations (`cpms.connector.command`) stay possible, and the charge stations of the tenant connect,
authorize and charge as before. The instance of the manager that the platform called applies a suspension or a
resumption at once, any other instance within `[tenancy] cache_ttl`.

The entitlements that the platform sends with a tenant (`mode = "multi"`) can limit its connectors with
`cpms.connectors.max`. While the `enforcement` of that limit is `hard`, a new charge station is not registered
(`POST /api/v0/cs/{csId}` is answered with `403` and an error that starts with `entitlement_limit_exceeded`)
once the connectors that the charge stations of the tenant have reported use up the limit. A limit with the
`scope` `subtree` is shared by the org in its `pool_org_id` and the tenants below that org. A `soft` limit
refuses nothing, and a changed limit applies to the next request on every instance of the manager. Only
registering a new charge station is limited: the charge stations that are registered connect, report
connectors and charge as before, and registering one of them again is always possible.

#### Tenant config

With `mode = "multi"` the platform sends a config document with each tenant. Its JSON Schema is
[`tenancy/config_schema.json`](../tenancy/config_schema.json), published as `config_schema` in the manifest
of the control plane; a document that does not fit it is refused (`400`, error `invalid_config`) and nothing
of the request is stored. The settings apply without a restart: at once on the instance of the manager that
the platform called, within `[tenancy] cache_ttl` on any other instance, and for the gateway after its own
`--registry-cache-ttl` on top of that.

| Setting                    | Applies to | Default |
|----------------------------|------------|---------|
| `default_security_profile` | A charge station that is registered through the API without `securityProfile`: `SP1`, `SP2` or `SP3` (stored as `0`, `1`, `2`). A `securityProfile` in the request always wins | `SP2` (TLS with basic auth) |
| `accepted_cert_org_names`  | The organisation names (`O`) that the gateway accepts in the client certificate of a charge station of the tenant (OCPP security profile 3, `securityProfile` `2`), instead of its `--org-name`. The first name is the organization of the client certificates that the admin pages issue | empty: the organisation is not checked |
| `heartbeat_interval_s`     | The interval in the answer to a BootNotification of a charge station of the tenant, 60 to 86400 seconds | `[ocpp] heartbeat_interval` |
| `default_tariff_ref`       | Stored for the tariffs; not applied yet | none |
| `onboarding_mode`          | Stored for the onboarding of charge stations that are not registered (`pending_approval` or `preregistered_only`); not applied yet | `pending_approval` |

A tenant that was never sent a config has the defaults, and so has an org that is not a tenant and every org
in `mode = "single"`.

What that means for client certificates: for a charge station of a tenant the `--org-name` of the gateway no
longer applies, and a tenant whose `accepted_cert_org_names` is empty or missing (also a tenant without a
config) has no check of the organisation. Until the certificate of a charge station is bound to its station
id and to the certificate authority of its tenant, such a tenant accepts, for each of its charge stations with
OCPP security profile 3, every client certificate that the gateway trusts: a tenant that uses client certificates
should list its organisation names. The gateway keeps checking against its own `--org-name` list where there
is no tenant config to follow: in `mode = "single"`, for a charge station whose org is not a tenant, and for a
tenant whose stored config cannot be read (which is logged as an error; such a tenant has the defaults for
the other settings).

```toml
[api.auth]
mode = "jwt"
jwks_url = "http://platform.internal:8080/.well-known/jwks.json"
```

### Bootstrap tenants

Each `[[tenancy.bootstrap_tenants]]` table is one tenant. The manager does not start when one of them is not valid
or clashes with another tenant.

| Key       | Type             | Description                                                                              |
|-----------|------------------|------------------------------------------------------------------------------------------|
| org_id    | string           | Id of the org (lowercase UUID)                                                           |
| slug      | string           | Name of the tenant in URLs, `^[a-z0-9-]{3,32}$`. Cannot be changed once the tenant exists |
| org_key   | string           | Prefix of the station keys of the tenant, `^[a-z0-9]{4,8}$`. Cannot be changed once the tenant exists |
| hostnames | array of strings | Hosts that the charge stations of the tenant connect to: lowercase DNS names without a port. A host belongs to one tenant |

e.g.

```toml
[tenancy]
mode = "multi"

[[tenancy.bootstrap_tenants]]
org_id = "0193b2c4-0000-7000-8000-000000000001"
slug = "acme"
org_key = "acme"
hostnames = ["acme.ocpp.example.com"]
```

## Transport settings

This section consists of a `type` parameter and a set of parameters specific to that type prefixed by the type name.

e.g.

```toml
[transport]
type = "mqtt"
mqtt.urls = ["mqtt://mqtt-server.example.com:1883"]
```

### MQTT

Configures the MQTT transport.

| Section | Key                 | Type             | Description                                            |
|---------|---------------------|------------------|--------------------------------------------------------|
| mqtt    | urls                | array of strings | List of MQTT broker URLs, e.g. [mqtt://localhost:1883] |
| mqtt    | prefix              | string           | MQTT topic prefix, e.g. "cs"                           |
| mqtt    | group               | string           | MQTT subscriber group name, e.g. "manager"             |
| mqtt    | connect_timeout     | string           | MQTT connection timeout, e.g. "10s"                    |
| mqtt    | connect_retry_delay | string           | MQTT connection retry delay, e.g. "1s"                 |
| mqtt    | keep_alive_interval | string           | MQTT keep alive interval, e.g. "10s"                   |

### NATS

Configures the NATS transport (`type = "nats"`). The manager subscribes to
`<subject_prefix>.in.<ocpp16|ocpp201>.<org_key>.<station>` in a queue group, so that each
message is handled by one manager replica, and publishes to
`<subject_prefix>.out.<ocpp16|ocpp201>.<org_key>.<station>`. A station key without an org
part (single tenancy mode) uses `_` as `<org_key>`; the station id is one subject token in
which every byte outside `[A-Za-z0-9_~:@=+-]` is written as `%XX`. The connection itself
is configured in the [`[nats]`](#nats-connection) section, which is required with this
transport. The default transport is still `mqtt`.

| Section | Key            | Type    | Description                                                                                          |
|---------|----------------|---------|------------------------------------------------------------------------------------------------------|
| nats    | subject_prefix | string  | First token of the subjects, default "ocpp"                                                          |
| nats    | queue_group    | string  | Queue group of the manager replicas, default "maeve-manager"                                         |
| nats    | workers        | integer | Messages handled at the same time per OCPP version (1 to 1024), default 16; one station stays in order |

```toml
[transport]
type = "nats"
nats.queue_group = "maeve-manager"

[nats]
urls = ["nats://nats.cpms.svc:4222"]
creds_file = "/vault/secrets/nats.creds"
tls_ca_file = "/vault/secrets/nats-ca.pem"
```

### NATS connection

The `[nats]` section configures the one connection to NATS that the manager opens when
something uses NATS (today the NATS transport). It is only read and validated then.

Credentials and keys are files: in Kubernetes they come from the secret store and are
mounted into the pod; the TOML only holds their paths. `user`, `password` and `token` are
meant for local development. Secrets are never logged.

| Section | Key            | Type             | Description                                                                                 |
|---------|----------------|------------------|---------------------------------------------------------------------------------------------|
| nats    | urls           | array of strings | NATS server URLs, required, e.g. ["nats://localhost:4222"]                                  |
| nats    | name           | string           | Client name shown in NATS monitoring, default "maeve-manager"                               |
| nats    | creds_file     | string           | File with the user JWT and nkey seed (production)                                           |
| nats    | nkey_seed_file | string           | File with an nkey seed, for servers that authenticate nkeys without JWTs                    |
| nats    | user           | string           | User name (local development)                                                               |
| nats    | password       | string           | Password (local development)                                                                |
| nats    | token          | string           | Token (local development)                                                                   |
| nats    | tls_ca_file    | string           | CA certificates that sign the certificate of the server                                     |
| nats    | tls_cert_file  | string           | Client certificate, together with `tls_key_file`                                            |
| nats    | tls_key_file   | string           | Key of the client certificate, together with `tls_cert_file`                                |
| nats    | connect_timeout | string          | Timeout of one connection attempt, default "5s"                                             |
| nats    | reconnect_wait | string           | Pause between two connection attempts, default "2s"                                         |
| nats    | max_reconnects | integer          | Connection attempts after the connection is lost or the first attempt fails, default -1 (forever) |

The manager does not fail when NATS cannot be reached at startup: the connection keeps
trying in the background, but the transport needs the server within 5 seconds to register
its subscriptions, otherwise the manager stops with an error.

The NATS server accepts messages up to its `max_payload` (1 MiB by default). An OCPP
message above it cannot be published: the manager logs `message is larger than the max
payload of the NATS server` with the sizes. Raise `max_payload` on the server if stations
send larger frames.

## Service settings

The following types of service can be configured, each service has its own section:
* [`storage`](#storage) - configures the backing store will be used
* [`contract_cert_validator`](#contract-certificate-validator) - configures how contract certificates will be validated
* [`contract_cert_provider`](#contract-certificate-provider) - configures how contract certificates are provided
* [`charge_station_cert_provider`](#charge-station-certificate-provider) - configures how charge station certificates are provided
* [`tariff_service`](#tariff-service) - configures how tariffs are calculated

Each section consists of a `type` parameter and a set of parameters specific to that type prefixed by the type name.

e.g.

```toml
[storage]
type = "firestore"
firestore.project_id = "my-google-project"
```

### Storage

There are two storage implementations:
* [`firestore`](#firestore) - Google Firestore
* [`in_memory`](#in-memory) - in-memory storage for testing

#### Firestore

| Key        | Type   | Description             |
|------------|--------|-------------------------|
| project_id | string | Google Cloud project ID |

#### In-memory

There is no additional configuration for in-memory storage.

### Contract certificate validator

There is just one contract certificate validator implementation:
* [`ocsp`](#ocsp-contract-certificate-validator) - checks the certificate chain and validates the OCSP status of each provided certificate 

#### OCSP contract certificate validator

| Key          | Type                                           | Description                                                          |
|--------------|------------------------------------------------|----------------------------------------------------------------------|
| root_certs   | [RootCertProvider](#root-certificate-provider) | Configures how to retrieve the trusted root certificates             |
| max_attempts | int                                            | Maximum number of attempts to check the OCSP status of a certificate |

### Contract certificate provider

There are two contract certificate provider implementations:
* [`opcp`](#opcp-contract-certificate-provider) - contract certificates are retrieved from a contract certificate pool using the Open Plug&Charge Protocol (OPCP)
* [`default`](#default-contract-certificate-provider) - returns an error for all requests

#### OPCP contract certificate provider

| Key        | Type                                  | Description                                                           |
|------------|---------------------------------------|-----------------------------------------------------------------------|
| url        | string                                | Base URL for OPCP service that provides the contract certificate pool |
| auth       | [HttpAuthService](#http-auth-service) | Configures how to authenticate with the OPCP service                  |

#### Default contract certificate provider

There is no additional configuration for the default contract certificate provider.

### Charge station certificate provider

There are four charge station certificate provider implementations:
* [`opcp`](#opcp-charge-station-certificate-provider) - charge station certificates are issued using the EST service from the Open Plug&Charge Protocol (OPCP)
* [`local`](#local-charge-station-certificate-provider) - charge station certificates are issued using a CA implemented by the CSMS
* [`delegating`](#delegating-charge-station-certificate-provider) - supports different charge station certificate providers for issuing V2G and CSO certificates
* [`default`](#default-charge-station-certificate-provider) - returns an error for all requests

#### OPCP charge station certificate provider

| Key        | Type                                  | Description                                             |
|------------|---------------------------------------|---------------------------------------------------------|
| url        | string                                | Base URL for OPCP service that provides the EST service |
| auth       | [HttpAuthService](#http-auth-service) | Configures how to authenticate with the OPCP service    |

#### Local charge station certificate provider

| Key  | Type                         | Description                                                                         |
|------|------------------------------|-------------------------------------------------------------------------------------|
| cert | [LocalSource](#local-source) | The source that provides the signing certificate, must be a PEM encoded certificate |
| key  | [LocalSource](#local-source) | The source that provides the signing key, must be a PEM encoded private key         |

#### Delegating charge station certificate provider

| Key | Type                                                                     | Description                                                  |
|-----|--------------------------------------------------------------------------|--------------------------------------------------------------|
| v2g | [ChargeStationCertificateProvider](#charge-station-certificate-provider) | The charge station certificate provider for V2G certificates |
| cso | [ChargeStationCertificateProvider](#charge-station-certificate-provider) | The charge station certificate provider for CSO certificates |

#### Default charge station certificate provider

There is no additional configuration for the default charge station certificate provider.

### Tariff service

There is a single tariff service implementation:
* [`kwh`](#kwh-tariff-service) - calculates the tariff based on the energy consumed

#### kWh tariff service

There is no additional configuration for the kWh tariff service.

### Root certificate provider

There are several implementations of RootCertProvider:
* [`opcp`](#opcp-root-certificate-provider) - root certificates are retrieved from a root certificate pool using the Open Plug&Charge Protocol (OPCP)
* [`file`](#file-root-certificate-provider) - root certificates are retrieved from a file

#### OPCP root certificate provider

| Key  | Type                                  | Description                                                       |
|------|---------------------------------------|-------------------------------------------------------------------|
| url  | string                                | Base URL for OPCP service that provides the root certificate pool |
| ttl  | string                                | Time before cached values are discarded, e.g. "1h"                |
| auth | [HttpAuthService](#http-auth-service) | Configures how to authenticate with the OPCP service              |

#### File root certificate provider

| Key   | Type             | Description                                |
|-------|------------------|--------------------------------------------|
| files | array of strings | List of files containing root certificates |

### Http auth service

There are several implementation of HttpAuthService:
* [`env_token`](#environment-token-auth-service) - token is read from an environment variable
* [`fixed_token`](#fixed-token-auth-service) - token is read from the configuration
* [`oauth2_token`](#oauth2-token-auth-service) - token is retrieved using OAuth2 client credentials grant
* [`hubject_test_token`](#hubject-test-token-auth-service) - token is scraped from the Hubject test environment authorization page

#### Environment token auth service

| Key      | Type   | Description                                          |
|----------|--------|------------------------------------------------------|
| variable | string | Name of the environment variable                     |

#### Fixed token auth service

| Key   | Type   | Description     |
|-------|--------|-----------------|
| token | string | The token value |

#### OAuth2 token auth service

| Key                   | Type   | Description                                                                                    |
|-----------------------|--------|------------------------------------------------------------------------------------------------|
| url                   | string | The URL of the OAuth2 Authorization Server token endpoint                                      |
| client_id             | string | The client id to use in the client credentials grant                                           |
| client_secret         | string | The client secret to use in the client credentials grant                                       |
| client_secret_env_var | string | The environment variable to read the client secret from to use in the client credentials grant |

#### Hubject test token auth service

| Key | Type   | Description                                            |
|-----|--------|--------------------------------------------------------|
| url | string | URL of the Hubject test environment authorization page |
| ttl | string | The duration for which the token is valid, e.g. "1h"   |

### Local source

There are two different local source implementations:
* [`file`](#file-local-source) - data is read from a file
* [`google_cloud_secret`](#google-cloud-secret-local-source) - data is read from a google cloud secret

#### File local source

The value is a file to be read from the operating system.

#### Google cloud secret local source

The value is the name of a secret to be read from google cloud secrets. The name must be of the form:

```
projects/<project-number>/secrets/<secret-name>/[latest|<version>]
```

## Example configuration

```toml
[api]
addr = ":9410"

[mqtt]
urls = ["mqtt://mqtt:1883"]

[observability]
otel_collector_addr = "otel-collector:4317"

[storage]
type = "firestore"
firestore.project_id = "*detect-project-id*"

[contract_cert_validator]
type = "ocsp"

[contract_cert_validator.ocsp.root_certs]
type = "opcp"
opcp.url = "https://open.plugncharge-test.hubject.com/mo/cacerts/ISO15118-2"
opcp.auth.type = "env_token"
opcp.auth.env_token.variable = "HUBJECT_TOKEN"

[contract_cert_provider]
type = "opcp"
opcp.url = "https://open.plugncharge-test.hubject.com"
opcp.auth.type = "env_token"
opcp.auth.env_token.variable = "HUBJECT_TOKEN"

[charge_station_cert_provider]
type = "opcp"
opcp.url = "https://open.plugncharge-test.hubject.com"
opcp.auth.type = "env_token"
opcp.auth.env_token.variable = "HUBJECT_TOKEN"
```
