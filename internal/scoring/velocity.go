package scoring

import "time"

// velocityTracker keeps a per-account sliding window of transaction
// timestamps. Each call appends the new timestamp and prunes everything that
// fell out of the window. Every timestamp is appended once and pruned at most
// once, so the cost per call is amortized O(1).
type velocityTracker struct {
	window  time.Duration
	history map[string][]time.Time
}

func newVelocityTracker(window time.Duration) *velocityTracker {
	return &velocityTracker{
		window:  window,
		history: make(map[string][]time.Time),
	}
}

// recordAndCount records ts for the account and returns how many
// transactions (including this one) remain inside the window.
func (v *velocityTracker) recordAndCount(accountID string, ts time.Time) int {
	h := append(v.history[accountID], ts)

	cutoff := ts.Add(-v.window)
	i := 0
	for i < len(h) && h[i].Before(cutoff) {
		i++
	}
	if i > 0 {
		// Copy the survivors to a fresh slice so pruned timestamps are
		// released instead of pinned by the old backing array.
		h = append([]time.Time(nil), h[i:]...)
	}

	v.history[accountID] = h
	return len(h)
}

// size returns the number of timestamps currently retained for an account.
func (v *velocityTracker) size(accountID string) int {
	return len(v.history[accountID])
}
