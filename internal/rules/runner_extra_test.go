package rules

import (
	"context"
	"sync"
	"testing"
)

// Two Steps at once must not fire one rule twice: the state is shared, and both
// would see the same untreated edge.
func TestConcurrentStepsFireOnce(t *testing.T) {
	set := one("r", "<", 180)
	r, exec, hm, _ := newTestRunner(t, set)
	hm.set("sensor", "temp", float64(250))
	r.Step(context.Background()) // prime

	hm.set("sensor", "temp", float64(150))
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); r.Step(context.Background()) }()
	}
	wg.Wait()
	if got := exec.sentCount(); got != 1 {
		t.Fatalf("the rule fired %d times under concurrent steps, want 1", got)
	}
}
