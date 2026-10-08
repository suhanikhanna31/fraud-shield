package scoring

import (
	"testing"
	"time"
)

func TestVelocityCountsInsideWindow(t *testing.T) {
	v := newVelocityTracker(time.Minute)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 1; i <= 4; i++ {
		got := v.recordAndCount("a", base.Add(time.Duration(i)*time.Second))
		if got != i {
			t.Fatalf("event %d: count=%d want %d", i, got, i)
		}
	}
}

func TestVelocityPrunesExpiredTimestamps(t *testing.T) {
	v := newVelocityTracker(10 * time.Second)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	v.recordAndCount("a", base)
	v.recordAndCount("a", base.Add(2*time.Second))
	v.recordAndCount("a", base.Add(4*time.Second))

	// At 15s the cutoff is 5s, so all three earlier events have expired.
	got := v.recordAndCount("a", base.Add(15*time.Second))
	if got != 1 {
		t.Fatalf("count=%d want 1 after old events expired", got)
	}
	if v.size("a") != 1 {
		t.Fatalf("retained=%d want 1: pruned timestamps must be dropped", v.size("a"))
	}
}

func TestVelocityPartialPrune(t *testing.T) {
	v := newVelocityTracker(10 * time.Second)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	v.recordAndCount("a", base)                    // expires
	v.recordAndCount("a", base.Add(8*time.Second)) // stays
	got := v.recordAndCount("a", base.Add(12*time.Second))
	if got != 2 {
		t.Fatalf("count=%d want 2 (events at 8s and 12s)", got)
	}
}

func TestVelocityMemoryBoundedUnderSteadyLoad(t *testing.T) {
	v := newVelocityTracker(time.Minute)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// One event per second for an hour: at most ~61 should ever be retained.
	for i := 0; i < 3600; i++ {
		v.recordAndCount("a", base.Add(time.Duration(i)*time.Second))
	}
	if n := v.size("a"); n > 61 {
		t.Fatalf("retained %d timestamps, window should bound this to ~60", n)
	}
}

func TestVelocityAccountsAreIndependent(t *testing.T) {
	v := newVelocityTracker(time.Minute)
	now := time.Now()
	v.recordAndCount("a", now)
	v.recordAndCount("a", now)
	if got := v.recordAndCount("b", now); got != 1 {
		t.Fatalf("account b count=%d want 1", got)
	}
}
