package jobs

import "testing"

func TestFailTransitionRetriesWhenAttemptsRemain(t *testing.T) {
	t.Parallel()

	transition := failTransition(1, 3)

	if transition.NextState != "retry" {
		t.Fatalf("expected retry, got %q", transition.NextState)
	}
	if transition.NextAttempt != 2 {
		t.Fatalf("expected attempt 2, got %d", transition.NextAttempt)
	}
}

func TestFailTransitionFailsWhenMaxAttemptsReached(t *testing.T) {
	t.Parallel()

	transition := failTransition(2, 3)

	if transition.NextState != "failed" {
		t.Fatalf("expected failed, got %q", transition.NextState)
	}
	if transition.NextAttempt != 3 {
		t.Fatalf("expected attempt 3, got %d", transition.NextAttempt)
	}
}

func TestValidStateTransitions(t *testing.T) {
	t.Parallel()

	valid := [][2]string{{"queued", "running"}, {"running", "done"}, {"running", "retry"}, {"retry", "queued"}}
	for _, transition := range valid {
		if !validStateTransition(transition[0], transition[1]) {
			t.Fatalf("expected transition %s -> %s to be valid", transition[0], transition[1])
		}
	}

	invalid := [][2]string{{"queued", "done"}, {"done", "queued"}, {"retry", "done"}}
	for _, transition := range invalid {
		if validStateTransition(transition[0], transition[1]) {
			t.Fatalf("expected transition %s -> %s to be invalid", transition[0], transition[1])
		}
	}
}
