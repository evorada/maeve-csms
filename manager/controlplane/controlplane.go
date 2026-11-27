// SPDX-License-Identifier: Apache-2.0

// Package controlplane is the API that the console uses to provision and manage the
// tenants of the manager. It is served at /_evorada/v1 on the internal listener, and
// only to the services of the console that have the permission to manage tenants.
package controlplane

//go:generate sh -c "cd .. && make controlplane-generate"
