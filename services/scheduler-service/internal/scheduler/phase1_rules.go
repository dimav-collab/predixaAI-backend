package scheduler

import (
	"fmt"
	"math"
	"time"
)

const (
	defaultBaselineMinN      = 20
	defaultBaselineSubgroups = 10
	defaultTrendWindowSize   = 6
	defaultTPAEpsilon        = 0.0
	defaultSpecLimitEpsilon  = 0.0
	defaultRegressionBasis   = "timestamp"
)

func EvaluateSpecLimit(sample Sample, spec SpecLimitSpec) DetectorResult {
	mode := spec.Mode
	if mode == "" {
		mode = "spec"
	}
	epsilon := defaultSpecLimitEpsilon
	if spec.Epsilon != nil {
		epsilon = *spec.Epsilon
	}
	result := DetectorResult{
		Hit:       false,
		Status:    statusOK,
		Severity:  "high",
		Observed:  fmt.Sprint(sample.Value),
		LimitExpr: mode,
		Metadata: map[string]any{
			"mode":    mode,
			"epsilon": epsilon,
		},
	}
	if !sample.TS.IsZero() {
		result.WindowStart = &sample.TS
		result.WindowEnd = &sample.TS
	}
	breach := func(limit float64, kind string) {
		result.Hit = true
		result.Status = statusViolation
		result.Metadata["limitBreached"] = kind
		result.Metadata["limitValue"] = limit
		result.Metadata["delta"] = sample.Value - limit
		addViolation(&result, Violation{
			Timestamp:  timePtr(sample.TS),
			Value:      sample.Value,
			Reason:     "limit_breach",
			LimitName:  kind,
			LimitValue: limit,
			Delta:      sample.Value - limit,
		})
	}
	if mode == "spec" || mode == "both" {
		if spec.SpecLimits == nil || (spec.SpecLimits.USL == nil && spec.SpecLimits.LSL == nil) {
			return invalidConfig("spec limits required")
		}
		if spec.SpecLimits.USL != nil {
			result.Metadata["spec_usl"] = *spec.SpecLimits.USL
		}
		if spec.SpecLimits.LSL != nil {
			result.Metadata["spec_lsl"] = *spec.SpecLimits.LSL
		}
		if spec.SpecLimits.USL != nil && sample.Value > *spec.SpecLimits.USL+epsilon {
			breach(*spec.SpecLimits.USL, "USL")
		}
		if spec.SpecLimits.LSL != nil && sample.Value < *spec.SpecLimits.LSL-epsilon {
			breach(*spec.SpecLimits.LSL, "LSL")
		}
	}
	if mode == "control" || mode == "both" {
		if spec.ControlLimits == nil || (spec.ControlLimits.UCL == nil && spec.ControlLimits.LCL == nil) {
			return invalidConfig("control limits required")
		}
		if spec.ControlLimits.UCL != nil {
			result.Metadata["control_ucl"] = *spec.ControlLimits.UCL
		}
		if spec.ControlLimits.LCL != nil {
			result.Metadata["control_lcl"] = *spec.ControlLimits.LCL
		}
		if spec.ControlLimits.UCL != nil && sample.Value > *spec.ControlLimits.UCL+epsilon {
			breach(*spec.ControlLimits.UCL, "UCL")
		}
		if spec.ControlLimits.LCL != nil && sample.Value < *spec.ControlLimits.LCL-epsilon {
			breach(*spec.ControlLimits.LCL, "LCL")
		}
	}
	return result
}

func EvaluateShewhart(samples []Sample, spec ShewhartSpec, sigmaMultiplier float64) DetectorResult {
	values := extractValues(samples)
	minBaseline := spec.MinBaselineN
	if minBaseline == 0 {
		minBaseline = defaultBaselineMinN
	}
	if len(values) < minBaseline {
		return insufficientData("baseline too small")
	}
	lastSample := samples[len(samples)-1]
	mean := Mean(values)
	sigma := StdDev(values, spec.PopulationSigma)
	ucl := mean + sigmaMultiplier*sigma
	lcl := mean - sigmaMultiplier*sigma
	latest := lastSample.Value
	result := DetectorResult{
		Hit:       false,
		Status:    statusOK,
		Severity:  "high",
		Observed:  fmt.Sprint(latest),
		LimitExpr: fmt.Sprintf("mean±%.1fσ", sigmaMultiplier),
		Metadata: map[string]any{
			"mu":              mean,
			"sigma":           sigma,
			"ucl":             ucl,
			"lcl":             lcl,
			"sigmaMultiplier": sigmaMultiplier,
		},
	}
	if sigma == 0 {
		if latest != mean {
			result.Hit = true
			result.Status = statusViolation
			result.Metadata["limitBreached"] = "mean"
			addViolation(&result, Violation{
				Timestamp:  timePtr(lastSample.TS),
				Value:      latest,
				Reason:     "mean_shift",
				LimitName:  "mean",
				LimitValue: mean,
				Delta:      latest - mean,
			})
		}
		return result
	}
	if latest > ucl {
		result.Hit = true
		result.Status = statusViolation
		result.Metadata["limitBreached"] = "UCL"
		result.Metadata["delta"] = latest - ucl
		addViolation(&result, Violation{
			Timestamp:  timePtr(lastSample.TS),
			Value:      latest,
			Reason:     "above_ucl",
			LimitName:  "UCL",
			LimitValue: ucl,
			Delta:      latest - ucl,
		})
	}
	if latest < lcl {
		result.Hit = true
		result.Status = statusViolation
		result.Metadata["limitBreached"] = "LCL"
		result.Metadata["delta"] = latest - lcl
		addViolation(&result, Violation{
			Timestamp:  timePtr(lastSample.TS),
			Value:      latest,
			Reason:     "below_lcl",
			LimitName:  "LCL",
			LimitValue: lcl,
			Delta:      latest - lcl,
		})
	}
	return result
}

