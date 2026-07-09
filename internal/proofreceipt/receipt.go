package proofreceipt

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"keydeck.local/feasibilitylab/internal/secretbroker"
	"keydeck.local/feasibilitylab/internal/tasks"
	"keydeck.local/feasibilitylab/internal/timeline"
)

const Version = 1

var ErrSecretDetected = errors.New("proof receipt input contains secret-like material")

type Artifact struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type TimelineRef struct {
	EventID   string          `json:"event_id"`
	Sequence  uint64          `json:"sequence"`
	At        time.Time       `json:"at"`
	Domain    timeline.Domain `json:"domain"`
	Kind      string          `json:"kind"`
	SourceRef string          `json:"source_ref,omitempty"`
	Summary   string          `json:"summary,omitempty"`
}

type Receipt struct {
	Version          int                     `json:"version"`
	ReceiptID        string                  `json:"receipt_id"`
	InputDigest      string                  `json:"input_digest"`
	TaskID           string                  `json:"task_id"`
	SessionID        string                  `json:"session_id"`
	GeneratedAt      time.Time               `json:"generated_at"`
	Goal             string                  `json:"goal"`
	Status           tasks.Status            `json:"status"`
	Progress         tasks.Progress          `json:"progress"`
	AcceptanceChecks []tasks.AcceptanceCheck `json:"acceptance_checks"`
	TimelineRefs     []TimelineRef           `json:"timeline_refs"`
	Artifacts        []Artifact              `json:"artifacts,omitempty"`
}

type buildInput struct {
	TaskID           string                  `json:"task_id"`
	SessionID        string                  `json:"session_id"`
	Goal             string                  `json:"goal"`
	Status           tasks.Status            `json:"status"`
	Progress         tasks.Progress          `json:"progress"`
	AcceptanceChecks []tasks.AcceptanceCheck `json:"acceptance_checks"`
	TimelineRefs     []TimelineRef           `json:"timeline_refs"`
	Artifacts        []Artifact              `json:"artifacts,omitempty"`
}

func Build(state tasks.State, events []timeline.Event, artifacts []Artifact) (Receipt, error) {
	return build(state, events, artifacts, false)
}

// BuildRedacted produces a receipt that may include timeline summaries while
// replacing exact sensitive values before they enter receipt identity or Markdown.
// Existing Build behavior remains unchanged for stable historical receipt identities.
func BuildRedacted(state tasks.State, events []timeline.Event, artifacts []Artifact, sensitiveValues []string) (Receipt, error) {
	state = redactState(state, sensitiveValues)
	events = redactEvents(events, sensitiveValues)
	artifacts = redactArtifacts(artifacts, sensitiveValues)
	receipt, err := build(state, events, artifacts, true)
	if err != nil {
		return Receipt{}, err
	}
	raw, err := json.Marshal(receipt)
	if err != nil {
		return Receipt{}, err
	}
	for _, value := range sensitiveValues {
		if value != "" && strings.Contains(string(raw), value) {
			return Receipt{}, ErrSecretDetected
		}
	}
	return receipt, nil
}

func build(state tasks.State, events []timeline.Event, artifacts []Artifact, includeSummary bool) (Receipt, error) {
	if state.TaskID == "" || state.SessionID == "" || state.Contract.Goal == "" {
		return Receipt{}, errors.New("task state is incomplete")
	}

	refs := make([]TimelineRef, 0, len(events))
	for _, event := range events {
		if event.TaskID != state.TaskID || event.SessionID != state.SessionID || event.Domain == timeline.DomainProof {
			continue
		}
		ref := TimelineRef{
			EventID: event.EventID, Sequence: event.Sequence, At: event.At,
			Domain: event.Domain, Kind: event.Kind, SourceRef: event.SourceRef,
		}
		if includeSummary {
			ref.Summary = event.Summary
		}
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].Sequence < refs[j].Sequence })

	artifacts = append([]Artifact(nil), artifacts...)
	sort.Slice(artifacts, func(i, j int) bool {
		if artifacts[i].Name == artifacts[j].Name {
			return artifacts[i].Path < artifacts[j].Path
		}
		return artifacts[i].Name < artifacts[j].Name
	})

	checks := append([]tasks.AcceptanceCheck(nil), state.Contract.Checks...)
	input := buildInput{
		TaskID: state.TaskID, SessionID: state.SessionID, Goal: state.Contract.Goal,
		Status: state.Status, Progress: state.Progress(), AcceptanceChecks: checks,
		TimelineRefs: refs, Artifacts: artifacts,
	}
	if err := rejectSecrets(input); err != nil {
		return Receipt{}, err
	}
	canonical, err := json.Marshal(input)
	if err != nil {
		return Receipt{}, err
	}
	sum := sha256.Sum256(canonical)
	digest := hex.EncodeToString(sum[:])
	generatedAt := state.UpdatedAt
	if len(refs) > 0 {
		generatedAt = refs[len(refs)-1].At
	}
	return Receipt{
		Version: Version, ReceiptID: "receipt-" + digest[:20], InputDigest: digest,
		TaskID: state.TaskID, SessionID: state.SessionID, GeneratedAt: generatedAt,
		Goal: state.Contract.Goal, Status: state.Status, Progress: state.Progress(),
		AcceptanceChecks: checks, TimelineRefs: refs, Artifacts: artifacts,
	}, nil
}

