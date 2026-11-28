// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
)

// RedactedValue replaces a secret in a payload.
const RedactedValue = "***"

// RedactPayload returns the payload with the value at each of the paths replaced by
// RedactedValue, and whether a value was replaced that was not RedactedValue already. A
// path is a dot-separated list of object keys and array indexes ("value",
// "setVariableData.0.attributeValue"); a path that the payload does not have is left
// out. A payload that is not a JSON document is replaced as a whole.
func RedactPayload(payload json.RawMessage, paths []string) (json.RawMessage, bool) {
	if len(paths) == 0 {
		return payload, false
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	var doc any
	if err := decoder.Decode(&doc); err != nil {
		redacted, _ := json.Marshal(RedactedValue)
		return redacted, true
	}
	changed := false
	for _, path := range paths {
		if redactPath(doc, strings.Split(path, ".")) {
			changed = true
		}
	}
	if !changed {
		return payload, false
	}
	redacted, err := json.Marshal(doc)
	if err != nil {
		// the document was decoded from JSON: it can be encoded again
		redacted, _ = json.Marshal(RedactedValue)
	}
	return redacted, true
}

// redactPath replaces the value at the path in doc, and reports whether it was not
// RedactedValue already.
func redactPath(doc any, path []string) bool {
	if len(path) == 0 {
		return false
	}
	key, rest := path[0], path[1:]
	switch node := doc.(type) {
	case map[string]any:
		value, found := node[key]
		if !found {
			return false
		}
		if len(rest) > 0 {
			return redactPath(value, rest)
		}
		node[key] = RedactedValue
		return value != RedactedValue
	case []any:
		index, err := strconv.Atoi(key)
		if err != nil || index < 0 || index >= len(node) {
			return false
		}
		if len(rest) > 0 {
			return redactPath(node[index], rest)
		}
		value := node[index]
		node[index] = RedactedValue
		return value != RedactedValue
	}
	return false
}
