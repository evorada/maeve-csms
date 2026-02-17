// SPDX-License-Identifier: Apache-2.0

package ocpp201

// MessageInfoType contains details of a message to be displayed by the charging station.
type MessageInfoType struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// Display identifies the component where the message should be displayed.
	Display *ComponentType `json:"display,omitempty" yaml:"display,omitempty" mapstructure:"display,omitempty"`

	// Id is the unique identifier of the message.
	Id int `json:"id" yaml:"id" mapstructure:"id"`

	// Priority controls display priority behavior.
	Priority MessagePriorityEnumType `json:"priority" yaml:"priority" mapstructure:"priority"`

	// State controls in which station state the message is shown.
	State *MessageStateEnumType `json:"state,omitempty" yaml:"state,omitempty" mapstructure:"state,omitempty"`

	// StartDateTime indicates when the message should start being displayed.
	StartDateTime *string `json:"startDateTime,omitempty" yaml:"startDateTime,omitempty" mapstructure:"startDateTime,omitempty"`

	// EndDateTime indicates when the message should stop being displayed.
	EndDateTime *string `json:"endDateTime,omitempty" yaml:"endDateTime,omitempty" mapstructure:"endDateTime,omitempty"`

	// TransactionId scopes the message to a transaction when provided.
	TransactionId *string `json:"transactionId,omitempty" yaml:"transactionId,omitempty" mapstructure:"transactionId,omitempty"`

	// Message contains content and formatting details.
	Message MessageContentType `json:"message" yaml:"message" mapstructure:"message"`
}
