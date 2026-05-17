package service

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/authidentity"
	"github.com/Wei-Shaw/sub2api/ent/authidentitychannel"
	"github.com/Wei-Shaw/sub2api/ent/identityadoptiondecision"
	"github.com/Wei-Shaw/sub2api/ent/redeemcode"
	dbuser "github.com/Wei-Shaw/sub2api/ent/user"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/lib/pq"
)

const (
	CleanupReasonInactiveLogin = "inactive_login"
	CleanupReasonMissingAPIKey = "missing_api_key"
	CleanupReasonNoUsage       = "no_usage"
)

type CleanupRulesCutoffs struct {
	InvitationCodeBefore time.Time `json:"invitation_code_before"`
	BalanceCodeBefore    time.Time `json:"balance_code_before"`
	InactiveLoginBefore  time.Time `json:"inactive_login_before"`
	MissingAPIKeyBefore  time.Time `json:"missing_api_key_before"`
	NoUsageBefore        time.Time `json:"no_usage_before"`
}

type CleanupRulesCodeCandidate struct {
	ID        int64     `json:"id"`
	Code      string    `json:"code"`
	Type      string    `json:"type"`
	Value     float64   `json:"value"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type CleanupRulesUserCandidate struct {
	ID           int64      `json:"id"`
	Email        string     `json:"email"`
	Username     string     `json:"username"`
	SignupSource string     `json:"signup_source"`
	LastLoginAt  *time.Time `json:"last_login_at,omitempty"`
	LastActiveAt *time.Time `json:"last_active_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	Reasons      []string   `json:"reasons"`
}

type CleanupRulesCodeBucket struct {
	Count  int                         `json:"count"`
	Sample []CleanupRulesCodeCandidate `json:"sample"`
}

type CleanupRulesRedeemCodePreview struct {
	Invitation CleanupRulesCodeBucket `json:"invitation"`
	Balance    CleanupRulesCodeBucket `json:"balance"`
	Total      int                    `json:"total"`
}

type CleanupRulesUserPreview struct {
	Count        int                         `json:"count"`
	ReasonCounts map[string]int              `json:"reason_counts"`
	Sample       []CleanupRulesUserCandidate `json:"sample"`
}

type CleanupRulesPreview struct {
	Settings        *CleanupRulesSettings         `json:"settings"`
	Cutoffs         CleanupRulesCutoffs           `json:"cutoffs"`
	RedeemCodes     CleanupRulesRedeemCodePreview `json:"redeem_codes"`
	Users           CleanupRulesUserPreview       `json:"users"`
	TotalCandidates int                           `json:"total_candidates"`
}

type CleanupRulesRunResult struct {
	Preview                     *CleanupRulesPreview `json:"preview"`
	DeletedInvitationCodes      int                  `json:"deleted_invitation_codes"`
	DeletedBalanceCodes         int                  `json:"deleted_balance_codes"`
	SoftDeletedAPIKeys          int64                `json:"soft_deleted_api_keys"`
	DeletedAuthIdentities       int                  `json:"deleted_auth_identities"`
	DeletedUserAffiliates       int64                `json:"deleted_user_affiliates"`
	ClearedAffiliateInviterRefs int64                `json:"cleared_affiliate_inviter_refs"`
	SoftDeletedUsers            int                  `json:"soft_deleted_users"`
}

type CleanupRulesService struct {
	entClient            *dbent.Client
	settingService       *SettingService
	authCacheInvalidator APIKeyAuthCacheInvalidator
	billingCacheService  *BillingCacheService
}

type cleanupUserQueryExecer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

