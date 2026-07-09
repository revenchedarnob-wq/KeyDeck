package conformance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type EvidenceStore struct {
	Path string
}

func (s EvidenceStore) Save(bundle ProviderEvidenceBundle, now time.Time) error {
	if s.Path == "" {
		return fmt.Errorf("%w: evidence store path is required", ErrInvalidEvidence)
	}
	if err := bundle.Validate(now); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.Path), ".provider-evidence-*.tmp")
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
	if err := os.Rename(tmpName, s.Path); err != nil {
		return err
	}
	return nil
}

func (s EvidenceStore) Load(now time.Time) (ProviderEvidenceBundle, error) {
	data, err := os.ReadFile(s.Path)
	if err != nil {
		return ProviderEvidenceBundle{}, err
	}
	var bundle ProviderEvidenceBundle
	if err := json.Unmarshal(data, &bundle); err != nil {
		return ProviderEvidenceBundle{}, fmt.Errorf("decode provider evidence: %w", err)
	}
	if err := bundle.Validate(now); err != nil {
		return ProviderEvidenceBundle{}, err
	}
	return bundle, nil
}
