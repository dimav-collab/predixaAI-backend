package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"predixaai-backend/services/scheduler-service/internal/mcp"
	"predixaai-backend/services/scheduler-service/internal/monitor"
	"predixaai-backend/services/scheduler-service/internal/security"
	"predixaai-backend/services/scheduler-service/internal/storage"
)

type Registry struct {
	mu         sync.Mutex
	jobs       map[string]*Job
	queue      chan JobRun
	workers    int
	repo       *storage.Repository
	ctx        context.Context
	cancel     context.CancelFunc
	jobTimeout time.Duration
	limits     security.Limits
}

type Job struct {
	ruleID    string
	spec      RuleSpec
	adapter   mcp.DbMcpAdapter
	stop      chan struct{}
	lastRunMu sync.Mutex
	lastRunAt *time.Time // in-memory cache; persisted to DB after each run
}

type JobInfo struct {
	RuleID             string `json:"ruleId"`
	PollIntervalSecond int    `json:"pollIntervalSeconds"`
}

type JobRun struct {
	ruleID  string
	job     *Job
	spec    RuleSpec
	adapter mcp.DbMcpAdapter
}

func NewRegistry(repo *storage.Repository, limits security.Limits, workers int, jobTimeout time.Duration) *Registry {
	ctx, cancel := context.WithCancel(context.Background())
	reg := &Registry{
		jobs:       map[string]*Job{},
		queue:      make(chan JobRun, 128),
		workers:    workers,
		repo:       repo,
		ctx:        ctx,
		cancel:     cancel,
		jobTimeout: jobTimeout,
		limits:     limits,
	}
	for i := 0; i < workers; i++ {
		go reg.worker()
	}
	return reg
}

func (r *Registry) Stop() {
	r.cancel()
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, job := range r.jobs {
		close(job.stop)
	}
	r.jobs = map[string]*Job{}
}

func (r *Registry) Schedule(ruleID string, spec RuleSpec, adapter mcp.DbMcpAdapter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.jobs[ruleID]; ok {
		close(existing.stop)
	}
	job := &Job{ruleID: ruleID, spec: spec, adapter: adapter, stop: make(chan struct{})}
	// Load persisted last_run_at from DB so we do not re-evaluate old data on restart
	if dbRec, err := r.repo.GetRule(context.Background(), ruleID); err == nil && dbRec.LastRunAt != nil {
		t := *dbRec.LastRunAt
		job.lastRunAt = &t
	}
	r.jobs[ruleID] = job
	go r.runTicker(job)
}

func (r *Registry) Unschedule(ruleID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if job, ok := r.jobs[ruleID]; ok {
		close(job.stop)
		delete(r.jobs, ruleID)
	}
}

func (r *Registry) ListJobs() []JobInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	jobs := make([]JobInfo, 0, len(r.jobs))
	for id, job := range r.jobs {
		jobs = append(jobs, JobInfo{RuleID: id, PollIntervalSecond: job.spec.PollIntervalSeconds})
	}
	return jobs
}

func (r *Registry) runTicker(job *Job) {
	ticker := time.NewTicker(time.Duration(job.spec.PollIntervalSeconds) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			r.queue <- JobRun{ruleID: job.ruleID, job: job, spec: job.spec, adapter: job.adapter}
		case <-job.stop:
			return
		case <-r.ctx.Done():
			return
		}
	}
}

func (r *Registry) worker() {
	for {
		select {
		case run := <-r.queue:
			r.execute(run)
		case <-r.ctx.Done():
			return
		}
	}
}

// getLastRunAt returns the last run time for a job, guarded by its mutex.
func (r *Registry) getLastRunAt(job *Job) *time.Time {
	job.lastRunMu.Lock()
	defer job.lastRunMu.Unlock()
	if job.lastRunAt == nil {
		return nil
	}
	t := *job.lastRunAt
	return &t
}

// setLastRunAt updates the in-memory and persisted last run timestamp.
func (r *Registry) setLastRunAt(ctx context.Context, job *Job, ts time.Time) {
	job.lastRunMu.Lock()
	job.lastRunAt = &ts
	job.lastRunMu.Unlock()
	_ = r.repo.UpdateLastRunAt(ctx, job.ruleID, ts)
}

