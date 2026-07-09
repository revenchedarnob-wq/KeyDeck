package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"keydeck.local/feasibilitylab/internal/codexapp"
	"keydeck.local/feasibilitylab/internal/contextbench"
	"keydeck.local/feasibilitylab/internal/contextcompiler"
)

type accountSummary struct {
	Type string `json:"type"`
	Plan string `json:"plan"`
}

type armReport struct {
	Name             string                  `json:"name"`
	Project          string                  `json:"project"`
	ThreadID         string                  `json:"thread_id"`
	TurnID           string                  `json:"turn_id"`
	DurationMS       int64                   `json:"duration_ms"`
	AssistantText    string                  `json:"assistant_text"`
	Metrics          codexapp.TurnMetrics    `json:"metrics"`
	Acceptance       contextbench.Acceptance `json:"acceptance"`
	Diagnostics      []string                `json:"diagnostics,omitempty"`
	EnvironmentClean bool                    `json:"environment_clean"`
	Error            string                  `json:"error,omitempty"`
}

type efficiency struct {
	BaselineCommands            int   `json:"baseline_commands"`
	AssistedCommands            int   `json:"assisted_commands"`
	CommandReduction            int   `json:"command_reduction"`
	BaselineInputTokens         int64 `json:"baseline_input_tokens"`
	AssistedInputTokens         int64 `json:"assisted_input_tokens"`
	InputTokenReduction         int64 `json:"input_token_reduction"`
	BaselineUncachedInputTokens int64 `json:"baseline_uncached_input_tokens"`
	AssistedUncachedInputTokens int64 `json:"assisted_uncached_input_tokens"`
	UncachedInputTokenReduction int64 `json:"uncached_input_token_reduction"`
	TokenComparisonAvailable    bool  `json:"token_comparison_available"`
	Improved                    bool  `json:"improved"`
	NoMajorTokenRegression      bool  `json:"no_major_token_regression"`
}

type report struct {
	Proof             string                      `json:"proof"`
	Passed            bool                        `json:"passed"`
	Platform          string                      `json:"platform"`
	SandboxMode       string                      `json:"sandbox_mode"`
	Root              string                      `json:"root"`
	ScoutProject      string                      `json:"scout_project"`
	Account           accountSummary              `json:"account"`
	ToolReceipt       contextcompiler.ToolReceipt `json:"tool_receipt"`
	ContextPacket     contextcompiler.Packet      `json:"context_packet"`
	ContextPacketPath string                      `json:"context_packet_path"`
	Baseline          armReport                   `json:"baseline"`
	Assisted          armReport                   `json:"assisted"`
	Efficiency        efficiency                  `json:"efficiency"`
	Decision          string                      `json:"decision"`
	Error             string                      `json:"error,omitempty"`
}

