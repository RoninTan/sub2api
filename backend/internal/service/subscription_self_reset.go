package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

const (
	SettingKeySubscriptionSelfReset = "subscription_self_reset"

	SelfResetPeriodCalendarMonth = "calendar_month"
	SelfResetPeriodRolling30D    = "rolling_30d"
	SelfResetPeriodCycle         = "subscription_cycle"

	SelfResetScopeDaily     = "daily"
	SelfResetScopeAll       = "all"
	SelfResetScopeExhausted = "exhausted"

	SelfResetPerUser         = "user"
	SelfResetPerSubscription = "subscription"
)

var (
	ErrSelfResetDisabled     = infraerrors.Forbidden("SELF_RESET_DISABLED", "self-reset is disabled")
	ErrSelfResetNotFound     = infraerrors.NotFound("SELF_RESET_NOT_FOUND", "subscription not found")
	ErrSelfResetNotOwner     = infraerrors.Forbidden("SELF_RESET_NOT_OWNER", "subscription does not belong to this user")
	ErrSelfResetNotActive    = infraerrors.Forbidden("SELF_RESET_NOT_ACTIVE", "subscription is not active")
	ErrSelfResetLimitReached = infraerrors.Conflict("SELF_RESET_LIMIT_REACHED", "no self-reset chances left in this period")
	ErrSelfResetCooldown     = infraerrors.TooManyRequests("SELF_RESET_COOLDOWN", "self-reset cooldown is active")
	ErrSelfResetScope        = infraerrors.BadRequest("SELF_RESET_SCOPE_INVALID", "no eligible quota window can be reset")
	ErrSelfResetNotSupported = infraerrors.BadRequest("SELF_RESET_NOT_SUPPORTED", "only subscription-type groups support self-reset")
	ErrSelfResetDeduct       = infraerrors.BadRequest("SELF_RESET_DEDUCT_INVALID", "not enough subscription days for this reset")
	ErrSelfResetBelowGate    = infraerrors.BadRequest("SELF_RESET_USAGE_GATE", "usage is below the configured self-reset threshold")
)

// SelfResetSettings is stored as JSON under the subscription_self_reset setting key.
type SelfResetSettings struct {
	Enabled         bool    `json:"enabled"`
	MaxResets       int     `json:"max_resets"`
	Period          string  `json:"period"`
	Scope           string  `json:"scope"`
	MinUsagePct     float64 `json:"min_usage_pct"`
	CooldownMinutes int     `json:"cooldown_minutes"`
	DeductDays      int     `json:"deduct_days"`
	Per             string  `json:"per"`
}

func DefaultSelfResetSettings() SelfResetSettings {
	return SelfResetSettings{
		Enabled:     true,
		MaxResets:   3,
		Period:      SelfResetPeriodCalendarMonth,
		Scope:       SelfResetScopeDaily,
		MinUsagePct: 0,
		Per:         SelfResetPerUser,
	}
}

func (s *SelfResetSettings) Normalize() {
	if s.MaxResets <= 0 {
		s.MaxResets = 3
	}
	if s.MaxResets > 100 {
		s.MaxResets = 100
	}
	switch s.Period {
	case SelfResetPeriodCalendarMonth, SelfResetPeriodRolling30D, SelfResetPeriodCycle:
	default:
		s.Period = SelfResetPeriodCalendarMonth
	}
	switch s.Scope {
	case SelfResetScopeDaily, SelfResetScopeAll, SelfResetScopeExhausted:
	default:
		s.Scope = SelfResetScopeDaily
	}
	switch s.Per {
	case SelfResetPerUser, SelfResetPerSubscription:
	default:
		s.Per = SelfResetPerUser
	}
	if s.MinUsagePct < 0 {
		s.MinUsagePct = 0
	}
	if s.MinUsagePct > 100 {
		s.MinUsagePct = 100
	}
	if s.CooldownMinutes < 0 {
		s.CooldownMinutes = 0
	}
	if s.DeductDays < 0 {
		s.DeductDays = 0
	}
	if s.DeductDays > 30 {
		s.DeductDays = 30
	}
}