func EvaluateTrend6(samples []Sample, spec TrendSpec) DetectorResult {
	window := spec.WindowSize
	if window == 0 {
		window = defaultTrendWindowSize
	}
	if len(samples) < window {
		return insufficientData("not enough points")
	}
	segment := samples[len(samples)-window:]
	epsilon := spec.Epsilon
	lastSample := segment[len(segment)-1]
	increasing := true
	decreasing := true
	for i := 0; i < len(segment)-1; i++ {
		if !(segment[i+1].Value > segment[i].Value+epsilon) {
			increasing = false
		}
		if !(segment[i+1].Value < segment[i].Value-epsilon) {
			decreasing = false
		}
	}
	result := DetectorResult{
		Hit:       false,
		Status:    statusOK,
		Severity:  "high",
		Observed:  fmt.Sprint(lastSample.Value),
		LimitExpr: fmt.Sprintf("trend_%d", window),
		Metadata: map[string]any{
			"direction":  "none",
			"windowSize": window,
			"epsilon":    epsilon,
		},
	}
	if increasing || decreasing {
		result.Hit = true
		result.Status = statusViolation
		if increasing {
			result.Metadata["direction"] = "up"
		} else {
			result.Metadata["direction"] = "down"
		}
		reason := "decreasing"
		if increasing {
			reason = "increasing"
		}
		idx := len(samples) - 1
		addViolation(&result, Violation{
			Timestamp:  timePtr(lastSample.TS),
			Index:      &idx,
			Value:      lastSample.Value,
			Reason:     reason,
			LimitName:  "trend",
			LimitValue: 0,
			Delta:      0,
		})
	}
	return result
}

func EvaluateRangeChart(groups [][]Sample, spec RangeChartSpec) DetectorResult {
	if len(groups) == 0 {
		return insufficientData("no valid subgroups")
	}
	minGroups := spec.MinBaselineSubgroups
	if minGroups == 0 {
		minGroups = defaultBaselineSubgroups
	}
	if len(groups) < minGroups {
		return insufficientData("baseline subgroups too small")
	}
	consts, ok := rangeChartConstants[spec.SubgroupSize]
	if !ok {
		return invalidConfig("unsupported subgroup size")
	}
	ranges := make([]float64, 0, len(groups))
	for _, group := range groups {
		ranges = append(ranges, subgroupRange(group))
	}
	avg := Mean(ranges)
	ucl := consts.D4 * avg
	lcl := consts.D3 * avg
	latestRange := ranges[len(ranges)-1]
	lastGroup := groups[len(groups)-1]
	lastSample := lastGroup[len(lastGroup)-1]
	result := DetectorResult{
		Hit:       false,
		Status:    statusOK,
		Severity:  "high",
		Observed:  fmt.Sprint(latestRange),
		LimitExpr: "range_chart",
		Metadata: map[string]any{
			"rbar":         avg,
			"ucl_r":        ucl,
			"lcl_r":        lcl,
			"subgroupSize": spec.SubgroupSize,
		},
	}
	if latestRange > ucl || latestRange < lcl {
		result.Hit = true
		result.Status = statusViolation
		result.Metadata["limitBreached"] = "range"
		result.Metadata["delta"] = latestRange - ucl
		reason := "above_ucl_r"
		limit := ucl
		if latestRange < lcl {
			reason = "below_lcl_r"
			limit = lcl
		}
		addViolation(&result, Violation{
			Timestamp:  timePtr(lastSample.TS),
			Value:      latestRange,
			Reason:     reason,
			LimitName:  "R",
			LimitValue: limit,
			Delta:      latestRange - limit,
		})
	}
	return result
}

