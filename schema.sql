-- Fraud Shield database schema (PostgreSQL)

CREATE TABLE IF NOT EXISTS alerts (
    id             UUID PRIMARY KEY,
    transaction_id TEXT        NOT NULL,
    account_id     TEXT        NOT NULL,
    score          DOUBLE PRECISION NOT NULL,
    reasons        TEXT,               -- '|'-delimited list of reason strings
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_alerts_account_id  ON alerts (account_id);
CREATE INDEX IF NOT EXISTS idx_alerts_created_at  ON alerts (created_at DESC);
