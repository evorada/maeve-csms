# Internal API

The API that the gateway uses to look up charge stations and certificates, and to record the connections of charge
stations that connect without being registered. It is served at `/_internal/v1` on the
internal listener of the manager (`[api] internal_addr`, default `localhost:9412`) and is not part of the public
OpenAPI specification.

**The lookups are not authenticated.** Whoever can reach the listener can read the auth details of a charge station
(security profile and the SHA-256 hash of its password) for a host and station id that they name, can find out
which hosts belong to a tenant, and can create pending charge stations (up to the limit of each tenant). Bind it to an address that only the gateway, the health probes and the metrics
scraper can reach.

The same listener serves the health check (`/health`), the metrics (`/metrics`) and the OpenAPI document of the
public API (`/api/openapi.json`), and the legacy pages `/transactions` and `/adminui` when the
[configuration](../config/README.md) enables them, all without authentication. The control plane of the platform
(`/_control/v1`) is on this listener too; it is the only part that needs a token.

Every response is JSON and carries `Cache-Control: no-store`. A response that is not successful has this body, where
`error` may be left out:

```json
{"status": "Not Found", "error": "unknown_station"}
```

## Look up the auth details of a charge station

```
GET /_internal/v1/station-auth?host={host}&stationId={stationId}
```

| Parameter | Description |
|-----------|-------------|
| host      | The `Host` header of the request of the charge station, as the gateway received it. A port, upper case and a trailing dot are accepted (`Acme.ocpp.example.com:443`) |
| stationId | The station id of the URL that the charge station connects to (`/ws/{stationId}`): 1 to 48 bytes, none of `/ + # * >`, no `.` at the start or the end, no whitespace or characters that are not printable |

Both parameters are required and must be given once; their values are query-escaped.

In single-tenant mode (`[tenancy] mode = "single"`, the default) the host is ignored and the station is looked up by
its station id. In multi-tenant mode the host names the tenant and the station is looked up by
`{org_key}.{stationId}`, so that a station of another tenant is never returned.

```
GET /_internal/v1/station-auth?host=acme.ocpp.example.com%3A443&stationId=CP001
```

```json
{
  "orgId": "0193b2c4-0000-7000-8000-000000000001",
  "orgKey": "acme",
  "stationKey": "acme.CP001",
  "tenantStatus": "active",
  "securityProfile": 1,
  "base64SHA256Password": "XohImNooBHFR0OVvjcYpJ3NgPQ1qq73WKhHvch0VQtg=",
  "invalidUsernameAllowed": false,
  "acceptedCertOrgNames": ["Acme Charging"]
}
```

| Field                  | Description |
|------------------------|-------------|
| orgId                  | The org that owns the station: the org of the station when it has one, otherwise the org of the tenant of the host (single-tenant mode: the default org) |
| orgKey                 | The org key of the tenant of the host. Left out in single-tenant mode |
| stationKey             | The name of the station inside the manager: use it for the message topics, the client id on the broker and every call to the manager. `{orgKey}.{stationId}`; the station id in single-tenant mode |
| tenantStatus           | `active`, `suspended` or `cancelled`: the status of the tenant of the host. Informational, never a reason to refuse a station. Left out in single-tenant mode |
| securityProfile        | `0` unsecured transport with basic auth, `1` TLS with basic auth, `2` TLS with client certificate. Left out for a station that is not known (see the onboarding below) |
| base64SHA256Password   | The base64 encoded SHA-256 hash of the password of the station. Left out when the station has none |
| pendingBase64SHA256Password | The hash of the new password of the station while the station is being told to use it: the station may connect with either password until then. Left out when there is none |
| invalidUsernameAllowed | Whether a basic auth username that is not the station id is accepted |
| onboarding             | Set when the station may connect for its onboarding (see below): `openSp1` (connect without TLS and without a password) and `base64SHA256Password` (the hash of the onboarding password of the tenant; left out when it has none). Left out for the other stations |
| acceptedCertOrgNames   | The organisation names (`O`) that are accepted in the client certificate of the station (security profile `2`): the `accepted_cert_org_names` of the config of the tenant that owns the station. An empty list (`[]`, also for a tenant without a config) means that the organisation is not checked. Left out when there is no list of a tenant, and the gateway then checks against its own list (`--org-name`): in single-tenant mode, for a station whose org is not a tenant, and when the stored config of the tenant cannot be read |

The same station in single-tenant mode:

```json
{
  "orgId": "00000000-0000-0000-0000-000000000000",
  "stationKey": "CP001",
  "securityProfile": 1,
  "base64SHA256Password": "XohImNooBHFR0OVvjcYpJ3NgPQ1qq73WKhHvch0VQtg=",
  "invalidUsernameAllowed": false
}
```

