// SPDX-License-Identifier: Apache-2.0

package controlplane

import (
	"runtime/debug"
	"slices"

	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
)

// Version is the version of the manager in the manifest. It is set when the manager is
// built, with
//
//	-ldflags "-X github.com/thoughtworks/maeve-csms/manager/controlplane.Version=..."
//
// and falls back to the version of the main module in the build information, then to
// "dev".
var Version = ""

func version() string {
	if Version != "" {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

// BuildManifest describes the manager to the platform: the product, the kinds of
// resources, the permissions that the platform can give to the callers of the API, the
// types of the domain events (with the product, like "cpms.session.completed", the type
// of their CloudEvents) and the schema of the tenant config. The permission to manage
// tenants is not part of it.
func BuildManifest() Manifest {
	return Manifest{
		Product:      events.Product,
		Service:      "maeve",
		Version:      version(),
		Resources:    []string{"station", "connector", "location", "token", "session"},
		Permissions:  slices.Clone(tenancy.ManifestPermissions),
		Events:       eventTypes(),
		Meters:       []map[string]interface{}{},
		ConfigSchema: tenancy.ConfigSchema(),
	}
}

// eventTypes returns the full names of the types of the event catalogue.
func eventTypes() []string {
	types := make([]string, len(events.AllTypes))
	for i, eventType := range events.AllTypes {
		types[i] = events.Product + "." + eventType
	}
	return types
}
