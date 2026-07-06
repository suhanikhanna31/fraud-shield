// Package scoring implements the core fraud-detection algorithms:
//
//   - A sliding-window velocity check (per account) to catch bursts of
//     transactions in a short time window — O(1) amortized per event.
//   - A union-find (disjoint set) structure to cluster accounts that share
//     a device ID or IP address, so coordinated multi-account fraud rings
//     can be detected in near O(alpha(n)) per operation.
//   - A weighted, rule-based scoring engine that combines the above signals
//     with simple static rules (amount caps, new counterparties, etc.)
//     into a single risk score in [0, 100].
package scoring

import (
	"sync"
	"time"

	"fraud-shield/internal/models"
)

// Engine is the fraud scoring engine. It is safe for concurrent use by
// multiple goroutines (e.g. one per inbound transaction request).
type Engine struct {
	mu sync.Mutex

	// velocity tracks recent transaction timestamps per account for the
	// sliding-window rate check.
	velocity map[string][]time.Time
	window   time.Duration
	maxTxn   int // max transactions allowed inside the window before flagging

	// linkGraph clusters accounts sharing a device/IP fingerprint.
	linkGraph *unionFind

	// seenCounterparty tracks whether an account has transacted with a
	// given counterparty before, to flag brand-new payees.
	seenCounterparty map[string]map[string]bool

	rules []models.Rule

	// flaggedRingAccounts marks accounts known to be part of a fraud ring
	// (e.g. from a prior investigation), used to boost scores for
	// anyone linked to them.
	flaggedRingAccounts map[string]bool
}

// Config controls the tunable thresholds of the Engine.
type Config struct {
	VelocityWindow    time.Duration // e.g. 1 * time.Minute
	VelocityMaxTxns   int           // e.g. 5 transactions per window
	LargeAmountCap    float64       // e.g. 10000.00
	FlaggedRingWeight float64       // score added if linked to a known-bad account
}

func DefaultConfig() Config {
	return Config{
		VelocityWindow:    time.Minute,
		VelocityMaxTxns:   5,
		LargeAmountCap:    10000.00,
		FlaggedRingWeight: 40,
	}
}

// NewEngine builds a scoring engine with the default rule set. Additional
// rules can be registered later with AddRule.
func NewEngine(cfg Config) *Engine {
	e := &Engine{
		velocity:            make(map[string][]time.Time),
		window:              cfg.VelocityWindow,
		maxTxn:              cfg.VelocityMaxTxns,
		linkGraph:           newUnionFind(),
		seenCounterparty:    make(map[string]map[string]bool),
		flaggedRingAccounts: make(map[string]bool),
	}
	e.rules = []models.Rule{
		{ID: "large_amount", Name: "Large transaction amount", Weight: 25, Enabled: true, Threshold: cfg.LargeAmountCap},
		{ID: "velocity", Name: "Too many transactions in window", Weight: 30, Enabled: true},
		{ID: "new_counterparty", Name: "First-time counterparty", Weight: 10, Enabled: true},
		{ID: "linked_ring", Name: "Linked to flagged fraud ring", Weight: cfg.FlaggedRingWeight, Enabled: true},
	}
	return e
}

// AddRule registers or replaces a rule by ID.
func (e *Engine) AddRule(r models.Rule) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i, existing := range e.rules {
		if existing.ID == r.ID {
			e.rules[i] = r
			return
		}
	}
	e.rules = append(e.rules, r)
}

// Rules returns a copy of the currently configured rules.
func (e *Engine) Rules() []models.Rule {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]models.Rule, len(e.rules))
	copy(out, e.rules)
	return out
}

// MarkRingAccount flags an account as a known participant in a fraud ring
// (e.g. confirmed by a human investigator), boosting the score of anyone
// later found to be linked to it via shared device/IP.
func (e *Engine) MarkRingAccount(accountID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.flaggedRingAccounts[accountID] = true
}

