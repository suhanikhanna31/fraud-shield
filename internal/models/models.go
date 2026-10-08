package models

import "time"

// Transaction represents an incoming payment/transfer event to be scored.
type Transaction struct {
	ID           string    `json:"id"`
	AccountID    string    `json:"account_id"`
	DeviceID     string    `json:"device_id,omitempty"`
	IPAddress    string    `json:"ip_address,omitempty"`
	Amount       float64   `json:"amount"`
	Currency     string    `json:"currency"`
	Timestamp    time.Time `json:"timestamp"`
	MerchantID   string    `json:"merchant_id,omitempty"`
	CounterParty string    `json:"counter_party,omitempty"` // e.g. recipient account
}

// Alert is produced when a transaction is scored above a risk threshold.
type Alert struct {
	ID            string    `json:"id"`
	TransactionID string    `json:"transaction_id"`
	AccountID     string    `json:"account_id"`
	Score         float64   `json:"score"`
	Reasons       []string  `json:"reasons"`
	CreatedAt     time.Time `json:"created_at"`
}

// Rule defines a single scoring rule with a name, weight, and threshold.
// Weight is added to the running risk score whenever the rule's condition fires.
type Rule struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Weight    float64 `json:"weight"`
	Enabled   bool    `json:"enabled"`
	Threshold float64 `json:"threshold,omitempty"` // rule-specific threshold, e.g. amount cap
}

// ScoreResult is returned to callers of the scoring engine.
type ScoreResult struct {
	TransactionID string   `json:"transaction_id"`
	Score         float64  `json:"score"`
	Flagged       bool     `json:"flagged"`
	Reasons       []string `json:"reasons"`
}
