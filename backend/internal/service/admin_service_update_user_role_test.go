//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdminService_UpdateUser_ProtectedRoleInvalidatesAuthCache(t *testing.T) {
	base := &userRepoStub{user: &User{ID: 42, Email: "u@example.com", Role: RoleUser}}
	repo := &rpmUserRepoStub{userRepoStub: base}
	invalidator := &authCacheInvalidatorStub{}
	svc := &adminServiceImpl{
		userRepo:             repo,
		redeemCodeRepo:       &redeemRepoStub{},
		authCacheInvalidator: invalidator,
	}

	updated, err := svc.UpdateUser(context.Background(), 42, &UpdateUserInput{
		Role: RoleProtected,
	})
	require.NoError(t, err)
	require.Equal(t, RoleProtected, updated.Role)
	require.Equal(t, RoleProtected, repo.lastUpdated.Role)
	require.Equal(t, []int64{42}, invalidator.userIDs)
}

func TestAdminService_UpdateUser_CannotChangeAdminRole(t *testing.T) {
	base := &userRepoStub{user: &User{ID: 1, Email: "admin@example.com", Role: RoleAdmin}}
	repo := &rpmUserRepoStub{userRepoStub: base}
	invalidator := &authCacheInvalidatorStub{}
	svc := &adminServiceImpl{
		userRepo:             repo,
		redeemCodeRepo:       &redeemRepoStub{},
		authCacheInvalidator: invalidator,
	}

	_, err := svc.UpdateUser(context.Background(), 1, &UpdateUserInput{
		Role: RoleProtected,
	})
	require.EqualError(t, err, "cannot change admin user role")
	require.Nil(t, repo.lastUpdated)
	require.Empty(t, invalidator.userIDs)
}

func TestAdminService_UpdateUser_CanDisableDailyCheckInWithoutAuthCacheInvalidation(t *testing.T) {
	base := &userRepoStub{user: &User{ID: 42, Email: "u@example.com", Role: RoleUser}}
	repo := &rpmUserRepoStub{userRepoStub: base}
	invalidator := &authCacheInvalidatorStub{}
	svc := &adminServiceImpl{
		userRepo:             repo,
		redeemCodeRepo:       &redeemRepoStub{},
		authCacheInvalidator: invalidator,
	}
	enabled := false

	updated, err := svc.UpdateUser(context.Background(), 42, &UpdateUserInput{
		DailyCheckInEnabled: &enabled,
	})
	require.NoError(t, err)
	require.True(t, updated.DailyCheckInDisabled)
	require.True(t, repo.lastUpdated.DailyCheckInDisabled)
	require.Empty(t, invalidator.userIDs)
}