// windowSince returns the start of the evaluation window.
//   - When lastRunAt is known (subsequent runs): use that timestamp so only new rows are evaluated.
//   - On the first run (lastRunAt is nil): fall back to now - pollIntervalSeconds so the
//     window exactly matches one schedule period and we do not flood with historical alerts.
func windowSince(spec RuleSpec, lastRunAt *time.Time) time.Time {
	if lastRunAt != nil {
		return *lastRunAt
	}
	interval := spec.PollIntervalSeconds
	if interval <= 0 {
		interval = 60
	}
	return time.Now().UTC().Add(-time.Duration(interval) * time.Second)
}

func (r *Registry) execute(run JobRun) {
	ctx, cancel := context.WithTimeout(context.Background(), r.jobTimeout)
	defer cancel()

	runAt := time.Now().UTC()
	params := normalizeParameters(run.spec)
	if len(params) == 0 {
		return
	}

	lastRunAt := r.getLastRunAt(run.job)

	for _, param := range params {
		results, err := r.evaluateParameterSince(ctx, run.spec, param, run.adapter, lastRunAt)
		if err != nil {
			continue
		}
		for _, result := range results {
			if !result.Hit {
				continue
			}
			cooldown := 0
			if run.spec.CooldownSeconds != nil {
				cooldown = *run.spec.CooldownSeconds
			}
			if cooldown > 0 {
				lastAlert, err := r.repo.GetLastAlertForKey(ctx, run.ruleID, param.ParameterName, param.Detector.Type)
				if err == nil && monitor.WithinCooldown(lastAlert, cooldown) {
					continue
				}
			}
			metadataMap := map[string]any{
				"table":           run.spec.Source.Table,
				"valueColumn":     param.ValueColumn,
				"timestampColumn": run.spec.Source.TimestampColumn,
				"detector":        param.Detector.Type,
			}
			for k, v := range result.Metadata {
				metadataMap[k] = v
			}
			if result.WindowStart != nil {
				metadataMap["windowStart"] = result.WindowStart.Format(time.RFC3339)
			}
			if result.WindowEnd != nil {
				metadataMap["windowEnd"] = result.WindowEnd.Format(time.RFC3339)
			}
			if result.BaselineStart != nil {
				metadataMap["baselineStart"] = result.BaselineStart.Format(time.RFC3339)
			}
			if result.BaselineEnd != nil {
				metadataMap["baselineEnd"] = result.BaselineEnd.Format(time.RFC3339)
			}
			if len(result.Violations) > 0 {
				metadataMap["violations"] = result.Violations
			}
			metadataMap["explain"] = buildExplain(result, param)
			metadata, _ := json.Marshal(metadataMap)
			_ = r.repo.CreateAlert(ctx, storage.AlertRecord{
				RuleID:         run.ruleID,
				TSUTC:          time.Now().UTC(),
				ParameterName:  param.ParameterName,
				ObservedValue:  result.Observed,
				LimitExpr:      result.LimitExpr,
				DetectorType:   param.Detector.Type,
				Severity:       result.Severity,
				AnomalyScore:   result.AnomalyScore,
				BaselineMedian: result.BaselineMedian,
				BaselineMAD:    result.BaselineMAD,
				Hit:            true,
				Treated:        false,
				Metadata:       metadata,
			})
		}
	}

	// Advance the window: next run will only see rows newer than this run started.
	r.setLastRunAt(ctx, run.job, runAt)
}

