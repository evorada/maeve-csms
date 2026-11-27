// SPDX-License-Identifier: Apache-2.0

package nats_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/schemas"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"github.com/thoughtworks/maeve-csms/manager/transport"
	natstransport "github.com/thoughtworks/maeve-csms/manager/transport/nats"
	"github.com/thoughtworks/maeve-csms/manager/transport/nats/natstest"
	"k8s.io/utils/clock"
)

// The OCPP 1.6 router answers a charge station over NATS, with the subjects that the
// gateway uses. The test runs on the embedded server, with every `go test`.
func TestRouterAnswersAHeartbeatOverNats(t *testing.T) {
	_, url := natstest.RunServer(t, false)
	managerConn := connect(t, url)
	gatewayConn := connect(t, url)

	engine := inmemory.NewStore(clock.RealClock{})
	router := ocpp16.NewRouter(natstransport.NewEmitter(managerConn),
		clock.RealClock{},
		engine,
		events.NopEmitter{},
		&tenancy.Resolver{Stations: engine},
		nil,
		nil,
		nil,
		nil,
		time.Minute,
		schemas.OcppSchemas, nil)
	listen(t, managerConn, transport.OcppVersion16, nil, router)

	for _, stationKey := range []string{"k7f2.CP001", "CP001", "k7f2.CP.001"} {
		t.Run(stationKey, func(t *testing.T) {
			outSubject, err := natstransport.OutSubject("ocpp", transport.OcppVersion16, stationKey)
			require.NoError(t, err)
			inSubject, err := natstransport.InSubject("ocpp", transport.OcppVersion16, stationKey)
			require.NoError(t, err)

			responses, err := gatewayConn.SubscribeSync(outSubject)
			require.NoError(t, err)
			defer func() { _ = responses.Unsubscribe() }()
			require.NoError(t, gatewayConn.Flush())

			publish(t, gatewayConn, inSubject, &transport.Message{
				MessageType:    transport.MessageTypeCall,
				Action:         "Heartbeat",
				MessageId:      "heartbeat-1",
				RequestPayload: json.RawMessage(`{}`),
			})

			got, err := responses.NextMsg(waitTimeout)
			require.NoError(t, err)
			var resp transport.Message
			require.NoError(t, json.Unmarshal(got.Data, &resp))
			assert.Equal(t, transport.MessageTypeCallResult, resp.MessageType)
			assert.Equal(t, "Heartbeat", resp.Action)
			assert.Equal(t, "heartbeat-1", resp.MessageId)
			var payload struct {
				CurrentTime string `json:"currentTime"`
			}
			require.NoError(t, json.Unmarshal(resp.ResponsePayload, &payload))
			_, err = time.Parse(time.RFC3339, payload.CurrentTime)
			assert.NoError(t, err)
		})
	}
}
