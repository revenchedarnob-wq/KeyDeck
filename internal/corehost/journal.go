package corehost

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

type RequestRecord struct {
	Sequence       uint64          `json:"sequence"`
	Key            string          `json:"key"`
	RequestSHA256  string          `json:"request_sha256"`
	ResponseStatus int             `json:"response_status"`
	Response       json.RawMessage `json:"response"`
	PreviousSHA256 string          `json:"previous_sha256,omitempty"`
	RecordSHA256   string          `json:"record_sha256"`
}

type RequestJournal struct {
	mu      sync.Mutex
	path    string
	records []RequestRecord
	byKey   map[string]RequestRecord
}

func OpenRequestJournal(path string) (*RequestJournal, error) {
	if path == "" {
		return nil, errors.New("request journal path required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	j := &RequestJournal{path: path, byKey: map[string]RequestRecord{}}
	if err := j.load(); err != nil {
		return nil, err
	}
	return j, nil
}

func (j *RequestJournal) Count() int {
	j.mu.Lock()
	defer j.mu.Unlock()
	return len(j.records)
}

func (j *RequestJournal) Lookup(key string) (RequestRecord, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	r, ok := j.byKey[key]
	return r, ok
}

func (j *RequestJournal) Append(key, requestSHA string, status int, response any) (RequestRecord, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if key == "" || requestSHA == "" || status < 100 || status > 599 {
		return RequestRecord{}, errors.New("invalid request record")
	}
	if existing, ok := j.byKey[key]; ok {
		if existing.RequestSHA256 != requestSHA {
			return RequestRecord{}, ErrIdempotencyConflict
		}
		return existing, nil
	}
	raw, err := json.Marshal(response)
	if err != nil {
		return RequestRecord{}, err
	}
	r := RequestRecord{Sequence: uint64(len(j.records) + 1), Key: key, RequestSHA256: requestSHA, ResponseStatus: status, Response: raw}
	if len(j.records) > 0 {
		r.PreviousSHA256 = j.records[len(j.records)-1].RecordSHA256
	}
	r.RecordSHA256 = requestRecordDigest(r)
	if err := appendJSONLineSync(j.path, r); err != nil {
		return RequestRecord{}, err
	}
	j.records = append(j.records, r)
	j.byKey[key] = r
	return r, nil
}

func (j *RequestJournal) load() error {
	f, err := os.Open(j.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
	var seq uint64 = 1
	prev := ""
	for sc.Scan() {
		var r RequestRecord
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			return fmt.Errorf("decode request journal: %w", err)
		}
		if r.Sequence != seq || r.PreviousSHA256 != prev || r.RecordSHA256 != requestRecordDigest(r) || r.Key == "" || r.RequestSHA256 == "" {
			return errors.New("request journal tampered")
		}
		if _, exists := j.byKey[r.Key]; exists {
			return errors.New("duplicate idempotency key in request journal")
		}
		j.records = append(j.records, r)
		j.byKey[r.Key] = r
		prev = r.RecordSHA256
		seq++
	}
	return sc.Err()
}

func requestRecordDigest(r RequestRecord) string {
	r.RecordSHA256 = ""
	raw, _ := json.Marshal(r)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func appendJSONLineSync(path string, value any) error {
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

func sha256Hex(raw []byte) string {
	s := sha256.Sum256(raw)
	return hex.EncodeToString(s[:])
}