func (s SelfResetSettings) Validate() error {
	if strings.TrimSpace(s.Period) == "" || strings.TrimSpace(s.Scope) == "" || strings.TrimSpace(s.Per) == "" {
		return infraerrors.BadRequest("SELF_RESET_CONFIG_INVALID", "period, scope and per are required")
	}
	return nil
}

type SelfResetStatus struct {
	SubscriptionID int64   `json:"subscription_id"`
	GroupID        int64   `json:"group_id"`
	GroupName      string  `json:"group_name"`
	Enabled        bool    `json:"enabled"`
	MaxResets      int     `json:"max_resets"`
	Used           int     `json:"used"`
	Remaining      int     `json:"remaining"`
	Period         string  `json:"period"`
	PeriodKey      string  `json:"period_key"`
	PeriodResetsAt string  `json:"period_resets_at"`
	Scope          string  `json:"scope"`
	CanReset       bool    `json:"can_reset"`
	Reason         string  `json:"reason"`
	DailyUsageUSD  float64 `json:"daily_usage_usd"`
	DailyLimitUSD  float64 `json:"daily_limit_usd,omitempty"`
}

type SelfResetResult struct {
	SubscriptionID int64  `json:"subscription_id"`
	Used           int    `json:"used"`
	Remaining      int    `json:"remaining"`
	MaxResets      int    `json:"max_resets"`
	Period         string `json:"period"`
	PeriodKey      string `json:"period_key"`
	Scope          string `json:"scope"`
}

type selfResetQueryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func (s *SubscriptionService) selfResetNow() time.Time {
	if s != nil && s.now != nil {
		return s.now()
	}
	return time.Now()
}

// GetSelfResetSettings reads the JSON setting; missing/corrupt values fall back to defaults.
func (s *SubscriptionService) GetSelfResetSettings(ctx context.Context) SelfResetSettings {
	cfg := DefaultSelfResetSettings()
	if s == nil || s.entClient == nil {
		return cfg
	}
	rows, err := s.entClient.QueryContext(ctx, `SELECT value FROM settings WHERE key = $1`, SettingKeySubscriptionSelfReset)
	if err != nil {
		return cfg
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return cfg
	}
	var raw string
	if err := rows.Scan(&raw); err != nil {
		return cfg
	}
	stored := cfg
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return cfg
	}
	stored.Normalize()
	return stored
}

// UpdateSelfResetSettings validates and upserts the JSON setting.
func (s *SubscriptionService) UpdateSelfResetSettings(ctx context.Context, cfg SelfResetSettings) (SelfResetSettings, error) {
	cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return cfg, err
	}
	if s == nil || s.entClient == nil {
		return cfg, fmt.Errorf("self-reset settings store is unavailable")
	}
	_, err = s.entClient.ExecContext(ctx,
		`INSERT INTO settings (key, value, updated_at) VALUES ($1, $2, now())
		 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`,
		SettingKeySubscriptionSelfReset, string(raw))
	if err != nil {
		return cfg, err
	}
	return cfg, nil
}

func (s *SubscriptionService) selfResetPeriodKey(now time.Time, period string, startsAt time.Time) string {
	switch period {
	case SelfResetPeriodRolling30D:
		return "rolling_30d"
	case SelfResetPeriodCycle:
		return "cycle:" + startsAt.UTC().Format("20060102T150405")
	default:
		return now.In(timezone.Location()).Format("2006-01")
	}
}

func (s *SubscriptionService) selfResetPeriodResetsAt(now time.Time, period string, startsAt time.Time) string {
	switch period {
	case SelfResetPeriodRolling30D:
		return now.Add(30 * 24 * time.Hour).Format(time.RFC3339)
	case SelfResetPeriodCycle:
		return startsAt.AddDate(0, 0, 30).Format(time.RFC3339)
	default:
		loc := timezone.Location()
		t := now.In(loc)
		return time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, loc).Format(time.RFC3339)
	}
}

