package scoring

import "fraud-shield/internal/models"

// Reason strings returned to callers. Exported so API clients and tests can
// match on them without copying string literals.
const (
	ReasonLargeAmount     = "transaction amount exceeds configured cap"
	ReasonHighVelocity    = "unusually high transaction frequency for this account"
	ReasonNewCounterparty = "first-time transfer to this counterparty"
	ReasonLinkedRing      = "account is linked (via shared device/IP) to a known fraud ring"
)

// Rule IDs of the built-in rules.
const (
	RuleLargeAmount     = "large_amount"
	RuleVelocity        = "velocity"
	RuleNewCounterparty = "new_counterparty"
	RuleLinkedRing      = "linked_ring"
)

// Signals are the precomputed facts about a transaction that rules judge.
// The engine fills them in from its stateful trackers; rules themselves are
// pure functions of (transaction, config, signals), so each one can be unit
// tested without constructing an Engine.
type Signals struct {
	VelocityCount   int  // transactions in the window, including this one
	NewCounterparty bool // first time this account pays this counterparty
	LinkedToRing    bool // shares a device/IP cluster with a flagged account
}

// Evaluator is one scoring rule. Check returns the reason and true when the
// rule fires. The engine adds the rule's configured Weight when it does.
type Evaluator interface {
	ID() string
	Check(tx models.Transaction, cfg models.Rule, s Signals) (reason string, fired bool)
}

type largeAmountRule struct{}

func (largeAmountRule) ID() string { return RuleLargeAmount }
func (largeAmountRule) Check(tx models.Transaction, cfg models.Rule, _ Signals) (string, bool) {
	return ReasonLargeAmount, tx.Amount >= cfg.Threshold
}

type velocityRule struct{}

func (velocityRule) ID() string { return RuleVelocity }
func (velocityRule) Check(_ models.Transaction, cfg models.Rule, s Signals) (string, bool) {
	// cfg.Threshold is the max transactions allowed inside the window.
	return ReasonHighVelocity, float64(s.VelocityCount) > cfg.Threshold
}

type newCounterpartyRule struct{}

func (newCounterpartyRule) ID() string { return RuleNewCounterparty }
func (newCounterpartyRule) Check(tx models.Transaction, _ models.Rule, s Signals) (string, bool) {
	return ReasonNewCounterparty, tx.CounterParty != "" && s.NewCounterparty
}

type linkedRingRule struct{}

func (linkedRingRule) ID() string { return RuleLinkedRing }
func (linkedRingRule) Check(_ models.Transaction, _ models.Rule, s Signals) (string, bool) {
	return ReasonLinkedRing, s.LinkedToRing
}

// builtinEvaluators returns the default rule implementations, in the order
// their reasons appear in a score result.
func builtinEvaluators() []Evaluator {
	return []Evaluator{
		largeAmountRule{},
		velocityRule{},
		newCounterpartyRule{},
		linkedRingRule{},
	}
}
