// SPDX-License-Identifier: Apache-2.0

package ocpp16

type ExtendedTriggerMessageJson struct {
	// ConnectorId corresponds to the JSON schema field "connectorId".
	ConnectorId *int `json:"connectorId,omitempty" yaml:"connectorId,omitempty" mapstructure:"connectorId,omitempty"`

	// RequestedMessage corresponds to the JSON schema field "requestedMessage".
	RequestedMessage ExtendedTriggerMessageJsonRequestedMessage `json:"requestedMessage" yaml:"requestedMessage" mapstructure:"requestedMessage"`
}

func (*ExtendedTriggerMessageJson) IsRequest() {}

type ExtendedTriggerMessageJsonRequestedMessage string

const ExtendedTriggerMessageJsonRequestedMessageBootNotification ExtendedTriggerMessageJsonRequestedMessage = "BootNotification"
const ExtendedTriggerMessageJsonRequestedMessageLogStatusNotification ExtendedTriggerMessageJsonRequestedMessage = "LogStatusNotification"
const ExtendedTriggerMessageJsonRequestedMessageFirmwareStatusNotification ExtendedTriggerMessageJsonRequestedMessage = "FirmwareStatusNotification"
const ExtendedTriggerMessageJsonRequestedMessageHeartbeat ExtendedTriggerMessageJsonRequestedMessage = "Heartbeat"
const ExtendedTriggerMessageJsonRequestedMessageMeterValues ExtendedTriggerMessageJsonRequestedMessage = "MeterValues"
const ExtendedTriggerMessageJsonRequestedMessageSignChargePointCertificate ExtendedTriggerMessageJsonRequestedMessage = "SignChargePointCertificate"
const ExtendedTriggerMessageJsonRequestedMessageStatusNotification ExtendedTriggerMessageJsonRequestedMessage = "StatusNotification"
