package ocpp16

type ClearChargingProfileResponseJsonStatus string

const ClearChargingProfileResponseJsonStatusAccepted ClearChargingProfileResponseJsonStatus = "Accepted"
const ClearChargingProfileResponseJsonStatusUnknown ClearChargingProfileResponseJsonStatus = "Unknown"

type ClearChargingProfileResponseJson struct {
	// Status corresponds to the JSON schema field "status".
	Status ClearChargingProfileResponseJsonStatus `json:"status" yaml:"status" mapstructure:"status"`
}

func (*ClearChargingProfileResponseJson) IsResponse() {}
