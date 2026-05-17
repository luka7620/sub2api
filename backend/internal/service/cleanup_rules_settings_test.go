//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestGetCleanupRulesSettings_DefaultsWhenNotSet(t *testing.T) {
	repo := newMockSettingRepo()
	svc := NewSettingService(repo, &config.Config{})

	settings, err := svc.GetCleanupRulesSettings(context.Background())
	require.NoError(t, err)
	require.Equal(t, DefaultCleanupRulesSettings(), settings)
}

func TestSetCleanupRulesSettings_NormalizesAndPersists(t *testing.T) {
	repo := newMockSettingRepo()
	svc := NewSettingService(repo, &config.Config{})

	err := svc.SetCleanupRulesSettings(context.Background(), &CleanupRulesSettings{
		InvitationCodeEnabled:   true,
		InvitationCodeTTLHours:  0,
		BalanceCodeEnabled:      true,
		BalanceCodeTTLHours:     -1,
		InactiveLoginEnabled:    true,
		InactiveLoginTTLHours:   0,
		MissingAPIKeyEnabled:    true,
		MissingAPIKeyTTLHours:   -10,
		NoUsageEnabled:          true,
		NoUsageTTLHours:         0,
		CleanupAffiliateEnabled: true,
		PreviewSampleLimit:      999,
	})
	require.NoError(t, err)

	var stored CleanupRulesSettings
	require.NoError(t, json.Unmarshal([]byte(repo.data[SettingKeyCleanupRulesSettings]), &stored))
	require.Equal(t, 24, stored.InvitationCodeTTLHours)
	require.Equal(t, 24, stored.BalanceCodeTTLHours)
	require.Equal(t, 72, stored.InactiveLoginTTLHours)
	require.Equal(t, 72, stored.MissingAPIKeyTTLHours)
	require.Equal(t, 168, stored.NoUsageTTLHours)
	require.Equal(t, 500, stored.PreviewSampleLimit)
}
