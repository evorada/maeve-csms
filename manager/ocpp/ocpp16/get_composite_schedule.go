package ocpp16

type GetCompositeScheduleJsonChargingRateUnit string

const GetCompositeScheduleJsonChargingRateUnitA GetCompositeScheduleJsonChargingRateUnit = "A"
const GetCompositeScheduleJsonChargingRateUnitW GetCompositeScheduleJsonChargingRateUnit = "W"

type GetCompositeScheduleJson struct {
	// ConnectorId corresponds to the JSON schema field "connectorId".
	ConnectorId int `json:"connectorId" yaml:"connectorId" mapstructure:"connectorId"`

	// Duration corresponds to the JSON schema field "duration".
	Duration int `json:"duration" yaml:"duration" mapstructure:"duration"`

	// ChargingRateUnit corresponds to the JSON schema field "chargingRateUnit".
	ChargingRateUnit *GetCompositeScheduleJsonChargingRateUnit `json:"chargingRateUnit,omitempty" yaml:"chargingRateUnit,omitempty" mapstructure:"chargingRateUnit,omitempty"`
}

func (*GetCompositeScheduleJson) IsRequest() {}
