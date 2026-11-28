// SPDX-License-Identifier: Apache-2.0

package nats

import (
	"errors"
	"fmt"
	"strings"
)

// This file is a copy of manager/transport/nats/subjects.go (the gateway is a separate
// Go module): both sides must build the same subject for a station key, so the two
// copies and their test vectors change together. The only difference is that the
// gateway names the OCPP version with the websocket subprotocol.

// DefaultSubjectPrefix is the first token of every OCPP subject.
const DefaultSubjectPrefix = "ocpp"

// The direction token of a subject, as seen from the charge station.
const (
	DirectionIn  = "in"  // charge station → manager
	DirectionOut = "out" // manager → charge station
)

// NoOrgToken is the org token of a station key without an org part: the plain station
// ids of single tenancy mode.
const NoOrgToken = "_"

// The websocket subprotocols of the OCPP versions.
const (
	ProtocolOcpp16  = "ocpp1.6"
	ProtocolOcpp201 = "ocpp2.0.1"
)

const (
	versionToken16  = "ocpp16"
	versionToken201 = "ocpp201"
)

// ConnToken is the second token of the subjects of the connection events.
const ConnToken = "conn"

// stationSubjectTokens is the number of tokens of a station subject:
// <prefix>.<direction>.<version>.<org>.<station>.
const stationSubjectTokens = 5

// connSubjectTokens is the number of tokens of a connection event subject:
// <prefix>.conn.<org>.<station>.
const connSubjectTokens = 4

// VersionToken maps the websocket subprotocol "ocpp1.6" → "ocpp16" and "ocpp2.0.1" →
// "ocpp201". The subprotocols contain dots, so they cannot be subject tokens.
func VersionToken(protocol string) (string, error) {
	switch protocol {
	case ProtocolOcpp16:
		return versionToken16, nil
	case ProtocolOcpp201:
		return versionToken201, nil
	default:
		return "", fmt.Errorf("unknown OCPP protocol %q", protocol)
	}
}

// VersionFromToken is the reverse of VersionToken.
func VersionFromToken(tok string) (string, error) {
	switch tok {
	case versionToken16:
		return ProtocolOcpp16, nil
	case versionToken201:
		return ProtocolOcpp201, nil
	default:
		return "", fmt.Errorf("unknown OCPP version token %q", tok)
	}
}

// EncodeStationKey turns a station key into the org token and the station token of a
// subject.
//
// A key "{org_key}.{stationId}" is split at its first dot: the org key is the org
// token and the station id is encoded as one token (see below). A key that has no
// org part gets the org token "_" and is encoded whole as the station token: a key
// without a dot (single tenancy mode), and also a key whose part before the first dot
// cannot be an org token (it does not match [A-Za-z0-9_-]+, or is "_") or whose part
// after it is empty. Single tenancy mode allows dots inside station ids, so these keys
// exist; this way every key round-trips through DecodeStationKey.
//
// Encoding of the station token: every byte outside [A-Za-z0-9_~:@=+-] is written as
// %XX (uppercase hex), so ".", "*", ">", "%", whitespace, control and non-ASCII bytes
// never appear raw. The token of an empty key is empty, which is not a valid subject
// token: InSubject and OutSubject refuse it.
func EncodeStationKey(key string) (orgToken, stationToken string) {
	org, station, found := strings.Cut(key, ".")
	if found && org != NoOrgToken && validOrgToken(org) && station != "" {
		return org, escapeStationToken(station)
	}
	return NoOrgToken, escapeStationToken(key)
}

// DecodeStationKey is the reverse of EncodeStationKey. It refuses an org token that
// is not "_" or [A-Za-z0-9_-]+, a malformed escape, and tokens that EncodeStationKey
// would not have produced (a byte that should have been escaped, an escape of a byte
// that is written raw, lowercase hex), so that every station has exactly one subject.
func DecodeStationKey(orgToken, stationToken string) (string, error) {
	if orgToken != NoOrgToken && !validOrgToken(orgToken) {
		return "", fmt.Errorf("invalid org token %q", orgToken)
	}
	if stationToken == "" {
		return "", errors.New("empty station token")
	}
	station, err := unescapeStationToken(stationToken)
	if err != nil {
		return "", err
	}

	key := station
	if orgToken != NoOrgToken {
		key = orgToken + "." + station
	}
	if o, s := EncodeStationKey(key); o != orgToken || s != stationToken {
		return "", fmt.Errorf("station tokens %q.%q are not in canonical form (want %q.%q)", orgToken, stationToken, o, s)
	}
	return key, nil
}

// InSubject is the subject of the messages that the charge station with the key sends:
// <prefix>.in.<ver>.<org>.<station>.
func InSubject(prefix, protocol, key string) (string, error) {
	return stationSubject(prefix, DirectionIn, protocol, key)
}

// OutSubject is the subject of the messages for the charge station with the key:
// <prefix>.out.<ver>.<org>.<station>.
func OutSubject(prefix, protocol, key string) (string, error) {
	return stationSubject(prefix, DirectionOut, protocol, key)
}

