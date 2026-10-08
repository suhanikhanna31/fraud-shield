// Package store persists alerts and rules. It defines a small interface so
// the API layer can run against either a real PostgreSQL database or an
// in-memory implementation (used in tests / local demos without a DB).
package store

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"fraud-shield/internal/models"

	_ "github.com/lib/pq"
)

// Store is the persistence interface used by the API layer.
type Store interface {
	SaveAlert(ctx context.Context, a models.Alert) error
	ListAlerts(ctx context.Context, accountID string, limit int) ([]models.Alert, error)
	Ping(ctx context.Context) error
}

// ---- PostgreSQL implementation ----

type PostgresStore struct {
	db *sql.DB
}

// NewPostgresStore opens a connection pool to Postgres using a standard DSN,
// e.g. "postgres://user:pass@localhost:5432/fraudshield?sslmode=disable".
func NewPostgresStore(dsn string) (*PostgresStore, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return &PostgresStore{db: db}, nil
}

func (p *PostgresStore) Ping(ctx context.Context) error {
	return p.db.PingContext(ctx)
}

func (p *PostgresStore) SaveAlert(ctx context.Context, a models.Alert) error {
	const q = `
		INSERT INTO alerts (id, transaction_id, account_id, score, reasons, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (id) DO NOTHING`
	_, err := p.db.ExecContext(ctx, q, a.ID, a.TransactionID, a.AccountID, a.Score, reasonsToText(a.Reasons), a.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert alert: %w", err)
	}
	return nil
}

func (p *PostgresStore) ListAlerts(ctx context.Context, accountID string, limit int) ([]models.Alert, error) {
	if limit <= 0 {
		limit = 50
	}
	var rows *sql.Rows
	var err error
	if accountID == "" {
		rows, err = p.db.QueryContext(ctx,
			`SELECT id, transaction_id, account_id, score, reasons, created_at
			 FROM alerts ORDER BY created_at DESC LIMIT $1`, limit)
	} else {
		rows, err = p.db.QueryContext(ctx,
			`SELECT id, transaction_id, account_id, score, reasons, created_at
			 FROM alerts WHERE account_id = $1 ORDER BY created_at DESC LIMIT $2`, accountID, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("query alerts: %w", err)
	}
	defer rows.Close()

	var out []models.Alert
	for rows.Next() {
		var a models.Alert
		var reasonsText string
		if err := rows.Scan(&a.ID, &a.TransactionID, &a.AccountID, &a.Score, &reasonsText, &a.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan alert: %w", err)
		}
		a.Reasons = textToReasons(reasonsText)
		out = append(out, a)
	}
	return out, rows.Err()
}

func reasonsToText(reasons []string) string {
	out := ""
	for i, r := range reasons {
		if i > 0 {
			out += "|"
		}
		out += r
	}
	return out
}

func textToReasons(text string) []string {
	if text == "" {
		return nil
	}
	var out []string
	start := 0
	for i := 0; i < len(text); i++ {
		if text[i] == '|' {
			out = append(out, text[start:i])
			start = i + 1
		}
	}
	out = append(out, text[start:])
	return out
}

// ---- In-memory implementation (for tests and running without Postgres) ----

type MemoryStore struct {
	mu     sync.Mutex
	alerts []models.Alert
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{}
}

func (m *MemoryStore) Ping(ctx context.Context) error { return nil }

func (m *MemoryStore) SaveAlert(ctx context.Context, a models.Alert) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.alerts = append(m.alerts, a)
	return nil
}

func (m *MemoryStore) ListAlerts(ctx context.Context, accountID string, limit int) ([]models.Alert, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if limit <= 0 {
		limit = 50
	}
	var out []models.Alert
	for i := len(m.alerts) - 1; i >= 0 && len(out) < limit; i-- {
		if accountID == "" || m.alerts[i].AccountID == accountID {
			out = append(out, m.alerts[i])
		}
	}
	return out, nil
}
