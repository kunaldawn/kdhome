package main

import "testing"

// resetProbeStates clears the in-memory debounce map so subtests start clean.
func resetProbeStates() {
	probeStateMu.Lock()
	probeStates = map[string]*probeState{}
	probeStateMu.Unlock()
}

func TestDebounceProbe(t *testing.T) {
	resetProbeStates()

	t.Run("first probe success reports up", func(t *testing.T) {
		if !debounceProbe("up-first", true) {
			t.Fatal("a successful first probe must report up")
		}
	})

	// Regression: an archive that has never succeeded (e.g. a dead DNS name
	// right after a restart) must NOT show a misleading "online" — it has no
	// "up" baseline to protect, so it's down on its first failure.
	t.Run("never-succeeded archive is down on first failure", func(t *testing.T) {
		if debounceProbe("dead", false) {
			t.Fatal("an archive that has never responded OK must be down on its first failure")
		}
		// stays down while it keeps failing
		for i := 2; i <= 6; i++ {
			if debounceProbe("dead", false) {
				t.Fatalf("failure #%d on a never-up archive: must remain down", i)
			}
		}
	})

	// The 5-consecutive-failure grace only protects an archive that WAS up.
	t.Run("established archive flips down only on the 5th consecutive failure", func(t *testing.T) {
		const id = "flapper"
		if !debounceProbe(id, true) { // establish an "up" baseline
			t.Fatal("precondition: a success should report up")
		}
		for i := 1; i <= 4; i++ {
			if !debounceProbe(id, false) {
				t.Fatalf("failure #%d: an established archive should still be up (threshold 5)", i)
			}
		}
		if debounceProbe(id, false) {
			t.Fatal("failure #5: should be down")
		}
		if debounceProbe(id, false) {
			t.Fatal("failure #6: should remain down")
		}
	})

	t.Run("a single success resets the counter and recovers immediately", func(t *testing.T) {
		const id = "recoverer"
		debounceProbe(id, true) // baseline up
		for i := 0; i < 5; i++ {
			debounceProbe(id, false)
		}
		if debounceProbe(id, false) {
			t.Fatal("precondition: should be down after 5+ failures")
		}
		if !debounceProbe(id, true) {
			t.Fatal("a success should recover immediately (successThreshold 1)")
		}
		for i := 1; i <= 4; i++ {
			if !debounceProbe(id, false) {
				t.Fatalf("post-recovery failure #%d should not flip down (counter was reset)", i)
			}
		}
	})

	t.Run("interleaved success keeps an established archive up across short failure runs", func(t *testing.T) {
		const id = "interleaved"
		// Establish up, then 3 fails, a success, 3 more fails — never 5 in a
		// row after the baseline, so it must stay up the whole time.
		for i, ok := range []bool{true, false, false, false, true, false, false, false} {
			if !debounceProbe(id, ok) {
				t.Fatalf("step %d (raw ok=%v): should remain up, never 5 consecutive failures", i, ok)
			}
		}
	})
}
