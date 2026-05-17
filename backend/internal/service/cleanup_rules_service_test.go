//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestCleanupCollectUserCandidates_ExcludesAdminAndProtectedRoles(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	settings := DefaultCleanupRulesSettings()
	settings.PreviewSampleLimit = 10
	cutoffs := CleanupRulesCutoffs{
		InactiveLoginBefore: time.Date(2026, 5, 13, 0, 0, 0, 0, time.UTC),
		MissingAPIKeyBefore: time.Date(2026, 5, 13, 0, 0, 0, 0, time.UTC),
		NoUsageBefore:       time.Date(2026, 5, 9, 0, 0, 0, 0, time.UTC),
	}
	excludedRoles := pq.Array([]string{RoleAdmin, RoleProtected})

	mock.ExpectQuery("SELECT COUNT\\(\\*\\)::bigint").
		WithArgs(
			settings.InactiveLoginEnabled,
			cutoffs.InactiveLoginBefore,
			settings.MissingAPIKeyEnabled,
			cutoffs.MissingAPIKeyBefore,
			settings.NoUsageEnabled,
			cutoffs.NoUsageBefore,
			excludedRoles,
		).
		WillReturnRows(sqlmock.NewRows([]string{"count", "inactive", "missing_key", "no_usage"}).
			AddRow(int64(1), int64(1), int64(1), int64(1)))

	mock.ExpectQuery("SELECT u\\.id,").
		WithArgs(
			settings.InactiveLoginEnabled,
			cutoffs.InactiveLoginBefore,
			settings.MissingAPIKeyEnabled,
			cutoffs.MissingAPIKeyBefore,
			settings.NoUsageEnabled,
			cutoffs.NoUsageBefore,
			excludedRoles,
			settings.PreviewSampleLimit,
		).
		WillReturnRows(sqlmock.NewRows([]string{
			"id",
			"email",
			"username",
			"signup_source",
			"last_login_at",
			"last_active_at",
			"created_at",
			"inactive_login",
			"missing_api_key",
			"no_usage",
		}).AddRow(
			int64(42),
			"candidate@example.test",
			"candidate",
			"email",
			nil,
			nil,
			time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			true,
			true,
			true,
		))

	users, total, reasonCounts, err := cleanupCollectUserCandidates(context.Background(), db, settings, cutoffs, settings.PreviewSampleLimit, false)
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Equal(t, 1, reasonCounts[CleanupReasonInactiveLogin])
	require.Equal(t, 1, reasonCounts[CleanupReasonMissingAPIKey])
	require.Equal(t, 1, reasonCounts[CleanupReasonNoUsage])
	require.Len(t, users, 1)
	require.Equal(t, int64(42), users[0].ID)
	require.ElementsMatch(t, []string{CleanupReasonInactiveLogin, CleanupReasonMissingAPIKey, CleanupReasonNoUsage}, users[0].Reasons)
	require.NoError(t, mock.ExpectationsWereMet())
}
