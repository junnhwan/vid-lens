package eval

import (
	"fmt"
	"math/rand"
	"sort"
	"strings"
)

type PairedObservation struct {
	CaseID      string  `json:"case_id"`
	SourceGroup string  `json:"source_group"`
	VideoID     string  `json:"video_id"`
	Baseline    float64 `json:"baseline"`
	Candidate   float64 `json:"candidate"`
}

type BootstrapConfig struct {
	Iterations      int     `json:"iterations" yaml:"iterations"`
	ConfidenceLevel float64 `json:"confidence_level" yaml:"confidence_level"`
	Seed            int64   `json:"seed" yaml:"seed"`
}

type BootstrapResult struct {
	ObservedEffect  float64 `json:"observed_effect"`
	Lower           float64 `json:"lower"`
	Upper           float64 `json:"upper"`
	ConfidenceLevel float64 `json:"confidence_level"`
	Iterations      int     `json:"iterations"`
	Seed            int64   `json:"seed"`
	ClusterCount    int     `json:"cluster_count"`
	CaseCount       int     `json:"case_count"`
}

func (c BootstrapConfig) Validate() error {
	if c.Iterations <= 0 {
		return fmt.Errorf("bootstrap iterations must be positive")
	}
	if c.ConfidenceLevel <= 0 || c.ConfidenceLevel >= 1 {
		return fmt.Errorf("bootstrap confidence_level must be in (0,1)")
	}
	return nil
}

func PairedClusterBootstrap(observations []PairedObservation, cfg BootstrapConfig) (BootstrapResult, error) {
	if err := cfg.Validate(); err != nil {
		return BootstrapResult{}, err
	}
	if len(observations) == 0 {
		return BootstrapResult{}, fmt.Errorf("paired observations are empty")
	}
	seenCases := make(map[string]bool, len(observations))
	clusterValues := make(map[string][]float64)
	for i, observation := range observations {
		if strings.TrimSpace(observation.CaseID) == "" {
			return BootstrapResult{}, fmt.Errorf("observation %d missing case_id", i+1)
		}
		if seenCases[observation.CaseID] {
			return BootstrapResult{}, fmt.Errorf("duplicate case_id %q", observation.CaseID)
		}
		seenCases[observation.CaseID] = true
		if strings.TrimSpace(observation.SourceGroup) == "" {
			return BootstrapResult{}, fmt.Errorf("case %q missing source_group", observation.CaseID)
		}
		clusterValues[observation.SourceGroup] = append(clusterValues[observation.SourceGroup], observation.Candidate-observation.Baseline)
	}
	clusterNames := make([]string, 0, len(clusterValues))
	for name := range clusterValues {
		clusterNames = append(clusterNames, name)
	}
	sort.Strings(clusterNames)
	clusterMeans := make([]float64, len(clusterNames))
	for i, name := range clusterNames {
		clusterMeans[i] = mean(clusterValues[name])
	}

	result := BootstrapResult{
		ObservedEffect: mean(clusterMeans), ConfidenceLevel: cfg.ConfidenceLevel,
		Iterations: cfg.Iterations, Seed: cfg.Seed, ClusterCount: len(clusterMeans), CaseCount: len(observations),
	}
	random := rand.New(rand.NewSource(cfg.Seed))
	samples := make([]float64, cfg.Iterations)
	for iteration := range samples {
		total := 0.0
		for range clusterMeans {
			total += clusterMeans[random.Intn(len(clusterMeans))]
		}
		samples[iteration] = total / float64(len(clusterMeans))
	}
	sort.Float64s(samples)
	alpha := (1 - cfg.ConfidenceLevel) / 2
	result.Lower = percentile(samples, alpha)
	result.Upper = percentile(samples, 1-alpha)
	return result, nil
}

func mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	total := 0.0
	for _, value := range values {
		total += value
	}
	return total / float64(len(values))
}

func percentile(sorted []float64, probability float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if probability <= 0 {
		return sorted[0]
	}
	if probability >= 1 {
		return sorted[len(sorted)-1]
	}
	position := probability * float64(len(sorted)-1)
	lower := int(position)
	upper := lower + 1
	if upper >= len(sorted) {
		return sorted[lower]
	}
	fraction := position - float64(lower)
	return sorted[lower]*(1-fraction) + sorted[upper]*fraction
}
