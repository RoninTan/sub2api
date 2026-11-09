-- 250_subscription_self_reset.sql
-- User-facing subscription quota self-reset ledger.
-- Kept in a separate table so upstream ent schema/codegen stays untouched.

CREATE TABLE IF NOT EXISTS subscription_self_reset_logs (
    id                    BIGSERIAL PRIMARY KEY,
    user_id               BIGINT NOT NULL,
    subscription_id       BIGINT NOT NULL,
    group_id              BIGINT NOT NULL,
    period_key            VARCHAR(64) NOT NULL,
    reset_daily           BOOLEAN NOT NULL DEFAULT FALSE,
    reset_weekly          BOOLEAN NOT NULL DEFAULT FALSE,
    reset_monthly         BOOLEAN NOT NULL DEFAULT FALSE,
    usage_daily_before    NUMERIC(20,10),
    usage_weekly_before   NUMERIC(20,10),
    usage_monthly_before  NUMERIC(20,10),
    deduct_days           INTEGER NOT NULL DEFAULT 0,
    ip                    VARCHAR(64),
    user_agent            TEXT,
    idempotency_key       VARCHAR(64),
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_self_reset_user_period
    ON subscription_self_reset_logs (user_id, period_key, created_at);
CREATE INDEX IF NOT EXISTS idx_self_reset_sub
    ON subscription_self_reset_logs (subscription_id, created_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_self_reset_idem
    ON subscription_self_reset_logs (idempotency_key)
    WHERE idempotency_key IS NOT NULL;
