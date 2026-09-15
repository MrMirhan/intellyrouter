package store

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
)

// SubscriptionLimitsSetting holds the latest anthropic-ratelimit-* headers
// seen on a subscription response, as JSON.
const SubscriptionLimitsSetting = "subscription_rate_limits"

// FallbackRouteSetting names the route that serves model names without a route,
// such as Claude Code background calls. Empty means those requests get a 404.
const FallbackRouteSetting = "fallback_route"

// CaptureContentSetting is "true" when request and response content is stored.
const CaptureContentSetting = "capture_content"

// CaptureRetentionDaysSetting is how many days captured content is kept.
const (
	CaptureRetentionDaysSetting = "capture_retention_days"
	DefaultCaptureRetentionDays = 14
	MaxCaptureRetentionDays     = 365
)

func (s *Store) Setting(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return v, err == nil, err
}

func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// CaptureSettings reports whether content capture is on and how many days
// captured content is kept. Missing or out-of-range values use the defaults.
func (s *Store) CaptureSettings(ctx context.Context) (enabled bool, retentionDays int, err error) {
	on, _, err := s.Setting(ctx, CaptureContentSetting)
	if err != nil {
		return false, 0, err
	}
	v, _, err := s.Setting(ctx, CaptureRetentionDaysSetting)
	if err != nil {
		return false, 0, err
	}
	days, convErr := strconv.Atoi(v)
	if convErr != nil || days < 1 || days > MaxCaptureRetentionDays {
		days = DefaultCaptureRetentionDays
	}
	return on == "true", days, nil
}
