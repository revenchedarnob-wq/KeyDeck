package conformance

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrOptimizationNotVerified = errors.New("optimizer is not verified for the exact provider version")

type OptimizationMode string

const (
	OptimizationOff OptimizationMode = "off"
	OptimizationOn  OptimizationMode = "on"
)

type EvidenceStatus string

const (
	EvidenceVerified     EvidenceStatus = "verified"
	EvidenceExperimental EvidenceStatus = "experimental"
	EvidenceBlocked      EvidenceStatus = "blocked"
)

type OptimizerEvidence struct {
	Provider             string         `json:"provider"`
	Version              string         `json:"version"`
	TestedAt             time.Time      `json:"tested_at"`
	EvidenceID           string         `json:"evidence_id"`
	Status               EvidenceStatus `json:"status"`
	CorrectnessPreserved bool           `json:"correctness_preserved"`
	MeasurableBenefit    bool           `json:"measurable_benefit"`
}

type Activation struct {
	Active     bool   `json:"active"`
	Reason     string `json:"reason"`
	EvidenceID string `json:"evidence_id,omitempty"`
}

type Optimizer struct {
	Evidence  OptimizerEvidence
	Transform func([]byte) ([]byte, error)
}

// Apply enforces the user-visible Optimization ON/OFF contract.
// OFF is byte-preserving and never invokes the transform. ON fails closed and
// returns the original request unchanged unless exact, dated, verified evidence
// exists for the requested provider version.
func (o Optimizer) Apply(mode OptimizationMode, provider, version string, request []byte) ([]byte, Activation, error) {
	original := append([]byte(nil), request...)
	if mode == OptimizationOff {
		return original, Activation{Reason: "optimization off: provider-native request bytes preserved"}, nil
	}
	if mode != OptimizationOn {
		return original, Activation{Reason: "unknown optimization mode"}, fmt.Errorf("%w: unknown mode %q", ErrOptimizationNotVerified, mode)
	}
	if !strings.EqualFold(strings.TrimSpace(o.Evidence.Provider), strings.TrimSpace(provider)) || o.Evidence.Version != version || o.Evidence.TestedAt.IsZero() || strings.TrimSpace(o.Evidence.EvidenceID) == "" || o.Evidence.Status != EvidenceVerified || !o.Evidence.CorrectnessPreserved || !o.Evidence.MeasurableBenefit || o.Transform == nil {
		return original, Activation{Reason: "exact verified optimizer evidence not available"}, ErrOptimizationNotVerified
	}
	candidate, err := o.Transform(append([]byte(nil), original...))
	if err != nil {
		return original, Activation{Reason: "optimizer transform failed"}, err
	}
	if candidate == nil {
		return original, Activation{Reason: "optimizer returned nil output"}, ErrOptimizationNotVerified
	}
	if bytes.Equal(candidate, original) {
		return candidate, Activation{Active: true, Reason: "verified optimizer activated with no byte change", EvidenceID: o.Evidence.EvidenceID}, nil
	}
	return candidate, Activation{Active: true, Reason: "verified optimizer activated", EvidenceID: o.Evidence.EvidenceID}, nil
}
