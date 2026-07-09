package agentenvproof

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

const (
	expectedAPMVersion         = "0.24.0"
	expectedWazaVersion        = "0.38.0"
	expectedWazaBinarySHA256   = "ff7fe521d4f876de29d018a00fe282746109d8b788a6e9a9f288dbd8a3470364"
	expectedWazaEvidenceSHA256 = "595c381fe97c063e633aafe45f029702a91d10162d65df0faa0041255e849baf"
)

type APMEvidence struct {
	ProofComponent string `json:"proof_component"`
	Tool           struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"tool"`
	Mode   string `json:"mode"`
	Passed bool   `json:"passed"`
	Checks struct {
		InitialCleanAudit struct {
			Passed bool `json:"passed"`
		} `json:"initial_clean_audit"`
		DriftDetection struct {
			Passed        bool `json:"passed"`
			AuditExitCode int  `json:"audit_exit_code"`
		} `json:"drift_detection"`
		FrozenRestore struct {
			Passed bool `json:"passed"`
		} `json:"frozen_restore"`
		SecondCleanInstall struct {
			Passed bool `json:"passed"`
		} `json:"second_clean_install"`
		DeployedHashesReproducible struct {
			Passed bool `json:"passed"`
		} `json:"deployed_hashes_reproducible"`
		PluginBundleCreated struct {
			Passed bool `json:"passed"`
		} `json:"plugin_bundle_created"`
	} `json:"checks"`
	Coverage struct {
		PrimitiveTypes             []string `json:"primitive_types"`
		SourcePrimitiveFileCount   int      `json:"source_primitive_file_count"`
		GeneratedDeployedFileCount int      `json:"generated_deployed_file_count"`
		LockfilePresent            bool     `json:"lockfile_present"`
		MCPDependenciesDeclared    bool     `json:"mcp_dependencies_declared"`
		MCPDependencyCount         int      `json:"mcp_dependency_count"`
	} `json:"coverage"`
	Limitations []string `json:"limitations"`
}

type WazaEvidence struct {
	ProofComponent       string `json:"proof_component"`
	SourceEvidenceSHA256 string `json:"source_evidence_sha256"`
	Tool                 struct {
		Name             string `json:"name"`
		Version          string `json:"version"`
		BinarySHA256     string `json:"binary_sha256"`
		ChecksumVerified bool   `json:"checksum_verified"`
	} `json:"tool"`
	Mode   string `json:"mode"`
	Passed bool   `json:"passed"`
	Checks struct {
		VersionVerified             bool `json:"version_verified"`
		SkillCheck                  bool `json:"skill_check"`
		SpecCoverageGate            bool `json:"spec_coverage_gate"`
		TokenCount                  bool `json:"token_count"`
		TokenBudgetGate             bool `json:"token_budget_gate"`
		DeterministicMockEval       bool `json:"deterministic_mock_eval"`
		RegressionGate              bool `json:"regression_gate"`
		SnapshotCount               int  `json:"snapshot_count"`
		SnapshotReplaysPassed       bool `json:"snapshot_replays_passed"`
		AdversarialCatalogAvailable bool `json:"adversarial_catalog_available"`
	} `json:"checks"`
	Measurements struct {
		TaskCount            int `json:"task_count"`
		TrialsPerTask        int `json:"trials_per_task"`
		IntendedTrialCount   int `json:"intended_trial_count"`
		PositiveTriggerTasks int `json:"positive_trigger_tasks"`
		NegativeTriggerTasks int `json:"negative_trigger_tasks"`
	} `json:"measurements"`
	Limitations []string `json:"limitations"`
}

func ReadAPM(path string) (APMEvidence, error) {
	var evidence APMEvidence
	if err := readJSON(path, &evidence); err != nil {
		return evidence, err
	}
	return evidence, ValidateAPM(evidence)
}

func ReadWaza(path string) (WazaEvidence, error) {
	var evidence WazaEvidence
	if err := readJSON(path, &evidence); err != nil {
		return evidence, err
	}
	return evidence, ValidateWaza(evidence)
}

func ValidateAPM(e APMEvidence) error {
	if e.ProofComponent != "microsoft-apm-real-prototype" {
		return fmt.Errorf("unexpected APM proof component %q", e.ProofComponent)
	}
	if e.Tool.Name != "Microsoft Agent Package Manager" {
		return fmt.Errorf("unexpected APM tool name %q", e.Tool.Name)
	}
	if e.Tool.Version != expectedAPMVersion {
		return fmt.Errorf("unexpected APM version %q", e.Tool.Version)
	}
	if !e.Passed {
		return errors.New("APM proof component did not pass")
	}
	checks := []bool{
		e.Checks.InitialCleanAudit.Passed,
		e.Checks.DriftDetection.Passed,
		e.Checks.FrozenRestore.Passed,
		e.Checks.SecondCleanInstall.Passed,
		e.Checks.DeployedHashesReproducible.Passed,
		e.Checks.PluginBundleCreated.Passed,
	}
	for i, ok := range checks {
		if !ok {
			return fmt.Errorf("APM required check %d failed", i+1)
		}
	}
	if e.Checks.DriftDetection.AuditExitCode != 1 {
		return fmt.Errorf("APM drift audit exit code = %d, want 1", e.Checks.DriftDetection.AuditExitCode)
	}
	if !e.Coverage.LockfilePresent || !e.Coverage.MCPDependenciesDeclared {
		return errors.New("APM manifest/lockfile coverage incomplete")
	}
	if e.Coverage.SourcePrimitiveFileCount < 5 || e.Coverage.GeneratedDeployedFileCount < 2 {
		return errors.New("APM primitive/deployment coverage is incomplete")
	}
	required := map[string]bool{"instructions": false, "agents": false, "prompts": false, "skills": false, "hooks": false}
	for _, kind := range e.Coverage.PrimitiveTypes {
		if _, ok := required[kind]; ok {
			required[kind] = true
		}
	}
	for kind, ok := range required {
		if !ok {
			return fmt.Errorf("APM primitive %s not represented", kind)
		}
	}
	return nil
}

func ValidateWaza(e WazaEvidence) error {
	if e.ProofComponent != "microsoft-waza-real-offline-prototype" {
		return fmt.Errorf("unexpected Waza proof component %q", e.ProofComponent)
	}
	if e.SourceEvidenceSHA256 != expectedWazaEvidenceSHA256 {
		return fmt.Errorf("unexpected Waza source evidence SHA-256 %q", e.SourceEvidenceSHA256)
	}
	if e.Tool.Name != "Microsoft Waza" {
		return fmt.Errorf("unexpected Waza tool name %q", e.Tool.Name)
	}
	if e.Tool.Version != expectedWazaVersion {
		return fmt.Errorf("unexpected Waza version %q", e.Tool.Version)
	}
	if e.Tool.BinarySHA256 != expectedWazaBinarySHA256 || !e.Tool.ChecksumVerified {
		return errors.New("Waza binary checksum was not verified")
	}
	if !e.Passed {
		return errors.New("Waza proof component did not pass")
	}
	checks := []bool{
		e.Checks.VersionVerified,
		e.Checks.SkillCheck,
		e.Checks.SpecCoverageGate,
		e.Checks.TokenCount,
		e.Checks.TokenBudgetGate,
		e.Checks.DeterministicMockEval,
		e.Checks.RegressionGate,
		e.Checks.SnapshotReplaysPassed,
		e.Checks.AdversarialCatalogAvailable,
	}
	for i, ok := range checks {
		if !ok {
			return fmt.Errorf("Waza required check %d failed", i+1)
		}
	}
	if e.Measurements.TaskCount != 4 || e.Measurements.TrialsPerTask != 5 || e.Measurements.IntendedTrialCount != 20 {
		return errors.New("Waza deterministic trial plan is not the expected 4 tasks x 5 trials")
	}
	if e.Measurements.PositiveTriggerTasks != 2 || e.Measurements.NegativeTriggerTasks != 2 {
		return errors.New("Waza trigger-boundary coverage is incomplete")
	}
	if e.Measurements.IntendedTrialCount != e.Measurements.TaskCount*e.Measurements.TrialsPerTask {
		return errors.New("Waza trial-count evidence is inconsistent")
	}
	if e.Checks.SnapshotCount != e.Measurements.IntendedTrialCount {
		return fmt.Errorf("Waza snapshot count = %d, want %d", e.Checks.SnapshotCount, e.Measurements.IntendedTrialCount)
	}
	return nil
}

func readJSON(path string, dst any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, dst); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}
