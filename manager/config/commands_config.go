// SPDX-License-Identifier: Apache-2.0

package config

import (
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/thoughtworks/maeve-csms/manager/commands"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/handlers"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/schemas"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/transport"
	"golang.org/x/exp/slog"
	"k8s.io/utils/clock"
)

// CommandsConfig configures the queue of the commands sent to the charge stations.
type CommandsConfig struct {
	// ResponseTimeout is how long a sent command waits for the answer of the station.
	ResponseTimeout string `mapstructure:"response_timeout" toml:"response_timeout"`
	// ConnectDispatchDelay is the pause between the connection of a station and the
	// first command sent to it.
	ConnectDispatchDelay string `mapstructure:"connect_dispatch_delay" toml:"connect_dispatch_delay"`
	// SweepInterval is the pause between two runs of the sweep of the queue.
	SweepInterval string `mapstructure:"sweep_interval" toml:"sweep_interval"`
	// DefaultTTL is how long a command may wait in the queue unless its action or its
	// caller says otherwise.
	DefaultTTL string `mapstructure:"default_ttl" toml:"default_ttl"`
	// MaxTTL is the longest time a command may wait in the queue.
	MaxTTL string `mapstructure:"max_ttl" toml:"max_ttl"`
	// Retention is how long a completed command is kept.
	Retention string `mapstructure:"retention" toml:"retention"`
	// RemoteStartWindow is how long after an accepted remote start its transaction is
	// waited for.
	RemoteStartWindow string `mapstructure:"remote_start_window" toml:"remote_start_window"`
	// RemoteStopWindow is how long after an accepted remote stop the end of the
	// transaction is waited for.
	RemoteStopWindow string `mapstructure:"remote_stop_window" toml:"remote_stop_window"`
	// AvailabilityWindow is how long after an accepted change of availability the
	// connector status it asks for is waited for.
	AvailabilityWindow string `mapstructure:"availability_window" toml:"availability_window"`
	// TTL is how long a command may wait in the queue, by OCPP action.
	TTL map[string]string `mapstructure:"ttl" toml:"ttl,omitempty"`
}

// parseCommandsConfig checks [commands] and returns the timings of the command queue.
func parseCommandsConfig(cfg *CommandsConfig) (commands.Config, error) {
	var settings commands.Config
	for _, d := range []struct {
		name      string
		value     string
		into      *time.Duration
		allowZero bool
	}{
		{"response_timeout", cfg.ResponseTimeout, &settings.ResponseTimeout, false},
		{"connect_dispatch_delay", cfg.ConnectDispatchDelay, &settings.ConnectDispatchDelay, true},
		{"sweep_interval", cfg.SweepInterval, &settings.SweepInterval, false},
		{"default_ttl", cfg.DefaultTTL, &settings.DefaultTTL, false},
		{"max_ttl", cfg.MaxTTL, &settings.MaxTTL, false},
		{"retention", cfg.Retention, &settings.Retention, false},
		{"remote_start_window", cfg.RemoteStartWindow, &settings.RemoteStartWindow, false},
		{"remote_stop_window", cfg.RemoteStopWindow, &settings.RemoteStopWindow, false},
		{"availability_window", cfg.AvailabilityWindow, &settings.AvailabilityWindow, false},
	} {
		parsed, err := parsePositiveDuration("commands "+d.name, d.value, d.allowZero)
		if err != nil {
			return commands.Config{}, err
		}
		*d.into = parsed
	}

	known := knownCommandActions()
	settings.TTL = make(map[string]time.Duration, len(cfg.TTL))
	for action, value := range cfg.TTL {
		if !slices.Contains(known, action) {
			return commands.Config{}, fmt.Errorf("commands ttl: unknown action %q", action)
		}
		parsed, err := parsePositiveDuration("commands ttl."+action, value, false)
		if err != nil {
			return commands.Config{}, err
		}
		settings.TTL[action] = parsed
	}

	// a command whose default expiry is beyond max_ttl could never be enqueued
	if settings.DefaultTTL > settings.MaxTTL {
		return commands.Config{}, fmt.Errorf("commands default_ttl (%s) must not be longer than max_ttl (%s)",
			cfg.DefaultTTL, cfg.MaxTTL)
	}
	for _, action := range known {
		for _, version := range []transport.OcppVersion{transport.OcppVersion16, transport.OcppVersion201} {
			spec, ok := commands.Lookup(version, action)
			if !ok {
				continue
			}
			if ttl := settings.TTLFor(spec); ttl > settings.MaxTTL {
				return commands.Config{}, fmt.Errorf("commands: the expiry of %s (%s) must not be longer than max_ttl (%s); set commands.ttl.%s",
					action, ttl, cfg.MaxTTL, action)
			}
		}
	}
	return settings, nil
}

