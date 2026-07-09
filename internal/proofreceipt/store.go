package proofreceipt

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

var ErrReceiptConflict = errors.New("proof receipt id already exists with different input digest")

type Store struct {
	mu       sync.Mutex
	path     string
	receipts []Receipt
	byID     map[string]Receipt
}

func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("proof receipt store path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	store := &Store{path: path, byID: map[string]Receipt{}}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) SaveOnce(receipt Receipt) (Receipt, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if receipt.ReceiptID == "" || receipt.InputDigest == "" || receipt.TaskID == "" {
		return Receipt{}, false, errors.New("receipt id, input digest and task id are required")
	}
	if existing, ok := s.byID[receipt.ReceiptID]; ok {
		if existing.InputDigest != receipt.InputDigest {
			return Receipt{}, false, ErrReceiptConflict
		}
		return existing, false, nil
	}
	if err := appendJSONLine(s.path, receipt); err != nil {
		return Receipt{}, false, err
	}
	s.receipts = append(s.receipts, receipt)
	s.byID[receipt.ReceiptID] = receipt
	return receipt, true, nil
}

func (s *Store) Snapshot() []Receipt {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Receipt, len(s.receipts))
	copy(out, s.receipts)
	return out
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
	scanner.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for scanner.Scan() {
		var receipt Receipt
		if err := json.Unmarshal(scanner.Bytes(), &receipt); err != nil {
			return fmt.Errorf("decode proof receipt: %w", err)
		}
		if receipt.ReceiptID == "" || receipt.InputDigest == "" {
			return errors.New("stored proof receipt is missing identity")
		}
		if _, exists := s.byID[receipt.ReceiptID]; exists {
			return fmt.Errorf("duplicate proof receipt id %q", receipt.ReceiptID)
		}
		s.receipts = append(s.receipts, receipt)
		s.byID[receipt.ReceiptID] = receipt
	}
	return scanner.Err()
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
