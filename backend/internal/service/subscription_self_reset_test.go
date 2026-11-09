package service

import (
	"testing"
	"time"
)

func TestDefaultSelfResetSettings(t *testing.T) {
	cfg := DefaultSelfResetSettings()
	if !cfg.Enabled {
		t.Fatal("expected enabled by default")
	}
	if cfg.MaxResets != 3 {
		t.Fatalf("expected 3 resets, got %d", cfg.MaxResets)
	}
	if cfg.Scope != SelfResetScopeDaily {
		t.Fatalf("expected daily scope, got %s", cfg.Scope)
	}
	if cfg.MinUsagePct != 0 {
		t.Fatalf("expected no usage gate, got %v", cfg.MinUsagePct)
	}
	if cfg.Per != SelfResetPerUser {
		t.Fatalf("expected per-user counter, got %s", cfg.Per)
	}
}

func TestSelfResetSettingsNormalize(t *testing.T) {
	cfg := SelfResetSettings{MaxResets: -1, Period: "bad", Scope: "bad", Per: "bad", MinUsagePct: 150, DeductDays: 99}
	cfg.Normalize()
	if cfg.MaxResets != 3 {
		t.Fatalf("expected default max resets, got %d", cfg.MaxResets)
	}
	if cfg.Period != SelfResetPeriodCalendarMonth {
		t.Fatalf("expected calendar month, got %s", cfg.Period)
	}
	if cfg.Scope != SelfResetScopeDaily {
		t.Fatalf("expected daily scope, got %s", cfg.Scope)
	}
	if cfg.Per != SelfResetPerUser {
		t.Fatalf("expected per-user, got %s", cfg.Per)
	}
	if cfg.MinUsagePct != 100 {
		t.Fatalf("expected clamped usage gate, got %v", cfg.MinUsagePct)
	}
	if cfg.DeductDays != 30 {
		t.Fatalf("expected clamped deduct days, got %d", cfg.DeductDays)
	}
}

func TestSelfResetPeriodKey(t *testing.T) {
	svc := &SubscriptionService{}
	loc := time.FixedZone("CST", 8*60*60)
	now := time.Date(2026, 11, 9, 19, 0, 0, 0, loc)
	starts := time.Date(2026, 10, 15, 10, 0, 0, 0, loc)

	if got := svc.selfResetPeriodKey(now, SelfResetPeriodCalendarMonth, starts); got != "2026-11" {
		t.Fatalf("calendar month key = %q", got)
	}
	if got := svc.selfResetPeriodKey(now, SelfResetPeriodCalendarWeek, starts); got != "2026-W46" {
		t.Fatalf("calendar week key = %q", got)
	}
	if got := svc.selfResetPeriodKey(now, SelfResetPeriodRolling30D, starts); got != "rolling_30d" {
		t.Fatalf("rolling key = %q", got)
	}
	if got := svc.selfResetPeriodKey(now, SelfResetPeriodCycle, starts); got == "" {
		t.Fatal("cycle key should not be empty")
	}
}
