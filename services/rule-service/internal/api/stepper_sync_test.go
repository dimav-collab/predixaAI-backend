package api

import (
	"encoding/json"
	"testing"

	"predixaai-backend/services/rule-service/internal/storage"
)

func makeUnit(connectionRef, table, tsCol string, columns []string) storage.MachineUnit {
	return storage.MachineUnit{
		UnitID:          "unit-1",
		UnitName:        "Test",
		ConnectionRef:   connectionRef,
		SelectedTable:   table,
		TimestampColumn: tsCol,
		SelectedColumns: columns,
	}
}

func makeRule(id, ruleType, paramID string, cfgJSON string) storage.StepperRule {
	cfg := json.RawMessage("{}")
	if cfgJSON != "" {
		cfg = json.RawMessage(cfgJSON)
	}
	return storage.StepperRule{
		ID:          id,
		UnitID:      "unit-1",
		Name:        "test-rule",
		RuleType:    ruleType,
		ParameterID: paramID,
		Config:      cfg,
		Enabled:     true,
	}
}

func TestBuildRuleSpecFromStepper_Trend6Points(t *testing.T) {
	rule := makeRule("abc-123", "TREND_6_POINTS", "etchers_data.rf_power",
		`{"windowSize":6,"epsilon":1,"requireConsecutiveTimestamps":true}`)
	unit := makeUnit("conn-1", "etchers_data", "run_order", []string{"rf_power"})

	spec, err := BuildRuleSpecFromStepper(rule, unit)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.ConnectionRef != "conn-1" {
		t.Errorf("want connectionRef=conn-1, got %q", spec.ConnectionRef)
	}
	if spec.Source.Table != "etchers_data" {
		t.Errorf("want table=etchers_data, got %q", spec.Source.Table)
	}
	if spec.Source.TimestampColumn != "run_order" {
		t.Errorf("want timestampColumn=run_order, got %q", spec.Source.TimestampColumn)
	}
	if len(spec.Parameters) != 1 {
		t.Fatalf("want 1 parameter, got %d", len(spec.Parameters))
	}
	param := spec.Parameters[0]
	if param.ValueColumn != "rf_power" {
		t.Errorf("want valueColumn=rf_power, got %q", param.ValueColumn)
	}
	if param.Detector.Type != "trend" {
		t.Errorf("want detector type=trend, got %q", param.Detector.Type)
	}
	if param.Detector.Trend == nil {
		t.Fatal("expected Trend spec to be set")
	}
	if param.Detector.Trend.WindowSize != 6 {
		t.Errorf("want windowSize=6, got %d", param.Detector.Trend.WindowSize)
	}
	if param.Detector.Trend.Epsilon != 1.0 {
		t.Errorf("want epsilon=1, got %f", param.Detector.Trend.Epsilon)
	}
	if !param.Detector.Trend.RequireConsecutiveTimestamps {
		t.Errorf("want requireConsecutiveTimestamps=true")
	}
}

func TestBuildRuleSpecFromStepper_Trend6Points_Defaults(t *testing.T) {
	// Config is empty — should use defaults
	rule := makeRule("abc-123", "TREND_6_POINTS", "tbl.col", "")
	unit := makeUnit("c", "tbl", "ts", []string{"col"})
	spec, err := BuildRuleSpecFromStepper(rule, unit)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	d := spec.Parameters[0].Detector.Trend
	if d == nil {
		t.Fatal("expected Trend spec")
	}
	if d.WindowSize != 6 {
		t.Errorf("default windowSize should be 6, got %d", d.WindowSize)
	}
	if !d.RequireConsecutiveTimestamps {
		t.Errorf("default requireConsecutiveTimestamps should be true")
	}
}

