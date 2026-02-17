// SPDX-License-Identifier: Apache-2.0

package ocpp201

// EventNotificationEnumType specifies source of event notification.
type EventNotificationEnumType string

const (
	EventNotificationEnumTypeHardWiredNotification EventNotificationEnumType = "HardWiredNotification"
	EventNotificationEnumTypeHardWiredMonitor      EventNotificationEnumType = "HardWiredMonitor"
	EventNotificationEnumTypePreconfiguredMonitor  EventNotificationEnumType = "PreconfiguredMonitor"
	EventNotificationEnumTypeCustomMonitor         EventNotificationEnumType = "CustomMonitor"
)

// EventTriggerEnumType identifies what triggered the event.
type EventTriggerEnumType string

const (
	EventTriggerEnumTypeAlerting EventTriggerEnumType = "Alerting"
	EventTriggerEnumTypeDelta    EventTriggerEnumType = "Delta"
	EventTriggerEnumTypePeriodic EventTriggerEnumType = "Periodic"
)

// EventDataType represents one event notification payload element.
type EventDataType struct {
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	EventId int `json:"eventId" yaml:"eventId" mapstructure:"eventId"`

	Timestamp string `json:"timestamp" yaml:"timestamp" mapstructure:"timestamp"`

	Trigger EventTriggerEnumType `json:"trigger" yaml:"trigger" mapstructure:"trigger"`

	Cause *int `json:"cause,omitempty" yaml:"cause,omitempty" mapstructure:"cause,omitempty"`

	ActualValue string `json:"actualValue" yaml:"actualValue" mapstructure:"actualValue"`

	TechCode *string `json:"techCode,omitempty" yaml:"techCode,omitempty" mapstructure:"techCode,omitempty"`

	TechInfo *string `json:"techInfo,omitempty" yaml:"techInfo,omitempty" mapstructure:"techInfo,omitempty"`

	Cleared *bool `json:"cleared,omitempty" yaml:"cleared,omitempty" mapstructure:"cleared,omitempty"`

	TransactionId *string `json:"transactionId,omitempty" yaml:"transactionId,omitempty" mapstructure:"transactionId,omitempty"`

	Component ComponentType `json:"component" yaml:"component" mapstructure:"component"`

	VariableMonitoringId *int `json:"variableMonitoringId,omitempty" yaml:"variableMonitoringId,omitempty" mapstructure:"variableMonitoringId,omitempty"`

	EventNotificationType EventNotificationEnumType `json:"eventNotificationType" yaml:"eventNotificationType" mapstructure:"eventNotificationType"`

	Variable VariableType `json:"variable" yaml:"variable" mapstructure:"variable"`
}

// NotifyEventRequestJson contains event notifications sent from CS to CSMS.
type NotifyEventRequestJson struct {
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	GeneratedAt string `json:"generatedAt" yaml:"generatedAt" mapstructure:"generatedAt"`

	Tbc bool `json:"tbc,omitempty" yaml:"tbc,omitempty" mapstructure:"tbc,omitempty"`

	SeqNo int `json:"seqNo" yaml:"seqNo" mapstructure:"seqNo"`

	EventData []EventDataType `json:"eventData" yaml:"eventData" mapstructure:"eventData"`
}

func (*NotifyEventRequestJson) IsRequest() {}