func (s *SubscriptionService) selfResetCount(ctx context.Context, q selfResetQueryer, cfg SelfResetSettings, userID, subscriptionID int64, periodKey string, now time.Time) (int, error) {
	query := `SELECT COUNT(*) FROM subscription_self_reset_logs WHERE period_key = $1`
	args := []any{periodKey}
	if cfg.Period == SelfResetPeriodRolling30D {
		query = `SELECT COUNT(*) FROM subscription_self_reset_logs WHERE created_at >= $1`
		args = []any{now.Add(-30 * 24 * time.Hour)}
	}
	if cfg.Per == SelfResetPerSubscription {
		query += fmt.Sprintf(` AND subscription_id = $%d`, len(args)+1)
		args = append(args, subscriptionID)
	} else {
		query += fmt.Sprintf(` AND user_id = $%d`, len(args)+1)
		args = append(args, userID)
	}
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return 0, nil
	}
	var count int
	if err := rows.Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (s *SubscriptionService) GetSelfResetStatus(ctx context.Context, userID int64) ([]SelfResetStatus, error) {
	cfg := s.GetSelfResetSettings(ctx)
	now := s.selfResetNow()
	subs, err := s.userSubRepo.ListActiveByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]SelfResetStatus, 0, len(subs))
	for i := range subs {
		sub := &subs[i]
		status := SelfResetStatus{
			SubscriptionID: sub.ID,
			GroupID:        sub.GroupID,
			Enabled:        cfg.Enabled,
			MaxResets:      cfg.MaxResets,
			Period:         cfg.Period,
			Scope:          cfg.Scope,
			CanReset:       false,
			Reason:         "ok",
			DailyUsageUSD:  sub.DailyUsageUSD,
		}
		if sub.Group != nil {
			status.GroupName = sub.Group.Name
			if sub.Group.DailyLimitUSD != nil {
				status.DailyLimitUSD = *sub.Group.DailyLimitUSD
			}
		}
		status.PeriodKey = s.selfResetPeriodKey(now, cfg.Period, sub.StartsAt)
		status.PeriodResetsAt = s.selfResetPeriodResetsAt(now, cfg.Period, sub.StartsAt)
		used, countErr := s.selfResetCount(ctx, s.entClient, cfg, userID, sub.ID, status.PeriodKey, now)
		if countErr != nil {
			return nil, countErr
		}
		status.Used = used
		status.Remaining = cfg.MaxResets - used
		if status.Remaining < 0 {
			status.Remaining = 0
		}
		switch {
		case !cfg.Enabled:
			status.Reason = "disabled"
		case sub.Status != SubscriptionStatusActive || !sub.ExpiresAt.After(now):
			status.Reason = "inactive"
		case sub.Group == nil || !sub.Group.IsSubscriptionType():
			status.Reason = "not_subscription"
		case status.Remaining <= 0:
			status.Reason = "no_resets"
		default:
			status.CanReset = true
		}
		out = append(out, status)
	}
	return out, nil
}

