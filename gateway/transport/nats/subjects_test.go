// SPDX-License-Identifier: Apache-2.0

package nats_test

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	natstransport "github.com/thoughtworks/maeve-csms/gateway/transport/nats"
)

// stationKeyVectors is the subject encoding of station keys. The manager has the same
// table in manager/transport/nats/subjects_test.go: change both together.
var stationKeyVectors = []struct {
	key          string
	orgToken     string
	stationToken string
}{
	{key: "k7f2.CP001", orgToken: "k7f2", stationToken: "CP001"},
	{key: "k7f2.CP.001", orgToken: "k7f2", stationToken: "CP%2E001"},
	{key: "k7f2.Station *1", orgToken: "k7f2", stationToken: "Station%20%2A1"},
	{key: "k7f2.a>b%c", orgToken: "k7f2", stationToken: "a%3Eb%25c"},
	{key: "k7f2.Ünï", orgToken: "k7f2", stationToken: "%C3%9Cn%C3%AF"},
	{key: "CP001", orgToken: "_", stationToken: "CP001"},
	// every byte that is written raw
	{key: "k7f2.az-AZ_09~:@=+", orgToken: "k7f2", stationToken: "az-AZ_09~:@=+"},
	// control bytes and the other ASCII punctuation are escaped
	{key: "k7f2.a\tb\nc/d#e!f", orgToken: "k7f2", stationToken: "a%09b%0Ac%2Fd%23e%21f"},
	// single tenancy mode: plain station ids, which may contain dots
	{key: "CP 001", orgToken: "_", stationToken: "CP%20001"},
	{key: "CP.001", orgToken: "CP", stationToken: "001"},
	{key: "Ünï.1", orgToken: "_", stationToken: "%C3%9Cn%C3%AF%2E1"},
	{key: "cp:1.a", orgToken: "_", stationToken: "cp:1%2Ea"},
	// keys whose part before the first dot cannot be an org token are encoded whole
	{key: "_.CP001", orgToken: "_", stationToken: "_%2ECP001"},
	{key: ".CP001", orgToken: "_", stationToken: "%2ECP001"},
	{key: "k7f2.", orgToken: "_", stationToken: "k7f2%2E"},
}

// The manager decodes what the gateway encodes and the reverse, so both copies of the
// vector table must stay the same. The manager is not a dependency of the gateway: the
// test compares the source of the two tables when the repository is checked out whole.
func TestStationKeyVectorsMatchTheManager(t *testing.T) {
	gateway, err := os.ReadFile("subjects_test.go")
	require.NoError(t, err)
	manager, err := os.ReadFile("../../../manager/transport/nats/subjects_test.go")
	if os.IsNotExist(err) {
		t.Skip("the manager module is not checked out next to the gateway")
	}
	require.NoError(t, err)

	assert.Equal(t, vectorTable(t, string(manager)), vectorTable(t, string(gateway)),
		"the station key vectors of the gateway and the manager differ")
}

// vectorTable returns the source of the stationKeyVectors declaration.
func vectorTable(t *testing.T, source string) string {
	t.Helper()
	start := strings.Index(source, "var stationKeyVectors = ")
	require.GreaterOrEqual(t, start, 0, "no stationKeyVectors table")
	end := strings.Index(source[start:], "\n}\n")
	require.GreaterOrEqual(t, end, 0, "stationKeyVectors table not closed")
	return source[start : start+end+3]
}

func TestEncodeStationKey(t *testing.T) {
	for _, v := range stationKeyVectors {
		t.Run(v.key, func(t *testing.T) {
			orgToken, stationToken := natstransport.EncodeStationKey(v.key)
			assert.Equal(t, v.orgToken, orgToken)
			assert.Equal(t, v.stationToken, stationToken)
		})
	}
}

func TestDecodeStationKey(t *testing.T) {
	for _, v := range stationKeyVectors {
		t.Run(v.key, func(t *testing.T) {
			key, err := natstransport.DecodeStationKey(v.orgToken, v.stationToken)
			require.NoError(t, err)
			assert.Equal(t, v.key, key)
		})
	}
}

func TestDecodeStationKeyRejectsTokensThatEncodeDoesNotProduce(t *testing.T) {
	for name, tc := range map[string]struct{ orgToken, stationToken string }{
		"empty station token":           {"k7f2", ""},
		"empty org token":               {"", "CP001"},
		"org token with a space":        {"k7 f2", "CP001"},
		"org token with a wildcard":     {"*", "CP001"},
		"truncated escape":              {"k7f2", "CP%2"},
		"escape at the end":             {"k7f2", "CP%"},
		"escape that is not hex":        {"k7f2", "CP%G1"},
		"lowercase hex":                 {"k7f2", "CP%2e001"},
		"escape of a raw byte":          {"k7f2", "%43P001"},
		"raw byte that needs escaping":  {"k7f2", "CP!001"},
		"org part encoded in the token": {"_", "k7f2%2ECP001"},
		"dotted id encoded whole":       {"_", "CP%2E001"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := natstransport.DecodeStationKey(tc.orgToken, tc.stationToken)
			assert.Error(t, err)
		})
	}
}

