package corehost

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type LeaseRecord struct {
	Version     int       `json:"version"`
	InstanceID  string    `json:"instance_id"`
	PID         int       `json:"pid"`
	HeartbeatAt time.Time `json:"heartbeat_at"`
}

type Lease struct {
	mu          sync.Mutex
	dir         string
	ownerPath   string
	instanceID  string
	pid         int
	now         func() time.Time
	released    bool
	initialized bool
}

func AcquireLease(dir, instanceID string, pid int, now func() time.Time, staleAfter time.Duration) (*Lease, error) {
	dir = filepath.Clean(strings.TrimSpace(dir))
	instanceID = strings.TrimSpace(instanceID)
	if dir == "." || dir == "" || instanceID == "" || staleAfter <= 0 {
		return nil, ErrInvalidConfig
	}
	if now == nil {
		now = time.Now
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		return nil, err
	}
	for attempts := 0; attempts < 4; attempts++ {
		err := os.Mkdir(dir, 0o700)
		if err == nil {
			l := &Lease{dir: dir, ownerPath: filepath.Join(dir, "owner.json"), instanceID: instanceID, pid: pid, now: now}
			if err := l.Refresh(); err != nil {
				_ = os.RemoveAll(dir)
				return nil, err
			}
			return l, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		owner, readErr := readLeaseRecord(filepath.Join(dir, "owner.json"))
		if readErr == nil && now().UTC().Sub(owner.HeartbeatAt) <= staleAfter {
			return nil, ErrAlreadyRunning
		}
		if readErr != nil {
			info, statErr := os.Stat(dir)
			if statErr != nil {
				continue
			}
			if now().UTC().Sub(info.ModTime()) <= staleAfter {
				return nil, ErrAlreadyRunning
			}
		}
		tombstone := dir + ".stale-" + instanceID
		_ = os.RemoveAll(tombstone)
		if err := os.Rename(dir, tombstone); err != nil {
			continue
		}
		_ = os.RemoveAll(tombstone)
	}
	return nil, ErrAlreadyRunning
}

func (l *Lease) Refresh() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.released {
		return errors.New("lease released")
	}
	if l.initialized {
		owner, err := readLeaseRecord(l.ownerPath)
		if err != nil {
			return fmt.Errorf("lease ownership cannot be verified: %w", err)
		}
		if owner.InstanceID != l.instanceID {
			return errors.New("lease ownership changed")
		}
	}
	if err := atomicWriteJSON(l.ownerPath, LeaseRecord{Version: 1, InstanceID: l.instanceID, PID: l.pid, HeartbeatAt: l.now().UTC()}, 0o600); err != nil {
		return err
	}
	l.initialized = true
	return nil
}

func (l *Lease) Release() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.released {
		return nil
	}
	owner, err := readLeaseRecord(l.ownerPath)
	if err != nil {
		return fmt.Errorf("lease ownership cannot be verified: %w", err)
	}
	if owner.InstanceID != l.instanceID {
		return errors.New("lease ownership changed")
	}
	l.released = true
	return os.RemoveAll(l.dir)
}

func readLeaseRecord(path string) (LeaseRecord, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return LeaseRecord{}, err
	}
	var r LeaseRecord
	if err := json.Unmarshal(raw, &r); err != nil {
		return LeaseRecord{}, err
	}
	if r.Version != 1 || r.InstanceID == "" || r.HeartbeatAt.IsZero() {
		return LeaseRecord{}, errors.New("invalid lease record")
	}
	return r, nil
}