type cleanupUserExecQueryer interface {
	cleanupUserQueryExecer
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

type cleanupQueryExecutor interface {
	cleanupUserExecQueryer
}

type cleanupCandidates struct {
	settings        *CleanupRulesSettings
	cutoffs         CleanupRulesCutoffs
	invitationCodes []CleanupRulesCodeCandidate
	balanceCodes    []CleanupRulesCodeCandidate
	invitationCount int
	balanceCount    int
	users           []CleanupRulesUserCandidate
	userCount       int
	reasonCounts    map[string]int
}

func NewCleanupRulesService(
	entClient *dbent.Client,
	settingService *SettingService,
	authCacheInvalidator APIKeyAuthCacheInvalidator,
	billingCacheService *BillingCacheService,
) *CleanupRulesService {
	return &CleanupRulesService{
		entClient:            entClient,
		settingService:       settingService,
		authCacheInvalidator: authCacheInvalidator,
		billingCacheService:  billingCacheService,
	}
}

func (s *CleanupRulesService) Preview(ctx context.Context) (*CleanupRulesPreview, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	settings, err := s.settingService.GetCleanupRulesSettings(ctx)
	if err != nil {
		return nil, err
	}
	candidates, err := s.collectCandidates(ctx, s.entClient, s.entClient, settings, false)
	if err != nil {
		return nil, err
	}
	return cleanupPreviewFromCandidates(candidates), nil
}

func (s *CleanupRulesService) Run(ctx context.Context) (*CleanupRulesRunResult, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	settings, err := s.settingService.GetCleanupRulesSettings(ctx)
	if err != nil {
		return nil, err
	}

	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin cleanup transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	txCtx := dbent.NewTxContext(ctx, tx)
	txClient := tx.Client()
	candidates, err := s.collectCandidates(txCtx, txClient, txClient, settings, true)
	if err != nil {
		return nil, err
	}

	result := &CleanupRulesRunResult{
		Preview: cleanupPreviewFromCandidates(candidates),
	}

	if candidates.invitationCount > 0 {
		deleted, err := txClient.RedeemCode.Delete().
			Where(
				redeemcode.TypeEQ(RedeemTypeInvitation),
				redeemcode.StatusEQ(StatusUnused),
				redeemcode.CreatedAtLT(candidates.cutoffs.InvitationCodeBefore),
			).
			Exec(txCtx)
		if err != nil {
			return nil, fmt.Errorf("delete unused invitation codes: %w", err)
		}
		result.DeletedInvitationCodes = deleted
	}

	if candidates.balanceCount > 0 {
		deleted, err := txClient.RedeemCode.Delete().
			Where(
				redeemcode.TypeEQ(RedeemTypeBalance),
				redeemcode.StatusEQ(StatusUnused),
				redeemcode.CreatedAtLT(candidates.cutoffs.BalanceCodeBefore),
			).
			Exec(txCtx)
		if err != nil {
			return nil, fmt.Errorf("delete unused balance codes: %w", err)
		}
		result.DeletedBalanceCodes = deleted
	}

	userIDs := cleanupUserCandidateIDs(candidates.users)
	activeKeys := []string{}
	if len(userIDs) > 0 {
		activeKeys, err = cleanupListActiveAPIKeysForUsers(txCtx, txClient, userIDs)
		if err != nil {
			return nil, fmt.Errorf("list active api keys before cleanup: %w", err)
		}

		softDeletedKeys, err := cleanupSoftDeleteAPIKeysForUsers(txCtx, txClient, userIDs)
		if err != nil {
			return nil, fmt.Errorf("soft delete user api keys: %w", err)
		}
		result.SoftDeletedAPIKeys = softDeletedKeys

		if settings.CleanupAffiliateEnabled {
			affiliateResult, err := cleanupUserAffiliates(txCtx, txClient, userIDs)
			if err != nil {
				return nil, fmt.Errorf("cleanup user affiliates: %w", err)
			}
			result.DeletedUserAffiliates = affiliateResult.deletedRows
			result.ClearedAffiliateInviterRefs = affiliateResult.clearedInviterRefs
		}

		deletedIdentities, err := cleanupAuthIdentitiesForUsers(txCtx, txClient, userIDs)
		if err != nil {
			return nil, fmt.Errorf("cleanup auth identities: %w", err)
		}
		result.DeletedAuthIdentities = deletedIdentities

		now := time.Now().UTC()
		deletedUsers, err := txClient.User.Update().
			Where(
				dbuser.IDIn(userIDs...),
				dbuser.RoleNEQ(RoleAdmin),
				dbuser.RoleNEQ(RoleProtected),
				dbuser.DeletedAtIsNil(),
			).
			SetStatus(StatusDisabled).
			SetDeletedAt(now).
			Save(txCtx)
		if err != nil {
			return nil, fmt.Errorf("soft delete cleanup users: %w", err)
		}
		result.SoftDeletedUsers = deletedUsers
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit cleanup transaction: %w", err)
	}

	s.invalidateCleanedUsers(ctx, userIDs, activeKeys)
	return result, nil
}

func (s *CleanupRulesService) ready() error {
	if s == nil || s.entClient == nil || s.settingService == nil {
		return infraerrors.ServiceUnavailable("CLEANUP_RULES_UNAVAILABLE", "cleanup rules service unavailable")
	}
	return nil
}

func (s *CleanupRulesService) collectCandidates(ctx context.Context, client *dbent.Client, sqlExec cleanupQueryExecutor, settings *CleanupRulesSettings, allUsers bool) (*cleanupCandidates, error) {
	if settings == nil {
		settings = DefaultCleanupRulesSettings()
	}
	cutoffs := cleanupCutoffs(settings, time.Now().UTC())
	limit := settings.PreviewSampleLimit

	candidates := &cleanupCandidates{
		settings:     settings,
		cutoffs:      cutoffs,
		reasonCounts: cleanupEmptyReasonCounts(),
	}

	var err error
	if settings.InvitationCodeEnabled {
		candidates.invitationCodes, candidates.invitationCount, err = s.collectCodeCandidates(
			ctx,
			client,
			RedeemTypeInvitation,
			cutoffs.InvitationCodeBefore,
			limit,
		)
		if err != nil {
			return nil, err
		}
	}

	if settings.BalanceCodeEnabled {
		candidates.balanceCodes, candidates.balanceCount, err = s.collectCodeCandidates(
			ctx,
			client,
			RedeemTypeBalance,
			cutoffs.BalanceCodeBefore,
			limit,
		)
		if err != nil {
			return nil, err
		}
	}

	candidates.users, candidates.userCount, candidates.reasonCounts, err = cleanupCollectUserCandidates(ctx, sqlExec, settings, cutoffs, limit, allUsers)
	if err != nil {
		return nil, err
	}

	return candidates, nil
}

func (s *CleanupRulesService) collectCodeCandidates(ctx context.Context, client *dbent.Client, codeType string, cutoff time.Time, sampleLimit int) ([]CleanupRulesCodeCandidate, int, error) {
	q := client.RedeemCode.Query().
		Where(
			redeemcode.TypeEQ(codeType),
			redeemcode.StatusEQ(StatusUnused),
			redeemcode.CreatedAtLT(cutoff),
		)

	count, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count cleanup code candidates: %w", err)
	}
	if count == 0 {
		return []CleanupRulesCodeCandidate{}, 0, nil
	}

	models, err := q.
		Order(dbent.Asc(redeemcode.FieldCreatedAt), dbent.Asc(redeemcode.FieldID)).
		Limit(sampleLimit).
		All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list cleanup code candidates: %w", err)
	}

	out := make([]CleanupRulesCodeCandidate, 0, len(models))
	for _, m := range models {
		out = append(out, CleanupRulesCodeCandidate{
			ID:        m.ID,
			Code:      m.Code,
			Type:      m.Type,
			Value:     m.Value,
			Status:    m.Status,
			CreatedAt: m.CreatedAt,
		})
	}
	return out, count, nil
}

