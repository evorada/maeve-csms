package ocpp16

type RemoteStopTransactionResponseJsonStatus string

type RemoteStopTransactionResponseJson struct {
	// Status corresponds to the JSON schema field "status".
	Status RemoteStopTransactionResponseJsonStatus `json:"status" yaml:"status" mapstructure:"status"`
}

const RemoteStopTransactionResponseJsonStatusAccepted RemoteStopTransactionResponseJsonStatus = "Accepted"
const RemoteStopTransactionResponseJsonStatusRejected RemoteStopTransactionResponseJsonStatus = "Rejected"

func (*RemoteStopTransactionResponseJson) IsResponse() {}
