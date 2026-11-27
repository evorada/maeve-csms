# Internal API

The API that the gateway uses to look up charge stations and certificates. It is served at `/_internal/v1` on the
internal listener of the manager (`[api] internal_addr`, default `localhost:9412`) and is not part of the public
OpenAPI specification.

**The lookups are not authenticated.** Whoever can reach the listener can read the auth details of a charge station
(security profile and the SHA-256 hash of its password) for a host and station id that they name, and can find out
which hosts belong to a tenant. Bind it to an address that only the gateway, the health probes and the metrics
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
| securityProfile        | `0` unsecured transport with basic auth, `1` TLS with basic auth, `2` TLS with client certificate |
| base64SHA256Password   | The base64 encoded SHA-256 hash of the password of the station. Left out when the station has none |
| invalidUsernameAllowed | Whether a basic auth username that is not the station id is accepted |
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

| Status | Body | When |
|--------|------|------|
| 200    | see above | The station is registered |
| 400    | `{"status": "Bad Request", "error": "stationId: station id contains '#'"}` | A parameter is missing, empty or given more than once, or the station id is not valid |
| 404    | `{"status": "Not Found", "error": "unknown_host"}` | Multi-tenant mode: no tenant has the host |
| 404    | `{"status": "Not Found", "error": "unknown_station"}` | The station is not registered (for the tenant of the host) |
| 500    | `{"status": "Internal Server Error"}` | The lookup failed; the manager logs the reason |

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
