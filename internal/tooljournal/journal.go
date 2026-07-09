package tooljournal

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
	"time"
)

type ReplayPolicy string

const (
	ReplayForbidden  ReplayPolicy = "forbidden"
	ReplayIdempotent ReplayPolicy = "idempotent"
)

type State string

const (
	StateStarted   State = "started"
	StateCompleted State = "completed"
	StateFailed    State = "failed"
)

var (
	ErrAmbiguousOperation = errors.New("tool operation outcome is ambiguous; replay is forbidden")
	ErrOperationCollision = errors.New("operation id reused with different tool or arguments")
)

type Record struct {
	At        time.Time    `json:"at"`
	Operation string       `json:"operation_id"`
	Tool      string       `json:"tool"`
	ArgsHash  string       `json:"args_hash"`
	Policy    ReplayPolicy `json:"replay_policy"`
	State     State        `json:"state"`
	Result    string       `json:"result,omitempty"`
	Error     string       `json:"error,omitempty"`
}

type DecisionKind string

const (
	DecisionExecute        DecisionKind = "execute"
	DecisionReturnPrevious DecisionKind = "return_previous"
)

type Decision struct {
	Kind   DecisionKind `json:"kind"`
	Result string       `json:"result,omitempty"`
}

type Journal struct {
	mu     sync.Mutex
	path   string
	latest map[string]Record
}

func Open(path string) (*Journal, error) {
	j := &Journal{path: path, latest: map[string]Record{}}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return j, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64<<10), 2<<20)
	for scanner.Scan() {
		var rec Record
		if err := json.Unmarshal(scanner.Bytes(), &rec); err != nil {
			return nil, fmt.Errorf("decode tool journal: %w", err)
		}
		j.latest[rec.Operation] = rec
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return j, nil
}

func (j *Journal) Begin(operationID, tool string, args []byte, policy ReplayPolicy) (Decision, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	hash := digest(args)
	if prev, ok := j.latest[operationID]; ok {
		if prev.Tool != tool || prev.ArgsHash != hash {
			return Decision{}, ErrOperationCollision
		}
		switch prev.State {
		case StateCompleted:
			return Decision{Kind: DecisionReturnPrevious, Result: prev.Result}, nil
		case StateStarted:
			if prev.Policy == ReplayForbidden || policy == ReplayForbidden {
				return Decision{}, ErrAmbiguousOperation
			}
			// Idempotent operations may be executed again using the same operation ID.
		case StateFailed:
			if prev.Policy == ReplayForbidden || policy == ReplayForbidden {
				return Decision{}, ErrAmbiguousOperation
			}
		}
	}
	rec := Record{At: time.Now().UTC(), Operation: operationID, Tool: tool, ArgsHash: hash, Policy: policy, State: StateStarted}
	if err := j.appendLocked(rec); err != nil {
		return Decision{}, err
	}
	return Decision{Kind: DecisionExecute}, nil
}

func (j *Journal) Complete(operationID, result string) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	prev, ok := j.latest[operationID]
	if !ok {
		return fmt.Errorf("unknown operation %q", operationID)
	}
	prev.At = time.Now().UTC()
	prev.State = StateCompleted
	prev.Result = result
	prev.Error = ""
	return j.appendLocked(prev)
}

func (j *Journal) Fail(operationID, message string) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	prev, ok := j.latest[operationID]
	if !ok {
		return fmt.Errorf("unknown operation %q", operationID)
	}
	prev.At = time.Now().UTC()
	prev.State = StateFailed
	prev.Error = message
	return j.appendLocked(prev)
}

func (j *Journal) Snapshot() map[string]Record {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make(map[string]Record, len(j.latest))
	for k, v := range j.latest {
		out[k] = v
	}
	return out
}

func (j *Journal) appendLocked(rec Record) error {
	f, err := os.OpenFile(j.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	if err := enc.Encode(rec); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	j.latest[rec.Operation] = rec
	return nil
}

func digest(args []byte) string {
	sum := sha256.Sum256(args)
	return hex.EncodeToString(sum[:])
}
