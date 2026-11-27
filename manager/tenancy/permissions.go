// SPDX-License-Identifier: Apache-2.0

package tenancy

// The permissions that a token of the console can give to a caller of the manager.
const (
	PermStationRead      = "cpms.station.read"
	PermStationWrite     = "cpms.station.write"
	PermConnectorCommand = "cpms.connector.command"
	PermTariffWrite      = "cpms.tariff.write"
	PermSessionRead      = "cpms.session.read"
	PermTokenRead        = "cpms.token.read"  // #nosec G101 -- the name of a permission, not a credential
	PermTokenWrite       = "cpms.token.write" // #nosec G101 -- the name of a permission, not a credential
	// PermCertificateWrite covers the certificate table, which is shared by all orgs.
	PermCertificateWrite = "cpms.certificate.write"
	// PermTenantManage covers the control plane: creating, suspending and configuring
	// tenants.
	PermTenantManage   = "cpms.tenant.manage"
	PermOcpiConnection = "ocpi.connection.manage"
	// PermMessageLogReadSensitive covers unmasked token identifiers and other sensitive
	// fields in the OCPP message log.
	PermMessageLogReadSensitive = "cpms.message_log.read_sensitive"
)

// AllPermissions lists every permission that the manager knows.
var AllPermissions = []string{
	PermStationRead,
	PermStationWrite,
	PermConnectorCommand,
	PermTariffWrite,
	PermSessionRead,
	PermTokenRead,
	PermTokenWrite,
	PermCertificateWrite,
	PermTenantManage,
	PermOcpiConnection,
	PermMessageLogReadSensitive,
}

// ManifestPermissions lists the permissions that the console can give to the callers of
// the API: every permission but PermTenantManage, which is only for the services of the
// console that call the control plane.
var ManifestPermissions []string

func init() {
	for _, permission := range AllPermissions {
		if permission != PermTenantManage {
			ManifestPermissions = append(ManifestPermissions, permission)
		}
	}
}