### Stations that are not registered

A tenant onboards the charge stations that connect to it without being registered when its config has the
`onboarding_mode` `pending_approval` (the default; see the tenant config in [the configuration](../config/README.md)).
Such a station is answered with `onboarding` and without `securityProfile`; a station that waits for approval
(`pending`) with `onboarding` and the security profile that it was created with. The gateway admits such a station
with the onboarding rules instead of its security profile:

1. without TLS and `openSp1`: without a password (credential `open`);
2. otherwise with basic auth (any username) and the onboarding password, or the `pendingBase64SHA256Password` of a
   pending station (credentials `onboarding_password`, `pending_password`); a client certificate alone is refused.

It then records the connection (below) before it accepts the websocket. In single-tenant mode, for a tenant in the
mode `preregistered_only` and for a tenant whose stored config cannot be read, a station that is not registered is
`unknown_station`, and so is a pending station.

```json
{
  "orgId": "0193b2c4-0000-7000-8000-000000000001",
  "orgKey": "acme",
  "stationKey": "acme.CP002",
  "tenantStatus": "active",
  "invalidUsernameAllowed": false,
  "onboarding": {"openSp1": true, "base64SHA256Password": "XohImNooBHFR0OVvjcYpJ3NgPQ1qq73WKhHvch0VQtg="},
  "acceptedCertOrgNames": []
}
```

| Status | Body | When |
|--------|------|------|
| 200    | see above | The station is registered, pending, or may connect for its onboarding |
| 400    | `{"status": "Bad Request", "error": "stationId: station id contains '#'"}` | A parameter is missing, empty or given more than once, or the station id is not valid |
| 404    | `{"status": "Not Found", "error": "unknown_host"}` | Multi-tenant mode: no tenant has the host |
| 404    | `{"status": "Not Found", "error": "unknown_station"}` | The station is not registered (for the tenant of the host) and the tenant does not onboard such stations, or it is decommissioned |
| 500    | `{"status": "Internal Server Error"}` | The lookup failed; the manager logs the reason |

## Record an onboarding connection

```
PUT /_internal/v1/station-onboarding?host={host}&stationId={stationId}
```

```json
{"transport": "ws", "credential": "open"}
```

The parameters are those of the station-auth lookup. `transport` is `ws` (no TLS) or `wss` (TLS); `credential` is
what the gateway admitted the station with: `open` (only with `ws`), `onboarding_password` or `pending_password`.
A station that is not known becomes a pending station of the tenant of the host, with the security profile of the
transport (`ws`: `0`, `wss`: `1`) and no password; a pending station gets the time, transport and credential of the
connection as its last ones. The onboarding rules of the tenant are checked again: a gateway that keeps an older
answer of the lookup cannot admit a station that the tenant no longer admits.

```json
{"created": true}
```

| Status | Body | When |
|--------|------|------|
| 200    | see above | The connection is recorded; `created` is true when the station was created |
| 400    | `{"status": "Bad Request", "error": "..."}` | A parameter or the body is not valid |
| 403    | `{"status": "Forbidden", "error": "credential_not_allowed"}` | The tenant does not accept the credential (any more) |
| 404    | `{"status": "Not Found", "error": "unknown_host"}` | Multi-tenant mode: no tenant has the host |
| 404    | `{"status": "Not Found", "error": "onboarding_disabled"}` | The tenant does not onboard stations that are not registered (also in single-tenant mode) |
| 409    | `{"status": "Conflict", "error": "station_not_pending"}` | The station is registered, approved or decommissioned |
| 429    | `{"status": "Too Many Requests", "error": "pending_limit_reached"}` | The station is not known and the tenant has `onboarding_max_pending` pending stations; nothing is written |
| 500    | `{"status": "Internal Server Error"}` | The connection could not be recorded |

## Look up a certificate

```
GET /_internal/v1/certificates/{certificateHash}
```

`certificateHash` is the URL-safe base64 encoded (without padding) SHA-256 hash of the DER bytes of the certificate,
at most 64 characters. The response is the same as that of `GET /api/v0/certificate/{certificateHash}`:

```json
{"certificate": "-----BEGIN CERTIFICATE-----\nMIIB...\n-----END CERTIFICATE-----\n"}
```

| Status | Body | When |
|--------|------|------|
| 200    | see above | The certificate is known |
| 400    | `{"status": "Bad Request", "error": "..."}` | The hash is longer than 64 characters |
| 404    | `{"status": "Not Found"}` | No certificate has the hash |
| 500    | `{"status": "Internal Server Error"}` | The lookup failed |
