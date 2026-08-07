package adgo

import "time"

type Metrics struct {
	WallTime           time.Duration `json:"wallTime"`
	ActiveComputeTime  time.Duration `json:"activeComputeTime"`
	QueueTime          time.Duration `json:"queueTime"`
	Activities         int           `json:"activities"`
	Retries            int           `json:"retries"`
	Timeouts           int           `json:"timeouts"`
	Repairs            int           `json:"repairs"`
	HumanInterventions int           `json:"humanInterventions"`
	RecoveryEvents     int           `json:"recoveryEvents"`
	CacheHits          int           `json:"cacheHits"`
	ArtifactReuse      int           `json:"artifactReuse"`
	QualityGain        float64       `json:"qualityGain"`
	Cost               float64       `json:"cost"`
	Tokens             int64         `json:"tokens"`
}

func (m Metrics) QualityGainPerCost() float64 {
	if m.Cost <= 0 {
		return 0
	}
	return m.QualityGain / m.Cost
}
func (m Metrics) QualityGainPerRepair() float64 {
	if m.Repairs <= 0 {
		return 0
	}
	return m.QualityGain / float64(m.Repairs)
}
