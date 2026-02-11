package ocpp16

type GetCompositeScheduleResponseJsonStatus string

const GetCompositeScheduleResponseJsonStatusAccepted GetCompositeScheduleResponseJsonStatus = "Accepted"
const GetCompositeScheduleResponseJsonStatusRejected GetCompositeScheduleResponseJsonStatus = "Rejected"

type GetCompositeScheduleResponseJsonChargingRateUnit string

const GetCompositeScheduleResponseJsonChargingRateUnitA GetCompositeScheduleResponseJsonChargingRateUnit = "A"
const GetCompositeScheduleResponseJsonChargingRateUnitW GetCompositeScheduleResponseJsonChargingRateUnit = "W"

type GetCompositeScheduleResponseJsonChargingSchedulePeriod struct {
	// StartPeriod corresponds to the JSON schema field "startPeriod".
	StartPeriod int `json:"startPeriod" yaml:"startPeriod" mapstructure:"startPeriod"`

	// Limit corresponds to the JSON schema field "limit".
	Limit float64 `json:"limit" yaml:"limit" mapstructure:"limit"`

	// NumberPhases corresponds to the JSON schema field "numberPhases".
	NumberPhases *int `json:"numberPhases,omitempty" yaml:"numberPhases,omitempty" mapstructure:"numberPhases,omitempty"`
}

type GetCompositeScheduleResponseJsonChargingSchedule struct {
	// Duration corresponds to the JSON schema field "duration".
	Duration *int `json:"duration,omitempty" yaml:"duration,omitempty" mapstructure:"duration,omitempty"`

	// StartSchedule corresponds to the JSON schema field "startSchedule".
	StartSchedule *string `json:"startSchedule,omitempty" yaml:"startSchedule,omitempty" mapstructure:"startSchedule,omitempty"`

	// ChargingRateUnit corresponds to the JSON schema field "chargingRateUnit".
	ChargingRateUnit GetCompositeScheduleResponseJsonChargingRateUnit `json:"chargingRateUnit" yaml:"chargingRateUnit" mapstructure:"chargingRateUnit"`

	// ChargingSchedulePeriod corresponds to the JSON schema field "chargingSchedulePeriod".
	ChargingSchedulePeriod []GetCompositeScheduleResponseJsonChargingSchedulePeriod `json:"chargingSchedulePeriod" yaml:"chargingSchedulePeriod" mapstructure:"chargingSchedulePeriod"`

	// MinChargingRate corresponds to the JSON schema field "minChargingRate".
	MinChargingRate *float64 `json:"minChargingRate,omitempty" yaml:"minChargingRate,omitempty" mapstructure:"minChargingRate,omitempty"`
}

type GetCompositeScheduleResponseJson struct {
	// Status corresponds to the JSON schema field "status".
	Status GetCompositeScheduleResponseJsonStatus `json:"status" yaml:"status" mapstructure:"status"`

	// ConnectorId corresponds to the JSON schema field "connectorId".
	ConnectorId *int `json:"connectorId,omitempty" yaml:"connectorId,omitempty" mapstructure:"connectorId,omitempty"`

	// ScheduleStart corresponds to the JSON schema field "scheduleStart".
	ScheduleStart *string `json:"scheduleStart,omitempty" yaml:"scheduleStart,omitempty" mapstructure:"scheduleStart,omitempty"`

	// ChargingSchedule corresponds to the JSON schema field "chargingSchedule".
	ChargingSchedule *GetCompositeScheduleResponseJsonChargingSchedule `json:"chargingSchedule,omitempty" yaml:"chargingSchedule,omitempty" mapstructure:"chargingSchedule,omitempty"`
}

func (*GetCompositeScheduleResponseJson) IsResponse() {}