// ConnSubject is the subject of the connection events of the charge station with the
// key: <prefix>.conn.<org>.<station>.
func ConnSubject(prefix, key string) (string, error) {
	if err := ValidatePrefix(prefix); err != nil {
		return "", err
	}
	if key == "" {
		return "", errors.New("empty station key")
	}
	org, station := EncodeStationKey(key)
	return strings.Join([]string{prefix, ConnToken, org, station}, "."), nil
}

// ParseConnSubject returns the station key of a subject made by ConnSubject. The
// prefix is not checked against a configured one.
func ParseConnSubject(subject string) (key string, err error) {
	tokens := strings.Split(subject, ".")
	if len(tokens) != connSubjectTokens {
		return "", fmt.Errorf("subject %q has %d tokens, want %d", subject, len(tokens), connSubjectTokens)
	}
	if err := ValidatePrefix(tokens[0]); err != nil {
		return "", fmt.Errorf("subject %q: %w", subject, err)
	}
	if tokens[1] != ConnToken {
		return "", fmt.Errorf("subject %q is not a %s subject", subject, ConnToken)
	}
	key, err = DecodeStationKey(tokens[2], tokens[3])
	if err != nil {
		return "", fmt.Errorf("subject %q: %w", subject, err)
	}
	return key, nil
}

// ParseStationSubject splits a subject made by InSubject or OutSubject into its
// direction, websocket subprotocol and station key. The prefix is not checked against
// a configured one.
func ParseStationSubject(subject string) (direction, protocol, key string, err error) {
	tokens := strings.Split(subject, ".")
	if len(tokens) != stationSubjectTokens {
		return "", "", "", fmt.Errorf("subject %q has %d tokens, want %d", subject, len(tokens), stationSubjectTokens)
	}
	if err := ValidatePrefix(tokens[0]); err != nil {
		return "", "", "", fmt.Errorf("subject %q: %w", subject, err)
	}
	direction = tokens[1]
	if direction != DirectionIn && direction != DirectionOut {
		return "", "", "", fmt.Errorf("subject %q: unknown direction %q", subject, direction)
	}
	protocol, err = VersionFromToken(tokens[2])
	if err != nil {
		return "", "", "", fmt.Errorf("subject %q: %w", subject, err)
	}
	key, err = DecodeStationKey(tokens[3], tokens[4])
	if err != nil {
		return "", "", "", fmt.Errorf("subject %q: %w", subject, err)
	}
	return direction, protocol, key, nil
}

// ValidatePrefix checks that a subject prefix is one subject token of
// [A-Za-z0-9_-].
func ValidatePrefix(prefix string) error {
	if !validOrgToken(prefix) {
		return fmt.Errorf("invalid subject prefix %q: must be one token of [A-Za-z0-9_-]", prefix)
	}
	return nil
}

func stationSubject(prefix, direction, protocol, key string) (string, error) {
	if err := ValidatePrefix(prefix); err != nil {
		return "", err
	}
	ver, err := VersionToken(protocol)
	if err != nil {
		return "", err
	}
	if key == "" {
		return "", errors.New("empty station key")
	}
	org, station := EncodeStationKey(key)
	return strings.Join([]string{prefix, direction, ver, org, station}, "."), nil
}

// validOrgToken reports whether s matches [A-Za-z0-9_-]+.
func validOrgToken(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !isAlphaNum(c) && c != '_' && c != '-' {
			return false
		}
	}
	return true
}

func isAlphaNum(c byte) bool {
	return ('A' <= c && c <= 'Z') || ('a' <= c && c <= 'z') || ('0' <= c && c <= '9')
}

// isRawStationByte reports whether c is written raw in a station token.
func isRawStationByte(c byte) bool {
	if isAlphaNum(c) {
		return true
	}
	switch c {
	case '_', '~', ':', '@', '=', '+', '-':
		return true
	}
	return false
}

const upperHex = "0123456789ABCDEF"

func escapeStationToken(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if isRawStationByte(c) {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(upperHex[c>>4])
		b.WriteByte(upperHex[c&0x0f])
	}
	return b.String()
}

func unescapeStationToken(tok string) (string, error) {
	var b strings.Builder
	b.Grow(len(tok))
	for i := 0; i < len(tok); i++ {
		c := tok[i]
		if c != '%' {
			b.WriteByte(c)
			continue
		}
		if i+2 >= len(tok) {
			return "", fmt.Errorf("station token %q: truncated escape at byte %d", tok, i)
		}
		hi, okHi := fromUpperHex(tok[i+1])
		lo, okLo := fromUpperHex(tok[i+2])
		if !okHi || !okLo {
			return "", fmt.Errorf("station token %q: malformed escape %q", tok, tok[i:i+3])
		}
		b.WriteByte(hi<<4 | lo)
		i += 2
	}
	return b.String(), nil
}

func fromUpperHex(c byte) (byte, bool) {
	switch {
	case '0' <= c && c <= '9':
		return c - '0', true
	case 'A' <= c && c <= 'F':
		return c - 'A' + 10, true
	default:
		return 0, false
	}
}
