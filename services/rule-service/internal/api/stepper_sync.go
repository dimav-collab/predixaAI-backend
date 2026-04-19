package api

import (
	"encoding/json"
	"fmt"

	"predixaai-backend/services/rule-service/internal/rules"
	"predixaai-backend/services/rule-service/internal/storage"
)

const (
	defaultStepperPollIntervalSeconds = 60
)

// stepperConfig is a loose bag of config fields that any stepper rule type may use.
type stepperConfig struct {
	// TREND_6_POINTS
	WindowSize                   *int     `json:"windowSize,omitempty"`
	Epsilon                      *float64 `json:"epsilon,omitempty"`
	RequireConsecutiveTimestamps *bool    `json:"requireConsecutiveTimestamps,omitempty"`

	// SHEWHART_3SIGMA / SHEWHART_2SIGMA
	MinBaselineN    *int     `json:"minBaselineN,omitempty"`
	SigmaMultiplier *float64 `json:"sigmaMultiplier,omitempty"`
	Baseline        *struct {
		Selector *struct {
			Kind  string `json:"kind,omitempty"`
			Value *int   `json:"value,omitempty"`
			Start string `json:"start,omitempty"`
			End   string `json:"end,omitempty"`
		} `json:"selector,omitempty"`
		LastN     *int `json:"lastN,omitempty"`
		TimeRange *struct {
			Start string `json:"start"`
			End   string `json:"end"`
		} `json:"timeRange,omitempty"`
	} `json:"baseline,omitempty"`

	// RANGE_CHART_R
	SubgroupSize         *int    `json:"subgroupSize,omitempty"`
	MinBaselineSubgroups *int    `json:"minBaselineSubgroups,omitempty"`
	Subgrouping          *struct {
		Kind         string `json:"kind,omitempty"`
		Column       string `json:"column,omitempty"`
		SubgroupSize *int   `json:"subgroupSize,omitempty"`
	} `json:"subgrouping,omitempty"`

	// SPEC_LIMIT_VIOLATION
	Mode          string   `json:"mode,omitempty"`
	SpecLimitsUSL *float64 `json:"specLimits.usl,omitempty"`
	SpecLimitsLSL *float64 `json:"specLimits.lsl,omitempty"`
	ControlUCL    *float64 `json:"controlLimits.ucl,omitempty"`
	ControlLCL    *float64 `json:"controlLimits.lcl,omitempty"`
	SpecLimits    *struct {
		USL *float64 `json:"usl,omitempty"`
		LSL *float64 `json:"lsl,omitempty"`
	} `json:"specLimits,omitempty"`
	ControlLimits *struct {
		UCL *float64 `json:"ucl,omitempty"`
		LCL *float64 `json:"lcl,omitempty"`
	} `json:"controlLimits,omitempty"`

	// TPA
	WindowN             *int     `json:"windowN,omitempty"`
	RegressionTimeBasis string   `json:"regressionTimeBasis,omitempty"`
	SlopeThreshold      *float64 `json:"slopeThreshold,omitempty"`
	TimeToSpecThreshold *float64 `json:"timeToSpecThreshold,omitempty"`
	RequireSpecLimits   *bool    `json:"requireSpecLimits,omitempty"`

	// PollIntervalSeconds override
	PollIntervalSeconds *int `json:"pollIntervalSeconds,omitempty"`
	CooldownSeconds     *int `json:"cooldownSeconds,omitempty"`
}

