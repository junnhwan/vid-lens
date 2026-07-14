package eval

import (
	"math"
	"testing"
)

func TestBootstrapResamplesPairedSourceGroupClusters(t *testing.T) {
	observations := make([]PairedObservation, 0, 200)
	for i := 0; i < 100; i++ {
		observations = append(observations,
			PairedObservation{CaseID: "a-" + itoa(i), SourceGroup: "group-a", VideoID: "video-a", Baseline: 0, Candidate: 1},
			PairedObservation{CaseID: "b-" + itoa(i), SourceGroup: "group-b", VideoID: "video-b", Baseline: 1, Candidate: 0},
		)
	}

	result, err := PairedClusterBootstrap(observations, BootstrapConfig{Iterations: 5_000, ConfidenceLevel: 0.95, Seed: 7})
	if err != nil {
		t.Fatalf("PairedClusterBootstrap() error = %v", err)
	}
	if result.ClusterCount != 2 || result.CaseCount != 200 || math.Abs(result.ObservedEffect) > 1e-12 {
		t.Fatalf("result = %+v, want 2 clusters, 200 cases, zero paired effect", result)
	}
	// Resampling cases independently would create a misleading narrow interval around zero.
	// Cluster resampling must retain the perfectly correlated +/-1 source effects.
	if result.Lower > -0.9 || result.Upper < 0.9 {
		t.Fatalf("cluster CI = [%.3f, %.3f], want interval retaining source-level uncertainty", result.Lower, result.Upper)
	}
}

func TestBootstrapUsesEqualSourceGroupWeightAndIsDeterministic(t *testing.T) {
	observations := []PairedObservation{
		{CaseID: "large-1", SourceGroup: "large", VideoID: "video-large", Baseline: 0, Candidate: 1},
		{CaseID: "large-2", SourceGroup: "large", VideoID: "video-large", Baseline: 0, Candidate: 1},
		{CaseID: "large-3", SourceGroup: "large", VideoID: "video-large", Baseline: 0, Candidate: 1},
		{CaseID: "small-1", SourceGroup: "small", VideoID: "video-small", Baseline: 1, Candidate: 0},
	}
	cfg := BootstrapConfig{Iterations: 1_000, ConfidenceLevel: 0.95, Seed: 99}
	first, err := PairedClusterBootstrap(observations, cfg)
	if err != nil {
		t.Fatal(err)
	}
	second, err := PairedClusterBootstrap(observations, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("same seed produced different results: %+v != %+v", first, second)
	}
	if math.Abs(first.ObservedEffect) > 1e-12 {
		t.Fatalf("ObservedEffect = %.3f, want equal-weight source-group mean 0", first.ObservedEffect)
	}
}

func TestBootstrapRejectsDuplicateCaseOrMissingSourceGroup(t *testing.T) {
	tests := []struct {
		name string
		data []PairedObservation
	}{
		{name: "duplicate", data: []PairedObservation{{CaseID: "x", SourceGroup: "a"}, {CaseID: "x", SourceGroup: "b"}}},
		{name: "missing group", data: []PairedObservation{{CaseID: "x"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := PairedClusterBootstrap(tt.data, BootstrapConfig{Iterations: 100, ConfidenceLevel: 0.95, Seed: 1}); err == nil {
				t.Fatal("PairedClusterBootstrap() error = nil")
			}
		})
	}
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := make([]byte, 0, 4)
	for value > 0 {
		digits = append(digits, byte('0'+value%10))
		value /= 10
	}
	for i, j := 0, len(digits)-1; i < j; i, j = i+1, j-1 {
		digits[i], digits[j] = digits[j], digits[i]
	}
	return string(digits)
}
