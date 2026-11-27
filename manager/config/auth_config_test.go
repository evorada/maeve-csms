// SPDX-License-Identifier: Apache-2.0

package config

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"golang.org/x/exp/slog"
)

func TestDevModeIsLoudAtStartup(t *testing.T) {
	var logged bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))
	defer slog.SetDefault(previous)

	cfg := DefaultConfig.Api.Auth
	cfg.Mode = "dev"

	t.Setenv("ENVIRONMENT", "production")
	verifier, err := getAuthVerifier(context.Background(), &cfg, store.LegacyOrgId)
	require.Error(t, err)
	assert.Nil(t, verifier)
	assert.Empty(t, logged.String())

	t.Setenv("ENVIRONMENT", "dev")
	verifier, err = getAuthVerifier(context.Background(), &cfg, store.LegacyOrgId)
	require.NoError(t, err)
	assert.IsType(t, tenancy.DevVerifier{}, verifier)
	assert.Contains(t, logged.String(), "level=WARN")
	assert.Contains(t, logged.String(), "API authentication disabled (dev mode)")
}