func TestBuildRuleSpecFromStepper_Shewhart3Sigma(t *testing.T) {
	rule := makeRule("id2", "SHEWHART_3SIGMA", "t.col", `{"minBaselineN":30}`)
	unit := makeUnit("c2", "t", "ts", []string{"col"})
	spec, err := BuildRuleSpecFromStepper(rule, unit)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	d := spec.Parameters[0].Detector
	if d.Type != "shewhart" {
		t.Errorf("want shewhart, got %q", d.Type)
	}
	if d.Shewhart == nil {
		t.Fatal("expected Shewhart spec")
	}
	if d.Shewhart.SigmaMultiplier != 3.0 {
		t.Errorf("want 3σ, got %f", d.Shewhart.SigmaMultiplier)
	}
	if d.Shewhart.MinBaselineN != 30 {
		t.Errorf("want minBaselineN=30, got %d", d.Shewhart.MinBaselineN)
	}
}

func TestBuildRuleSpecFromStepper_Shewhart2Sigma(t *testing.T) {
	rule := makeRule("id3", "SHEWHART_2SIGMA", "t.col", "")
	unit := makeUnit("c", "t", "ts", []string{"col"})
	spec, err := BuildRuleSpecFromStepper(rule, unit)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Parameters[0].Detector.Shewhart.SigmaMultiplier != 2.0 {
		t.Errorf("want 2σ")
	}
}

func TestBuildRuleSpecFromStepper_RangeChart(t *testing.T) {
	rule := makeRule("id4", "RANGE_CHART_R", "t.col",
		`{"subgroupSize":5,"minBaselineSubgroups":12,"subgrouping":{"kind":"consecutive"}}`)
	unit := makeUnit("c", "t", "ts", []string{"col"})
	spec, err := BuildRuleSpecFromStepper(rule, unit)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	d := spec.Parameters[0].Detector
	if d.Type != "range_chart" {
		t.Errorf("want range_chart, got %q", d.Type)
	}
	if d.RangeChart.SubgroupSize != 5 {
		t.Errorf("want subgroupSize=5, got %d", d.RangeChart.SubgroupSize)
	}
	if d.RangeChart.MinBaselineSubgroups != 12 {
		t.Errorf("want minBaselineSubgroups=12, got %d", d.RangeChart.MinBaselineSubgroups)
	}
}

func TestBuildRuleSpecFromStepper_SpecLimitViolation(t *testing.T) {
	rule := makeRule("id5", "SPEC_LIMIT_VIOLATION", "t.col",
		`{"mode":"spec","specLimits":{"usl":100,"lsl":10}}`)
	unit := makeUnit("c", "t", "ts", []string{"col"})
	spec, err := BuildRuleSpecFromStepper(rule, unit)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	d := spec.Parameters[0].Detector
	if d.Type != "spec_limit" {
		t.Errorf("want spec_limit, got %q", d.Type)
	}
	if d.SpecLimit == nil || d.SpecLimit.SpecLimits == nil {
		t.Fatal("expected SpecLimit with limits")
	}
	if d.SpecLimit.SpecLimits.USL == nil || *d.SpecLimit.SpecLimits.USL != 100 {
		t.Errorf("want USL=100")
	}
	if d.SpecLimit.SpecLimits.LSL == nil || *d.SpecLimit.SpecLimits.LSL != 10 {
		t.Errorf("want LSL=10")
	}
}

func TestBuildRuleSpecFromStepper_TPA(t *testing.T) {
	rule := makeRule("id6", "TPA", "t.col",
		`{"windowN":7,"regressionTimeBasis":"index","slopeThreshold":0.5}`)
	unit := makeUnit("c", "t", "ts", []string{"col"})
	spec, err := BuildRuleSpecFromStepper(rule, unit)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	d := spec.Parameters[0].Detector
	if d.Type != "tpa" {
		t.Errorf("want tpa, got %q", d.Type)
	}
	if d.TPA == nil {
		t.Fatal("expected TPA spec")
	}
	if d.TPA.WindowN != 7 {
		t.Errorf("want windowN=7, got %d", d.TPA.WindowN)
	}
	if d.TPA.RegressionTimeBasis != "index" {
		t.Errorf("want basis=index, got %q", d.TPA.RegressionTimeBasis)
	}
	if d.TPA.SlopeThreshold == nil || *d.TPA.SlopeThreshold != 0.5 {
		t.Errorf("want slopeThreshold=0.5")
	}
}

