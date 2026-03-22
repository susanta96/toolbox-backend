-- 002_create_exchange_rates.sql
-- Stores latest and historical FX snapshots per currency pair.

CREATE TABLE IF NOT EXISTS exchange_rates (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    base       VARCHAR(3) NOT NULL,
    target     VARCHAR(3) NOT NULL,
    rate       DECIMAL(18, 8) NOT NULL,
    rate_date  DATE NOT NULL,
    source     VARCHAR(20) NOT NULL,
    fetched_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (base, target, rate_date)
);

CREATE INDEX IF NOT EXISTS idx_exchange_rates_pair_date ON exchange_rates (base, target, rate_date DESC);
CREATE INDEX IF NOT EXISTS idx_exchange_rates_expires_at ON exchange_rates (expires_at);