// Score evaluates a transaction against all enabled rules and returns a
// combined risk score plus the human-readable reasons that contributed to it.
func (e *Engine) Score(tx models.Transaction) models.ScoreResult {
	e.mu.Lock()
	defer e.mu.Unlock()

	var score float64
	var reasons []string

	ruleEnabled := func(id string) (models.Rule, bool) {
		for _, r := range e.rules {
			if r.ID == id && r.Enabled {
				return r, true
			}
		}
		return models.Rule{}, false
	}

	// 1. Large amount rule.
	if r, ok := ruleEnabled("large_amount"); ok && tx.Amount >= r.Threshold {
		score += r.Weight
		reasons = append(reasons, "transaction amount exceeds configured cap")
	}

	// 2. Velocity rule (sliding window).
	if r, ok := ruleEnabled("velocity"); ok {
		count := e.recordAndCountVelocity(tx.AccountID, tx.Timestamp)
		if count > e.maxTxn {
			score += r.Weight
			reasons = append(reasons, "unusually high transaction frequency for this account")
		}
	}

	// 3. New counterparty rule.
	if r, ok := ruleEnabled("new_counterparty"); ok && tx.CounterParty != "" {
		if !e.hasSeenCounterparty(tx.AccountID, tx.CounterParty) {
			score += r.Weight
			reasons = append(reasons, "first-time transfer to this counterparty")
		}
		e.markCounterpartySeen(tx.AccountID, tx.CounterParty)
	}

	// 4. Linked-account (fraud ring) rule via union-find.
	if tx.DeviceID != "" {
		e.linkGraph.union(tx.AccountID, "device:"+tx.DeviceID)
	}
	if tx.IPAddress != "" {
		e.linkGraph.union(tx.AccountID, "ip:"+tx.IPAddress)
	}
	if r, ok := ruleEnabled("linked_ring"); ok && e.isLinkedToRing(tx.AccountID) {
		score += r.Weight
		reasons = append(reasons, "account is linked (via shared device/IP) to a known fraud ring")
	}

	if score > 100 {
		score = 100
	}

	return models.ScoreResult{
		TransactionID: tx.ID,
		Score:         score,
		Flagged:       score >= 50,
		Reasons:       reasons,
	}
}

// recordAndCountVelocity appends the timestamp to the account's history,
// prunes anything outside the sliding window, and returns the count of
// transactions still inside the window. Amortized O(1) per call since each
// timestamp is pruned exactly once.
func (e *Engine) recordAndCountVelocity(accountID string, ts time.Time) int {
	history := e.velocity[accountID]
	history = append(history, ts)

	cutoff := ts.Add(-e.window)
	i := 0
	for i < len(history) && history[i].Before(cutoff) {
		i++
	}
	history = history[i:]

	e.velocity[accountID] = history
	return len(history)
}

func (e *Engine) hasSeenCounterparty(accountID, counterparty string) bool {
	seen, ok := e.seenCounterparty[accountID]
	if !ok {
		return false
	}
	return seen[counterparty]
}

func (e *Engine) markCounterpartySeen(accountID, counterparty string) {
	if _, ok := e.seenCounterparty[accountID]; !ok {
		e.seenCounterparty[accountID] = make(map[string]bool)
	}
	e.seenCounterparty[accountID][counterparty] = true
}

// isLinkedToRing checks whether the given account shares a connected
// component (via device/IP union) with any account flagged as part of a
// known fraud ring.
func (e *Engine) isLinkedToRing(accountID string) bool {
	root := e.linkGraph.find(accountID)
	for flagged := range e.flaggedRingAccounts {
		if e.linkGraph.find(flagged) == root {
			return true
		}
	}
	return false
}

// ---- Union-Find (Disjoint Set Union) with path compression + union by rank ----

type unionFind struct {
	parent map[string]string
	rank   map[string]int
}

func newUnionFind() *unionFind {
	return &unionFind{
		parent: make(map[string]string),
		rank:   make(map[string]int),
	}
}

func (u *unionFind) find(x string) string {
	if _, ok := u.parent[x]; !ok {
		u.parent[x] = x
		u.rank[x] = 0
		return x
	}
	if u.parent[x] != x {
		u.parent[x] = u.find(u.parent[x]) // path compression
	}
	return u.parent[x]
}

func (u *unionFind) union(a, b string) {
	ra, rb := u.find(a), u.find(b)
	if ra == rb {
		return
	}
	// union by rank
	if u.rank[ra] < u.rank[rb] {
		ra, rb = rb, ra
	}
	u.parent[rb] = ra
	if u.rank[ra] == u.rank[rb] {
		u.rank[ra]++
	}
}
