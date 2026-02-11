package ocpp16

type ClearChargingProfileJsonChargingProfilePurpose string

const ClearChargingProfileJsonChargingProfilePurposeChargePointMaxProfile ClearChargingProfileJsonChargingProfilePurpose = "ChargePointMaxProfile"
const ClearChargingProfileJsonChargingProfilePurposeTxDefaultProfile ClearChargingProfileJsonChargingProfilePurpose = "TxDefaultProfile"
const ClearChargingProfileJsonChargingProfilePurposeTxProfile ClearChargingProfileJsonChargingProfilePurpose = "TxProfile"

type ClearChargingProfileJson struct {
	// Id corresponds to the JSON schema field "id".
	Id *int `json:"id,omitempty" yaml:"id,omitempty" mapstructure:"id,omitempty"`

	// ConnectorId corresponds to the JSON schema field "connectorId".
	ConnectorId *int `json:"connectorId,omitempty" yaml:"connectorId,omitempty" mapstructure:"connectorId,omitempty"`

	// ChargingProfilePurpose corresponds to the JSON schema field "chargingProfilePurpose".
	ChargingProfilePurpose *ClearChargingProfileJsonChargingProfilePurpose `json:"chargingProfilePurpose,omitempty" yaml:"chargingProfilePurpose,omitempty" mapstructure:"chargingProfilePurpose,omitempty"`

	// StackLevel corresponds to the JSON schema field "stackLevel".
	StackLevel *int `json:"stackLevel,omitempty" yaml:"stackLevel,omitempty" mapstructure:"stackLevel,omitempty"`
}

func (*ClearChargingProfileJson) IsRequest() {}
