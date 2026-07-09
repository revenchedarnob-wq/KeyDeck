package contextscout

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"keydeck.local/feasibilitylab/internal/contextcompiler"
)

func ValidateHygiene(packet contextcompiler.Packet, projectRoot string, maxChars, maxFiles int, forbiddenExactValues []string) (HygieneReport, error) {
	if maxChars <= 0 || maxFiles <= 0 {
		return HygieneReport{}, fmt.Errorf("%w: positive character and file budgets are required", ErrHygiene)
	}
	rendered := packet.Render()
	report := HygieneReport{RenderedChars: len(rendered), SourceSnippetCount: len(packet.SourceSnippets), StructuralReceipts: len(packet.StructuralEvidence), OmittedEvidenceCount: packet.OmittedEvidenceCount}
	if len(rendered) > maxChars || packet.RenderedChars != len(rendered) {
		return report, fmt.Errorf("%w: rendered context is outside the exact character budget", ErrHygiene)
	}
	if len(packet.SourceSnippets) > maxFiles {
		return report, fmt.Errorf("%w: source snippet count exceeds the file budget", ErrHygiene)
	}
	if strings.TrimSpace(packet.StructuralProvider) == "" || packet.StructuralProvider == "none" || len(packet.StructuralEvidence) == 0 {
		return report, fmt.Errorf("%w: structural provider receipts are missing", ErrHygiene)
	}
	for _, receipt := range packet.StructuralEvidence {
		if strings.TrimSpace(receipt.Tool) == "" || strings.TrimSpace(receipt.Arguments) == "" {
			return report, fmt.Errorf("%w: structural receipt lost tool or argument evidence", ErrHygiene)
		}
	}
	root, err := filepath.Abs(projectRoot)
	if err != nil {
		return report, err
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		realRoot = root
	}
	seen := map[string]bool{}
	for _, snippet := range packet.SourceSnippets {
		keyRaw, _ := json.Marshal(snippet)
		key := digest(keyRaw)
		if seen[key] {
			return report, fmt.Errorf("%w: duplicate source snippet", ErrHygiene)
		}
		seen[key] = true
		full := filepath.Join(root, filepath.Clean(filepath.FromSlash(snippet.Path)))
		realFull, err := filepath.EvalSymlinks(full)
		if err != nil {
			return report, fmt.Errorf("%w: snippet path cannot be resolved: %v", ErrHygiene, err)
		}
		rel, err := filepath.Rel(realRoot, realFull)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return report, fmt.Errorf("%w: snippet path escapes project root", ErrHygiene)
		}
	}
	packetJSON, err := json.Marshal(packet)
	if err != nil {
		return report, err
	}
	for _, value := range forbiddenExactValues {
		if value != "" && (strings.Contains(string(packetJSON), value) || strings.Contains(rendered, value)) {
			return report, fmt.Errorf("%w: forbidden exact value detected", ErrHygiene)
		}
	}
	return report, nil
}
