// SPDX-License-Identifier: Apache-2.0

package config

import (
	"bytes"
	"log"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/exp/slog"
)

// The manager cannot read the alive interval of the gateways: a stale_after that does
// not leave room for one lost alive event at the gateway default is only warned about.
func TestParseConnectivityConfigWarnsAboutAShortStaleAfter(t *testing.T) {
	var logs bytes.Buffer
	previous, previousOutput, previousFlags := slog.Default(), log.Writer(), log.Flags()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer func() {
		// SetDefault also sends the output of the log package to the handler
		slog.SetDefault(previous)
		log.SetOutput(previousOutput)
		log.SetFlags(previousFlags)
	}()

	for staleAfter, warns := range map[string]bool{"5m": false, "4m1s": false, "4m": true, "1m": true} {
		logs.Reset()
		_, err := parseConnectivityConfig(&ConnectivityConfig{LastSeenWriteInterval: "30s", StaleAfter: staleAfter, SweepInterval: "30s"})
		require.NoError(t, err)
		assert.Equal(t, warns, strings.Contains(logs.String(), "level=WARN"), "stale_after %s: %s", staleAfter, logs.String())
	}
}
