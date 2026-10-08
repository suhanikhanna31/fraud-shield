package scoring

import (
	"testing"

	"fraud-shield/internal/models"
)

// Each rule is tested in isolation: no Engine, no shared state.

func TestLargeAmountRule(t *testing.T) {
	cfg := models.Rule{ID: RuleLargeAmount, Weight: 25, Enabled: true, Threshold: 1000}
	tests := []struct {
		name   string
		amount float64
		want   bool
	}{
		{"below cap", 999.99, false},
		{"exactly at cap", 1000, true},
		{"above cap", 5000, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reason, fired := largeAmountRule{}.Check(models.Transaction{Amount: tc.amount}, cfg, Signals{})
			if fired != tc.want {
				t.Fatalf("fired=%v want %v", fired, tc.want)
			}
			if reason != ReasonLargeAmount {
				t.Fatalf("unexpected reason %q", reason)
			}
		})
	}
}

func TestVelocityRule(t *testing.T) {
	cfg := models.Rule{ID: RuleVelocity, Weight: 30, Enabled: true, Threshold: 3}
	tests := []struct {
		count int
		want  bool
	}{{1, false}, {3, false}, {4, true}, {10, true}}
	for _, tc := range tests {
		_, fired := velocityRule{}.Check(models.Transaction{}, cfg, Signals{VelocityCount: tc.count})
		if fired != tc.want {
			t.Fatalf("count=%d fired=%v want %v", tc.count, fired, tc.want)
		}
	}
}

func TestNewCounterpartyRule(t *testing.T) {
	cfg := models.Rule{ID: RuleNewCounterparty, Weight: 10, Enabled: true}
	tests := []struct {
		name string
		cp   string
		sig  Signals
		want bool
	}{
		{"new payee", "bob", Signals{NewCounterparty: true}, true},
		{"known payee", "bob", Signals{NewCounterparty: false}, false},
		{"no counterparty given", "", Signals{NewCounterparty: true}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, fired := newCounterpartyRule{}.Check(models.Transaction{CounterParty: tc.cp}, cfg, tc.sig)
			if fired != tc.want {
				t.Fatalf("fired=%v want %v", fired, tc.want)
			}
		})
	}
}

func TestLinkedRingRule(t *testing.T) {
	cfg := models.Rule{ID: RuleLinkedRing, Weight: 40, Enabled: true}
	if _, fired := (linkedRingRule{}).Check(models.Transaction{}, cfg, Signals{LinkedToRing: true}); !fired {
		t.Fatal("expected rule to fire when linked")
	}
	if _, fired := (linkedRingRule{}).Check(models.Transaction{}, cfg, Signals{}); fired {
		t.Fatal("expected rule not to fire when unlinked")
	}
}
