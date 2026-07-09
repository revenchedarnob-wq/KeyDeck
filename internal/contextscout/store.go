package contextscout

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"keydeck.local/feasibilitylab/internal/contextcompiler"
	"keydeck.local/feasibilitylab/internal/proofreceipt"
)

type Store struct {
	mu        sync.Mutex
	path      string
	artifacts string
	records   []Record
	byPacket  map[string]Record
}

func OpenStore(path string) (*Store, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("context packet store path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	artifacts := strings.TrimSuffix(path, filepath.Ext(path)) + "-artifacts"
	if err := os.MkdirAll(artifacts, 0o700); err != nil {
		return nil, err
	}
	s := &Store{path: path, artifacts: artifacts, byPacket: map[string]Record{}}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Path() string { return s.path }
func (s *Store) Count() int   { s.mu.Lock(); defer s.mu.Unlock(); return len(s.records) }
func (s *Store) NextSequence() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return uint64(len(s.records) + 1)
}

func (s *Store) Save(in SaveInput) (Record, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateSaveInput(in); err != nil {
		return Record{}, false, err
	}
	canonicalPacket, err := json.Marshal(in.Packet)
	if err != nil {
		return Record{}, false, err
	}
	packetSHA := digest(canonicalPacket)
	packetID := DerivePacketID(in.CacheKey, in.ProjectFingerprint, packetSHA)
	if existing, ok := s.byPacket[packetID]; ok {
		if existing.CacheKey != in.CacheKey || existing.ProjectFingerprint != in.ProjectFingerprint || existing.PacketSHA256 != packetSHA {
			return Record{}, false, ErrStoreState
		}
		if _, _, _, err := s.verifyRecordArtifactsLocked(existing); err != nil {
			return Record{}, false, err
		}
		return existing, false, nil
	}
	packetJSON, err := json.MarshalIndent(in.Packet, "", "  ")
	if err != nil {
		return Record{}, false, err
	}
	packetJSON = append(packetJSON, '\n')
	rendered := []byte(in.Packet.Render())
	providerJSON, err := json.MarshalIndent(in.ProviderEvidence, "", "  ")
	if err != nil {
		return Record{}, false, err
	}
	providerJSON = append(providerJSON, '\n')
	jsonName, textName, providerName := packetID+".json", packetID+".txt", packetID+".provider.json"
	if err := atomicWrite(filepath.Join(s.artifacts, jsonName), packetJSON, 0o600); err != nil {
		return Record{}, false, err
	}
	if err := atomicWrite(filepath.Join(s.artifacts, textName), rendered, 0o600); err != nil {
		return Record{}, false, err
	}
	if err := atomicWrite(filepath.Join(s.artifacts, providerName), providerJSON, 0o600); err != nil {
		return Record{}, false, err
	}
	previous := ""
	if len(s.records) > 0 {
		previous = s.records[len(s.records)-1].RecordSHA256
	}
	record := Record{
		Sequence: uint64(len(s.records) + 1), CreatedAt: time.Now().UTC(), PacketID: packetID, CacheKey: in.CacheKey,
		ProjectFingerprint: in.ProjectFingerprint, PacketSHA256: packetSHA, ProviderServerID: in.ProviderServerID,
		ProviderSchemaSHA256: in.ProviderSchemaSHA256, ProjectRoot: filepath.Clean(in.ProjectRoot), Objective: in.Objective,
		MaxChars: in.MaxChars, MaxFiles: in.MaxFiles, PacketJSONPath: jsonName, PacketJSONSHA256: digest(packetJSON),
		RenderedTextPath: textName, RenderedTextSHA256: digest(rendered), ProviderEvidencePath: providerName,
		ProviderEvidenceSHA256: digest(providerJSON), PreviousRecordSHA256: previous,
	}
	record.RecordSHA256 = recordDigest(record)
	if err := appendJSONLine(s.path, record); err != nil {
		return Record{}, false, err
	}
	s.records = append(s.records, record)
	s.byPacket[record.PacketID] = record
	return record, true, nil
}

func (s *Store) FindFresh(cacheKey, projectFingerprint string) (contextcompiler.Packet, Record, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.records) - 1; i >= 0; i-- {
		record := s.records[i]
		if record.CacheKey == cacheKey && record.ProjectFingerprint == projectFingerprint {
			return s.verifyRecordArtifactsLocked(record)
		}
	}
	return contextcompiler.Packet{}, Record{}, false, nil
}

