// SPDX-License-Identifier: Apache-2.0

package config

type MqttSettingsConfig struct {
	Urls              []string `mapstructure:"urls" toml:"urls" validate:"required,dive,required"`
	Prefix            string   `mapstructure:"prefix" toml:"prefix" validate:"required"`
	Group             string   `mapstructure:"group" toml:"group" validate:"required"`
	ConnectTimeout    string   `mapstructure:"connect_timeout" toml:"connect_timeout" validate:"required"`
	ConnectRetryDelay string   `mapstructure:"connect_retry_delay" toml:"connect_retry_delay" validate:"required"`
	KeepAliveInterval string   `mapstructure:"keep_alive_interval" toml:"keep_alive_interval" validate:"required"`
}

// TransportConfig selects how the manager exchanges OCPP messages with the gateway:
// "mqtt" or "nats". The NATS transport uses the connection of [nats].
type TransportConfig struct {
	Type string               `mapstructure:"type" toml:"type" validate:"required,oneof=mqtt nats"`
	Mqtt *MqttSettingsConfig  `mapstructure:"mqtt,omitempty" toml:"mqtt,omitempty" validate:"required_if=Type mqtt"`
	Nats *NatsTransportConfig `mapstructure:"nats,omitempty" toml:"nats,omitempty"`
}
