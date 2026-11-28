// SPDX-License-Identifier: Apache-2.0

// Package entitlements reads the entitlement document that the platform sends with a
// tenant and enforces the hard limits in it when something is created.
//
// Only creation is limited, never what exists: a charge station that is registered
// keeps connecting, reporting new connectors and charging whatever the limits say. The
// messages of the charge stations never reach the Enforcer. A soft limit is never
// enforced either: the platform bills what goes beyond it. The status and the features
// of the document are read but not acted on.
//
// # Connectors
//
// Enforcer.CheckConnectors is the check to call before connectors are created for an
// org: with 1 before a charge station is registered (a new charge station brings at
// least one connector, and how many it really has is only known once it reports them),
// with 1 before a single connector is created, and with the number of connectors of a
// charge station before that charge station is approved. Changing something that
// exists, such as registering a charge station again, needs no check.
//
// The limit is cpms.connectors.max. With the scope org it counts the connectors of the
// org itself. With the scope subtree it is shared by the org named in pool_org_id and
// every tenant below that org; without pool_org_id by the org itself and the tenants
// below it. What is counted is what the ConnectorCounter knows: today the connectors
// that registered charge stations that are active have reported a status for (a pending
// or decommissioned charge station takes up nothing), so a charge station that
// is registered and has not reported yet takes up nothing. A counter of the connectors
// that are stored as such should take its place once they exist.
//
// The check is best effort: it reads the count and the caller writes afterwards, so
// requests that arrive together can each pass and end up above the limit. That is
// accepted for limits on creation; nothing locks.
//
// # When the check cannot be made
//
// A limit must not stop a tenant because of a mistake of the platform, and it must not
// be skipped because the storage is down:
//
//   - single mode, an org that is not a tenant, no document ({} or nothing), no
//     cpms.connectors.max, an enforcement other than hard, nothing to add: allowed.
//   - a document that cannot be read, a negative limit, a pool_org_id of a subtree
//     limit that is not a UUID: logged as an error and allowed.
//   - a scope that is neither org nor subtree: logged as a warning, and the limit
//     counts the org alone.
//   - a storage backend that has no tenants or cannot count connectors
//     (store.ErrNotImplemented): logged as a warning and allowed; such a backend only
//     serves single mode.
//   - any other error of the tenant store or the counter: returned to the caller, which
//     must create nothing.
package entitlements
