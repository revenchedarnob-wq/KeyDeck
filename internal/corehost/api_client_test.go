package corehost

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConnectRejectsRuntimeChangeDuringIdentityRoundTrip(t *testing.T) {
	dir := t.TempDir()
	layout, err := BuildLayout(dir)
	if err != nil {
		t.Fatal(err)
	}
	credential := Credential{Version: 1, InstallID: strings.Repeat("a", 32), Token: strings.Repeat("b", 64)}
	if err := writeTestJSON(layout.CredentialPath, credential); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	original := RuntimeInfo{Version: 1, InstanceID: strings.Repeat("c", 32), InstallID: credential.InstallID, Address: ln.Addr().String(), BuildID: "build", APIVersion: DefaultAPIVersion, PID: 1}
	if err := writeTestJSON(layout.RuntimePath, original); err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		changed := original
		changed.InstanceID = strings.Repeat("d", 32)
		if err := writeTestJSON(layout.RuntimePath, changed); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Identity{Product: "KeyDeck", BuildID: "build", APIVersion: DefaultAPIVersion, InstallID: credential.InstallID, InstanceID: original.InstanceID})
	})}
	go server.Serve(ln)
	defer server.Close()
	if _, err := Connect(context.Background(), layout, "build", DefaultAPIVersion, nil); err != ErrIdentityMismatch {
		t.Fatalf("expected identity mismatch, got %v", err)
	}
}

func writeTestJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o600)
}
