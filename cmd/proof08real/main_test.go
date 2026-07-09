package main

import "testing"

func TestDiagnosticsEnvironmentClean(t *testing.T) {
	clean := []string{"codex app-server: harmless informational diagnostic"}
	if !diagnosticsEnvironmentClean(clean) {
		t.Fatal("harmless diagnostics should remain clean")
	}
	for _, line := range []string{
		"windows sandbox: CreateProcessWithLogonW failed: 1056",
		"The term 'go' is not recognized as a name of a cmdlet",
		"Loading managed Windows PowerShell failed",
		"windows sandbox: orchestrator_helper_launch_failed",
	} {
		if diagnosticsEnvironmentClean([]string{line}) {
			t.Fatalf("expected environment contamination for %q", line)
		}
	}
}
