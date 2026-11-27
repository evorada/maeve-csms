// SPDX-License-Identifier: Apache-2.0

package tenancy

import "encoding/json"

// ConfigSchema returns the JSON Schema of the config of a tenant, as the manifest of
// the control plane publishes it. Any object is accepted for now.
func ConfigSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
