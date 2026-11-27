# Extending the Manager

## Adding a new call handler

1. Compile the JSON request and response schemas using [json-to-go.sh](../scripts/json-to-go.sh)
2. Remove all the custom serialization from the generated classes in the appropriate [ocpp subdirectory](../manager/ocpp)
3. Implement the ocpp.Request or ocpp.Response stub method on the Request or Response type
4. Implement the ocpp.CallHandler interface in the appropriate [handlers subdirectory](../manager/handlers)
5. Add the new handler to the appropriate router (either [ocpp16](../manager/handlers/ocpp16/routing.go) or [ocpp201](../manager/handlers/ocpp201/routing.go))

The process for DataTransfer messages is identical. The only difference is the router configuration where there is a
generic data-transfer handler that uses the data transfer vendor and message ids to route the message.

## Adding a new call response handler

As above, but implement the ocpp.CallResultHandler interface.

## Adding a new CSMS initiated message

Compile up the schemas (as above) and add the new handler to the appropriate call maker (either [ocpp16](../manager/handlers/ocpp16/routing.go) or [ocpp201](../manager/handlers/ocpp201/routing.go))

## Adding a new API method

The OpenAPI spec is split over several files (see the [API README](../manager/api/README.md)). The root
[api-spec.yaml](../manager/api/api-spec.yaml) only holds `$ref`s.

1. Edit or add the operation in `manager/api/paths/<module>.yaml` (a new module gets a new file). Put schemas in
   [schemas.yaml](../manager/api/schemas/schemas.yaml) and refer to them from path files as
   `'../schemas/schemas.yaml#/components/schemas/<Name>'`.
2. In [api-spec.yaml](../manager/api/api-spec.yaml) add one `$ref` line per new path (under `paths`) and per new
   schema (under `components.schemas`).
3. Run `make api-generate` in `manager/` (bundles the spec, then generates the types) and commit the changed source
   files together with `api-spec.bundled.yaml` and `api.gen.go`. Never edit those two files by hand.
4. Update the [server](../manager/api/server.go) so it implements the [api.ServerInterface](../manager/api/api.gen.go)