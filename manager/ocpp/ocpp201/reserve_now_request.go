// SPDX-License-Identifier: Apache-2.0

package ocpp201

// ConnectorEnumType specifies the connector type.
type ConnectorEnumType string

const ConnectorEnumTypeCCCS1 ConnectorEnumType = "cCCS1"
const ConnectorEnumTypeCCCS2 ConnectorEnumType = "cCCS2"
const ConnectorEnumTypeCG105 ConnectorEnumType = "cG105"
const ConnectorEnumTypeCTesla ConnectorEnumType = "cTesla"
const ConnectorEnumTypeCType1 ConnectorEnumType = "cType1"
const ConnectorEnumTypeCType2 ConnectorEnumType = "cType2"
const ConnectorEnumTypeS3091P16A ConnectorEnumType = "s309-1P-16A"
const ConnectorEnumTypeS3091P32A ConnectorEnumType = "s309-1P-32A"
const ConnectorEnumTypeS3093P16A ConnectorEnumType = "s309-3P-16A"
const ConnectorEnumTypeS3093P32A ConnectorEnumType = "s309-3P-32A"
const ConnectorEnumTypeSBS1361 ConnectorEnumType = "sBS1361"
const ConnectorEnumTypeSCEE77 ConnectorEnumType = "sCEE-7-7"
const ConnectorEnumTypeSType2 ConnectorEnumType = "sType2"
const ConnectorEnumTypeSType3 ConnectorEnumType = "sType3"
const ConnectorEnumTypeOther1PhMax16A ConnectorEnumType = "Other1PhMax16A"
const ConnectorEnumTypeOther1PhOver16A ConnectorEnumType = "Other1PhOver16A"
const ConnectorEnumTypeOther3Ph ConnectorEnumType = "Other3Ph"
const ConnectorEnumTypePan ConnectorEnumType = "Pan"
const ConnectorEnumTypeWInductive ConnectorEnumType = "wInductive"
const ConnectorEnumTypeWResonant ConnectorEnumType = "wResonant"
const ConnectorEnumTypeUndetermined ConnectorEnumType = "Undetermined"
const ConnectorEnumTypeUnknown ConnectorEnumType = "Unknown"

// ReserveNowRequestJson requests a charging station to reserve an EVSE for an id token.
type ReserveNowRequestJson struct {
	CustomData     *CustomDataType    `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`
	Id             int                `json:"id" yaml:"id" mapstructure:"id"`
	ExpiryDateTime string             `json:"expiryDateTime" yaml:"expiryDateTime" mapstructure:"expiryDateTime"`
	ConnectorType  *ConnectorEnumType `json:"connectorType,omitempty" yaml:"connectorType,omitempty" mapstructure:"connectorType,omitempty"`
	IdToken        IdTokenType        `json:"idToken" yaml:"idToken" mapstructure:"idToken"`
	EvseId         *int               `json:"evseId,omitempty" yaml:"evseId,omitempty" mapstructure:"evseId,omitempty"`
	GroupIdToken   *IdTokenType       `json:"groupIdToken,omitempty" yaml:"groupIdToken,omitempty" mapstructure:"groupIdToken,omitempty"`
}

func (*ReserveNowRequestJson) IsRequest() {}
