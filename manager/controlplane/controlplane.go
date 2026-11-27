// SPDX-License-Identifier: Apache-2.0

// Package controlplane is the API that the platform uses to provision and manage the
// tenants of the manager. It is served at /_control/v1 on the internal listener, and
// only to the services of the platform that have the permission to manage tenants.
package controlplane

//go:generate sh -c "cd .. && make controlplane-generate"
