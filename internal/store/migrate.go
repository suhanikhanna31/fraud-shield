package store

import (
	"context"
	"fmt"
)

// schema mirrors schema.sql. It is applied on startup so a fresh database
// (e.g. a new Postgres on OpenShift or Render) works without a manual
// migration step. All statements are idempotent.
const schema = `
CREATE TABLE IF NOT EXISTS alerts (
    id             UUID PRIMARY KEY,
    transaction_id TEXT        NOT NULL,
    account_id     TEXT        NOT NULL,
    score          DOUBLE PRECISION NOT NULL,
    reasons        TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_alerts_account_id ON alerts (account_id);
CREATE INDEX IF NOT EXISTS idx_alerts_created_at ON alerts (created_at DESC);
`

// Migrate creates the schema if it does not exist yet.
func (p *PostgresStore) Migrate(ctx context.Context) error {
	if _, err := p.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	return nil
}

// Close releases the connection pool.
func (p *PostgresStore) Close() error { return p.db.Close() }
