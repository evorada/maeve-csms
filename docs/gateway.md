# Gateway

The gateway component is stateful and is responsible for accepting connections from charge stations and proxying
OCPP messages to/from the stateless backend via a message broker: NATS (the default) or an MQTT broker.

The core logic of the gateway is implemented by the [Pipe](../gateway/pipe/pipe.go) type. The pipe has four main 
go channels:
1. `ChargeStationRx` - receives messages from the charge station
2. `ChargeStationTx` - transmits messages to the charge station
3. `CSMSRx` - receives messages from the CSMS manager
4. `CSMSTx` - transmits messages to the CSMS manager

![Diagram showing the gateway reading from a websocket connected to the charge station sending it to the MQTT broker in topic via the Pipe whilst subscribing to the MQTT broker out topic and writing it to the charge station websocket via the pipe](assets/gateway.png)

The Pipe implements a small state machine. The state machine has 3 states:
1. `Waiting` - no OCPP call is in progress, a call will be accepted from the charge station or the CSMS manager.
Calls from the charge station are prioritized (as the charge station will be expecting a response).
2. `ChargeStationCall` - an OCPP call initiated by the charge station is in progress, any calls from the CSMS
manager will be buffered waiting for the call response for the ongoing call.
3. `CSMSCall` - on OCPP call initiated by the CSMS manager is in progress, will wait for a response from the
charge station.

Both the call states have an associated time-out that returns them to the `Waiting` state. However, any late
arriving response from the charge station will be forwarded to the CSMS manager and any late arriving response
from the CSMS manager will be forwarded to the charge station as long as another call has not occurred.

The `Pipe` works in terms of the [GatewayMessage](../gateway/pipe/message.go) type which is an amalgamation of the
OCPP call, call result and call error messages. The Pipe caches CSMS originated calls and merges these with the 
call result or call error received from the charge station before sending them on to the CSMS manager. This is 
needed as the CSMS manager is stateless and has no knowledge of the context of a call result. 

The [WebsocketHandler](../gateway/server/ws.go) establishes the websocket connection and implements the interfaces 
between the various `Pipe` channels and the websocket - converting between OCPP messages and gateway messages - 
on one side and the [transport](../gateway/transport/transport.go) on the other: it opens a session of the
transport for each charge station, which receives the outgoing (to the charge station) messages and publishes the
incoming (from the charge station) messages.

The charge station is named by its station key: `<org_key>.<cs-id>` in multi tenancy mode, the plain `<cs-id>` in
single tenancy mode. The manager must use the same transport (its `[transport] type`, see the
[manager configuration](../manager/config/README.md#transport-settings)).

## NATS transport (default)

`--transport nats`. All charge stations share one connection to NATS. There are individual subjects for each
charge station:
* `<prefix>.in.<ocpp-version>.<org>.<station>`
* `<prefix>.out.<ocpp-version>.<org>.<station>`

Where `<prefix>` is `--nats-subject-prefix` (defaults to `ocpp`), `<ocpp-version>` is `ocpp16` or `ocpp201`,
`<org>` is the org key of the station key (`_` for a key without one) and `<station>` is the station id, with every
byte outside `[A-Za-z0-9_~:@=+-]` written as `%XX`. The trace context travels in the W3C `traceparent` header.
The gateway waits until NATS has registered the subscription of a charge station before it reads from its
websocket; when that does not happen within 5 seconds, the websocket is closed.

| Flag                    | Default                 | Description                                             |
|-------------------------|-------------------------|---------------------------------------------------------|
| `--nats-url`            | `nats://127.0.0.1:4222` | Comma-separated list of NATS server URLs                |
| `--nats-creds`          |                         | File with the user JWT and nkey seed                    |
| `--nats-tls-ca`         |                         | File with the CA certificates of the NATS server        |
| `--nats-name`           | `maeve-gateway`         | Client name shown in NATS monitoring                    |
| `--nats-subject-prefix` | `ocpp`                  | First token of the subjects, as the manager's           |

## MQTT transport

`--transport mqtt`. Each charge station has a connection of its own to the MQTT broker, with the station key as
client id. There are individual MQTT topics for each charge station:
* `<prefix>/in/<ocpp-version>/<station key>`
* `<prefix>/out/<ocpp-version>/<station key>`

Where `<prefix>` is `--mqtt-prefix` (defaults to `cs`) and `<ocpp-version>` is the websocket subprotocol:
`ocpp1.6` or `ocpp2.0.1`. The broker is `--mqtt-addr` (defaults to `mqtt://127.0.0.1:1883`).

To run the docker-compose setup with MQTT: add a broker service (e.g. `eclipse-mosquitto:2` with the configuration
in [config/mosquitto](../config/mosquitto)), start the gateway with `--transport mqtt --mqtt-addr mqtt://mqtt:1883`
instead of `--nats-url`, and set `[transport] type = "mqtt"` and `mqtt.urls = ["mqtt://mqtt:1883"]` in the
manager configuration.

## Connection events, keepalive and shutdown

For every station websocket the gateway publishes connection events, so that the manager knows which stations are
connected:
* NATS: `<prefix>.conn.<org>.<station>` (tokens as above)
* MQTT: `<prefix>/conn/<station key>`, on the connection of the station

Each event is a JSON object: `type` (`connected`, `alive` or `disconnected`), `station_key`, `ocpp_version` (the
websocket subprotocol), `connection_id` (random 128-bit hex, one per websocket), `gateway_id`, `at` (gateway clock,
UTC), `remote_addr` (the first `X-Forwarded-For` entry with `--trust-proxy`), and on `disconnected` the websocket
`close_code` (-1 when the socket ended without one) and the `reason`: `closed`, `ping_timeout` or `gateway_shutdown`.
The events are best effort: when one cannot be published the gateway logs it, and the websocket and its OCPP messages
carry on.

The gateway pings every station every `--ws-ping-interval` and closes the websocket (1008) when the pong does not
arrive within `--ws-pong-timeout`. While the gateway itself has not read the station's messages yet (the CSMS has not
answered an earlier call), it waits for the pong again instead. An `alive` event is published every
`--conn-alive-interval` while the station answers.

On SIGINT or SIGTERM the gateway stops accepting connections, closes every station websocket with 1001 ("gateway
shutdown"), waits up to `--shutdown-timeout` for the `disconnected` events, then closes the transport. On Kubernetes
the pod's `terminationGracePeriodSeconds` must be longer than `--shutdown-timeout` (e.g. 30 s against 10 s).

| Flag                    | Default   | Description                                                               |
|-------------------------|-----------|---------------------------------------------------------------------------|
| `--gateway-id`          | host name | Id of the gateway in the events (the pod name on Kubernetes)              |
| `--ws-ping-interval`    | `30s`     | How often each station is pinged, `0` sends no pings                      |
| `--ws-pong-timeout`     | `10s`     | How long the gateway waits for a pong before it closes the websocket      |
| `--conn-alive-interval` | `2m`      | How often an `alive` event is published per websocket, `0` publishes none |
| `--shutdown-timeout`    | `10s`     | How long a shutdown waits for the websockets to close                     |

The authentication details for the charge station are read via the [manager](manager.md) API.
