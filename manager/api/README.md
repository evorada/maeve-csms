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
3. Run `make api-generate` in `manager/` and commit the changed source files together with
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