func cleanupCutoffs(settings *CleanupRulesSettings, now time.Time) CleanupRulesCutoffs {
	return CleanupRulesCutoffs{
		InvitationCodeBefore: now.Add(-time.Duration(settings.InvitationCodeTTLHours) * time.Hour),
		BalanceCodeBefore:    now.Add(-time.Duration(settings.BalanceCodeTTLHours) * time.Hour),
		InactiveLoginBefore:  now.Add(-time.Duration(settings.InactiveLoginTTLHours) * time.Hour),
		MissingAPIKeyBefore:  now.Add(-time.Duration(settings.MissingAPIKeyTTLHours) * time.Hour),
		NoUsageBefore:        now.Add(-time.Duration(settings.NoUsageTTLHours) * time.Hour),
	}
}

func cleanupCollectUserCandidates(ctx context.Context, q cleanupUserQueryExecer, settings *CleanupRulesSettings, cutoffs CleanupRulesCutoffs, sampleLimit int, all bool) ([]CleanupRulesUserCandidate, int, map[string]int, error) {
	reasonCounts := cleanupEmptyReasonCounts()
	if !settings.InactiveLoginEnabled && !settings.MissingAPIKeyEnabled && !settings.NoUsageEnabled {
		return []CleanupRulesUserCandidate{}, 0, reasonCounts, nil
	}

	args := cleanupUserCandidateArgs(settings, cutoffs)
	countSQL := `
SELECT COUNT(*)::bigint,
       COALESCE(SUM(CASE WHEN ($1::boolean AND u.created_at < $2 AND u.last_login_at IS NULL) THEN 1 ELSE 0 END), 0)::bigint,
       COALESCE(SUM(CASE WHEN ($3::boolean AND u.created_at < $4 AND NOT EXISTS (SELECT 1 FROM api_keys ak WHERE ak.user_id = u.id)) THEN 1 ELSE 0 END), 0)::bigint,
       COALESCE(SUM(CASE WHEN ($5::boolean AND u.created_at < $6 AND NOT EXISTS (SELECT 1 FROM usage_logs ul WHERE ul.user_id = u.id)) THEN 1 ELSE 0 END), 0)::bigint
FROM users u
WHERE u.deleted_at IS NULL
  AND u.role <> ALL($7::text[])
  AND (
       ($1::boolean AND u.created_at < $2 AND u.last_login_at IS NULL)
       OR ($3::boolean AND u.created_at < $4 AND NOT EXISTS (SELECT 1 FROM api_keys ak WHERE ak.user_id = u.id))
       OR ($5::boolean AND u.created_at < $6 AND NOT EXISTS (SELECT 1 FROM usage_logs ul WHERE ul.user_id = u.id))
  )`
	rows, err := q.QueryContext(ctx, countSQL, args...)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("count cleanup user candidates: %w", err)
	}
	var total, inactiveCount, missingKeyCount, noUsageCount int64
	if rows.Next() {
		if err := rows.Scan(&total, &inactiveCount, &missingKeyCount, &noUsageCount); err != nil {
			_ = rows.Close()
			return nil, 0, nil, err
		}
	}
	if err := rows.Close(); err != nil {
		return nil, 0, nil, err
	}
	reasonCounts[CleanupReasonInactiveLogin] = int(inactiveCount)
	reasonCounts[CleanupReasonMissingAPIKey] = int(missingKeyCount)
	reasonCounts[CleanupReasonNoUsage] = int(noUsageCount)
	if total == 0 {
		return []CleanupRulesUserCandidate{}, 0, reasonCounts, nil
	}

	listSQL := `
SELECT u.id,
       u.email,
       u.username,
       u.signup_source,
       u.last_login_at,
       u.last_active_at,
       u.created_at,
       ($1::boolean AND u.created_at < $2 AND u.last_login_at IS NULL) AS inactive_login,
       ($3::boolean AND u.created_at < $4 AND NOT EXISTS (SELECT 1 FROM api_keys ak WHERE ak.user_id = u.id)) AS missing_api_key,
       ($5::boolean AND u.created_at < $6 AND NOT EXISTS (SELECT 1 FROM usage_logs ul WHERE ul.user_id = u.id)) AS no_usage
FROM users u
WHERE u.deleted_at IS NULL
  AND u.role <> ALL($7::text[])
  AND (
       ($1::boolean AND u.created_at < $2 AND u.last_login_at IS NULL)
       OR ($3::boolean AND u.created_at < $4 AND NOT EXISTS (SELECT 1 FROM api_keys ak WHERE ak.user_id = u.id))
       OR ($5::boolean AND u.created_at < $6 AND NOT EXISTS (SELECT 1 FROM usage_logs ul WHERE ul.user_id = u.id))
  )
ORDER BY u.created_at ASC, u.id ASC`
	if !all {
		listSQL += " LIMIT $8"
		args = append(args, sampleLimit)
	}

	rows, err = q.QueryContext(ctx, listSQL, args...)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("list cleanup user candidates: %w", err)
	}
	defer func() { _ = rows.Close() }()

	users := make([]CleanupRulesUserCandidate, 0)
	for rows.Next() {
		var item CleanupRulesUserCandidate
		var lastLogin, lastActive sql.NullTime
		var inactiveLogin, missingAPIKey, noUsage bool
		if err := rows.Scan(
			&item.ID,
			&item.Email,
			&item.Username,
			&item.SignupSource,
			&lastLogin,
			&lastActive,
			&item.CreatedAt,
			&inactiveLogin,
			&missingAPIKey,
			&noUsage,
		); err != nil {
			return nil, 0, nil, err
		}
		if lastLogin.Valid {
			item.LastLoginAt = &lastLogin.Time
		}
		if lastActive.Valid {
			item.LastActiveAt = &lastActive.Time
		}
		if inactiveLogin {
			item.Reasons = append(item.Reasons, CleanupReasonInactiveLogin)
		}
		if missingAPIKey {
			item.Reasons = append(item.Reasons, CleanupReasonMissingAPIKey)
		}
		if noUsage {
			item.Reasons = append(item.Reasons, CleanupReasonNoUsage)
		}
		users = append(users, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, nil, err
	}
	return users, int(total), reasonCounts, nil
}

