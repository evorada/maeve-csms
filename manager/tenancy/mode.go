// SPDX-License-Identifier: Apache-2.0

package tenancy

// Mode says how the charge stations of a deployment are mapped to orgs.
type Mode string

const (
	// ModeSingle keeps plain station ids and ignores the host that a station connects
	// to. The empty Mode is ModeSingle.
	ModeSingle Mode = "single"
	// ModeMulti takes the tenant from the host that a station connects to and names the
	// station "{org_key}.{stationId}".
	ModeMulti Mode = "multi"
)
