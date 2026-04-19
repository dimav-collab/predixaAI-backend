package storage

import (
	"encoding/json"
	"time"
)

type DBConnection struct {
	ID        string
	Name      string
	Type      string
	Host      string
	Port      int
	User      string
	Password  string
	Database  string
	CreatedAt time.Time
}

type RuleRecord struct {
	ID              string
	Name            string
	Description     string
	ConnectionRef   string
	ParameterName   string
	RuleJSON        []byte
	Enabled         bool
	Status          string
	LastError       []byte
	LastValidatedAt *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type AlertRecord struct {
	ID             int64
	RuleID         string
	TSUTC          time.Time
	ParameterName  string
	ObservedValue  string
	LimitExpr      string
	DetectorType   string
	Severity       string
	AnomalyScore   *float64
	BaselineMedian *float64
	BaselineMAD    *float64
	Hit            bool
	Treated        bool
	Metadata       []byte
}

type MachineUnit struct {
	UnitID          string
	UnitName        string
	ConnectionRef   string
	SelectedTable   string
	TimestampColumn string
	SelectedColumns []string
	LiveParameters  json.RawMessage
	RuleIDs         []string
	PosX            float64
	PosY            float64
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type StepperRule struct {
	ID          string
	UnitID      string
	Name        string
	RuleType    string
	ParameterID string
	Config      json.RawMessage
	Enabled     bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// SimulationStatus values.
const (
	SimStatusRunning = "running"
	SimStatusStopped = "stopped"
	SimStatusError   = "error"
)

// SimulationMode controls which kind of data the generator produces.
const (
	SimModeNormal     = "normal"      // values near the process mean — no violation expected
	SimModeViolation  = "violation"   // values that immediately breach spec/control limits
	SimModeTrendUp    = "trend_up"    // steadily increasing values — triggers TREND_6_POINTS / TPA
	SimModeTrendDown  = "trend_down"  // steadily decreasing values
	SimModeSpike      = "spike"       // random spikes — useful for range chart tests
)

// SimulationJob represents one active or stopped simulation run attached to a rule.
type SimulationJob struct {
	ID              string
	RuleID          string
	Status          string          // SimStatus*
	Mode            string          // SimMode*
	IntervalSeconds int             // how often a new row is inserted
	InsertedCount   int             // cumulative rows inserted so far
	LastError       string          // last error message if status=error
	Config          json.RawMessage // extra per-mode config (e.g. baseValue, step, usl, lsl)
	CreatedAt       time.Time
	UpdatedAt       time.Time
}
