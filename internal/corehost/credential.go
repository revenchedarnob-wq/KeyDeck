package corehost

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func BuildLayout(dataDir string) (Layout, error) {
	dataDir = filepath.Clean(strings.TrimSpace(dataDir))
	if dataDir == "." || dataDir == "" {
		return Layout{}, ErrInvalidConfig
	}
	stateDir := filepath.Join(dataDir, "state")
	return Layout{
		DataDir:        dataDir,
		CredentialPath: filepath.Join(dataDir, "install-credential.json"),
		RuntimePath:    filepath.Join(dataDir, "runtime.json"),
		LeaseDir:       filepath.Join(dataDir, "core.lock"),
		TaskDir:        filepath.Join(stateDir, "tasks"),
		TimelinePath:   filepath.Join(stateDir, "timeline.jsonl"),
		RequestJournal: filepath.Join(stateDir, "api-requests.jsonl"),
	}, nil
}

func LoadOrCreateCredential(path string, random io.Reader) (Credential, bool, error) {
	if random == nil {
		random = rand.Reader
	}
	raw, err := os.ReadFile(path)
	if err == nil {
		var c Credential
		if err := json.Unmarshal(raw, &c); err != nil {
			return Credential{}, false, err
		}
		if c.Version != 1 || len(c.InstallID) != 32 || len(c.Token) != 64 {
			return Credential{}, false, errors.New("invalid install credential record")
		}
		if _, err := hex.DecodeString(c.InstallID); err != nil {
			return Credential{}, false, errors.New("invalid install id")
		}
		if _, err := hex.DecodeString(c.Token); err != nil {
			return Credential{}, false, errors.New("invalid install token")
		}
		return c, false, nil
	}
	if !os.IsNotExist(err) {
		return Credential{}, false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return Credential{}, false, err
	}
	installID, err := randomHex(random, 16)
	if err != nil {
		return Credential{}, false, err
	}
	token, err := randomHex(random, 32)
	if err != nil {
		return Credential{}, false, err
	}
	c := Credential{Version: 1, InstallID: installID, Token: token}
	if err := atomicWriteJSON(path, c, 0o600); err != nil {
		return Credential{}, false, err
	}
	return c, true, nil
}

func ReadCredential(path string) (Credential, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Credential{}, err
	}
	var c Credential
	if err := json.Unmarshal(raw, &c); err != nil {
		return Credential{}, err
	}
	if c.Version != 1 || len(c.InstallID) != 32 || len(c.Token) != 64 {
		return Credential{}, errors.New("invalid install credential record")
	}
	if _, err := hex.DecodeString(c.InstallID); err != nil {
		return Credential{}, errors.New("invalid install id")
	}
	if _, err := hex.DecodeString(c.Token); err != nil {
		return Credential{}, errors.New("invalid install token")
	}
	return c, nil
}

func randomHex(r io.Reader, n int) (string, error) {
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func atomicWriteJSON(path string, value any, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".keydeck-tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
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
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(tmpName, path)
}