// Error paths

func TestBuildRuleSpecFromStepper_MissingRuleID(t *testing.T) {
	rule := makeRule("", "TREND_6_POINTS", "t.col", "")
	unit := makeUnit("c", "t", "ts", []string{"col"})
	_, err := BuildRuleSpecFromStepper(rule, unit)
	if err == nil {
		t.Fatal("expected error for empty rule ID")
	}
}

func TestBuildRuleSpecFromStepper_MissingConnectionRef(t *testing.T) {
	rule := makeRule("id", "TREND_6_POINTS", "t.col", "")
	unit := makeUnit("", "t", "ts", []string{"col"})
	_, err := BuildRuleSpecFromStepper(rule, unit)
	if err == nil {
		t.Fatal("expected error for missing connectionRef")
	}
}

func TestBuildRuleSpecFromStepper_MissingTable(t *testing.T) {
	rule := makeRule("id", "TREND_6_POINTS", "t.col", "")
	unit := makeUnit("c", "", "ts", []string{"col"})
	_, err := BuildRuleSpecFromStepper(rule, unit)
	if err == nil {
		t.Fatal("expected error for missing table")
	}
}

func TestBuildRuleSpecFromStepper_MissingTimestampColumn(t *testing.T) {
	rule := makeRule("id", "TREND_6_POINTS", "t.col", "")
	unit := makeUnit("c", "t", "", []string{"col"})
	_, err := BuildRuleSpecFromStepper(rule, unit)
	if err == nil {
		t.Fatal("expected error for missing timestamp_column")
	}
}

func TestBuildRuleSpecFromStepper_InvalidParameterID(t *testing.T) {
	rule := makeRule("id", "TREND_6_POINTS", "noDotHere", "")
	unit := makeUnit("c", "t", "ts", []string{"col"})
	_, err := BuildRuleSpecFromStepper(rule, unit)
	if err == nil {
		t.Fatal("expected error for invalid parameterId format")
	}
}

func TestBuildRuleSpecFromStepper_UnsupportedRuleType(t *testing.T) {
	rule := makeRule("id", "UNKNOWN_RULE_XYZ", "t.col", "")
	unit := makeUnit("c", "t", "ts", []string{"col"})
	_, err := BuildRuleSpecFromStepper(rule, unit)
	if err == nil {
		t.Fatal("expected error for unsupported rule type")
	}
}

func TestBuildRuleSpecFromStepper_InvalidConfigJSON(t *testing.T) {
	rule := makeRule("id", "TREND_6_POINTS", "t.col", `{notvalidjson`)
	unit := makeUnit("c", "t", "ts", []string{"col"})
	_, err := BuildRuleSpecFromStepper(rule, unit)
	if err == nil {
		t.Fatal("expected error for invalid config JSON")
	}
}

// Edge case: pollIntervalSeconds override from config
func TestBuildRuleSpecFromStepper_PollIntervalOverride(t *testing.T) {
	rule := makeRule("id", "TREND_6_POINTS", "t.col", `{"pollIntervalSeconds":30}`)
	unit := makeUnit("c", "t", "ts", []string{"col"})
	spec, err := BuildRuleSpecFromStepper(rule, unit)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.PollIntervalSeconds != 30 {
		t.Errorf("want pollIntervalSeconds=30, got %d", spec.PollIntervalSeconds)
	}
}

// Edge case: disabled rule propagates enabled=false
func TestBuildRuleSpecFromStepper_DisabledRule(t *testing.T) {
	rule := makeRule("id", "TREND_6_POINTS", "t.col", "")
	rule.Enabled = false
	unit := makeUnit("c", "t", "ts", []string{"col"})
	spec, err := BuildRuleSpecFromStepper(rule, unit)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Enabled {
		t.Errorf("want enabled=false on disabled rule")
	}
}