// evaluateParameterSince evaluates a parameter for all new rows in the current poll window.
// The window is [windowSince(spec, lastRunAt), now].
// For spec_limit it returns one DetectorResult per violating row.
// For all other detectors it returns a single result (unchanged behaviour).
func (r *Registry) evaluateParameterSince(ctx context.Context, spec RuleSpec, param ParameterSpec, adapter mcp.DbMcpAdapter, lastRunAt *time.Time) ([]DetectorResult, error) {
	if adapter == nil {
		return nil, errors.New("adapter not configured")
	}

	switch param.Detector.Type {
	case "missing_data":
		if param.Detector.MissingData == nil {
			return nil, errors.New("missing_data detector missing config")
		}
		queryCtx, cancel := context.WithTimeout(ctx, r.limits.MaxQueryDuration)
		defer cancel()
		resp, err := adapter.QueryLatestValue(queryCtx, mcp.LatestValueRequest{
			ConnectionRef:   spec.ConnectionRef,
			Table:           spec.Source.Table,
			ValueColumn:     spec.Source.TimestampColumn,
			TimestampColumn: spec.Source.TimestampColumn,
			Where:           toWhere(spec.Source.Where),
		})
		if err != nil {
			if strings.Contains(err.Error(), "no rows") {
				return []DetectorResult{EvaluateMissingData(time.Time{}, param.Detector.MissingData.MaxGapSeconds, time.Now().UTC())}, nil
			}
			return nil, err
		}
		timestamp, err := parseTimeValue(resp.Value)
		if err != nil {
			timestamp, err = parseTimeValue(resp.TS)
			if err != nil {
				return nil, err
			}
		}
		return []DetectorResult{EvaluateMissingData(timestamp, param.Detector.MissingData.MaxGapSeconds, time.Now().UTC())}, nil

	case "robust_zscore":
		if param.Detector.RobustZ == nil {
			return nil, errors.New("robust_zscore detector missing config")
		}
		baseline := param.Detector.RobustZ.BaselineWindowSeconds
		since := time.Now().Add(-time.Duration(baseline) * time.Second).UTC().Format(time.RFC3339)
		queryCtx, cancel := context.WithTimeout(ctx, r.limits.MaxQueryDuration)
		defer cancel()
		rows, err := adapter.FetchRecentRows(queryCtx, mcp.FetchRecentRowsRequest{
			ConnectionRef:   spec.ConnectionRef,
			Table:           spec.Source.Table,
			Columns:         []string{param.ValueColumn, spec.Source.TimestampColumn},
			TimestampColumn: spec.Source.TimestampColumn,
			Where:           toWhere(spec.Source.Where),
			Since:           since,
			Limit:           r.limits.MaxSampleRows,
		})
		if err != nil {
			return nil, err
		}
		if len(rows.Rows) < param.Detector.RobustZ.MinSamples {
			return []DetectorResult{{Hit: false}}, nil
		}
		samples := make([]float64, 0, len(rows.Rows))
		latest := 0.0
		for i, row := range rows.Rows {
			val, ok := row[param.ValueColumn]
			if !ok {
				continue
			}
			floatVal, err := toFloat(val)
			if err != nil {
				continue
			}
			if i == 0 {
				latest = floatVal
			}
			samples = append(samples, floatVal)
		}
		if len(samples) < param.Detector.RobustZ.MinSamples {
			return []DetectorResult{{Hit: false}}, nil
		}
		result := EvaluateRobustZ(samples, latest, param.Detector.RobustZ.ZWarn, param.Detector.RobustZ.ZCrit)
		return []DetectorResult{result}, nil

	case "spec_limit":
		if param.Detector.SpecLimit == nil {
			return nil, errors.New("spec_limit detector missing config")
		}
		// Window = [lastRunAt, now], falling back to one poll interval on first run.
		since := windowSince(spec, lastRunAt)
		queryCtx, cancel := context.WithTimeout(ctx, r.limits.MaxQueryDuration)
		defer cancel()
		samples, err := fetchSamples(queryCtx, adapter, spec, param, nil, since, r.limits.MaxSampleRows, "")
		if err != nil {
			return nil, err
		}
		if len(samples) == 0 {
			return []DetectorResult{{Hit: false}}, nil
		}
		// Evaluate every new row individually — one alert per out-of-spec value.
		results := make([]DetectorResult, 0)
		for _, s := range samples {
			res := EvaluateSpecLimit(s, *param.Detector.SpecLimit)
			if res.Hit {
				results = append(results, res)
			}
		}
		if len(results) == 0 {
			return []DetectorResult{{Hit: false}}, nil
		}
		return results, nil

	case "shewhart":
		if param.Detector.Shewhart == nil {
			return nil, errors.New("shewhart detector missing config")
		}
		now := time.Now().UTC()
		since, start, end, limit, err := buildBaselineWindow(now, param.Detector.Shewhart.Baseline, r.limits.MaxSampleRows)
		if err != nil {
			return nil, err
		}
		queryCtx, cancel := context.WithTimeout(ctx, r.limits.MaxQueryDuration)
		defer cancel()
		samples, err := fetchSamples(queryCtx, adapter, spec, param, nil, since, limit, "")
		if err != nil {
			return nil, err
		}
		samples = filterSamplesByRange(samples, start, end)
		sigma := param.Detector.Shewhart.SigmaMultiplier
		if sigma == 0 {
			sigma = 3
		}
		result := EvaluateShewhart(samples, *param.Detector.Shewhart, sigma)
		applyWindowAndBaseline(&result, samples, start, end, true)
		return []DetectorResult{result}, nil

	case "range_chart":
		if param.Detector.RangeChart == nil {
			return nil, errors.New("range_chart detector missing config")
		}
		now := time.Now().UTC()
		since, start, end, limit, err := buildBaselineWindow(now, param.Detector.RangeChart.Baseline, r.limits.MaxSampleRows)
		if err != nil {
			return nil, err
		}
		mode := param.Detector.RangeChart.Subgrouping.Mode
		subgroupColumn := ""
		if mode == "column" {
			subgroupColumn = param.Detector.RangeChart.Subgrouping.Column
		}
		queryCtx, cancel := context.WithTimeout(ctx, r.limits.MaxQueryDuration)
		defer cancel()
		samples, err := fetchSamples(queryCtx, adapter, spec, param, nil, since, limit, subgroupColumn)
		if err != nil {
			return nil, err
		}
		samples = filterSamplesByRange(samples, start, end)
		groups := [][]Sample{}
		size := param.Detector.RangeChart.SubgroupSize
		if subgroupColumn != "" {
			groups = groupBySubgroup(samples, size)
		} else {
			groups = groupConsecutive(samples, size)
		}
		result := EvaluateRangeChart(groups, *param.Detector.RangeChart)
		applyWindowAndBaseline(&result, samples, start, end, true)
		return []DetectorResult{result}, nil

	case "trend":
		if param.Detector.Trend == nil {
			return nil, errors.New("trend detector missing config")
		}
		window := param.Detector.Trend.WindowSize
		if window == 0 {
			window = 6
		}
		// Use poll-interval window so we only look at genuinely new rows.
		since := windowSince(spec, lastRunAt)
		queryCtx, cancel := context.WithTimeout(ctx, r.limits.MaxQueryDuration)
		defer cancel()
		samples, err := fetchSamples(queryCtx, adapter, spec, param, nil, since, clampLimit(window, r.limits.MaxSampleRows), "")
		if err != nil {
			return nil, err
		}
		if param.Detector.Trend.RequireConsecutiveTimestamps && !hasConsecutiveTimestamps(samples) {
			result := insufficientData("non-consecutive timestamps")
			applyWindowAndBaseline(&result, samples, nil, nil, false)
			return []DetectorResult{result}, nil
		}
		result := EvaluateTrend6(samples, *param.Detector.Trend)
		applyWindowAndBaseline(&result, samples, nil, nil, false)
		return []DetectorResult{result}, nil

	case "tpa":
		if param.Detector.TPA == nil {
			return nil, errors.New("tpa detector missing config")
		}
		limit := param.Detector.TPA.WindowN
		if limit == 0 {
			limit = 3
		}
		// Use poll-interval window so we only look at genuinely new rows.
		since := windowSince(spec, lastRunAt)
		queryCtx, cancel := context.WithTimeout(ctx, r.limits.MaxQueryDuration)
		defer cancel()
		samples, err := fetchSamples(queryCtx, adapter, spec, param, nil, since, clampLimit(limit, r.limits.MaxSampleRows), "")
		if err != nil {
			return nil, err
		}
		result := EvaluateTPA(samples, *param.Detector.TPA)
		applyWindowAndBaseline(&result, samples, nil, nil, false)
		return []DetectorResult{result}, nil

	default:
		if param.Detector.Threshold == nil {
			return nil, errors.New("threshold detector missing config")
		}
		if spec.Aggregation != "" && spec.Aggregation != "latest" && spec.WindowSeconds != nil {
			queryCtx, cancel := context.WithTimeout(ctx, r.limits.MaxQueryDuration)
			defer cancel()
			resp, err := adapter.QueryAggregate(queryCtx, mcp.AggregateRequest{
				ConnectionRef:   spec.ConnectionRef,
				Table:           spec.Source.Table,
				ValueColumn:     param.ValueColumn,
				TimestampColumn: spec.Source.TimestampColumn,
				Where:           toWhere(spec.Source.Where),
				Agg:             spec.Aggregation,
				WindowSeconds:   *spec.WindowSeconds,
			})
			if err != nil {
				return nil, err
			}
			r2, err2 := EvaluateThresholdDetector(*param.Detector.Threshold, resp.Value)
			if err2 != nil {
				return nil, err2
			}
			return []DetectorResult{r2}, nil
		}
		queryCtx, cancel := context.WithTimeout(ctx, r.limits.MaxQueryDuration)
		defer cancel()
		resp, err := adapter.QueryLatestValue(queryCtx, mcp.LatestValueRequest{
			ConnectionRef:   spec.ConnectionRef,
			Table:           spec.Source.Table,
			ValueColumn:     param.ValueColumn,
			TimestampColumn: spec.Source.TimestampColumn,
			Where:           toWhere(spec.Source.Where),
		})
		if err != nil {
			return nil, err
		}
		r2, err2 := EvaluateThresholdDetector(*param.Detector.Threshold, resp.Value)
		if err2 != nil {
			return nil, err2
		}
		return []DetectorResult{r2}, nil
	}
}