func TestVersionToken(t *testing.T) {
	tok, err := natstransport.VersionToken("ocpp1.6")
	require.NoError(t, err)
	assert.Equal(t, "ocpp16", tok)

	tok, err = natstransport.VersionToken("ocpp2.0.1")
	require.NoError(t, err)
	assert.Equal(t, "ocpp201", tok)

	_, err = natstransport.VersionToken("ocpp2.1")
	assert.Error(t, err)
	_, err = natstransport.VersionToken("")
	assert.Error(t, err)

	protocol, err := natstransport.VersionFromToken("ocpp16")
	require.NoError(t, err)
	assert.Equal(t, "ocpp1.6", protocol)

	protocol, err = natstransport.VersionFromToken("ocpp201")
	require.NoError(t, err)
	assert.Equal(t, "ocpp2.0.1", protocol)

	_, err = natstransport.VersionFromToken("ocpp1.6")
	assert.Error(t, err)
}

func TestSubjects(t *testing.T) {
	in, err := natstransport.InSubject("ocpp", "ocpp1.6", "k7f2.CP.001")
	require.NoError(t, err)
	assert.Equal(t, "ocpp.in.ocpp16.k7f2.CP%2E001", in)

	out, err := natstransport.OutSubject("ocpp", "ocpp2.0.1", "CP001")
	require.NoError(t, err)
	assert.Equal(t, "ocpp.out.ocpp201._.CP001", out)

	_, err = natstransport.InSubject("ocpp", "ocpp1.6", "")
	assert.Error(t, err, "empty key")
	_, err = natstransport.OutSubject("ocpp", "ocpp2.1", "k7f2.CP001")
	assert.Error(t, err, "unknown protocol")
	for _, prefix := range []string{"", "a.b", "a b", "*", ">"} {
		_, err = natstransport.InSubject(prefix, "ocpp1.6", "k7f2.CP001")
		assert.Error(t, err, "prefix %q", prefix)
		_, err = natstransport.OutSubject(prefix, "ocpp1.6", "k7f2.CP001")
		assert.Error(t, err, "prefix %q", prefix)
		assert.Error(t, natstransport.ValidatePrefix(prefix), "prefix %q", prefix)
	}
	assert.NoError(t, natstransport.ValidatePrefix("csms-1_a"))
}

func TestSubjectsRoundTrip(t *testing.T) {
	for _, v := range stationKeyVectors {
		for _, protocol := range []string{"ocpp1.6", "ocpp2.0.1"} {
			in, err := natstransport.InSubject("ocpp", protocol, v.key)
			require.NoError(t, err)
			direction, gotProtocol, key, err := natstransport.ParseStationSubject(in)
			require.NoError(t, err, in)
			assert.Equal(t, natstransport.DirectionIn, direction)
			assert.Equal(t, protocol, gotProtocol)
			assert.Equal(t, v.key, key)

			out, err := natstransport.OutSubject("ocpp", protocol, v.key)
			require.NoError(t, err)
			direction, gotProtocol, key, err = natstransport.ParseStationSubject(out)
			require.NoError(t, err, out)
			assert.Equal(t, natstransport.DirectionOut, direction)
			assert.Equal(t, protocol, gotProtocol)
			assert.Equal(t, v.key, key)
		}
	}
}

func TestParseStationSubjectRejectsBadSubjects(t *testing.T) {
	for _, subject := range []string{
		"",
		"ocpp.in.ocpp16.k7f2",
		"ocpp.in.ocpp16.k7f2.CP.001",
		"x.ocpp.in.ocpp16.k7f2.CP001",
		"ocpp.in.ocpp16.k7f2.",
		"ocpp.sideways.ocpp16.k7f2.CP001",
		"ocpp.in.ocpp1.6.k7f2.CP001",
		"ocpp.in.ocpp21.k7f2.CP001",
		"ocpp.in.ocpp16.k7f2.CP%2",
		"ocpp.in.ocpp16.k7f2.CP%ZZ",
		"ocpp.in.ocpp16.k7f2.CP%2e001",
		"ocpp.in.ocpp16.k 7f2.CP001",
		"ocpp.in.ocpp16.*.*",
	} {
		_, _, _, err := natstransport.ParseStationSubject(subject)
		assert.Error(t, err, "subject %q", subject)
	}
}
