# Rule Creation UI Specification — predixaAI Stepper

**Audience:** Frontend / UI agent  
**Backend version:** Phase 1 (rule-service @ 8090)  
**Last updated:** 2026-02-19

---

## Table of Contents
1. [Stepper Flow Overview](#1-stepper-flow-overview)
2. [Shared Concepts — Selectors, Parameters, Errors](#2-shared-concepts)
3. [Rule Type: SPEC_LIMIT_VIOLATION](#3-spec_limit_violation)
4. [Rule Type: SHEWHART_3SIGMA](#4-shewhart_3sigma)
5. [Rule Type: SHEWHART_2SIGMA](#5-shewhart_2sigma)
6. [Rule Type: RANGE_CHART_R](#6-range_chart_r)
7. [Rule Type: TREND_6_POINTS](#7-trend_6_points)
8. [Rule Type: TPA](#8-tpa)
9. [API Reference — All Endpoints](#9-api-reference)
10. [Error Code Reference](#10-error-code-reference)
11. [UX Hints & Validation Table](#11-ux-hints--validation-table)

---

## 1. Stepper Flow Overview

The creation wizard always follows this 5-step flow:

```
Step 1: Select Machine Unit
  └─ GET /api/machine-units
Step 2: Select Parameter
  └─ GET /api/machine-units/{unitId}/parameters
Step 3: Select Rule Type
  └─ GET /api/rules/catalog
Step 4: Configure Rule
  └─ POST /api/rules/baseline/check   (only for rules where requiresBaseline=true)
  └─ POST /api/rules/preview          (always — show live results)
Step 5: Name & Save
  └─ POST /api/rules
```

### State to carry across all steps

```typescript
interface WizardState {
  unitId: string;           // from step 1
  connectionRef: string;    // from the machine unit object
  parameterId: string;      // e.g. "etchers_data.rf_power"   (table.column)
  ruleType: string;         // from catalog, e.g. "TREND_6_POINTS"
  config: object;           // built in step 4
  baselineSelector?: Selector;   // only for baseline rules
  evalSelector?: Selector;
  subgrouping?: Subgrouping;     // only for RANGE_CHART_R
}
```

---

## 2. Shared Concepts

### 2.1 parameterId format
`parameterId` is always `"<table>.<column>"`, e.g. `"etchers_data.rf_power"`.  
It is validated server-side: the table must match the machine unit's `selectedTable` and the column must be in `selectedColumns`.

### 2.2 Selector (baseline / eval window)

```typescript
// kind = "lastN"   — use the last N rows ordered by timestampColumn DESC
// kind = "timeRange" — use rows between start and end (ISO 8601)
type Selector =
  | { kind: "lastN";    value: number }          // value >= 1
  | { kind: "timeRange"; start: string; end: string }  // RFC3339
```

**Default when omitted:**
- `baselineSelector` → `{ kind: "lastN", value: 50 }`
- `evalSelector` → `{ kind: "lastN", value: 50 }`

### 2.3 Subgrouping (RANGE_CHART_R only)

```typescript
type Subgrouping =
  | { kind: "consecutive"; subgroupSize: number }   // group rows in order
  | { kind: "column";      subgroupSize: number; column: string }  // group by column value
```

`subgroupSize` must be **2–10** (D3/D4 constants only exist for these values).

### 2.4 GET /api/machine-units/{unitId}/parameters response

```jsonc
{
  "unitId": "machine-e69997e9-...",
  "parameters": [
    {
      "parameterId": "etchers_data.rf_power",
      "unitName": "Etcher Unit 1",
      "table": "etchers_data",
      "valueColumn": "rf_power",
      "dataType": "float",
      "timestampColumn": "run_order",      // column used for ordering
      "subgroupCandidateColumns": ["batch_id", "recipe_id"],
      "supportsTrend": true,               // timestampColumn is time type
      "supportsShewhart": true,
      "supportsRangeChart": true,
      "notes": ["timestamp column 'run_order' is not a time type — trend continuity disabled"]
    }
  ]
}
```

**UI guidance:**
- If `supportsTrend: false` → grey out / hide `TREND_6_POINTS` and `TPA` in the catalog.
- If `supportsRangeChart: false` → grey out `RANGE_CHART_R`.
- Show `notes[]` as info banners beneath the parameter selection.

### 2.5 Preview response shape

```jsonc
{
  "status": "OK",              // "OK" | "VIOLATION" | "INSUFFICIENT_DATA" | "INVALID_CONFIG"
  "window": { "start": "2026-01-01T00:00:00Z", "end": "2026-02-01T00:00:00Z" },
  "baseline": { "start": "...", "end": "...", "count": 150 },
  "computed": { /* rule-specific — see per-rule sections below */ },
  "violations": [
    {
      "kind": "point",
      "value": 105.3,
      "reason": "above_ucl",    // rule-specific reason strings — see each section
      "limitName": "UCL",
      "limitValue": 100.0,
      "delta": 5.3,
      "timestamp": "2026-01-15T08:00:00Z"
    }
  ],
  "explain": "Latest value 105.3 exceeded UCL 100.0 (delta +5.3)"
}
```

---

## 3. SPEC_LIMIT_VIOLATION

### What it does
Compares the **single latest data point** against fixed upper/lower spec or control limits. No baseline needed. Triggers immediately if the value is outside the configured limits (plus optional tolerance epsilon).

### When to use
Use when engineering or process specifications define hard limits (e.g. a temperature must never exceed 200°C, or a diameter must be between 9.95–10.05 mm).

### Config fields

| Field | Type | Required | Default | Constraint | Help text |
|-------|------|----------|---------|-----------|-----------|
| `mode` | enum | ✅ | `"spec"` | `"spec"` \| `"control"` \| `"both"` | Spec limits = fixed engineering limits. Control limits = statistical process limits. Both checks both. |
| `specLimits.usl` | number | if mode includes spec | — | any float | Upper spec limit. Leave blank for one-sided check. |
| `specLimits.lsl` | number | if mode includes spec | — | any float | Lower spec limit. |
| `controlLimits.ucl` | number | if mode includes control | — | any float | Upper control limit. |
| `controlLimits.lcl` | number | if mode includes control | — | any float | Lower control limit. |
| `epsilon` | number | ❌ | `0` | `>= 0` | Tolerance: violations only fire when value exceeds limit by more than epsilon. |

**Conditional visibility:**
- `specLimits.usl` / `specLimits.lsl` → only visible when `mode` = `"spec"` or `"both"`
- `controlLimits.ucl` / `controlLimits.lcl` → only visible when `mode` = `"control"` or `"both"`

**Frontend validation (before sending):**
- If `mode = "spec"` or `"both"`: at least one of `usl` or `lsl` must be set.
- If `mode = "control"` or `"both"`: at least one of `ucl` or `lcl` must be set.
- If both USL and LSL are set: USL > LSL.
- If both UCL and LCL are set: UCL > LCL.

**Create rule payload:**
```jsonc
{
  "unitId": "machine-...",
  "name": "RF Power Spec Limit",
  "ruleType": "SPEC_LIMIT_VIOLATION",
  "parameterId": "etchers_data.rf_power",
  "enabled": true,
  "config": {
    "mode": "spec",
    "specLimits": { "usl": 500, "lsl": 100 },
    "epsilon": 0
  }
}
```

**Preview payload:**
```jsonc
{
  "unitId": "machine-...",
  "parameterId": "etchers_data.rf_power",
  "ruleType": "SPEC_LIMIT_VIOLATION",
  "connectionRef": "9963a777-...",
  "config": { "mode": "spec", "specLimits": { "usl": 500, "lsl": 100 } },
  "evalSelector": { "kind": "lastN", "value": 1 }
}
```

**Preview `computed` fields:**
```jsonc
{
  "mode": "spec",
  "epsilon": 0,
  "spec_usl": 500,
  "spec_lsl": 100,
  // on violation:
  "limitBreached": "USL",   // "USL" | "LSL" | "UCL" | "LCL"
  "limitValue": 500,
  "delta": 12.5
}
```

**Violation reasons:** `"limit_breach"`

**Status meanings:**
| Status | Meaning | UI |
|--------|---------|-----|
| `OK` | Latest value within limits | Green |
| `VIOLATION` | Value outside limit | Red — show `limitBreached`, `delta` |
| `INVALID_CONFIG` | Missing limits for the chosen mode | Show `explain` as error |
| `INSUFFICIENT_DATA` | No data rows found | Yellow warning |

---

## 4. SHEWHART_3SIGMA

### What it does
Calculates **mean (μ) and standard deviation (σ)** from a baseline period, then checks if the **latest point** falls outside `μ ± 3σ`. The control limits are data-driven — they adapt to normal process variation.

### When to use
Use for **process monitoring** when you don't have fixed spec limits but want to detect statistical out-of-control points (3σ ≈ 99.7% of normal variation is within limits).

### Requires baseline: ✅ YES

Minimum: **20 baseline samples** (enforced server-side).

### Config fields

| Field | Type | Required | Default | Constraint | Help text |
|-------|------|----------|---------|-----------|-----------|
| `minBaselineN` | number | ❌ | `20` | `>= 2` | Minimum number of baseline samples required to compute μ/σ. Increase for more stable limits. |

**Note:** `sigmaMultiplier` is fixed at `3.0` — it is not user-configurable. The rule type name `SHEWHART_3SIGMA` encodes it.

### Baseline check (required before preview/save)

**Request:**
```jsonc
{
  "unitId": "machine-...",
  "parameterId": "etchers_data.rf_power",
  "ruleType": "SHEWHART_3SIGMA",
  "connectionRef": "9963a777-...",
  "baselineSelector": { "kind": "lastN", "value": 100 }
}
```

**Response:**
```jsonc
{
  "status": "OK",              // "OK" | "INSUFFICIENT_DATA" | "INVALID_CONFIG"
  "available": { "samples": 150 },
  "required": { "minBaselineSamples": 20 },
  "continuity": { "gapsDetected": false, "largestGapSeconds": 3.0 },
  "messages": []
}
```

**UI for baseline step:**
- Show `available.samples` vs `required.minBaselineSamples` as a progress bar.
- If `status = "INSUFFICIENT_DATA"`: show warning "Not enough baseline data — increase baseline window or add more data."
- If `continuity.gapsDetected`: show info "Timestamp gaps detected (largest: Xs). Check that data is continuous."

**Create rule payload:**
```jsonc
{
  "unitId": "machine-...",
  "name": "RF Power 3σ Control",
  "ruleType": "SHEWHART_3SIGMA",
  "parameterId": "etchers_data.rf_power",
  "enabled": true,
  "config": { "minBaselineN": 20 }
}
```

**Preview payload:**
```jsonc
{
  "unitId": "machine-...",
  "parameterId": "etchers_data.rf_power",
  "ruleType": "SHEWHART_3SIGMA",
  "connectionRef": "9963a777-...",
  "config": { "minBaselineN": 20 },
  "baselineSelector": { "kind": "lastN", "value": 100 },
  "evalSelector": { "kind": "lastN", "value": 1 }
}
```

**Preview `computed` fields:**
```jsonc
{
  "mu": 312.4,
  "sigma": 18.7,
  "ucl": 368.5,
  "lcl": 256.3,
  "sigmaMultiplier": 3,
  // on violation:
  "limitBreached": "UCL",
  "delta": 5.2
}
```

**Violation reasons:** `"above_ucl"` | `"below_lcl"` | `"mean_shift"` (when σ=0 and value ≠ mean)

**UI display recommendations:**
- Show a mini time-series chart with UCL / μ / LCL lines.
- Highlight violating point in red.

---

## 5. SHEWHART_2SIGMA

### What it does
Identical to `SHEWHART_3SIGMA` but uses **μ ± 2σ** (tighter limits, more sensitive, ~95.4% of normal variation within bounds).

### When to use
Use for **early warning** — catches drift before a 3σ violation. Often used as a "warning" level alongside a 3σ "alarm" rule.

All config, payloads, computed fields, and violation reasons are **identical to SHEWHART_3SIGMA** except:
- `sigmaMultiplier` is `2.0` (fixed, not user-configurable)
- The rule type string is `"SHEWHART_2SIGMA"`

---

## 6. RANGE_CHART_R

### What it does
Groups samples into **subgroups** of size n, computes the **range (max–min)** of each subgroup, then uses baseline subgroups to compute `R̄` (mean range) and applies Shewhart D3/D4 constants to derive control limits `LCL_R` and `UCL_R`. Flags when the **latest subgroup's range** is out of control.

### When to use
Use to monitor **within-batch or within-group variation** (e.g. variation within a single wafer lot, or across a tool set). Detects instability in dispersion rather than mean shifts.

### Requires baseline: ✅ YES

Minimum: **10 subgroups** (enforced server-side).

### Config fields

| Field | Type | Required | Default | Constraint | Help text |
|-------|------|----------|---------|-----------|-----------|
| `subgroupSize` | number | ✅ | `5` | `2–10` | How many samples per subgroup. Must be 2–10 (D3/D4 constants only defined for these values). |
| `minBaselineSubgroups` | number | ❌ | `10` | `>= 2` | Minimum subgroup count in baseline to compute R̄. |

**Subgrouping object (sent separately in preview/baseline, not in `config`):**
```jsonc
// Consecutive: groups rows in arrival order  
{ "kind": "consecutive", "subgroupSize": 5 }

// By column: groups rows sharing a column value
{ "kind": "column", "column": "batch_id", "subgroupSize": 5 }
```

**Frontend validation:**
- `subgroupSize` must be an integer between 2 and 10 inclusive.
- If `kind = "column"`: require the user to pick a column from `parameter.subgroupCandidateColumns`.

### Baseline check request:
```jsonc
{
  "unitId": "machine-...",
  "parameterId": "etchers_data.rf_power",
  "ruleType": "RANGE_CHART_R",
  "connectionRef": "9963a777-...",
  "baselineSelector": { "kind": "lastN", "value": 100 },
  "subgrouping": { "kind": "consecutive", "subgroupSize": 5 }
}
```

**Baseline check response** (adds `subgroups` to `available`):
```jsonc
{
  "status": "OK",
  "available": { "samples": 100, "subgroups": 20 },
  "required": { "minBaselineSamples": 0, "minBaselineSubgroups": 10 },
  "continuity": { "gapsDetected": false, "largestGapSeconds": 3.0 },
  "messages": []
}
```

**UI for baseline step:**
- Show both `available.samples` and `available.subgroups` vs `required.minBaselineSubgroups`.
- Warn if `subgroups < minBaselineSubgroups`.

**Create rule payload:**
```jsonc
{
  "unitId": "machine-...",
  "name": "RF Power Range Chart",
  "ruleType": "RANGE_CHART_R",
  "parameterId": "etchers_data.rf_power",
  "enabled": true,
  "config": {
    "subgroupSize": 5,
    "minBaselineSubgroups": 10
  }
}
```

**Note:** The `subgrouping` object belongs in the *preview* and *baseline check* request body, not in `config`. On save (`POST /api/rules`), only `config` is stored — so embed subgrouping info inside `config` if you need to remember it:
```jsonc
"config": {
  "subgroupSize": 5,
  "minBaselineSubgroups": 10,
  "subgroupingKind": "consecutive"
}
```

**Preview payload:**
```jsonc
{
  "unitId": "machine-...",
  "parameterId": "etchers_data.rf_power",
  "ruleType": "RANGE_CHART_R",
  "connectionRef": "9963a777-...",
  "config": { "subgroupSize": 5, "minBaselineSubgroups": 10 },
  "baselineSelector": { "kind": "lastN", "value": 100 },
  "evalSelector": { "kind": "lastN", "value": 50 },
  "subgrouping": { "kind": "consecutive", "subgroupSize": 5 }
}
```

**Preview `computed` fields:**
```jsonc
{
  "rbar": 12.4,
  "ucl_r": 26.2,
  "lcl_r": 0,
  "subgroupSize": 5,
  // on violation:
  "limitBreached": "range",
  "delta": 5.1
}
```

**Violation reasons:** `"above_ucl_r"` | `"below_lcl_r"`

**D3/D4 constants (for UI reference):**

| Subgroup Size | D3 | D4 |
|---|---|---|
| 2 | 0 | 3.267 |
| 3 | 0 | 2.574 |
| 4 | 0 | 2.282 |
| 5 | 0 | 2.114 |
| 6 | 0 | 2.004 |
| 7 | 0.076 | 1.924 |
| 8 | 0.136 | 1.864 |
| 9 | 0.184 | 1.816 |
| 10 | 0.223 | 1.777 |

Show these in a tooltip or info panel — users often don't know what D3/D4 mean.

---

## 7. TREND_6_POINTS

### What it does
Looks at the **last N consecutive data points** and checks if they form a strictly monotonic trend (each point strictly greater or strictly less than the previous by more than `epsilon`). Flags when all N consecutive points are uniformly increasing or decreasing.

### When to use
Use to detect **gradual drift** or wear patterns — e.g. tool degradation causing slowly increasing power consumption, or a chamber gradually cooling. Does NOT need a baseline.

### Requires baseline: ❌ NO

Minimum: **6 eval samples** (equal to `windowSize`).

### Important: timestampColumn requirement

⚠️ If the machine unit's `timestampColumn` is **not a native time type** (e.g. it's an integer `run_order`), `supportsTrend` will be `false` and the server may reject the preview with `TIMESTAMP_COLUMN_INVALID`. Show the note from the parameters endpoint as a warning.

Actually for trend detection, the server uses `timestampColumn` only for **ordering**, not for gap math. However the validation in `validateStepperMetadata` requires a time type for trend/TPA. If `supportsTrend: false`, **disable this rule type in the UI** and show the note.

### Config fields

| Field | Type | Required | Default | Constraint | Help text |
|-------|------|----------|---------|-----------|-----------|
| `windowSize` | number | ❌ | `6` | `>= 3` | Number of consecutive points to check. The classic SPC rule uses 6. |
| `epsilon` | number | ❌ | `0` | `>= 0` | Minimum step size. A point only counts as "up" if it exceeds the previous by more than epsilon. Use 0 for any increase, or e.g. 0.5 to ignore noise. |
| `requireConsecutiveTimestamps` | boolean | ❌ | `true` | — | If true, the window is reset when a timestamp gap larger than 2× median interval is detected. Recommended: true. |

**Frontend validation:**
- `windowSize >= 3` (server default is 6, minimum meaningful is 3).
- `epsilon >= 0`.

**Create rule payload:**
```jsonc
{
  "unitId": "machine-...",
  "name": "RF Power Upward Trend",
  "ruleType": "TREND_6_POINTS",
  "parameterId": "etchers_data.rf_power",
  "enabled": true,
  "config": {
    "windowSize": 6,
    "epsilon": 0,
    "requireConsecutiveTimestamps": true
  }
}
```

**Preview payload:**
```jsonc
{
  "unitId": "machine-...",
  "parameterId": "etchers_data.rf_power",
  "ruleType": "TREND_6_POINTS",
  "connectionRef": "9963a777-...",
  "config": { "windowSize": 6, "epsilon": 0, "requireConsecutiveTimestamps": true },
  "evalSelector": { "kind": "lastN", "value": 50 }
}
```

**Preview `computed` fields:**
```jsonc
{
  "direction": "none",    // "none" | "up" | "down"
  "windowSize": 6,
  "epsilon": 0
}
```

**Violation reasons:** `"increasing"` | `"decreasing"`

**Status meanings:**
| Status | computed.direction | Meaning | UI |
|--------|-------------------|---------|-----|
| `OK` | `"none"` | No trend in the latest window | Green |
| `VIOLATION` | `"up"` | 6 consecutive increases detected | Red — "Upward trend detected" |
| `VIOLATION` | `"down"` | 6 consecutive decreases detected | Red — "Downward trend detected" |
| `INSUFFICIENT_DATA` | — | Fewer than `windowSize` points available | Yellow |

**UX hint:** Show the last `windowSize` data points as a small sparkline with each step coloured green/red to illustrate the trend.

---

## 8. TPA

### What it does
Runs a **linear regression** on the last `windowN` data points to compute `slope`, `intercept`, and `R²`. Can trigger violations in two ways:
1. **Slope threshold** — if `|slope|` exceeds `slopeThreshold`.
2. **Time-to-spec** — if the projected line will reach a spec limit within `timeToSpecThreshold` units.

### When to use
Use for **predictive alerting** — detecting that a parameter is heading toward a limit even if it hasn't reached it yet. Good for tool end-of-life prediction (e.g. "RF power will exceed USL in 3 hours").

### Requires baseline: ❌ NO

Minimum: **3 eval samples** (`windowN >= 3` is enforced).

### Important: timestampColumn requirement

⚠️ Same as TREND_6_POINTS — requires the `timestampColumn` to be a **time type** for `regressionTimeBasis: "timestamp"`. Use `regressionTimeBasis: "index"` if the timestamp column is an integer (e.g. run_order). Check `supportsTrend` from the parameters endpoint.

### Config fields

| Field | Type | Required | Default | Constraint | Help text |
|-------|------|----------|---------|-----------|-----------|
| `windowN` | number | ✅ | `5` | `>= 3` | Number of recent points to include in the regression. |
| `regressionTimeBasis` | enum | ❌ | `"timestamp"` | `"timestamp"` \| `"index"` | `timestamp`: X axis is Unix epoch (seconds). `index`: X axis is row index (1, 2, 3…). Use `index` if your timestamp column is not a true time type. |
| `slopeThreshold` | number | ❌ | — | `> 0` | Trigger violation if `|slope|` ≥ this value. Required if `timeToSpecThreshold` is not set. |
| `timeToSpecThreshold` | number | ❌ | — | `> 0` | Trigger violation if the projected value will reach a spec limit within this many **seconds** (when `regressionTimeBasis=timestamp`) or **steps** (when `regressionTimeBasis=index`). Requires `specLimits` to be set. |
| `specLimits.usl` | number | conditionally | — | any float | Required if `timeToSpecThreshold` is set and `requireSpecLimits=true`. |
| `specLimits.lsl` | number | conditionally | — | any float | Required if `timeToSpecThreshold` is set and `requireSpecLimits=true`. |
| `requireSpecLimits` | boolean | ❌ | `false` | — | If true, the rule will fail with `INVALID_CONFIG` if `specLimits` is not set. |

**Frontend validation:**
- At least one of `slopeThreshold` or `timeToSpecThreshold` should be provided (otherwise the rule will never fire).
- If `timeToSpecThreshold` is set: prompt user for `specLimits.usl` / `specLimits.lsl`.
- `windowN >= 3`.

**Conditional visibility:**
- `specLimits.usl` / `specLimits.lsl` → visible when `timeToSpecThreshold` is non-empty.
- `requireSpecLimits` → visible when `timeToSpecThreshold` is non-empty.

**Create rule payload:**
```jsonc
{
  "unitId": "machine-...",
  "name": "RF Power Slope Trend",
  "ruleType": "TPA",
  "parameterId": "etchers_data.rf_power",
  "enabled": true,
  "config": {
    "windowN": 5,
    "regressionTimeBasis": "index",
    "slopeThreshold": 2.5
  }
}
```

**Preview payload:**
```jsonc
{
  "unitId": "machine-...",
  "parameterId": "etchers_data.rf_power",
  "ruleType": "TPA",
  "connectionRef": "9963a777-...",
  "config": {
    "windowN": 5,
    "regressionTimeBasis": "index",
    "slopeThreshold": 2.5
  },
  "evalSelector": { "kind": "lastN", "value": 50 }
}
```

**Preview `computed` fields:**
```jsonc
{
  "slope": 3.1,
  "intercept": 287.4,
  "r2": 0.91,
  "windowN": 5,
  "regressionBasis": "index",
  // when timeToSpec is computable:
  "timeToSpec": 12.3,
  // on violation:
  "trigger": "slope"   // "slope" | "timeToSpec"
}
```

**Violation reasons:** `"slope_threshold"` | `"time_to_spec"`

**Status meanings:**
| Status | Meaning | UI |
|--------|---------|-----|
| `OK` | Slope below threshold or trend moving away from spec | Green |
| `VIOLATION` / trigger=`slope` | Slope exceeds threshold | Red — show slope value |
| `VIOLATION` / trigger=`timeToSpec` | Will hit spec within threshold | Red — show time remaining |
| `INVALID_CONFIG` | windowN < 3, or specLimits missing when required | Show `explain` |
| `INSUFFICIENT_DATA` | Fewer than `windowN` points | Yellow |

**UX hint:** Show a mini scatter plot with the regression line, projected forward to spec limit. Label `R²` — if it's below 0.7 warn "Low fit quality — regression may be unreliable."

---

## 9. API Reference

### GET /api/rules/catalog
Returns all rule types with metadata and config schemas.

**Response:**
```jsonc
{
  "version": "1.0",
  "types": [ /* catalogType[] — see section 3–8 for each */ ]
}
```
Each entry has `requiresBaseline`, `supportsSubgrouping`, `minData`, `configSchema.fields[]`.

---

### GET /api/machine-units/{unitId}/parameters
Returns parameters (value columns) for a machine unit with metadata for rule selection.

**Response:** `unitParametersResponse` — see section 2.4.

---

### POST /api/rules/baseline/check
Check if enough baseline data exists. Only meaningful for rules where `requiresBaseline=true`.

**Request:** `baselineCheckRequest`
```jsonc
{
  "unitId": "string",
  "parameterId": "string",         // "table.column"
  "ruleType": "string",
  "connectionRef": "uuid",
  "baselineSelector": { "kind": "lastN", "value": 100 },
  "subgrouping": null              // only for RANGE_CHART_R
}
```

**Response:** `baselineCheckResponse` — see section 4 / 6.

**Errors:**
- `400 INVALID_REQUEST` — missing/invalid fields
- `404` — connectionRef or unit not found
- `400 PARAMETER_NOT_FOUND` — parameterId invalid
- `400 TIMESTAMP_COLUMN_INVALID` — timestampColumn not found in table

---

### POST /api/rules/preview
Run the rule against live data and return a sample result. Safe to call multiple times.

**Request:** `previewRequest`
```jsonc
{
  "unitId": "string",
  "parameterId": "string",
  "ruleType": "string",
  "connectionRef": "uuid",
  "config": { /* rule-specific — see each section */ },
  "baselineSelector": { "kind": "lastN", "value": 100 },  // nullable
  "evalSelector":    { "kind": "lastN", "value": 50 },   // nullable
  "subgrouping":     null                                  // only RANGE_CHART_R
}
```

**Response:** `previewResponse` — see section 2.5.

**Errors:**
- `400 INVALID_REQUEST` — missing fields
- `404` — connectionRef not found
- `400 PARAMETER_NOT_FOUND`
- `400 TIMESTAMP_COLUMN_INVALID`
- `502` — scheduler unavailable

---

### POST /api/rules
Create and persist a rule.

**Request:** `stepperRuleRequest`
```jsonc
{
  "unitId": "string",
  "name": "string",
  "ruleType": "string",
  "parameterId": "string",
  "enabled": true,
  "config": { /* rule-specific */ }
}
```

**Response:** `stepperRuleResponse`
```jsonc
{
  "id": "uuid",
  "unitId": "string",
  "name": "string",
  "ruleType": "string",
  "parameterId": "string",
  "enabled": true,
  "config": { /* stored config */ },
  "createdAt": "2026-02-19T11:03:03Z",
  "updatedAt": "2026-02-19T11:03:03Z"
}
```

**Errors:**
- `400 INVALID_REQUEST` — missing unitId, ruleType, parameterId, or config
- `404` — machine unit not found
- `400 PARAMETER_NOT_FOUND` — parameterId not in unit's selectedColumns
- `500` — DB error (rare)

---

### GET /api/rules?unitId={unitId}
List all rules for a unit.

**Response:** `{ "rules": stepperRuleResponse[] }`

---

### PATCH /api/rules/{ruleId}
Update a rule. Same payload as POST. Returns updated `stepperRuleResponse`.

---

### DELETE /api/rules/{ruleId}
Delete a rule. Returns `{ "ok": true }`.

---

### POST /api/rules/{ruleId}/enable
### POST /api/rules/{ruleId}/disable
Toggle rule enabled state. Returns `{ "ok": true }`.

---

### GET /api/machine-units/{unitId}/health
Returns health warnings for all rules on a unit.

**Response:**
```jsonc
{
  "unitId": "string",
  "warningsCount": 1,
  "errorsCount": 0,
  "items": [
    {
      "severity": "warning",    // "warning" | "error"
      "code": "INSUFFICIENT_BASELINE",
      "message": "Baseline too small for SHEWHART_3SIGMA",
      "ruleId": "uuid",
      "parameterId": "etchers_data.rf_power"
    }
  ]
}
```

---

## 10. Error Code Reference

| HTTP | code | Meaning | UI action |
|------|------|---------|-----------|
| 400 | `INVALID_REQUEST` | Missing or invalid field. See `fieldErrors[]`. | Highlight per-field errors inline. |
| 400 | `PARAMETER_NOT_FOUND` | `parameterId` not in the machine unit's columns. | "Parameter not found — select a different parameter." |
| 400 | `TIMESTAMP_COLUMN_INVALID` | The machine unit's `timestampColumn` doesn't exist in the table. | "Timestamp column misconfigured — edit the machine unit." |
| 404 | — | Unit or connectionRef not found. | "Resource not found." |
| 502 | — | Scheduler unavailable. | "Preview unavailable — backend unreachable. Try again." |
| 500 | — | Unexpected error. | "Unexpected error. Contact support." |

**`fieldErrors[]` shape:**
```jsonc
[
  { "field": "unitId",    "problem": "missing" },
  { "field": "config",    "problem": "missing" },
  { "field": "baselineSelector.kind", "problem": "invalid" }
]
```

**Preview `status` strings from the rule engine:**
| Status | Meaning |
|--------|---------|
| `OK` | Rule evaluated — no violation |
| `VIOLATION` | Rule evaluated — violation detected |
| `INSUFFICIENT_DATA` | Not enough rows to evaluate the rule |
| `INVALID_CONFIG` | Config error (e.g. missing spec limits, bad subgroupSize) — check `explain` |

---

## 11. UX Hints & Validation Table

### Global wizard rules
1. **Never show the "Save" button** until the preview has returned `status: "OK"` or `status: "VIOLATION"` at least once. Block save on `INSUFFICIENT_DATA` or `INVALID_CONFIG`.
2. **Auto-populate defaults** from the catalog `configSchema.fields[].default` — never show an empty form.
3. **Debounce preview calls** — wait 600ms after the last config change before calling preview.
4. **Show the `explain` string** from the preview response as a human-readable summary beneath the result chart.

### Per-rule UX hints

| Rule | Key hint |
|------|---------|
| `SPEC_LIMIT_VIOLATION` | Show a simple gauge/meter with USL/LSL marked. The epsilon slider helps users understand the tolerance concept. |
| `SHEWHART_3SIGMA` | Baseline selector is the most important input. Explain: "Stable process data only — exclude known outliers or process changes." |
| `SHEWHART_2SIGMA` | Label clearly as "Warning level" to distinguish from 3σ "Alarm level". |
| `RANGE_CHART_R` | Subgroup size is the most confusing field. Show a diagram: "5 samples → 1 subgroup → range = max–min". Link to D3/D4 table in a tooltip. |
| `TREND_6_POINTS` | `epsilon` is subtle. Default 0 means any increase counts. Show example: "epsilon=0.5 means each step must increase by at least 0.5". |
| `TPA` | `timeToSpecThreshold` units depend on `regressionTimeBasis`: seconds (for timestamp) or steps (for index). Make this explicit in the label. Show R² with a quality rating: ≥0.9 = good, 0.7–0.9 = fair, <0.7 = warn. |

### Disable rules based on parameter metadata

```typescript
function isRuleTypeAvailable(ruleType: string, param: ParameterResponse): boolean {
  switch (ruleType) {
    case "TREND_6_POINTS":
    case "TPA":
      return param.supportsTrend;
    case "RANGE_CHART_R":
      return param.supportsRangeChart;
    default:
      return true;
  }
}
```

### Selector UI component

Recommend a segmented control:

```
[ Last N rows ]  [ Time range ]

Last N rows: [___100___] rows
Time range:  [2026-01-01 00:00]  →  [2026-02-01 00:00]
```

- `value` for `lastN` must be `>= 1` and `<= 10000` (server-side max row limit).
- For `timeRange`, validate that `end > start` on the client before sending.

### Baseline selector placement

Show the baseline selector only for rules where `catalog.type.requiresBaseline = true`:
- `SHEWHART_3SIGMA` ✅
- `SHEWHART_2SIGMA` ✅
- `RANGE_CHART_R` ✅
- All others ❌ (hide baseline selector entirely)

---

*End of spec. Source of truth: `services/rule-service/internal/api/` and `services/scheduler-service/internal/scheduler/phase1_rules.go`.*