func toWhere(spec *WhereSpec) *mcp.WhereSpec {
	if spec == nil {
		return nil
	}
	clauses := make([]mcp.WhereClause, 0, len(spec.Clauses))
	for _, c := range spec.Clauses {
		clauses = append(clauses, mcp.WhereClause{Column: c.Column, Op: c.Op, Value: c.Value})
	}
	return &mcp.WhereSpec{Type: spec.Type, Clauses: clauses}
}

func normalizeParameters(spec RuleSpec) []ParameterSpec {
	if len(spec.Parameters) > 0 {
		for i := range spec.Parameters {
			if spec.Parameters[i].ParameterName == "" {
				spec.Parameters[i].ParameterName = spec.Parameters[i].ValueColumn
			}
		}
		return spec.Parameters
	}
	if spec.Source.ValueColumn == "" {
		return nil
	}
	return []ParameterSpec{{
		ParameterName: fallbackParamName(spec.ParameterName, spec.Source.ValueColumn),
		ValueColumn:   spec.Source.ValueColumn,
		Detector: DetectorSpec{
			Type: "threshold",
			Threshold: &ThresholdSpec{
				Op:    spec.Condition.Op,
				Value: spec.Condition.Value,
				Min:   spec.Condition.Min,
				Max:   spec.Condition.Max,
			},
		},
	}}
}