func (r Receipt) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# KeyDeck Proof Receipt\n\n")
	fmt.Fprintf(&b, "- Receipt: `%s`\n", r.ReceiptID)
	fmt.Fprintf(&b, "- Task: `%s`\n", r.TaskID)
	fmt.Fprintf(&b, "- Session: `%s`\n", r.SessionID)
	fmt.Fprintf(&b, "- Status: `%s`\n", r.Status)
	fmt.Fprintf(&b, "- Verified progress: %.2f%% (%d/%d checks)\n\n", r.Progress.Percent, r.Progress.PassedChecks, r.Progress.TotalChecks)
	fmt.Fprintf(&b, "## Goal\n\n%s\n\n", r.Goal)
	fmt.Fprintf(&b, "## Acceptance evidence\n\n")
	for _, check := range r.AcceptanceChecks {
		fmt.Fprintf(&b, "- [%s] **%s** — %s", check.Status, check.ID, check.Description)
		if check.Evidence != "" {
			fmt.Fprintf(&b, " — Evidence: %s", check.Evidence)
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "\n## Timeline references\n\n")
	for _, ref := range r.TimelineRefs {
		fmt.Fprintf(&b, "- #%d `%s` — %s/%s", ref.Sequence, ref.EventID, ref.Domain, ref.Kind)
		if ref.SourceRef != "" {
			fmt.Fprintf(&b, " — `%s`", ref.SourceRef)
		}
		if ref.Summary != "" {
			fmt.Fprintf(&b, " — %s", ref.Summary)
		}
		b.WriteString("\n")
	}
	if len(r.Artifacts) > 0 {
		fmt.Fprintf(&b, "\n## Artifacts\n\n")
		for _, artifact := range r.Artifacts {
			fmt.Fprintf(&b, "- **%s** — `%s` — SHA-256 `%s` — %d bytes\n", artifact.Name, artifact.Path, artifact.SHA256, artifact.Size)
		}
	}
	fmt.Fprintf(&b, "\nInput digest: `%s`\n", r.InputDigest)
	return b.String()
}

func redactState(state tasks.State, sensitiveValues []string) tasks.State {
	state.Contract.Goal = redactKnown(state.Contract.Goal, sensitiveValues)
	for i := range state.Contract.RequiredOutcomes {
		state.Contract.RequiredOutcomes[i] = redactKnown(state.Contract.RequiredOutcomes[i], sensitiveValues)
	}
	for i := range state.Contract.ForbiddenScope {
		state.Contract.ForbiddenScope[i] = redactKnown(state.Contract.ForbiddenScope[i], sensitiveValues)
	}
	for i := range state.Contract.Checks {
		state.Contract.Checks[i].Description = redactKnown(state.Contract.Checks[i].Description, sensitiveValues)
		state.Contract.Checks[i].Evidence = redactKnown(state.Contract.Checks[i].Evidence, sensitiveValues)
	}
	return state
}

func redactEvents(events []timeline.Event, sensitiveValues []string) []timeline.Event {
	out := append([]timeline.Event(nil), events...)
	for i := range out {
		out[i].SourceRef = redactKnown(out[i].SourceRef, sensitiveValues)
		out[i].Summary = redactKnown(out[i].Summary, sensitiveValues)
	}
	return out
}

func redactArtifacts(artifacts []Artifact, sensitiveValues []string) []Artifact {
	out := append([]Artifact(nil), artifacts...)
	for i := range out {
		out[i].Name = redactKnown(out[i].Name, sensitiveValues)
		out[i].Path = redactKnown(out[i].Path, sensitiveValues)
	}
	return out
}

func redactKnown(text string, sensitiveValues []string) string {
	return secretbroker.RedactText(text, sensitiveValues)
}

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(api[_-]?key|password|access[_-]?token|secret)\s*[:=]\s*["']?[A-Za-z0-9_./+\-=]{8,}`),
	regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{8,}\b`),
	regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{20,}\b`),
	regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{10,}\b`),
	regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{20,}\b`),
	regexp.MustCompile(`-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----`),
}

func rejectSecrets(value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	text := string(b)
	for _, pattern := range secretPatterns {
		if pattern.MatchString(text) {
			return ErrSecretDetected
		}
	}
	return nil
}