// SelfResetQuota applies one self-reset for a user's subscription.
// Count and ownership checks run in one transaction; failed attempts never consume a chance.
func (s *SubscriptionService) SelfResetQuota(ctx context.Context, userID, subscriptionID int64, idempotencyKey, ip, userAgent string) (*SelfResetResult, error) {
	cfg := s.GetSelfResetSettings(ctx)
	if !cfg.Enabled {
		return nil, ErrSelfResetDisabled
	}
	if s.entClient == nil {
		return nil, fmt.Errorf("self-reset store is unavailable")
	}
	now := s.selfResetNow()

	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	q := tx.Client()

	// Serialize per user (or per subscription) so concurrent clicks cannot over-consume.
	lockQuery := `SELECT id FROM users WHERE id = $1 FOR UPDATE`
	lockArgs := []any{userID}
	if cfg.Per == SelfResetPerSubscription {
		lockQuery = `SELECT id FROM user_subscriptions WHERE id = $1 FOR UPDATE`
		lockArgs = []any{subscriptionID}
	}
	lockRows, err := q.QueryContext(ctx, lockQuery, lockArgs...)
	if err != nil {
		return nil, err
	}
	if !lockRows.Next() {
		_ = lockRows.Close()
		return nil, ErrSelfResetNotFound
	}
	_ = lockRows.Close()

	rows, err := q.QueryContext(ctx, `
		SELECT id, user_id, group_id, status, starts_at, expires_at,
		       daily_usage_usd::double precision, weekly_usage_usd::double precision, monthly_usage_usd::double precision
		FROM user_subscriptions
		WHERE id = $1 AND deleted_at IS NULL
		FOR UPDATE`, subscriptionID)
	if err != nil {
		return nil, err
	}
	if !rows.Next() {
		_ = rows.Close()
		return nil, ErrSelfResetNotFound
	}
	var (
		dbID, dbUserID, dbGroupID             int64
		dbStatus                              string
		startsAt, expiresAt                   time.Time
		dailyUsage, weeklyUsage, monthlyUsage float64
	)
	if err := rows.Scan(&dbID, &dbUserID, &dbGroupID, &dbStatus, &startsAt, &expiresAt, &dailyUsage, &weeklyUsage, &monthlyUsage); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()
	if dbUserID != userID {
		return nil, ErrSelfResetNotOwner
	}
	if dbStatus != SubscriptionStatusActive || !expiresAt.After(now) {
		return nil, ErrSelfResetNotActive
	}

	var (
		subscriptionType string
		dailyLimit       sql.NullFloat64
		weeklyLimit      sql.NullFloat64
		monthlyLimit     sql.NullFloat64
	)
	groupRows, err := q.QueryContext(ctx,
		`SELECT subscription_type,
		        COALESCE(daily_limit_usd, 0)::double precision,
		        COALESCE(weekly_limit_usd, 0)::double precision,
		        COALESCE(monthly_limit_usd, 0)::double precision
		 FROM groups WHERE id = $1`, dbGroupID)
	if err != nil {
		return nil, err
	}
	if !groupRows.Next() {
		_ = groupRows.Close()
		return nil, ErrSelfResetNotSupported
	}
	if err := groupRows.Scan(&subscriptionType, &dailyLimit, &weeklyLimit, &monthlyLimit); err != nil {
		_ = groupRows.Close()
		return nil, err
	}
	_ = groupRows.Close()
	if subscriptionType != SubscriptionTypeSubscription {
		return nil, ErrSelfResetNotSupported
	}

	resetDaily, resetWeekly, resetMonthly := false, false, false
	switch cfg.Scope {
	case SelfResetScopeDaily:
		if !dailyLimit.Valid || dailyLimit.Float64 <= 0 {
			return nil, ErrSelfResetScope
		}
		resetDaily = true
	case SelfResetScopeAll:
		resetDaily = dailyLimit.Valid && dailyLimit.Float64 > 0
		resetWeekly = weeklyLimit.Valid && weeklyLimit.Float64 > 0
		resetMonthly = monthlyLimit.Valid && monthlyLimit.Float64 > 0
		if !resetDaily && !resetWeekly && !resetMonthly {
			return nil, ErrSelfResetScope
		}
	case SelfResetScopeExhausted:
		if dailyLimit.Valid && dailyLimit.Float64 > 0 && dailyUsage >= dailyLimit.Float64 {
			resetDaily = true
		}
		if weeklyLimit.Valid && weeklyLimit.Float64 > 0 && weeklyUsage >= weeklyLimit.Float64 {
			resetWeekly = true
		}
		if monthlyLimit.Valid && monthlyLimit.Float64 > 0 && monthlyUsage >= monthlyLimit.Float64 {
			resetMonthly = true
		}
		if !resetDaily && !resetWeekly && !resetMonthly {
			return nil, ErrSelfResetScope
		}
	default:
		return nil, ErrSelfResetScope
	}

	if cfg.MinUsagePct > 0 {
		eligible := false
		if resetDaily && dailyLimit.Valid && dailyUsage >= dailyLimit.Float64*cfg.MinUsagePct/100 {
			eligible = true
		}
		if resetWeekly && weeklyLimit.Valid && weeklyUsage >= weeklyLimit.Float64*cfg.MinUsagePct/100 {
			eligible = true
		}
		if resetMonthly && monthlyLimit.Valid && monthlyUsage >= monthlyLimit.Float64*cfg.MinUsagePct/100 {
			eligible = true
		}
		if !eligible {
			return nil, ErrSelfResetBelowGate
		}
	}

	periodKey := s.selfResetPeriodKey(now, cfg.Period, startsAt)
	used, err := s.selfResetCount(ctx, q, cfg, userID, subscriptionID, periodKey, now)
	if err != nil {
		return nil, err
	}
	if used >= cfg.MaxResets {
		return nil, ErrSelfResetLimitReached
	}

	if cfg.CooldownMinutes > 0 {
		cooldownRows, err := q.QueryContext(ctx,
			`SELECT max(created_at) FROM subscription_self_reset_logs WHERE user_id = $1`, userID)
		if err != nil {
			return nil, err
		}
		var last sql.NullTime
		if cooldownRows.Next() {
			if err := cooldownRows.Scan(&last); err != nil {
				_ = cooldownRows.Close()
				return nil, err
			}
		}
		_ = cooldownRows.Close()
		if last.Valid && now.Sub(last.Time) < time.Duration(cfg.CooldownMinutes)*time.Minute {
			return nil, ErrSelfResetCooldown
		}
	}

	if cfg.DeductDays > 0 {
		newExpiresAt := expiresAt.AddDate(0, 0, -cfg.DeductDays)
		if !newExpiresAt.After(now) {
			return nil, ErrSelfResetDeduct
		}
		if _, err := q.ExecContext(ctx, `UPDATE user_subscriptions SET expires_at = $2 WHERE id = $1`, subscriptionID, newExpiresAt); err != nil {
			return nil, err
		}
	}

	if resetDaily {
		if _, err := q.ExecContext(ctx,
			`UPDATE user_subscriptions SET daily_usage_usd = 0, daily_window_start = $2 WHERE id = $1`,
			subscriptionID, timezone.StartOfDay(now)); err != nil {
			return nil, err
		}
	}
	if resetWeekly {
		if _, err := q.ExecContext(ctx,
			`UPDATE user_subscriptions SET weekly_usage_usd = 0, weekly_window_start = $2 WHERE id = $1`,
			subscriptionID, now); err != nil {
			return nil, err
		}
	}
	if resetMonthly {
		if _, err := q.ExecContext(ctx,
			`UPDATE user_subscriptions SET monthly_usage_usd = 0, monthly_window_start = $2 WHERE id = $1`,
			subscriptionID, now); err != nil {
			return nil, err
		}
	}

	var idem any
	if strings.TrimSpace(idempotencyKey) != "" {
		idem = strings.TrimSpace(idempotencyKey)
	}
	if _, err := q.ExecContext(ctx, `
		INSERT INTO subscription_self_reset_logs
			(user_id, subscription_id, group_id, period_key,
			 reset_daily, reset_weekly, reset_monthly,
			 usage_daily_before, usage_weekly_before, usage_monthly_before,
			 deduct_days, ip, user_agent, idempotency_key)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		userID, subscriptionID, dbGroupID, periodKey,
		resetDaily, resetWeekly, resetMonthly,
		dailyUsage, weeklyUsage, monthlyUsage,
		cfg.DeductDays, ip, userAgent, idem); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	s.InvalidateSubCacheSync(userID, dbGroupID)
	if s.billingCacheService != nil {
		_ = s.billingCacheService.InvalidateSubscription(ctx, userID, dbGroupID)
	}

	remaining := cfg.MaxResets - (used + 1)
	if remaining < 0 {
		remaining = 0
	}
	return &SelfResetResult{
		SubscriptionID: subscriptionID,
		Used:           used + 1,
		Remaining:      remaining,
		MaxResets:      cfg.MaxResets,
		Period:         cfg.Period,
		PeriodKey:      periodKey,
		Scope:          cfg.Scope,
	}, nil
}
