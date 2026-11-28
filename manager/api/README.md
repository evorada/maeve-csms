# Manager API

The API is described by an OpenAPI spec that is split over several files:

| File | Content |
|---|---|
| `api-spec.yaml` | Root document: `info`, `servers`, the security scheme and one `$ref` per path and per schema. It holds no inline paths or schemas. |
| `paths/<module>.yaml` | The operations, one file per module. |
| `schemas/schemas.yaml` | All schemas. |
| `api-spec.bundled.yaml` | Generated: the root with every `$ref` resolved. Do not edit. |
| `api.gen.go` | Generated: server interface, models and the embedded spec. Do not edit. |

## Changing the API

1. Edit or add the operation in `paths/<module>.yaml` (a new module gets a new file). Put schemas in
   `schemas/schemas.yaml` and refer to them from path files as
   `'../schemas/schemas.yaml#/components/schemas/<Name>'`.
2. In `api-spec.yaml` add one `$ref` line per new path (under `paths`) and per new schema (under
   `components.schemas`):

   ```yaml
   paths:
     /cs/{csId}/status:
       $ref: 'paths/status.yaml#/paths/~1cs~1{csId}~1status'
   components:
     schemas:
       ChargeStationStatus:
         $ref: 'schemas/schemas.yaml#/components/schemas/ChargeStationStatus'
   ```

   In the fragment of a path `$ref` every `/` of the path is written as `~1` (and `~` as `~0`).
3. Give every operation the permission that its caller needs, as `x-permission` next to `operationId`
   (one of `tenancy.AllPermissions`), and a `403` response. The manager does not start, and
   `TestEveryOperationHasPermission` fails, when an operation has none; the test also lists the permission of
   every operation, so add the new one there. An operation on a charge station has the station key as the path
   parameter `csId` (`/cs/{csId}/...`): `api.AuthzMiddleware` answers `404` for a charge station outside the
   scope of the caller before the operation runs. An operation that reads the sessions of a charge station (its
   transactions and meter values) also narrows them to the orgs of the caller (`Server.sessionOrgIds`): a
   session keeps the org that owned the charge station when it started, so a charge station that was
   transferred has sessions of an org that its new owner may not reach. Any other operation scopes what it
   reads and writes itself, with the `tenancy.Scoper` of the server. A request with a token whose path or query
   parameters cannot be read (a number that is not one, a required query parameter that is missing) is answered
   `400` by the generated code before the permission and the scope are checked; the answer says nothing about
   the charge station.

   An operation that creates or changes something that a tenant owns has one of `tenancy.WritePermissions` and
   is answered with `403` and the error `tenant_suspended` while that tenant is suspended: on a charge station
   by `api.AuthzMiddleware`, anywhere else by `Server.targetOrg`, which gives the operation the org that it
   writes to. `refusedForSuspendedTenant` in `authz_test.go` lists these operations, so add the new one there
   and name `tenant_suspended` in the description of its `403`. Reading and commands to a charge station
   (`cpms.connector.command`) never depend on the status of a tenant.

   An operation that creates connectors for a tenant (today: registering a charge station that is new, and
   approving a pending charge station) calls `entitlements.Enforcer.CheckConnectors` before it writes. Registering
   answers a `*entitlements.LimitExceededError` with `ErrLimitExceeded` (`403`, the error starts with
   `entitlement_limit_exceeded`), approving with `409` and the error `entitlement_exceeded`; the description of
   the answer names it. Changing what exists is never limited.
4. Run `make api-generate` in `manager/` and commit the changed source files together with
   `api-spec.bundled.yaml` and `api.gen.go`. Never edit those two files by hand.

```shell
$ cd manager
$ make api-generate
```

`make api-generate` bundles the spec with the [Redocly CLI](https://redocly.com/docs/cli/) version locked in
`package-lock.json` (needs Node.js; installed with `npm ci` on the first run) and then generates `api.gen.go` with
oapi-codegen v2.5.1 and the options in `cfg.yaml`. `go generate ./api/` runs the same command.

CI runs `make api-generate` and fails when `api-spec.bundled.yaml` or `api.gen.go` is out of date.
`spec_split_test.go` fails when the root spec defines a path or schema inline, or when a path or schema of the
split files is not referenced from the root.

## API docs

To generate the OpenAPI markdown docs:

```shell
$ npm install -g widdershins
$ widdershins api-spec.bundled.yaml -o API.md -c true
```