func main() {
	var keep bool
	flag.BoolVar(&keep, "keep", true, "Keep disposable benchmark workspace for inspection.")
	flag.Parse()
	rep := report{Proof: "0.8-real-context-compiler-efficiency", Platform: runtime.GOOS + "/" + runtime.GOARCH, SandboxMode: "unelevated"}
	if runtime.GOOS != "windows" {
		fail(&rep, errors.New("real Proof 0.8 is intended for Windows x64"))
	}
	if _, err := exec.LookPath("codex"); err != nil {
		fail(&rep, errors.New("official Codex CLI is not on PATH"))
	}
	if _, err := exec.LookPath("git"); err != nil {
		fail(&rep, errors.New("Git is required so Proof 0.8 can verify both benchmark repositories start identically and source files remain unchanged"))
	}
	root, err := os.MkdirTemp("", "keydeck-real-proof-08-")
	if err != nil {
		fail(&rep, err)
	}
	rep.Root = root
	if !keep {
		defer os.RemoveAll(root)
	} else {
		defer fmt.Println("Disposable benchmark workspace kept for inspection:", root)
	}
	template := filepath.Join(root, "template")
	baselineProject := filepath.Join(root, "baseline")
	scoutProject := filepath.Join(root, "context-scout")
	assistedProject := filepath.Join(root, "assisted")
	if err := contextbench.CreateRepository(template); err != nil {
		fail(&rep, err)
	}
	if err := contextbench.CopyRepository(template, baselineProject); err != nil {
		fail(&rep, err)
	}
	if err := contextbench.CopyRepository(template, scoutProject); err != nil {
		fail(&rep, err)
	}
	if err := contextbench.CopyRepository(template, assistedProject); err != nil {
		fail(&rep, err)
	}
	rep.ScoutProject = scoutProject

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()
	fmt.Println("Preparing pinned local structural context engine...")
	toolRoot := filepath.Join(root, ".keydeck-lab", "tools")
	receipt, err := contextcompiler.EnsurePinnedCBM(ctx, toolRoot)
	if err != nil {
		fail(&rep, fmt.Errorf("prepare codebase-memory-mcp: %w", err))
	}
	rep.ToolReceipt = receipt
	fmt.Printf("Structural engine ready: %s %s\n", receipt.Name, receipt.Version)

	fmt.Println("Compiling focused KeyDeck context packet from structural graph + exact source evidence...")
	cbmCache := filepath.Join(root, ".keydeck-lab", "cbm-cache")
	runner := &contextcompiler.CLIRunner{Binary: receipt.Binary, CacheDir: cbmCache}
	packet, err := (&contextcompiler.Compiler{Runner: runner}).Compile(ctx, contextcompiler.CompileOptions{ProjectRoot: scoutProject, Objective: contextbench.Objective(), MaxChars: 12000, MaxFiles: 6})
	if err != nil {
		fail(&rep, err)
	}
	packet = retargetPacket(packet, scoutProject, assistedProject)
	rep.ContextPacket = packet
	packetPath := filepath.Join(root, ".keydeck-lab", "context-packet.txt")
	if err := os.MkdirAll(filepath.Dir(packetPath), 0o700); err != nil {
		fail(&rep, err)
	}
	if err := os.WriteFile(packetPath, []byte(packet.Render()), 0o600); err != nil {
		fail(&rep, err)
	}
	rep.ContextPacketPath = packetPath
	if !structuralCoreSucceeded(packet) {
		printStructuralEvidence(packet)
		fail(&rep, errors.New("structural context provider did not successfully index and search the benchmark repo"))
	}
	fmt.Printf("Context packet ready: %d chars, %d focused source snippets.\n", packet.RenderedChars, len(packet.SourceSnippets))

	fmt.Println("Launching official Codex App Server for baseline arm (official unelevated Windows sandbox fallback)...")
	diag1 := &diagnosticSink{}
	client1, account, err := startAuthenticatedCodex(ctx, diag1)
	if err != nil {
		fail(&rep, err)
	}
	rep.Account = accountSummary{Type: account.Account.Type, Plan: account.Account.PlanType}
	baseline, err := runArm(ctx, client1, "baseline", baselineProject, contextbench.Prompt(""))
	_ = client1.Close()
	baseline.Diagnostics = diag1.Snapshot()
	baseline.EnvironmentClean = diagnosticsEnvironmentClean(baseline.Diagnostics)
	rep.Baseline = baseline
	if err != nil {
		writeReport(&rep)
		fail(&rep, fmt.Errorf("baseline arm: %w", err))
	}
	fmt.Printf("Baseline complete: commands=%d input=%d cached=%d correctness=%v\n", baseline.Metrics.CommandExecutions, baseline.Metrics.TokenUsage.Total.InputTokens, baseline.Metrics.TokenUsage.Total.CachedInputTokens, baseline.Acceptance.Passed)

	fmt.Println("Launching fresh official Codex App Server for context-assisted arm (same sandbox mode)...")
	diag2 := &diagnosticSink{}
	client2, _, err := startAuthenticatedCodex(ctx, diag2)
	if err != nil {
		fail(&rep, err)
	}
	assisted, err := runArm(ctx, client2, "context-assisted", assistedProject, contextbench.Prompt(packet.Render()))
	_ = client2.Close()
	assisted.Diagnostics = diag2.Snapshot()
	assisted.EnvironmentClean = diagnosticsEnvironmentClean(assisted.Diagnostics)
	rep.Assisted = assisted
	if err != nil {
		writeReport(&rep)
		fail(&rep, fmt.Errorf("context-assisted arm: %w", err))
	}
	fmt.Printf("Assisted complete: commands=%d input=%d cached=%d correctness=%v\n", assisted.Metrics.CommandExecutions, assisted.Metrics.TokenUsage.Total.InputTokens, assisted.Metrics.TokenUsage.Total.CachedInputTokens, assisted.Acceptance.Passed)

	rep.Efficiency = compare(baseline, assisted)
	correct := baseline.Acceptance.Passed && assisted.Acceptance.Passed
	environmentClean := baseline.EnvironmentClean && assisted.EnvironmentClean
	rep.Passed = correct && environmentClean && rep.Efficiency.Improved && rep.Efficiency.NoMajorTokenRegression
	if rep.Passed {
		rep.Decision = "ADOPT: hybrid Context Compiler reduced exploration work without correctness loss in this controlled real-Codex benchmark."
	} else if !correct {
		rep.Decision = "REJECT/REDESIGN: one or both arms failed acceptance checks; no efficiency claim is allowed."
	} else if !environmentClean {
		rep.Decision = "INCONCLUSIVE_ENVIRONMENT: Codex or Windows sandbox diagnostics could bias the benchmark. No efficiency claim is allowed."
	} else {
		rep.Decision = "INCONCLUSIVE: correctness held, but this run did not prove a safe efficiency improvement. Keep raw evidence and redesign before adoption."
	}
	writeReport(&rep)
	if !rep.Passed {
		fmt.Println("Proof did not meet adoption gate. Inspect exact evidence; no savings claim is allowed.")
		os.Exit(1)
	}
	fmt.Println("\nPASS: real Context Compiler benchmark reduced Codex exploration work without correctness loss.")
	fmt.Println("Report:", reportPath(root))
}

