package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"keydeck.local/feasibilitylab/internal/agentenvproof"
)

type component struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail any    `json:"detail,omitempty"`
}

type report struct {
	Proof                      string      `json:"proof"`
	Status                     string      `json:"status"`
	Passed                     bool        `json:"passed"`
	Decision                   string      `json:"decision"`
	Components                 []component `json:"components"`
	MeasuredEngineeringSavings []string    `json:"measured_engineering_savings"`
	AdoptionRules              []string    `json:"adoption_rules"`
	OwnershipBoundary          []string    `json:"ownership_boundary"`
	Limitations                []string    `json:"limitations"`
}

func main() {
	apmPath := flag.String("apm-evidence", "testdata/proof14/apm-evidence/KeyDeck-Proof-0.14-APM-evidence.json", "real APM evidence JSON")
	wazaPath := flag.String("waza-evidence", "testdata/proof14/waza-evidence/KeyDeck-Proof-0.14-Waza-evidence.json", "validated normalized Waza evidence JSON")
	flag.Parse()

	out := report{
		Proof:    "0.14-apm-waza-proving-ground",
		Status:   "failed",
		Decision: "PENDING",
		OwnershipBoundary: []string{
			"APM may own declarative Agent Environment packaging, deployment metadata, lockfiles and drift detection only",
			"Waza may own development-time skill and agent evaluation infrastructure only",
			"KeyDeck retains canonical state, runtime safety, recovery, tool replay safety, permissions orchestration and financial policy",
		},
	}

	apm, err := agentenvproof.ReadAPM(*apmPath)
	if err != nil {
		out.Components = append(out.Components, component{Name: "microsoft_apm_real_prototype", Passed: false, Detail: err.Error()})
		emit(out, 1)
	}
	out.Components = append(out.Components, component{
		Name:   "microsoft_apm_real_prototype",
		Passed: true,
		Detail: map[string]any{
			"version":                  apm.Tool.Version,
			"primitive_types":          apm.Coverage.PrimitiveTypes,
			"source_primitive_files":   apm.Coverage.SourcePrimitiveFileCount,
			"generated_deployed_files": apm.Coverage.GeneratedDeployedFileCount,
			"lockfile":                 apm.Coverage.LockfilePresent,
			"drift_detection":          apm.Checks.DriftDetection.Passed,
			"frozen_restore":           apm.Checks.FrozenRestore.Passed,
			"reproducible_deployment":  apm.Checks.DeployedHashesReproducible.Passed,
		},
	})

	waza, err := agentenvproof.ReadWaza(*wazaPath)
	if err != nil {
		out.Components = append(out.Components, component{Name: "microsoft_waza_real_prototype", Passed: false, Detail: err.Error()})
		emit(out, 1)
	}
	out.Components = append(out.Components, component{
		Name:   "microsoft_waza_real_prototype",
		Passed: true,
		Detail: map[string]any{
			"version":                waza.Tool.Version,
			"binary_sha256":          waza.Tool.BinarySHA256,
			"source_evidence_sha256": waza.SourceEvidenceSHA256,
			"tasks":                  waza.Measurements.TaskCount,
			"trials":                 waza.Measurements.IntendedTrialCount,
			"snapshots":              waza.Checks.SnapshotCount,
			"snapshot_replay":        waza.Checks.SnapshotReplaysPassed,
			"token_budget_gate":      waza.Checks.TokenBudgetGate,
			"regression_gate":        waza.Checks.RegressionGate,
			"adversarial_catalog":    waza.Checks.AdversarialCatalogAvailable,
		},
	})

	out.MeasuredEngineeringSavings = []string{
		"APM replaced custom prototype work for declarative primitive deployment, lockfile creation, audit, drift detection, frozen restore and reproducible deployment checks",
		"Waza replaced custom prototype work for skill validation, trigger-spec coverage, token-budget checks, deterministic evaluation, snapshot capture/replay, regression gating and adversarial catalog discovery",
		"Together they remove enough generic Agent Environment and evaluation plumbing to defer a large custom KeyDeck Skill Compiler and custom skill-evaluation stack",
	}
	out.AdoptionRules = []string{
		"Adopt APM as the preferred declarative Agent Environment prototype, not as KeyDeck canonical state or runtime orchestration",
		"Adopt Waza as a development-time skill/agent proving ground, not as a production runtime dependency",
		"Keep executable community extensions in Extism/WebAssembly and tools in MCP; APM packages declarative agent content",
		"Require later focused conformance before relying on remote Git pin resolution or non-empty MCP dependency resolution",
		"Treat live-model quality comparison as an optional later Waza evaluation; this proof intentionally isolated infrastructure with the mock executor",
	}
	out.Limitations = append(out.Limitations, apm.Limitations...)
	out.Limitations = append(out.Limitations, waza.Limitations...)
	out.Status = "passed"
	out.Passed = true
	out.Decision = "ADOPT_APM_AND_WAZA_WITH_SCOPED_OWNERSHIP_AND_LIMITATIONS"
	emit(out, 0)
}

func emit(out report, code int) {
	b, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(b))
	os.Exit(code)
}
