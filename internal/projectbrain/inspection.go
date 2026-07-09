package projectbrain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"

	"keydeck.local/feasibilitylab/internal/contextcompiler"
	"keydeck.local/feasibilitylab/internal/contextscout"
)

func BuildInspection(packet contextcompiler.Packet, record contextscout.Record, currentFingerprint string) (ContextInspection, error) {
	if record.PacketID == "" || record.PacketSHA256 == "" || currentFingerprint == "" {
		return ContextInspection{}, errors.New("packet, record and current fingerprint are required")
	}
	canonical, err := json.Marshal(packet)
	if err != nil {
		return ContextInspection{}, err
	}
	sum := sha256.Sum256(canonical)
	packetSHA := hex.EncodeToString(sum[:])
	if packetSHA != record.PacketSHA256 || record.ProjectFingerprint != currentFingerprint {
		return ContextInspection{}, ErrStaleContext
	}
	evidence := make([]InspectionEvidence, 0, len(packet.StructuralEvidence)+len(packet.SourceSnippets))
	for _, e := range packet.StructuralEvidence {
		raw, _ := json.Marshal(e)
		h := sha256.Sum256(raw)
		evidence = append(evidence, InspectionEvidence{Kind: "structural:" + strings.TrimSpace(e.Tool), Reference: strings.TrimSpace(e.Arguments), SHA256: hex.EncodeToString(h[:]), Successful: e.Successful, Truncated: e.Truncated})
	}
	root := filepath.Clean(packet.ProjectRoot)
	for _, s := range packet.SourceSnippets {
		rel := filepath.Clean(filepath.FromSlash(s.Path))
		full := filepath.Join(root, rel)
		check, err := filepath.Rel(root, full)
		if err != nil || check == ".." || strings.HasPrefix(check, ".."+string(filepath.Separator)) {
			return ContextInspection{}, ErrInvalidState
		}
		raw, _ := json.Marshal(s)
		h := sha256.Sum256(raw)
		evidence = append(evidence, InspectionEvidence{Kind: "source", Reference: filepath.ToSlash(rel), SHA256: hex.EncodeToString(h[:]), Successful: true})
	}
	out := ContextInspection{PacketID: record.PacketID, PacketSHA256: record.PacketSHA256, ProjectFingerprint: currentFingerprint, IncludedEvidence: evidence, OmittedEvidenceCount: packet.OmittedEvidenceCount}
	out.InspectionSHA256 = inspectionDigest(out)
	return out, nil
}

func ValidateRevisionContext(rev Revision, packet contextcompiler.Packet, record contextscout.Record, currentFingerprint string) error {
	if rev.ProjectFingerprint != currentFingerprint || rev.Context.ProjectFingerprint != currentFingerprint {
		return ErrStaleContext
	}
	expected, err := BuildInspection(packet, record, currentFingerprint)
	if err != nil {
		return err
	}
	if rev.Context.InspectionSHA256 != expected.InspectionSHA256 || rev.Context.PacketID != expected.PacketID || rev.Context.PacketSHA256 != expected.PacketSHA256 {
		return ErrTampered
	}
	return nil
}