func cleanupUserCandidateArgs(settings *CleanupRulesSettings, cutoffs CleanupRulesCutoffs) []any {
	return []any{
		settings.InactiveLoginEnabled,
		cutoffs.InactiveLoginBefore,
		settings.MissingAPIKeyEnabled,
		cutoffs.MissingAPIKeyBefore,
		settings.NoUsageEnabled,
		cutoffs.NoUsageBefore,
		pq.Array([]string{RoleAdmin, RoleProtected}),
	}
}

func cleanupEmptyReasonCounts() map[string]int {
	return map[string]int{
		CleanupReasonInactiveLogin: 0,
		CleanupReasonMissingAPIKey: 0,
		CleanupReasonNoUsage:       0,
	}
}

func cleanupPreviewFromCandidates(candidates *cleanupCandidates) *CleanupRulesPreview {
	if candidates == nil {
		return nil
	}
	limit := candidates.settings.PreviewSampleLimit
	invitationSample := candidates.invitationCodes
	if len(invitationSample) > limit {
		invitationSample = invitationSample[:limit]
	}
	balanceSample := candidates.balanceCodes
	if len(balanceSample) > limit {
		balanceSample = balanceSample[:limit]
	}
	userSample := candidates.users
	if len(userSample) > limit {
		userSample = userSample[:limit]
	}
	totalCodes := candidates.invitationCount + candidates.balanceCount
	return &CleanupRulesPreview{
		Settings: candidates.settings,
		Cutoffs:  candidates.cutoffs,
		RedeemCodes: CleanupRulesRedeemCodePreview{
			Invitation: CleanupRulesCodeBucket{Count: candidates.invitationCount, Sample: invitationSample},
			Balance:    CleanupRulesCodeBucket{Count: candidates.balanceCount, Sample: balanceSample},
			Total:      totalCodes,
		},
		Users: CleanupRulesUserPreview{
			Count:        candidates.userCount,
			ReasonCounts: candidates.reasonCounts,
			Sample:       userSample,
		},
		TotalCandidates: totalCodes + candidates.userCount,
	}
}

