package store

import (
	"context"
	"database/sql"
	"errors"
)

// SubscriptionLimitsSetting holds the latest anthropic-ratelimit-* headers
// seen on a subscription response, as JSON.
const SubscriptionLimitsSetting = "subscription_rate_limits"

// FallbackRouteSetting names the route that serves model names without a route,
// such as Claude Code background calls. Empty means those requests get a 404.
const FallbackRouteSetting = "fallback_route"

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
