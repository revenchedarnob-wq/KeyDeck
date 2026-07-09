package conformance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type FragmentStore struct {
	Path string
}

func (s FragmentStore) Save(fragment ProviderObservationFragment, now time.Time) error {
	if s.Path == "" {
		return fmt.Errorf("%w: fragment store path is required", ErrInvalidCaptureFragment)
	}
	if err := fragment.Validate(now); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(fragment, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.Path), ".provider-fragment-*.tmp")
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

func (s FragmentStore) Load(now time.Time) (ProviderObservationFragment, error) {
	data, err := os.ReadFile(s.Path)
	if err != nil {
		return ProviderObservationFragment{}, err
	}
	var fragment ProviderObservationFragment
	if err := json.Unmarshal(data, &fragment); err != nil {
		return ProviderObservationFragment{}, fmt.Errorf("decode provider fragment: %w", err)
	}
	if err := fragment.Validate(now); err != nil {
		return ProviderObservationFragment{}, err
	}
	return fragment, nil
}
