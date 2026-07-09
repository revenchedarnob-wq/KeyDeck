package conformance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type PairedScopeEvidenceStore struct {
	Path string
}

func (s PairedScopeEvidenceStore) Save(evidence PairedScopeEvidence, now time.Time) error {
	if s.Path == "" {
		return fmt.Errorf("%w: paired scope store path is required", ErrInvalidPairedScopeEvidence)
	}
	if err := evidence.Validate(now); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.Path), ".paired-scope-evidence-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, s.Path)
}

func (s PairedScopeEvidenceStore) Load(now time.Time) (PairedScopeEvidence, error) {
	data, err := os.ReadFile(s.Path)
	if err != nil {
		return PairedScopeEvidence{}, err
	}
	var evidence PairedScopeEvidence
	if err := json.Unmarshal(data, &evidence); err != nil {
		return PairedScopeEvidence{}, fmt.Errorf("decode paired scope evidence: %w", err)
	}
	if err := evidence.Validate(now); err != nil {
		return PairedScopeEvidence{}, err
	}
	return evidence, nil
}