func cleanupUserCandidateIDs(items []CleanupRulesUserCandidate) []int64 {
	ids := make([]int64, 0, len(items))
	seen := make(map[int64]struct{}, len(items))
	for _, item := range items {
		if item.ID <= 0 {
			continue
		}
		if _, ok := seen[item.ID]; ok {
			continue
		}
		seen[item.ID] = struct{}{}
		ids = append(ids, item.ID)
	}
	return ids
}

func cleanupListActiveAPIKeysForUsers(ctx context.Context, q cleanupUserQueryExecer, userIDs []int64) ([]string, error) {
	if len(userIDs) == 0 {
		return []string{}, nil
	}
	rows, err := q.QueryContext(ctx, `SELECT key FROM api_keys WHERE user_id = ANY($1) AND deleted_at IS NULL`, pq.Array(userIDs))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	keys := make([]string, 0)
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

func cleanupSoftDeleteAPIKeysForUsers(ctx context.Context, q cleanupUserExecQueryer, userIDs []int64) (int64, error) {
	if len(userIDs) == 0 {
		return 0, nil
	}
	res, err := q.ExecContext(ctx, `
UPDATE api_keys
SET key = CONCAT('__deleted__', id::text, '__', FLOOR(EXTRACT(EPOCH FROM clock_timestamp()) * 1000000000)::bigint::text),
    deleted_at = NOW(),
    updated_at = NOW()
WHERE user_id = ANY($1)
  AND deleted_at IS NULL`, pq.Array(userIDs))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

type cleanupAffiliateResult struct {
	deletedRows        int64
	clearedInviterRefs int64
}

func cleanupUserAffiliates(ctx context.Context, q cleanupUserExecQueryer, userIDs []int64) (cleanupAffiliateResult, error) {
	if len(userIDs) == 0 {
		return cleanupAffiliateResult{}, nil
	}

	if _, err := q.ExecContext(ctx, `
WITH invitee_counts AS (
    SELECT inviter_id, COUNT(*)::integer AS cnt
    FROM user_affiliates
    WHERE user_id = ANY($1)
      AND inviter_id IS NOT NULL
    GROUP BY inviter_id
)
UPDATE user_affiliates ua
SET aff_count = GREATEST(ua.aff_count - invitee_counts.cnt, 0),
    updated_at = NOW()
FROM invitee_counts
WHERE ua.user_id = invitee_counts.inviter_id`, pq.Array(userIDs)); err != nil {
		return cleanupAffiliateResult{}, err
	}

	clearRes, err := q.ExecContext(ctx, `
UPDATE user_affiliates
SET inviter_id = NULL,
    updated_at = NOW()
WHERE inviter_id = ANY($1)`, pq.Array(userIDs))
	if err != nil {
		return cleanupAffiliateResult{}, err
	}
	cleared, _ := clearRes.RowsAffected()

	deleteRes, err := q.ExecContext(ctx, `DELETE FROM user_affiliates WHERE user_id = ANY($1)`, pq.Array(userIDs))
	if err != nil {
		return cleanupAffiliateResult{}, err
	}
	deleted, _ := deleteRes.RowsAffected()

	return cleanupAffiliateResult{deletedRows: deleted, clearedInviterRefs: cleared}, nil
}

func cleanupAuthIdentitiesForUsers(ctx context.Context, txClient *dbent.Client, userIDs []int64) (int, error) {
	if len(userIDs) == 0 {
		return 0, nil
	}
	identityIDs, err := txClient.AuthIdentity.Query().
		Where(authidentity.UserIDIn(userIDs...)).
		IDs(ctx)
	if err != nil {
		return 0, err
	}
	if len(identityIDs) == 0 {
		return 0, nil
	}

	if _, err := txClient.IdentityAdoptionDecision.Update().
		Where(identityadoptiondecision.IdentityIDIn(identityIDs...)).
		ClearIdentityID().
		Save(ctx); err != nil {
		return 0, err
	}
	if _, err := txClient.AuthIdentityChannel.Delete().
		Where(authidentitychannel.IdentityIDIn(identityIDs...)).
		Exec(ctx); err != nil {
		return 0, err
	}
	deleted, err := txClient.AuthIdentity.Delete().
		Where(authidentity.IDIn(identityIDs...)).
		Exec(ctx)
	if err != nil {
		return 0, err
	}
	return deleted, nil
}

func (s *CleanupRulesService) invalidateCleanedUsers(ctx context.Context, userIDs []int64, activeKeys []string) {
	if s.authCacheInvalidator != nil {
		for _, key := range activeKeys {
			s.authCacheInvalidator.InvalidateAuthCacheByKey(ctx, key)
		}
		for _, userID := range userIDs {
			s.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, userID)
		}
	}
	if s.billingCacheService != nil {
		for _, userID := range userIDs {
			_ = s.billingCacheService.InvalidateUserBalance(ctx, userID)
		}
	}
}
