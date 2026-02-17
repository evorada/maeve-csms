// SPDX-License-Identifier: Apache-2.0

package ocpp201

// PublishFirmwareStatusNotificationResponseJson is the response sent by the CSMS
// to a PublishFirmwareStatusNotification message. It has no fields other than
// optional custom data, as it merely acknowledges receipt.
type PublishFirmwareStatusNotificationResponseJson struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`
}

func (*PublishFirmwareStatusNotificationResponseJson) IsResponse() {}
