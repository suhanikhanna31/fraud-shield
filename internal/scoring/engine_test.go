package scoring

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"fraud-shield/internal/models"
)

func TestScoreReasonsAreNeverNil(t *testing.T) {
	e := NewEngine(DefaultConfig())
	res := e.Score(models.Transaction{ID: "x", AccountID: "a", Amount: 1, Timestamp: time.Now()})
	if res.Reasons == nil {
		t.Fatal("Reasons must be an empty slice so JSON encodes [] not null")
	}
}

func TestScoreIsSumOfWeightsAndFlagged(t *testing.T) {
	e := NewEngine(DefaultConfig())
	// large amount (25) + new counterparty (10) = 35: below the flag threshold.
	res := e.Score(models.Transaction{ID: "1", AccountID: "a", Amount: 15000, CounterParty: "b", Timestamp: time.Now()})
	if res.Score != 35 || res.Flagged {
		t.Fatalf("got score=%v flagged=%v, want 35/false", res.Score, res.Flagged)
	}
	if len(res.Reasons) != 2 || res.Reasons[0] != ReasonLargeAmount || res.Reasons[1] != ReasonNewCounterparty {
		t.Fatalf("unexpected reasons %v", res.Reasons)
	}
}

func TestScoreFlagsAtThreshold(t *testing.T) {
	e := NewEngine(DefaultConfig())
	e.MarkRingAccount("bad")
	now := time.Now()
	e.Score(models.Transaction{ID: "1", AccountID: "bad", DeviceID: "d", Timestamp: now})
	// ring link (40) + large amount (25) = 65 >= 50
	res := e.Score(models.Transaction{ID: "2", AccountID: "mule", DeviceID: "d", Amount: 20000, Timestamp: now})
	if !res.Flagged || res.Score != 65 {
		t.Fatalf("got score=%v flagged=%v, want 65/true", res.Score, res.Flagged)
	}
}

func TestScoreClampedTo100(t *testing.T) {
	e := NewEngine(DefaultConfig())
	e.AddRule(models.Rule{ID: RuleLargeAmount, Name: "x", Weight: 90, Enabled: true, Threshold: 1})
	e.AddRule(models.Rule{ID: RuleNewCounterparty, Name: "y", Weight: 90, Enabled: true})
	res := e.Score(models.Transaction{ID: "1", AccountID: "a", Amount: 10, CounterParty: "b", Timestamp: time.Now()})
	if res.Score != 100 {
		t.Fatalf("score=%v want clamp to 100", res.Score)
	}
}

func TestDisabledRuleDoesNotScore(t *testing.T) {
	e := NewEngine(DefaultConfig())
	e.AddRule(models.Rule{ID: RuleLargeAmount, Name: "Large", Weight: 25, Enabled: false, Threshold: 100})
	res := e.Score(models.Transaction{ID: "1", AccountID: "a", Amount: 1e6, Timestamp: time.Now()})
	if res.Score != 0 {
		t.Fatalf("score=%v want 0 with the only matching rule disabled", res.Score)
	}
}

func TestRuleWeightAndThresholdAreConfigurableAtRuntime(t *testing.T) {
	e := NewEngine(DefaultConfig())
	e.AddRule(models.Rule{ID: RuleLargeAmount, Name: "Large", Weight: 60, Enabled: true, Threshold: 500})
	res := e.Score(models.Transaction{ID: "1", AccountID: "a", Amount: 600, Timestamp: time.Now()})
	if res.Score != 60 || !res.Flagged {
		t.Fatalf("got score=%v flagged=%v, want 60/true after reconfiguring", res.Score, res.Flagged)
	}
}

func TestVelocityThresholdConfigurableViaRule(t *testing.T) {
	e := NewEngine(DefaultConfig())
	e.AddRule(models.Rule{ID: RuleVelocity, Name: "v", Weight: 30, Enabled: true, Threshold: 1})
	now := time.Now()
	e.Score(models.Transaction{ID: "1", AccountID: "a", Timestamp: now})
	res := e.Score(models.Transaction{ID: "2", AccountID: "a", Timestamp: now.Add(time.Second)})
	if res.Score != 30 {
		t.Fatalf("score=%v want 30: second txn exceeds threshold of 1", res.Score)
	}
}

type alwaysRule struct{}

func (alwaysRule) ID() string { return "always" }
func (alwaysRule) Check(models.Transaction, models.Rule, Signals) (string, bool) {
	return "custom rule fired", true
}

func TestCustomEvaluatorCanBePlugged(t *testing.T) {
	e := NewEngine(DefaultConfig())
	e.RegisterEvaluator(alwaysRule{})
	e.AddRule(models.Rule{ID: "always", Name: "Always", Weight: 55, Enabled: true})
	res := e.Score(models.Transaction{ID: "1", AccountID: "a", Timestamp: time.Now()})
	if res.Score != 55 || !res.Flagged || res.Reasons[0] != "custom rule fired" {
		t.Fatalf("unexpected result %+v", res)
	}
}

func TestRuleConfigWithoutEvaluatorIsInert(t *testing.T) {
	e := NewEngine(DefaultConfig())
	e.AddRule(models.Rule{ID: "ghost", Name: "No evaluator", Weight: 99, Enabled: true})
	res := e.Score(models.Transaction{ID: "1", AccountID: "a", Timestamp: time.Now()})
	if res.Score != 0 {
		t.Fatalf("score=%v want 0", res.Score)
	}
}

func TestSharedIPLinksToRing(t *testing.T) {
	e := NewEngine(DefaultConfig())
	e.MarkRingAccount("bad")
	now := time.Now()
	e.Score(models.Transaction{ID: "1", AccountID: "bad", IPAddress: "9.9.9.9", Timestamp: now})
	res := e.Score(models.Transaction{ID: "2", AccountID: "other", IPAddress: "9.9.9.9", Timestamp: now})
	if len(res.Reasons) != 1 || res.Reasons[0] != ReasonLinkedRing {
		t.Fatalf("reasons=%v want linked-ring only", res.Reasons)
	}
}

func TestEngineConcurrentUse(t *testing.T) {
	e := NewEngine(DefaultConfig())
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				e.Score(models.Transaction{
					ID: fmt.Sprint(g, "-", i), AccountID: fmt.Sprint("acc", g%3),
					DeviceID: fmt.Sprint("dev", g%2), Amount: float64(i * 100),
					CounterParty: fmt.Sprint("cp", i%5), Timestamp: time.Now(),
				})
				e.Rules()
				e.MarkRingAccount(fmt.Sprint("acc", g%3))
			}
		}(g)
	}
	wg.Wait()
}
