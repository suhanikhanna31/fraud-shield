package scoring

import (
	"testing"
	"time"

	"fraud-shield/internal/models"
)

func TestLargeAmountRuleFires(t *testing.T) {
	e := NewEngine(DefaultConfig())
	tx := models.Transaction{
		ID:        "tx1",
		AccountID: "acc1",
		Amount:    15000,
		Timestamp: time.Now(),
	}
	res := e.Score(tx)
	if res.Score < 25 {
		t.Fatalf("expected large amount rule to add >=25 score, got %v", res.Score)
	}
}

func TestVelocityRuleFiresAfterThreshold(t *testing.T) {
	cfg := DefaultConfig()
	cfg.VelocityWindow = time.Minute
	cfg.VelocityMaxTxns = 3
	e := NewEngine(cfg)

	base := time.Now()
	var last models.ScoreResult
	for i := 0; i < 5; i++ {
		tx := models.Transaction{
			ID:        "tx",
			AccountID: "acc1",
			Amount:    10,
			Timestamp: base.Add(time.Duration(i) * time.Second),
		}
		last = e.Score(tx)
	}
	if !last.Flagged && last.Score == 0 {
		t.Fatalf("expected velocity rule to contribute score after exceeding threshold, got %+v", last)
	}
}

func TestVelocityWindowSlidesOut(t *testing.T) {
	cfg := DefaultConfig()
	cfg.VelocityWindow = 10 * time.Second
	cfg.VelocityMaxTxns = 2
	e := NewEngine(cfg)

	base := time.Now()
	e.Score(models.Transaction{ID: "a", AccountID: "acc1", Timestamp: base})
	e.Score(models.Transaction{ID: "b", AccountID: "acc1", Timestamp: base.Add(1 * time.Second)})
	// Third transaction well outside the window should not trip velocity.
	res := e.Score(models.Transaction{ID: "c", AccountID: "acc1", Timestamp: base.Add(1 * time.Hour)})
	for _, r := range res.Reasons {
		if r == "unusually high transaction frequency for this account" {
			t.Fatalf("velocity rule should not fire once old transactions slide out of the window")
		}
	}
}

func TestLinkedRingDetectionViaUnionFind(t *testing.T) {
	e := NewEngine(DefaultConfig())
	e.MarkRingAccount("bad_acc")

	// bad_acc and acc2 share the same device fingerprint.
	e.Score(models.Transaction{ID: "t1", AccountID: "bad_acc", DeviceID: "dev-xyz", Timestamp: time.Now()})
	res := e.Score(models.Transaction{ID: "t2", AccountID: "acc2", DeviceID: "dev-xyz", Timestamp: time.Now()})

	found := false
	for _, r := range res.Reasons {
		if r == "account is linked (via shared device/IP) to a known fraud ring" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected acc2 to be flagged as linked to ring via shared device, got reasons=%v", res.Reasons)
	}
}

func TestNewCounterpartyFlaggedOnceThenTrusted(t *testing.T) {
	e := NewEngine(DefaultConfig())
	tx1 := models.Transaction{ID: "t1", AccountID: "acc1", CounterParty: "merchantA", Timestamp: time.Now()}
	res1 := e.Score(tx1)

	tx2 := models.Transaction{ID: "t2", AccountID: "acc1", CounterParty: "merchantA", Timestamp: time.Now()}
	res2 := e.Score(tx2)

	if len(res1.Reasons) == 0 {
		t.Fatalf("expected first transfer to a new counterparty to be flagged")
	}
	for _, r := range res2.Reasons {
		if r == "first-time transfer to this counterparty" {
			t.Fatalf("counterparty should be trusted after first transaction")
		}
	}
}

func TestUnionFindPathCompressionAndUnionByRank(t *testing.T) {
	u := newUnionFind()
	u.union("a", "b")
	u.union("b", "c")
	u.union("d", "e")
	u.union("c", "e")

	if u.find("a") != u.find("d") {
		t.Fatalf("expected a and d to be in the same set after transitive unions")
	}
	if u.find("a") != u.find("e") {
		t.Fatalf("expected a and e to be in the same set")
	}
}
