package scheduler

import (
	"testing"
	"time"
)

// TestSpecLimitWindowNewViolations verifies that when lastRunAt is provided, only
// samples with TS > lastRunAt are evaluated and each violation generates its own result.
func TestSpecLimitWindowNewViolations(t *testing.T) {
	lsl := 2.0
	usl := 4.0
	specLimitSpec := SpecLimitSpec{
		Mode:       "spec",
		SpecLimits: &SpecLimitBounds{LSL: &lsl, USL: &usl},
	}

	now := time.Now().UTC()
	samples := []Sample{
		{TS: now.Add(-10 * time.Second), Value: 1.0}, // below LSL — violation
		{TS: now.Add(-5 * time.Second), Value: 3.0},  // in spec — no violation
		{TS: now.Add(-2 * time.Second), Value: 5.0},  // above USL — violation
	}

	results := make([]DetectorResult, 0)
	for _, s := range samples {
		r := EvaluateSpecLimit(s, specLimitSpec)
		if r.Hit {
			results = append(results, r)
		}
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 violations, got %d", len(results))
	}
	for _, r := range results {
		if !r.Hit {
			t.Errorf("expected Hit=true for a violation result")
		}
	}
}

// TestSpecLimitWindowNoNewViolations verifies that all-in-spec samples produce no violations.
func TestSpecLimitWindowNoNewViolations(t *testing.T) {
	lsl := 2.0
	usl := 4.0
	specLimitSpec := SpecLimitSpec{
		Mode:       "spec",
		SpecLimits: &SpecLimitBounds{LSL: &lsl, USL: &usl},
	}

	now := time.Now().UTC()
	samples := []Sample{
		{TS: now.Add(-10 * time.Second), Value: 2.5},
		{TS: now.Add(-5 * time.Second), Value: 3.0},
		{TS: now.Add(-2 * time.Second), Value: 3.8},
	}

	results := make([]DetectorResult, 0)
	for _, s := range samples {
		r := EvaluateSpecLimit(s, specLimitSpec)
		if r.Hit {
			results = append(results, r)
		}
	}

	if len(results) != 0 {
		t.Fatalf("expected 0 violations, got %d", len(results))
	}
}

// TestSpecLimitWindowEmpty verifies that an empty sample slice yields no violations.
func TestSpecLimitWindowEmpty(t *testing.T) {
	lsl := 2.0
	usl := 4.0
	specLimitSpec := SpecLimitSpec{
		Mode:       "spec",
		SpecLimits: &SpecLimitBounds{LSL: &lsl, USL: &usl},
	}

	samples := []Sample{}
	results := make([]DetectorResult, 0)
	for _, s := range samples {
		r := EvaluateSpecLimit(s, specLimitSpec)
		if r.Hit {
			results = append(results, r)
		}
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results for empty window, got %d", len(results))
	}
}

// TestSetLastRunAtAdvancesWindow checks that setLastRunAt updates the in-memory job state.
func TestSetLastRunAtAdvancesWindow(t *testing.T) {
	job := &Job{
		ruleID: "test-rule",
		spec:   RuleSpec{},
		stop:   make(chan struct{}),
	}
	reg := &Registry{
		jobs: map[string]*Job{},
		repo: nil, // DB not needed for in-memory test
	}
	if got := reg.getLastRunAt(job); got != nil {
		t.Fatalf("expected nil lastRunAt before first run, got %v", got)
	}

	now := time.Now().UTC()
	job.lastRunMu.Lock()
	job.lastRunAt = &now
	job.lastRunMu.Unlock()

	got := reg.getLastRunAt(job)
	if got == nil {
		t.Fatal("expected non-nil lastRunAt after set")
	}
	if !got.Equal(now) {
		t.Errorf("expected lastRunAt=%v, got %v", now, *got)
	}
}

// TestWindowSinceWithLastRunAt verifies that windowSince returns lastRunAt when it is set.
func TestWindowSinceWithLastRunAt(t *testing.T) {
	spec := RuleSpec{PollIntervalSeconds: 60}
	last := time.Now().UTC().Add(-45 * time.Second)
	got := windowSince(spec, &last)
	if !got.Equal(last) {
		t.Errorf("expected windowSince to return lastRunAt=%v, got %v", last, got)
	}
}

// TestWindowSinceFallbackToPollInterval verifies that on first run (no lastRunAt)
// the window start is approximately now - pollIntervalSeconds.
func TestWindowSinceFallbackToPollInterval(t *testing.T) {
	spec := RuleSpec{PollIntervalSeconds: 60}
	before := time.Now().UTC()
	got := windowSince(spec, nil)
	after := time.Now().UTC()

	expectedMin := before.Add(-61 * time.Second)
	expectedMax := after.Add(-59 * time.Second)

	if got.Before(expectedMin) || got.After(expectedMax) {
		t.Errorf("windowSince fallback=%v not in expected range [%v, %v]", got, expectedMin, expectedMax)
	}
}

// TestWindowSinceFallbackDefaultInterval verifies that a zero pollInterval defaults to 60s.
func TestWindowSinceFallbackDefaultInterval(t *testing.T) {
	spec := RuleSpec{PollIntervalSeconds: 0}
	before := time.Now().UTC()
	got := windowSince(spec, nil)
	after := time.Now().UTC()

	expectedMin := before.Add(-61 * time.Second)
	expectedMax := after.Add(-59 * time.Second)

	if got.Before(expectedMin) || got.After(expectedMax) {
		t.Errorf("windowSince zero-interval fallback=%v not in range [%v, %v]", got, expectedMin, expectedMax)
	}
}
