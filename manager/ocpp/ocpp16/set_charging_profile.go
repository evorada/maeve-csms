package ocpp16

type SetChargingProfileJsonChargingProfilePurpose string

const SetChargingProfileJsonChargingProfilePurposeChargePointMaxProfile SetChargingProfileJsonChargingProfilePurpose = "ChargePointMaxProfile"
const SetChargingProfileJsonChargingProfilePurposeTxDefaultProfile SetChargingProfileJsonChargingProfilePurpose = "TxDefaultProfile"
const SetChargingProfileJsonChargingProfilePurposeTxProfile SetChargingProfileJsonChargingProfilePurpose = "TxProfile"

type SetChargingProfileJsonChargingProfileKind string

const SetChargingProfileJsonChargingProfileKindAbsolute SetChargingProfileJsonChargingProfileKind = "Absolute"
const SetChargingProfileJsonChargingProfileKindRecurring SetChargingProfileJsonChargingProfileKind = "Recurring"
const SetChargingProfileJsonChargingProfileKindRelative SetChargingProfileJsonChargingProfileKind = "Relative"

type SetChargingProfileJsonRecurrencyKind string

const SetChargingProfileJsonRecurrencyKindDaily SetChargingProfileJsonRecurrencyKind = "Daily"
const SetChargingProfileJsonRecurrencyKindWeekly SetChargingProfileJsonRecurrencyKind = "Weekly"

type SetChargingProfileJsonChargingRateUnit string

const SetChargingProfileJsonChargingRateUnitA SetChargingProfileJsonChargingRateUnit = "A"
const SetChargingProfileJsonChargingRateUnitW SetChargingProfileJsonChargingRateUnit = "W"

type SetChargingProfileJsonChargingSchedulePeriod struct {
	// StartPeriod corresponds to the JSON schema field "startPeriod".
	StartPeriod int `json:"startPeriod" yaml:"startPeriod" mapstructure:"startPeriod"`

	// Limit corresponds to the JSON schema field "limit".
	Limit float64 `json:"limit" yaml:"limit" mapstructure:"limit"`

	// NumberPhases corresponds to the JSON schema field "numberPhases".
	NumberPhases *int `json:"numberPhases,omitempty" yaml:"numberPhases,omitempty" mapstructure:"numberPhases,omitempty"`
}

type SetChargingProfileJsonChargingSchedule struct {
	// Duration corresponds to the JSON schema field "duration".
	Duration *int `json:"duration,omitempty" yaml:"duration,omitempty" mapstructure:"duration,omitempty"`

	// StartSchedule corresponds to the JSON schema field "startSchedule".
	StartSchedule *string `json:"startSchedule,omitempty" yaml:"startSchedule,omitempty" mapstructure:"startSchedule,omitempty"`

	// ChargingRateUnit corresponds to the JSON schema field "chargingRateUnit".
	ChargingRateUnit SetChargingProfileJsonChargingRateUnit `json:"chargingRateUnit" yaml:"chargingRateUnit" mapstructure:"chargingRateUnit"`

	// ChargingSchedulePeriod corresponds to the JSON schema field "chargingSchedulePeriod".
	ChargingSchedulePeriod []SetChargingProfileJsonChargingSchedulePeriod `json:"chargingSchedulePeriod" yaml:"chargingSchedulePeriod" mapstructure:"chargingSchedulePeriod"`

	// MinChargingRate corresponds to the JSON schema field "minChargingRate".
	MinChargingRate *float64 `json:"minChargingRate,omitempty" yaml:"minChargingRate,omitempty" mapstructure:"minChargingRate,omitempty"`
}

type SetChargingProfileJsonCsChargingProfiles struct {
	// ChargingProfileId corresponds to the JSON schema field "chargingProfileId".
	ChargingProfileId int `json:"chargingProfileId" yaml:"chargingProfileId" mapstructure:"chargingProfileId"`

	// TransactionId corresponds to the JSON schema field "transactionId".
	TransactionId *int `json:"transactionId,omitempty" yaml:"transactionId,omitempty" mapstructure:"transactionId,omitempty"`

	// StackLevel corresponds to the JSON schema field "stackLevel".
	StackLevel int `json:"stackLevel" yaml:"stackLevel" mapstructure:"stackLevel"`

	// ChargingProfilePurpose corresponds to the JSON schema field "chargingProfilePurpose".
	ChargingProfilePurpose SetChargingProfileJsonChargingProfilePurpose `json:"chargingProfilePurpose" yaml:"chargingProfilePurpose" mapstructure:"chargingProfilePurpose"`

	// ChargingProfileKind corresponds to the JSON schema field "chargingProfileKind".
	ChargingProfileKind SetChargingProfileJsonChargingProfileKind `json:"chargingProfileKind" yaml:"chargingProfileKind" mapstructure:"chargingProfileKind"`

	// RecurrencyKind corresponds to the JSON schema field "recurrencyKind".
	RecurrencyKind *SetChargingProfileJsonRecurrencyKind `json:"recurrencyKind,omitempty" yaml:"recurrencyKind,omitempty" mapstructure:"recurrencyKind,omitempty"`

	// ValidFrom corresponds to the JSON schema field "validFrom".
	ValidFrom *string `json:"validFrom,omitempty" yaml:"validFrom,omitempty" mapstructure:"validFrom,omitempty"`

	// ValidTo corresponds to the JSON schema field "validTo".
	ValidTo *string `json:"validTo,omitempty" yaml:"validTo,omitempty" mapstructure:"validTo,omitempty"`

	// ChargingSchedule corresponds to the JSON schema field "chargingSchedule".
	ChargingSchedule SetChargingProfileJsonChargingSchedule `json:"chargingSchedule" yaml:"chargingSchedule" mapstructure:"chargingSchedule"`
}

type SetChargingProfileJson struct {
	// ConnectorId corresponds to the JSON schema field "connectorId".
	ConnectorId int `json:"connectorId" yaml:"connectorId" mapstructure:"connectorId"`

	// CsChargingProfiles corresponds to the JSON schema field "csChargingProfiles".
	CsChargingProfiles SetChargingProfileJsonCsChargingProfiles `json:"csChargingProfiles" yaml:"csChargingProfiles" mapstructure:"csChargingProfiles"`
}

func (*SetChargingProfileJson) IsRequest() {}