// BuildRuleSpecFromStepper converts a ui_rules record + its machine_unit into a
// scheduler-compatible RuleSpec. Returns an error when required fields are missing.
func BuildRuleSpecFromStepper(rule storage.StepperRule, unit storage.MachineUnit) (rules.RuleSpec, error) {
	if rule.ID == "" {
		return rules.RuleSpec{}, fmt.Errorf("rule id is required")
	}
	if unit.ConnectionRef == "" {
		return rules.RuleSpec{}, fmt.Errorf("machine unit has no connection_ref")
	}
	if unit.SelectedTable == "" {
		return rules.RuleSpec{}, fmt.Errorf("machine unit has no selected_table")
	}
	if unit.TimestampColumn == "" {
		return rules.RuleSpec{}, fmt.Errorf("machine unit has no timestamp_column")
	}

	_, valueColumn := parseParameterID(rule.ParameterID)
	if valueColumn == "" {
		return rules.RuleSpec{}, fmt.Errorf("invalid parameterId %q: expected 'table.column'", rule.ParameterID)
	}

	var cfg stepperConfig
	if len(rule.Config) > 0 && string(rule.Config) != "{}" && string(rule.Config) != "null" {
		if err := json.Unmarshal(rule.Config, &cfg); err != nil {
			return rules.RuleSpec{}, fmt.Errorf("invalid config json: %w", err)
		}
	}

	poll := defaultStepperPollIntervalSeconds
	if cfg.PollIntervalSeconds != nil && *cfg.PollIntervalSeconds > 0 {
		poll = *cfg.PollIntervalSeconds
	}

	detector, err := buildDetector(rule.RuleType, cfg)
	if err != nil {
		return rules.RuleSpec{}, err
	}

	spec := rules.RuleSpec{
		Name:          rule.Name,
		Description:   "",
		ConnectionRef: unit.ConnectionRef,
		Source: rules.SourceSpec{
			Table:           unit.SelectedTable,
			TimestampColumn: unit.TimestampColumn,
		},
		Parameters: []rules.ParameterSpec{
			{
				ParameterName: valueColumn,
				ValueColumn:   valueColumn,
				Detector:      detector,
			},
		},
		PollIntervalSeconds: poll,
		CooldownSeconds:     cfg.CooldownSeconds,
		Enabled:             rule.Enabled,
	}
	return spec, nil
}

