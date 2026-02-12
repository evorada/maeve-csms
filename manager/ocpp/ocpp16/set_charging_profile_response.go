package ocpp16

type SetChargingProfileResponseJsonStatus string

const SetChargingProfileResponseJsonStatusAccepted SetChargingProfileResponseJsonStatus = "Accepted"
const SetChargingProfileResponseJsonStatusRejected SetChargingProfileResponseJsonStatus = "Rejected"
const SetChargingProfileResponseJsonStatusNotSupported SetChargingProfileResponseJsonStatus = "NotSupported"

type SetChargingProfileResponseJson struct {
	// Status corresponds to the JSON schema field "status".
	Status SetChargingProfileResponseJsonStatus `json:"status" yaml:"status" mapstructure:"status"`
}

func (*SetChargingProfileResponseJson) IsResponse() {}
