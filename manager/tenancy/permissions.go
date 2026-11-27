// SPDX-License-Identifier: Apache-2.0

package tenancy

// The permissions that a token of the platform can give to a caller of the manager.
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

// WritePermissions are the permissions of the operations that create or change what a
// tenant owns. A suspended tenant cannot call an operation with one of them, unless the
// operation only reads. PermConnectorCommand is not one of them: commands to a charge
// station stay possible, because they can affect a driver on site. PermCertificateWrite
// and PermOcpiConnection are not either: what they cover does not belong to a tenant.
var WritePermissions = map[string]bool{
	PermStationWrite: true,
	PermTariffWrite:  true,
	PermTokenWrite:   true,
}

// ManifestPermissions lists the permissions that the platform can give to the callers of
// the API: every permission but PermTenantManage, which is only for the services of the
// platform that call the control plane.
var ManifestPermissions []string

func init() {
	for _, permission := range AllPermissions {
		if permission != PermTenantManage {
			ManifestPermissions = append(ManifestPermissions, permission)
		}
	}
}
