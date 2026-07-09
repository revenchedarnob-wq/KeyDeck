package agentenvproof

import "testing"

func validAPMEvidence() APMEvidence {
	var e APMEvidence
	e.ProofComponent = "microsoft-apm-real-prototype"
	e.Tool.Name = "Microsoft Agent Package Manager"
	e.Tool.Version = expectedAPMVersion
	e.Passed = true
	e.Checks.InitialCleanAudit.Passed = true
	e.Checks.DriftDetection.Passed = true
	e.Checks.DriftDetection.AuditExitCode = 1
	e.Checks.FrozenRestore.Passed = true
	e.Checks.SecondCleanInstall.Passed = true
	e.Checks.DeployedHashesReproducible.Passed = true
	e.Checks.PluginBundleCreated.Passed = true
	e.Coverage.LockfilePresent = true
	e.Coverage.MCPDependenciesDeclared = true
	e.Coverage.SourcePrimitiveFileCount = 5
	e.Coverage.GeneratedDeployedFileCount = 3
	e.Coverage.PrimitiveTypes = []string{"instructions", "agents", "prompts", "skills", "hooks"}
	return e
}

func validWazaEvidence() WazaEvidence {
	var e WazaEvidence
	e.ProofComponent = "microsoft-waza-real-offline-prototype"
	e.SourceEvidenceSHA256 = expectedWazaEvidenceSHA256
	e.Tool.Name = "Microsoft Waza"
	e.Tool.Version = expectedWazaVersion
	e.Tool.BinarySHA256 = expectedWazaBinarySHA256
	e.Tool.ChecksumVerified = true
	e.Passed = true
	e.Checks.VersionVerified = true
	e.Checks.SkillCheck = true
	e.Checks.SpecCoverageGate = true
	e.Checks.TokenCount = true
	e.Checks.TokenBudgetGate = true
	e.Checks.DeterministicMockEval = true
	e.Checks.RegressionGate = true
	e.Checks.SnapshotCount = 20
	e.Checks.SnapshotReplaysPassed = true
	e.Checks.AdversarialCatalogAvailable = true
	e.Measurements.TaskCount = 4
	e.Measurements.TrialsPerTask = 5
	e.Measurements.IntendedTrialCount = 20
	e.Measurements.PositiveTriggerTasks = 2
	e.Measurements.NegativeTriggerTasks = 2
	return e
}

func TestValidateAPMAcceptsRequiredEvidence(t *testing.T) {
	if err := ValidateAPM(validAPMEvidence()); err != nil {
		t.Fatal(err)
	}
}

func TestValidateAPMRejectsMissingPrimitive(t *testing.T) {
	e := validAPMEvidence()
	e.Coverage.PrimitiveTypes = []string{"instructions", "agents", "prompts", "skills"}
	if err := ValidateAPM(e); err == nil {
		t.Fatal("expected missing primitive validation failure")
	}
}

func TestValidateWazaAcceptsPinnedEvidence(t *testing.T) {
	if err := ValidateWaza(validWazaEvidence()); err != nil {
		t.Fatal(err)
	}
}

func TestValidateWazaRejectsUnverifiedBinary(t *testing.T) {
	e := validWazaEvidence()
	e.Tool.ChecksumVerified = false
	if err := ValidateWaza(e); err == nil {
		t.Fatal("expected checksum validation failure")
	}
}

func TestValidateWazaRejectsWrongEvidenceHash(t *testing.T) {
	e := validWazaEvidence()
	e.SourceEvidenceSHA256 = "deadbeef"
	if err := ValidateWaza(e); err == nil {
		t.Fatal("expected source evidence hash validation failure")
	}
}

func TestValidateWazaRejectsIncompleteReplaySet(t *testing.T) {
	e := validWazaEvidence()
	e.Checks.SnapshotCount = 19
	if err := ValidateWaza(e); err == nil {
		t.Fatal("expected snapshot-count validation failure")
	}
}
