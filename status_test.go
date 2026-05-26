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

	t.Run("fresh archive starts up", func(t *testing.T) {
		if !debounceProbe("fresh", false) {
			t.Fatal("first failure must not flip a fresh archive down (optimistic start)")
		}
	})

	t.Run("flips down only on the 5th consecutive failure", func(t *testing.T) {
		const id = "flapper"
		for i := 1; i <= 4; i++ {
			if !debounceProbe(id, false) {
				t.Fatalf("failure #%d: effective should still be up (threshold 5), got down", i)
			}
		}
		if debounceProbe(id, false) {
			t.Fatal("failure #5: effective should be down")
		}
		if debounceProbe(id, false) {
			t.Fatal("failure #6: effective should remain down")
		}
	})

	t.Run("a single success resets the counter and recovers immediately", func(t *testing.T) {
		const id = "recoverer"
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

	t.Run("interleaved success keeps it up across short failure runs", func(t *testing.T) {
		const id = "interleaved"
		// 3 fails, a success, 3 more fails — never 5 in a row, must stay up.
		for i, ok := range []bool{false, false, false, true, false, false, false} {
			if !debounceProbe(id, ok) {
				t.Fatalf("step %d (raw ok=%v): should remain up, never 5 consecutive failures", i, ok)
			}
		}
	})
}