func buildDetector(ruleType string, cfg stepperConfig) (rules.DetectorSpec, error) {
	switch ruleType {
	case "TREND_6_POINTS":
		window := 6
		if cfg.WindowSize != nil && *cfg.WindowSize > 0 {
			window = *cfg.WindowSize
		}
		epsilon := 0.0
		if cfg.Epsilon != nil {
			epsilon = *cfg.Epsilon
		}
		requireConsecutive := true
		if cfg.RequireConsecutiveTimestamps != nil {
			requireConsecutive = *cfg.RequireConsecutiveTimestamps
		}
		return rules.DetectorSpec{
			Type: "trend",
			Trend: &rules.TrendSpec{
				WindowSize:                   window,
				Epsilon:                      epsilon,
				RequireConsecutiveTimestamps: requireConsecutive,
			},
		}, nil

	case "SHEWHART_3SIGMA", "SHEWHART_2SIGMA":
		sigma := 3.0
		if ruleType == "SHEWHART_2SIGMA" {
			sigma = 2.0
		}
		if cfg.SigmaMultiplier != nil && *cfg.SigmaMultiplier > 0 {
			sigma = *cfg.SigmaMultiplier
		}
		minN := 20
		if cfg.MinBaselineN != nil && *cfg.MinBaselineN > 0 {
			minN = *cfg.MinBaselineN
		}
		baseline := buildBaselineSpec(cfg)
		return rules.DetectorSpec{
			Type: "shewhart",
			Shewhart: &rules.ShewhartSpec{
				SigmaMultiplier: sigma,
				MinBaselineN:    minN,
				Baseline:        baseline,
			},
		}, nil

	case "RANGE_CHART_R":
		sgSize := 5
		if cfg.SubgroupSize != nil && *cfg.SubgroupSize > 0 {
			sgSize = *cfg.SubgroupSize
		}
		minSG := 10
		if cfg.MinBaselineSubgroups != nil && *cfg.MinBaselineSubgroups > 0 {
			minSG = *cfg.MinBaselineSubgroups
		}
		mode := "consecutive"
		subgroupCol := ""
		if cfg.Subgrouping != nil {
			if cfg.Subgrouping.Kind != "" {
				mode = cfg.Subgrouping.Kind
			}
			if cfg.Subgrouping.Column != "" {
				subgroupCol = cfg.Subgrouping.Column
			}
			if cfg.Subgrouping.SubgroupSize != nil && *cfg.Subgrouping.SubgroupSize > 0 {
				sgSize = *cfg.Subgrouping.SubgroupSize
			}
		}
		baseline := buildBaselineSpec(cfg)
		return rules.DetectorSpec{
			Type: "range_chart",
			RangeChart: &rules.RangeChartSpec{
				SubgroupSize: sgSize,
				Subgrouping: rules.SubgroupingSpec{
					Mode:   mode,
					Column: subgroupCol,
				},
				MinBaselineSubgroups: minSG,
				Baseline:             baseline,
			},
		}, nil

	case "SPEC_LIMIT_VIOLATION":
		mode := "spec"
		if cfg.Mode != "" {
			mode = cfg.Mode
		}
		specLimit := &rules.SpecLimitSpec{Mode: mode, Epsilon: cfg.Epsilon}
		// Nested specLimits object
		if cfg.SpecLimits != nil && (cfg.SpecLimits.USL != nil || cfg.SpecLimits.LSL != nil) {
			specLimit.SpecLimits = &rules.SpecLimitBounds{
				USL: cfg.SpecLimits.USL,
				LSL: cfg.SpecLimits.LSL,
			}
		} else if cfg.SpecLimitsUSL != nil || cfg.SpecLimitsLSL != nil {
			// flat dotted-key style
			specLimit.SpecLimits = &rules.SpecLimitBounds{
				USL: cfg.SpecLimitsUSL,
				LSL: cfg.SpecLimitsLSL,
			}
		}
		if cfg.ControlLimits != nil && (cfg.ControlLimits.UCL != nil || cfg.ControlLimits.LCL != nil) {
			specLimit.ControlLimits = &rules.ControlLimitBounds{
				UCL: cfg.ControlLimits.UCL,
				LCL: cfg.ControlLimits.LCL,
			}
		} else if cfg.ControlUCL != nil || cfg.ControlLCL != nil {
			specLimit.ControlLimits = &rules.ControlLimitBounds{
				UCL: cfg.ControlUCL,
				LCL: cfg.ControlLCL,
			}
		}
		return rules.DetectorSpec{
			Type:      "spec_limit",
			SpecLimit: specLimit,
		}, nil

	case "TPA":
		windowN := 5
		if cfg.WindowN != nil && *cfg.WindowN > 0 {
			windowN = *cfg.WindowN
		}
		basis := "timestamp"
		if cfg.RegressionTimeBasis != "" {
			basis = cfg.RegressionTimeBasis
		}
		requireSpec := false
		if cfg.RequireSpecLimits != nil {
			requireSpec = *cfg.RequireSpecLimits
		}
		epsilon := 0.0
		if cfg.Epsilon != nil {
			epsilon = *cfg.Epsilon
		}
		tpa := &rules.TPASpec{
			WindowN:             windowN,
			RegressionTimeBasis: basis,
			SlopeThreshold:      cfg.SlopeThreshold,
			TimeToSpecThreshold: cfg.TimeToSpecThreshold,
			RequireSpecLimits:   requireSpec,
			Epsilon:             epsilon,
		}
		if cfg.SpecLimits != nil {
			tpa.SpecLimits = &rules.SpecLimitBounds{
				USL: cfg.SpecLimits.USL,
				LSL: cfg.SpecLimits.LSL,
			}
		}
		return rules.DetectorSpec{
			Type: "tpa",
			TPA:  tpa,
		}, nil

	default:
		return rules.DetectorSpec{}, fmt.Errorf("unsupported rule_type %q", ruleType)
	}
}

// buildBaselineSpec converts the baseline config for Shewhart/RangeChart.
func buildBaselineSpec(cfg stepperConfig) rules.BaselineSpec {
	if cfg.Baseline == nil {
		return rules.BaselineSpec{}
	}
	b := cfg.Baseline
	// Nested selector style (from UI)
	if b.Selector != nil {
		sel := b.Selector
		switch sel.Kind {
		case "lastN":
			if sel.Value != nil {
				return rules.BaselineSpec{LastN: sel.Value}
			}
		case "dateRange":
			if sel.Start != "" && sel.End != "" {
				return rules.BaselineSpec{TimeRange: &rules.TimeRangeSpec{Start: sel.Start, End: sel.End}}
			}
		}
	}
	if b.LastN != nil {
		return rules.BaselineSpec{LastN: b.LastN}
	}
	if b.TimeRange != nil {
		return rules.BaselineSpec{TimeRange: &rules.TimeRangeSpec{Start: b.TimeRange.Start, End: b.TimeRange.End}}
	}
	return rules.BaselineSpec{}
}