// knownCommandActions returns the actions of the catalogue of every OCPP version.
func knownCommandActions() []string {
	var actions []string
	for _, version := range []transport.OcppVersion{transport.OcppVersion16, transport.OcppVersion201} {
		for _, action := range commands.Actions(version) {
			if !slices.Contains(actions, action) {
				actions = append(actions, action)
			}
		}
	}
	sort.Strings(actions)
	return actions
}

func parsePositiveDuration(name, value string, allowZero bool) (time.Duration, error) {
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("failed to parse %s: %s", name, err)
	}
	if parsed < 0 || (parsed == 0 && !allowZero) {
		return 0, fmt.Errorf("%s must be positive: %s", name, value)
	}
	return parsed, nil
}

// getCommandService creates the command queue: the service that the API uses, the
// dispatcher that sends the commands and sees the answers, and the tracker that follows
// the accepted commands to their effect (told by the dispatcher about the answers and
// sweeping in its sweep job). The Firestore storage has no command store: all three are
// nil there and the manager works as before.
func getCommandService(settings commands.Config, storageType string, engine store.Engine, domainEvents events.Emitter,
	emitter transport.Emitter) (*commands.Service, *commands.Dispatcher, *commands.Tracker) {
	if storageType == "firestore" {
		slog.Warn("the command queue is not available: the firestore storage does not implement it")
		return nil, nil, nil
	}
	tracker := &commands.Tracker{
		Store:  engine,
		Tx:     engine,
		Events: domainEvents,
		Clock:  clock.RealClock{},
		Config: settings,
	}
	dispatcher := &commands.Dispatcher{
		Store:  engine,
		Tx:     engine,
		Events: domainEvents,
		Senders: map[transport.OcppVersion]handlers.RawCallMaker{
			transport.OcppVersion16:  ocpp16.NewCallMaker(emitter),
			transport.OcppVersion201: ocpp201.NewCallMaker(emitter),
		},
		Connectivity: commands.StatusStoreConnectivity{Store: engine},
		SchemaFS:     schemas.OcppSchemas,
		Clock:        clock.RealClock{},
		Config:       settings,
		AfterResult:  []commands.ResultHook{tracker},
		AfterSweep:   []commands.Sweeper{tracker},
	}
	service := &commands.Service{
		Store:          engine,
		Tx:             engine,
		Events:         domainEvents,
		RuntimeDetails: engine,
		Dispatcher:     dispatcher,
		SchemaFS:       schemas.OcppSchemas,
		Clock:          clock.RealClock{},
		Config:         settings,
	}
	return service, dispatcher, tracker
}

// commandObserver returns the dispatcher as the observer of the answers of the stations,
// or a nil interface (not a nil pointer) when there is no command queue.
func commandObserver(dispatcher *commands.Dispatcher) handlers.CallOutcomeObserver {
	if dispatcher == nil {
		return nil
	}
	return dispatcher
}

// commandTracker returns the tracker as what the OCPP 1.6 handlers tell about the
// transactions and connector statuses, or a nil interface (not a nil pointer) when there
// is no command queue.
func commandTracker(tracker *commands.Tracker) ocpp16.CommandTracker {
	if tracker == nil {
		return nil
	}
	return tracker
}