func fallbackParamName(name, valueColumn string) string {
	if name != "" {
		return name
	}
	return valueColumn
}

func buildExplain(result DetectorResult, param ParameterSpec) string {
	switch param.Detector.Type {
	case "robust_zscore":
		if result.AnomalyScore == nil || result.BaselineMedian == nil || result.BaselineMAD == nil {
			return "robust_zscore"
		}
		return fmt.Sprintf("robust_zscore=%.2f (warn>=%.2f, crit>=%.2f), median=%.2f, mad=%.2f", *result.AnomalyScore, param.Detector.RobustZ.ZWarn, param.Detector.RobustZ.ZCrit, *result.BaselineMedian, *result.BaselineMAD)
	case "missing_data":
		return fmt.Sprintf("missing_data max_gap=%ds", param.Detector.MissingData.MaxGapSeconds)
	case "threshold":
		return result.LimitExpr
	case "spec_limit", "shewhart", "range_chart", "trend", "tpa":
		return result.LimitExpr
	default:
		return "detector"
	}
}

func applyWindowAndBaseline(result *DetectorResult, samples []Sample, baselineStart, baselineEnd *time.Time, baselineUsed bool) {
	if result == nil || len(samples) == 0 {
		return
	}
	first := samples[0].TS
	last := samples[len(samples)-1].TS
	if !first.IsZero() {
		result.WindowStart = &first
	}
	if !last.IsZero() {
		result.WindowEnd = &last
	}
	if baselineUsed {
		if baselineStart != nil {
			result.BaselineStart = baselineStart
		}
		if baselineEnd != nil {
			result.BaselineEnd = baselineEnd
		}
		if baselineStart == nil && baselineEnd == nil {
			result.BaselineStart = result.WindowStart
			result.BaselineEnd = result.WindowEnd
		}
	}
}
