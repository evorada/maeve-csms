// SPDX-License-Identifier: Apache-2.0

package ocpp16

type UploadLogStatusEnumType string

const UploadLogStatusEnumTypeBadMessage UploadLogStatusEnumType = "BadMessage"
const UploadLogStatusEnumTypeIdle UploadLogStatusEnumType = "Idle"
const UploadLogStatusEnumTypeNotSupportedOperation UploadLogStatusEnumType = "NotSupportedOperation"
const UploadLogStatusEnumTypePermissionDenied UploadLogStatusEnumType = "PermissionDenied"
const UploadLogStatusEnumTypeUploaded UploadLogStatusEnumType = "Uploaded"
const UploadLogStatusEnumTypeUploadFailure UploadLogStatusEnumType = "UploadFailure"
const UploadLogStatusEnumTypeUploading UploadLogStatusEnumType = "Uploading"

type LogStatusNotificationJson struct {
	// Status corresponds to the JSON schema field "status".
	Status UploadLogStatusEnumType `json:"status" yaml:"status" mapstructure:"status"`

	// RequestId corresponds to the JSON schema field "requestId".
	RequestId *int `json:"requestId,omitempty" yaml:"requestId,omitempty" mapstructure:"requestId,omitempty"`
}

func (*LogStatusNotificationJson) IsRequest() {}