func EvaluateTPA(samples []Sample, spec TPASpec) DetectorResult {
	if spec.WindowN < 3 {
		return invalidConfig("windowN must be >= 3")
	}
	if len(samples) < spec.WindowN {
		return insufficientData("not enough samples")
	}
	window := samples[len(samples)-spec.WindowN:]
	lastSample := window[len(window)-1]
	basis := spec.RegressionTimeBasis
	if basis == "" {
		basis = defaultRegressionBasis
	}
	xVals := make([]float64, 0, len(window))
	yVals := make([]float64, 0, len(window))
	for i, sample := range window {
		if basis == "timestamp" {
			xVals = append(xVals, float64(sample.TS.Unix()))
		} else {
			xVals = append(xVals, float64(i+1))
		}
		yVals = append(yVals, sample.Value)
	}
	// Center x values to avoid catastrophic cancellation with large Unix timestamps.
	if len(xVals) > 0 {
		x0 := xVals[0]
		for i := range xVals {
			xVals[i] -= x0
		}
	}
	slope, intercept, r2, ok := LinearRegression(xVals, yVals)
	if !ok {
		return invalidConfig("regression failed")
	}
	latest := window[len(window)-1].Value
	epsilon := spec.Epsilon
	if spec.Epsilon == 0 {
		epsilon = defaultTPAEpsilon
	}
	result := DetectorResult{
		Hit:       false,
		Status:    statusOK,
		Severity:  "high",
		Observed:  fmt.Sprint(latest),
		LimitExpr: "tpa",
		Metadata: map[string]any{
			"slope":         slope,
			"intercept":     intercept,
			"r2":            r2,
			"windowN":       spec.WindowN,
			"regressionBasis": basis,
		},
	}
	if math.Abs(slope) <= epsilon {
		return result
	}
	if spec.SlopeThreshold != nil && math.Abs(slope) >= *spec.SlopeThreshold {
		result.Hit = true
		result.Status = statusViolation
		result.Metadata["trigger"] = "slope"
		addViolation(&result, Violation{
			Timestamp:  timePtr(lastSample.TS),
			Value:      latest,
			Reason:     "slope_threshold",
			LimitName:  "slope",
			LimitValue: *spec.SlopeThreshold,
			Delta:      math.Abs(slope) - *spec.SlopeThreshold,
		})
	}
	if spec.TimeToSpecThreshold != nil {
		if spec.RequireSpecLimits && spec.SpecLimits == nil {
			return invalidConfig("spec limits required for timeToSpec")
		}
		if spec.SpecLimits != nil {
			timeToSpec, ok := computeTimeToSpec(slope, latest, spec.SpecLimits)
			if ok {
				result.Metadata["timeToSpec"] = timeToSpec
				if timeToSpec >= 0 && timeToSpec <= *spec.TimeToSpecThreshold {
					result.Hit = true
					result.Status = statusViolation
					result.Metadata["trigger"] = "timeToSpec"
					addViolation(&result, Violation{
						Timestamp:  timePtr(lastSample.TS),
						Value:      latest,
						Reason:     "time_to_spec",
						LimitName:  "timeToSpec",
						LimitValue: *spec.TimeToSpecThreshold,
						Delta:      timeToSpec - *spec.TimeToSpecThreshold,
					})
				}
			}
		}
	}
	return result
}

func computeTimeToSpec(slope float64, current float64, limits *SpecLimitBounds) (float64, bool) {
	if slope > 0 && limits.USL != nil {
		return (*limits.USL - current) / slope, true
	}
	if slope < 0 && limits.LSL != nil {
		return (current - *limits.LSL) / math.Abs(slope), true
	}
	return 0, false
}

func subgroupRange(group []Sample) float64 {
	min := math.Inf(1)
	max := math.Inf(-1)
	for _, sample := range group {
		if sample.Value < min {
			min = sample.Value
		}
		if sample.Value > max {
			max = sample.Value
		}
	}
	if math.IsInf(min, 0) || math.IsInf(max, 0) {
		return 0
	}
	return max - min
}

func extractValues(samples []Sample) []float64 {
	values := make([]float64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.Value)
	}
	return values
}

func addViolation(result *DetectorResult, violation Violation) {
	if result == nil {
		return
	}
	result.Violations = append(result.Violations, violation)
}

func timePtr(ts time.Time) *time.Time {
	if ts.IsZero() {
		return nil
	}
	return &ts
}

func invalidConfig(message string) DetectorResult {
	return DetectorResult{Hit: false, Status: statusInvalidConfig, Metadata: map[string]any{"error": message}}
}

func insufficientData(message string) DetectorResult {
	return DetectorResult{Hit: false, Status: statusInsufficient, Metadata: map[string]any{"error": message}}
}