func (s *Store) ReceiptArtifacts(record Record) ([]proofreceipt.Artifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.byPacket[record.PacketID]; !ok {
		return nil, ErrStoreState
	}
	if _, _, ok, err := s.verifyRecordArtifactsLocked(record); err != nil || !ok {
		return nil, err
	}
	storeHash, storeSize, err := fileHashSize(s.path)
	if err != nil {
		return nil, err
	}
	paths := []struct{ name, rel, hash string }{
		{"context packet JSON", record.PacketJSONPath, record.PacketJSONSHA256},
		{"context packet rendered text", record.RenderedTextPath, record.RenderedTextSHA256},
		{"context provider identity evidence", record.ProviderEvidencePath, record.ProviderEvidenceSHA256},
	}
	artifacts := []proofreceipt.Artifact{{Name: "context packet store", Path: s.path, SHA256: storeHash, Size: storeSize}}
	for _, item := range paths {
		full := filepath.Join(s.artifacts, item.rel)
		_, size, err := fileHashSize(full)
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, proofreceipt.Artifact{Name: item.name, Path: full, SHA256: item.hash, Size: size})
	}
	return artifacts, nil
}

func (s *Store) load() error {
	f, err := os.Open(s.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64<<10), 16<<20)
	expected, previous := uint64(1), ""
	for scanner.Scan() {
		var record Record
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return fmt.Errorf("decode context packet record: %w", err)
		}
		if record.Sequence != expected || record.PreviousRecordSHA256 != previous || record.RecordSHA256 != recordDigest(record) {
			return fmt.Errorf("%w: record sequence/hash chain at %d", ErrStoreState, expected)
		}
		if err := validateRecord(record); err != nil {
			return fmt.Errorf("%w: record %d: %v", ErrStoreState, expected, err)
		}
		if _, exists := s.byPacket[record.PacketID]; exists {
			return fmt.Errorf("%w: duplicate packet id %q", ErrStoreState, record.PacketID)
		}
		s.records = append(s.records, record)
		s.byPacket[record.PacketID] = record
		previous = record.RecordSHA256
		expected++
	}
	return scanner.Err()
}

func (s *Store) verifyRecordArtifactsLocked(record Record) (contextcompiler.Packet, Record, bool, error) {
	if err := validateRecord(record); err != nil {
		return contextcompiler.Packet{}, Record{}, false, err
	}
	jsonBytes, err := os.ReadFile(filepath.Join(s.artifacts, record.PacketJSONPath))
	if err != nil {
		return contextcompiler.Packet{}, Record{}, false, fmt.Errorf("%w: packet JSON unavailable: %v", ErrArtifactTampered, err)
	}
	rendered, err := os.ReadFile(filepath.Join(s.artifacts, record.RenderedTextPath))
	if err != nil {
		return contextcompiler.Packet{}, Record{}, false, fmt.Errorf("%w: rendered packet unavailable: %v", ErrArtifactTampered, err)
	}
	provider, err := os.ReadFile(filepath.Join(s.artifacts, record.ProviderEvidencePath))
	if err != nil {
		return contextcompiler.Packet{}, Record{}, false, fmt.Errorf("%w: provider evidence unavailable: %v", ErrArtifactTampered, err)
	}
	if digest(jsonBytes) != record.PacketJSONSHA256 || digest(rendered) != record.RenderedTextSHA256 || digest(provider) != record.ProviderEvidenceSHA256 {
		return contextcompiler.Packet{}, Record{}, false, ErrArtifactTampered
	}
	var packet contextcompiler.Packet
	if err := json.Unmarshal(jsonBytes, &packet); err != nil {
		return contextcompiler.Packet{}, Record{}, false, fmt.Errorf("%w: decode packet JSON: %v", ErrArtifactTampered, err)
	}
	canonical, _ := json.Marshal(packet)
	if digest(canonical) != record.PacketSHA256 || DerivePacketID(record.CacheKey, record.ProjectFingerprint, record.PacketSHA256) != record.PacketID {
		return contextcompiler.Packet{}, Record{}, false, ErrArtifactTampered
	}
	if string(rendered) != packet.Render() {
		return contextcompiler.Packet{}, Record{}, false, ErrArtifactTampered
	}
	var evidence ProviderEvidence
	if err := json.Unmarshal(provider, &evidence); err != nil {
		return contextcompiler.Packet{}, Record{}, false, ErrArtifactTampered
	}
	if evidence.ProviderServerID != record.ProviderServerID || evidence.ProviderSchemaSHA256 != record.ProviderSchemaSHA256 || evidence.CacheKey != record.CacheKey {
		return contextcompiler.Packet{}, Record{}, false, ErrArtifactTampered
	}
	return packet, record, true, nil
}

