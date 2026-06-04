package scheduler

import (
	"testing"
	"time"
)

func TestEvaluateSpecLimit(t *testing.T) {
	usl := 10.0
	spec := SpecLimitSpec{SpecLimits: &SpecLimitBounds{USL: &usl}, Mode: "spec"}
	result := EvaluateSpecLimit(Sample{Value: 12}, spec)
	if !result.Hit || result.Status != statusViolation {
		t.Fatalf("expected spec limit hit")
	}
}

func TestEvaluateShewhart(t *testing.T) {
	samples := []Sample{{Value: 10}, {Value: 10}, {Value: 10}, {Value: 10}, {Value: 10}, {Value: 10}, {Value: 20}}
	spec := ShewhartSpec{MinBaselineN: 5}
	result := EvaluateShewhart(samples, spec, 2)
	if !result.Hit || result.Status != statusViolation {
		t.Fatalf("expected shewhart hit")
	}
}

func TestEvaluateTrend6(t *testing.T) {
	samples := []Sample{{Value: 1}, {Value: 2}, {Value: 3}, {Value: 4}, {Value: 5}, {Value: 6}}
	result := EvaluateTrend6(samples, TrendSpec{WindowSize: 6, Epsilon: 0})
	if !result.Hit || result.Status != statusViolation {
		t.Fatalf("expected trend hit")
	}
}

func TestEvaluateRangeChart(t *testing.T) {
	groups := [][]Sample{
		{{Value: 1}, {Value: 2}},
		{{Value: 1}, {Value: 2}},
		{{Value: 1}, {Value: 2}},
		{{Value: 1}, {Value: 2}},
		{{Value: 1}, {Value: 20}},
	}
	spec := RangeChartSpec{SubgroupSize: 2, MinBaselineSubgroups: 5}
	result := EvaluateRangeChart(groups, spec)
	if !result.Hit || result.Status != statusViolation {
		t.Fatalf("expected range chart hit")
	}
}

func TestEvaluateRangeChartInvalidSubgroupSize(t *testing.T) {
	groups := [][]Sample{
		{{Value: 1}, {Value: 2}},
		{{Value: 1}, {Value: 3}},
		{{Value: 2}, {Value: 4}},
		{{Value: 2}, {Value: 5}},
		{{Value: 3}, {Value: 6}},
	}
	spec := RangeChartSpec{SubgroupSize: 12, MinBaselineSubgroups: 5}
	result := EvaluateRangeChart(groups, spec)
	if result.Status != statusInvalidConfig {
		t.Fatalf("expected invalid config status")
	}
}

func TestEvaluateTPA(t *testing.T) {
	threshold := 0.5
	spec := TPASpec{WindowN: 5, SlopeThreshold: &threshold, RegressionTimeBasis: "index"}
	samples := []Sample{{Value: 1}, {Value: 2}, {Value: 3}, {Value: 4}, {Value: 5}}
	result := EvaluateTPA(samples, spec)
	if !result.Hit || result.Status != statusViolation {
		t.Fatalf("expected tpa hit")
	}
}

func TestEvaluateTPATimestampBasis(t *testing.T) {
	// Regression: large Unix timestamps caused catastrophic float64 cancellation
	// in LinearRegression, making denom=0 and returning invalidConfig.
	// Centering x values before regression fixes this.
	threshold := 0.0
	spec := TPASpec{WindowN: 5, SlopeThreshold: &threshold, RegressionTimeBasis: "timestamp"}
	base := time.Now()
	samples := []Sample{
		{TS: base.Add(0 * time.Second), Value: 20},
		{TS: base.Add(5 * time.Second), Value: 40},
		{TS: base.Add(10 * time.Second), Value: 60},
		{TS: base.Add(15 * time.Second), Value: 80},
		{TS: base.Add(20 * time.Second), Value: 100},
	}
	result := EvaluateTPA(samples, spec)
	if result.Status == statusInvalidConfig {
		t.Fatalf("regression failed with timestamp basis (float64 cancellation bug): %v", result.Metadata)
	}
	if !result.Hit || result.Status != statusViolation {
		t.Fatalf("expected tpa hit with positive slope, got status=%s", result.Status)
	}
}

func TestEvaluateShewhartInsufficient(t *testing.T) {
	result := EvaluateShewhart([]Sample{{Value: 1}}, ShewhartSpec{MinBaselineN: 3}, 3)
	if result.Status != statusInsufficient {
		t.Fatalf("expected insufficient status")
	}
}
