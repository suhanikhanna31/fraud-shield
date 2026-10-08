// Package scoring implements the core fraud-detection algorithms:
//
//   - A sliding-window velocity check (per account) to catch bursts of
//     transactions in a short time window — amortized O(1) per event.
//   - A union-find (disjoint set) structure to cluster accounts that share
//     a device ID or IP address, so coordinated multi-account fraud rings
//     can be detected in near O(alpha(n)) per operation.
//   - A weighted, configurable rule engine. Each rule is an Evaluator that
//     judges precomputed Signals, so rules are independently testable, and
//     rule weights/thresholds/enabled flags are changeable at runtime.
//
// The combined risk score is clamped to [0, 100].
package scoring

import (
	"sync"
	"time"

	"fraud-shield/internal/models"
)

const defaultFlagThreshold = 50

// Engine is the fraud scoring engine. It is safe for concurrent use by
// multiple goroutines (e.g. one per inbound transaction request).
type Engine struct {
	mu sync.Mutex

	velocity  *velocityTracker
	linkGraph *unionFind

	// seenCounterparty tracks whether an account has transacted with a
	// given counterparty before, to flag brand-new payees.
	seenCounterparty map[string]map[string]bool

	// flaggedRingAccounts marks accounts known to be part of a fraud ring
	// (e.g. from a prior investigation), used to boost scores for anyone
	// linked to them.
	flaggedRingAccounts map[string]bool

	rules      []models.Rule
	evaluators map[string]Evaluator
	order      []string // evaluator IDs in evaluation order

	flagThreshold float64
}

// Config controls the tunable thresholds of the Engine.
type Config struct {
	VelocityWindow    time.Duration // e.g. 1 * time.Minute
	VelocityMaxTxns   int           // e.g. 5 transactions per window
	LargeAmountCap    float64       // e.g. 10000.00
	FlaggedRingWeight float64       // score added if linked to a known-bad account
	FlagThreshold     float64       // score at or above which a result is flagged (default 50)
}

func DefaultConfig() Config {
	return Config{
		VelocityWindow:    time.Minute,
		VelocityMaxTxns:   5,
		LargeAmountCap:    10000.00,
		FlaggedRingWeight: 40,
		FlagThreshold:     defaultFlagThreshold,
	}
}

// NewEngine builds a scoring engine with the default rule set. Custom rule
// logic can be added with RegisterEvaluator and configured with AddRule.
func NewEngine(cfg Config) *Engine {
	flag := cfg.FlagThreshold
	if flag <= 0 {
		flag = defaultFlagThreshold
	}
	e := &Engine{
		velocity:            newVelocityTracker(cfg.VelocityWindow),
		linkGraph:           newUnionFind(),
		seenCounterparty:    make(map[string]map[string]bool),
		flaggedRingAccounts: make(map[string]bool),
		evaluators:          make(map[string]Evaluator),
		flagThreshold:       flag,
	}
	for _, ev := range builtinEvaluators() {
		e.registerLocked(ev)
	}
	e.rules = []models.Rule{
		{ID: RuleLargeAmount, Name: "Large transaction amount", Weight: 25, Enabled: true, Threshold: cfg.LargeAmountCap},
		{ID: RuleVelocity, Name: "Too many transactions in window", Weight: 30, Enabled: true, Threshold: float64(cfg.VelocityMaxTxns)},
		{ID: RuleNewCounterparty, Name: "First-time counterparty", Weight: 10, Enabled: true},
		{ID: RuleLinkedRing, Name: "Linked to flagged fraud ring", Weight: cfg.FlaggedRingWeight, Enabled: true},
	}
	return e
}

func (e *Engine) registerLocked(ev Evaluator) {
	if _, exists := e.evaluators[ev.ID()]; !exists {
		e.order = append(e.order, ev.ID())
	}
	e.evaluators[ev.ID()] = ev
}

// RegisterEvaluator adds (or replaces) the logic behind a rule ID. A rule
// config added via AddRule only affects scores if an Evaluator with the
// same ID is registered.
func (e *Engine) RegisterEvaluator(ev Evaluator) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.registerLocked(ev)
}

// AddRule registers or replaces a rule's configuration by ID.
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

	sig := e.collectSignals(tx)

	var score float64
	reasons := []string{}

	// Evaluate in registration order so reasons are deterministic.
	byID := make(map[string]models.Rule, len(e.rules))
	for _, r := range e.rules {
		byID[r.ID] = r
	}
	for _, id := range e.order {
		cfg, ok := byID[id]
		if !ok || !cfg.Enabled {
			continue
		}
		if reason, fired := e.evaluators[id].Check(tx, cfg, sig); fired {
			score += cfg.Weight
			reasons = append(reasons, reason)
		}
	}

	if score > 100 {
		score = 100
	}
	if score < 0 {
		score = 0
	}

	return models.ScoreResult{
		TransactionID: tx.ID,
		Score:         score,
		Flagged:       score >= e.flagThreshold,
		Reasons:       reasons,
	}
}

// collectSignals updates the stateful trackers with tx and returns the
// resulting facts for rules to judge. Caller must hold e.mu.
func (e *Engine) collectSignals(tx models.Transaction) Signals {
	var s Signals

	s.VelocityCount = e.velocity.recordAndCount(tx.AccountID, tx.Timestamp)

	if tx.CounterParty != "" {
		s.NewCounterparty = !e.hasSeenCounterparty(tx.AccountID, tx.CounterParty)
		e.markCounterpartySeen(tx.AccountID, tx.CounterParty)
	}

	if tx.DeviceID != "" {
		e.linkGraph.union(tx.AccountID, "device:"+tx.DeviceID)
	}
	if tx.IPAddress != "" {
		e.linkGraph.union(tx.AccountID, "ip:"+tx.IPAddress)
	}
	s.LinkedToRing = e.isLinkedToRing(tx.AccountID)

	return s
}

func (e *Engine) hasSeenCounterparty(accountID, counterparty string) bool {
	return e.seenCounterparty[accountID][counterparty]
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