func validateSaveInput(in SaveInput) error {
	if in.CacheKey == "" || in.ProjectFingerprint == "" || in.ProviderServerID == "" || in.ProviderSchemaSHA256 == "" || in.ProjectRoot == "" || strings.TrimSpace(in.Objective) == "" || in.MaxChars <= 0 || in.MaxFiles <= 0 {
		return errors.New("context packet save input is incomplete")
	}
	if in.ProviderEvidence.ProviderServerID != in.ProviderServerID || in.ProviderEvidence.ProviderSchemaSHA256 != in.ProviderSchemaSHA256 || in.ProviderEvidence.CacheKey != in.CacheKey {
		return ErrProviderMismatch
	}
	return nil
}

func validateRecord(record Record) error {
	if record.Sequence == 0 || record.PacketID == "" || record.CacheKey == "" || record.ProjectFingerprint == "" || record.PacketSHA256 == "" || record.ProviderServerID == "" || record.ProviderSchemaSHA256 == "" || record.ProjectRoot == "" || strings.TrimSpace(record.Objective) == "" || record.MaxChars <= 0 || record.MaxFiles <= 0 || record.PacketJSONPath == "" || record.RenderedTextPath == "" || record.ProviderEvidencePath == "" || record.RecordSHA256 == "" {
		return errors.New("record fields are incomplete")
	}
	cacheKey := CacheKeyInput{ProviderServerID: record.ProviderServerID, ProviderSchemaSHA256: record.ProviderSchemaSHA256, ProjectRoot: record.ProjectRoot, Objective: record.Objective, MaxChars: record.MaxChars, MaxFiles: record.MaxFiles}.Hash()
	if cacheKey != record.CacheKey || DerivePacketID(record.CacheKey, record.ProjectFingerprint, record.PacketSHA256) != record.PacketID {
		return errors.New("record identity fields are invalid")
	}
	for _, rel := range []string{record.PacketJSONPath, record.RenderedTextPath, record.ProviderEvidencePath} {
		if filepath.IsAbs(rel) || strings.HasPrefix(filepath.Clean(rel), "..") {
			return errors.New("artifact path escapes context store")
		}
	}
	return nil
}

func recordDigest(record Record) string {
	type canonical Record
	copy := record
	copy.RecordSHA256 = ""
	raw, _ := json.Marshal(canonical(copy))
	return digest(raw)
}
func DerivePacketID(cacheKey, fingerprint, packetSHA string) string {
	raw, _ := json.Marshal(struct {
		CacheKey           string `json:"cache_key"`
		ProjectFingerprint string `json:"project_fingerprint"`
		PacketSHA256       string `json:"packet_sha256"`
	}{cacheKey, fingerprint, packetSHA})
	return "packet-" + digest(raw)[:20]
}
func atomicWrite(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	f, err := os.OpenFile(tmp, os.O_RDWR, mode)
	if err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
func appendJSONLine(path string, value any) error {
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
func fileHashSize(path string) (string, int64, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", 0, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", 0, err
	}
	return digest(b), info.Size(), nil
}