func runArm(ctx context.Context, client *codexapp.Client, name, project, prompt string) (armReport, error) {
	a := armReport{Name: name, Project: project}
	thread, err := client.StartThread(ctx, codexapp.StartThreadOptions{CWD: project, ApprovalPolicy: "never", Sandbox: codexapp.ThreadSandboxWorkspaceWrite, ServiceName: "keydeck_lab_proof_08"})
	if err != nil {
		a.Error = err.Error()
		return a, err
	}
	a.ThreadID = thread.ID
	turn, err := client.StartTurn(ctx, thread.ID, prompt, project)
	if err != nil {
		a.Error = err.Error()
		return a, err
	}
	a.TurnID = turn.ID
	start := time.Now()
	outcome, err := client.CollectTurnObserved(ctx, turn.ID, printCodexEvent)
	a.DurationMS = time.Since(start).Milliseconds()
	a.AssistantText = strings.TrimSpace(outcome.Text)
	a.Metrics = outcome.Metrics
	a.Acceptance = contextbench.Evaluate(project)
	if err != nil {
		a.Error = err.Error()
		return a, err
	}
	if !a.Acceptance.Passed {
		return a, errors.New("acceptance checks failed")
	}
	return a, nil
}

func compare(b, a armReport) efficiency {
	e := efficiency{BaselineCommands: b.Metrics.CommandExecutions, AssistedCommands: a.Metrics.CommandExecutions}
	e.CommandReduction = e.BaselineCommands - e.AssistedCommands
	e.BaselineInputTokens = b.Metrics.TokenUsage.Total.InputTokens
	e.AssistedInputTokens = a.Metrics.TokenUsage.Total.InputTokens
	e.InputTokenReduction = e.BaselineInputTokens - e.AssistedInputTokens
	e.BaselineUncachedInputTokens = max64(0, b.Metrics.TokenUsage.Total.InputTokens-b.Metrics.TokenUsage.Total.CachedInputTokens)
	e.AssistedUncachedInputTokens = max64(0, a.Metrics.TokenUsage.Total.InputTokens-a.Metrics.TokenUsage.Total.CachedInputTokens)
	e.UncachedInputTokenReduction = e.BaselineUncachedInputTokens - e.AssistedUncachedInputTokens
	e.TokenComparisonAvailable = b.Metrics.TokenUsageObserved && a.Metrics.TokenUsageObserved && e.BaselineInputTokens > 0
	e.Improved = e.CommandReduction > 0 || (e.TokenComparisonAvailable && (e.InputTokenReduction > 0 || e.UncachedInputTokenReduction > 0))
	e.NoMajorTokenRegression = true
	if e.TokenComparisonAvailable && e.AssistedInputTokens > e.BaselineInputTokens+(e.BaselineInputTokens/4) {
		e.NoMajorTokenRegression = false
	}
	return e
}

func structuralCoreSucceeded(p contextcompiler.Packet) bool {
	return p.StructuralIndexSucceeded && p.StructuralSearchSucceeded
}

func printStructuralEvidence(p contextcompiler.Packet) {
	fmt.Println("Structural provider evidence:")
	for _, e := range p.StructuralEvidence {
		status := "OK"
		if !e.Successful {
			status = "FAILED"
		}
		fmt.Printf("  [%s] %s args=%s\n", status, e.Tool, e.Arguments)
		if e.Error != "" {
			fmt.Println("    error:", e.Error)
		}
		if strings.TrimSpace(e.Output) != "" {
			out := strings.TrimSpace(e.Output)
			if len(out) > 800 {
				out = out[:800] + "..."
			}
			fmt.Println("    output:", out)
		}
	}
}

