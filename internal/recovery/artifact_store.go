package recovery

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

var ErrArtifactConflict = errors.New("artifact id already exists with different content")

type ArtifactStore struct {
	mu      sync.Mutex
	path    string
	records []ArtifactRecord
	byID    map[string]ArtifactRecord
}

func OpenArtifactStore(path string) (*ArtifactStore, error) {
	if path == "" {
		return nil, errors.New("artifact ledger path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	s := &ArtifactStore{path: path, byID: map[string]ArtifactRecord{}}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *ArtifactStore) SaveOnce(record ArtifactRecord) (ArtifactRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if record.ArtifactID == "" || record.TaskID == "" || record.SessionID == "" || record.Name == "" || record.Path == "" || record.SHA256 == "" || record.Size < 0 {
		return ArtifactRecord{}, false, errors.New("artifact record is incomplete")
	}
	if existing, ok := s.byID[record.ArtifactID]; ok {
		if artifactFingerprint(existing) != artifactFingerprint(record) {
			return ArtifactRecord{}, false, ErrArtifactConflict
		}
		return existing, false, nil
	}
	if err := appendArtifactJSONLine(s.path, record); err != nil {
		return ArtifactRecord{}, false, err
	}
	s.records = append(s.records, record)
	s.byID[record.ArtifactID] = record
	return record, true, nil
}

func (s *ArtifactStore) ForTask(taskID, sessionID string) []ArtifactRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []ArtifactRecord{}
	for _, record := range s.records {
		if record.TaskID == taskID && record.SessionID == sessionID {
			out = append(out, record)
		}
	}
	return out
}

func (s *ArtifactStore) load() error {
	f, err := os.Open(s.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for scanner.Scan() {
		var record ArtifactRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return fmt.Errorf("decode artifact ledger: %w", err)
		}
		if existing, exists := s.byID[record.ArtifactID]; exists && artifactFingerprint(existing) != artifactFingerprint(record) {
			return ErrArtifactConflict
		}
		if _, exists := s.byID[record.ArtifactID]; exists {
			return fmt.Errorf("duplicate artifact id %q", record.ArtifactID)
		}
		s.records = append(s.records, record)
		s.byID[record.ArtifactID] = record
	}
	return scanner.Err()
}

func artifactFingerprint(record ArtifactRecord) string {
	b, _ := json.Marshal(record)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func appendArtifactJSONLine(path string, value any) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	if err := enc.Encode(value); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
