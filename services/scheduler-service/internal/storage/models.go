package storage

import "time"

type RuleRecord struct {
	ID            string
	ConnectionRef string
	RuleJSON      []byte
	Enabled       bool
	Status        string
	LastError     []byte
	LastValidated *time.Time
	LastRunAt     *time.Time
}

type AlertRecord struct {
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