func retargetPacket(p contextcompiler.Packet, fromRoot, toRoot string) contextcompiler.Packet {
	p.ProjectRoot = toRoot
	p.ProjectID = ""
	replacements := [][2]string{
		{fromRoot, toRoot},
		{filepath.ToSlash(fromRoot), filepath.ToSlash(toRoot)},
		{strings.ReplaceAll(fromRoot, `\`, `\\`), strings.ReplaceAll(toRoot, `\`, `\\`)},
	}
	for i := range p.StructuralEvidence {
		for _, pair := range replacements {
			p.StructuralEvidence[i].Output = strings.ReplaceAll(p.StructuralEvidence[i].Output, pair[0], pair[1])
		}
	}
	p.RenderedChars = len(p.Render())
	return p
}

type diagnosticSink struct {
	mu    sync.Mutex
	lines []string
}

func (d *diagnosticSink) Add(line string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.lines = append(d.lines, line)
}

func (d *diagnosticSink) Snapshot() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.lines...)
}

func diagnosticsEnvironmentClean(lines []string) bool {
	bad := []string{
		"createprocesswithlogonw failed",
		"loading managed windows powershell failed",
		"the term 'go' is not recognized",
		"the term \"go\" is not recognized",
		"windows sandbox: orchestrator_helper_launch_failed",
	}
	for _, line := range lines {
		lower := strings.ToLower(line)
		for _, needle := range bad {
			if strings.Contains(lower, needle) {
				return false
			}
		}
	}
	return true
}

func startAuthenticatedCodex(ctx context.Context, sink *diagnosticSink) (*codexapp.Client, codexapp.AccountReadResult, error) {
	transport, err := codexapp.NewProcessTransport(ctx, "codex", "-c", `windows.sandbox="unelevated"`, "app-server")
	if err != nil {
		return nil, codexapp.AccountReadResult{}, err
	}
	client := codexapp.NewClient(transport, codexapp.ClientInfo{Name: "keydeck_lab", Title: "KeyDeck Feasibility Lab", Version: "0.5.3"})
	if err := client.Initialize(ctx); err != nil {
		_ = client.Close()
		return nil, codexapp.AccountReadResult{}, err
	}
	startDiagnosticPrinter(client, sink)
	account, err := client.AccountRead(ctx)
	if err != nil {
		_ = client.Close()
		return nil, account, err
	}
	if account.Account == nil || account.Account.Type != "chatgpt" {
		_ = client.Close()
		return nil, account, errors.New("Codex is not logged in with ChatGPT; use official ChatGPT login first")
	}
	fmt.Printf("Codex ChatGPT account detected (plan: %s).\n", account.Account.PlanType)
	return client, account, nil
}

func startDiagnosticPrinter(client *codexapp.Client, sink *diagnosticSink) {
	go func() {
		for {
			select {
			case err := <-client.Diagnostics():
				if err != nil {
					line := err.Error()
					if sink != nil {
						sink.Add(line)
					}
					fmt.Println("  [App Server diagnostic]", line)
				}
			case <-client.Done():
				return
			}
		}
	}()
}
func printCodexEvent(note codexapp.Notification) {
	switch note.Method {
	case "turn/started", "turn/completed":
		fmt.Println("  [Codex]", note.Method)
	case "item/completed":
		var p struct {
			Item struct {
				Type string `json:"type"`
			} `json:"item"`
		}
		if json.Unmarshal(note.Params, &p) == nil && (p.Item.Type == "commandExecution" || p.Item.Type == "fileChange") {
			fmt.Println("  [Codex] completed", p.Item.Type)
		}
	}
}
func reportPath(root string) string {
	return filepath.Join(root, ".keydeck-lab", "proof08-real-report.json")
}
func writeReport(rep *report) {
	if rep.Root == "" {
		return
	}
	_ = os.MkdirAll(filepath.Join(rep.Root, ".keydeck-lab"), 0o700)
	b, _ := json.MarshalIndent(rep, "", "  ")
	_ = os.WriteFile(reportPath(rep.Root), b, 0o600)
}
func fail(rep *report, err error) {
	rep.Error = err.Error()
	writeReport(rep)
	fmt.Fprintln(os.Stderr, "FAIL:", err)
	os.Exit(1)
}
func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
