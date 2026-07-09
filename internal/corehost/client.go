package corehost

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

func ReadRuntime(path string) (RuntimeInfo, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return RuntimeInfo{}, err
	}
	var info RuntimeInfo
	if err := json.Unmarshal(raw, &info); err != nil {
		return RuntimeInfo{}, err
	}
	if info.Version != 1 || info.InstanceID == "" || info.InstallID == "" || info.Address == "" || info.BuildID == "" || info.APIVersion == "" || info.PID <= 0 {
		return RuntimeInfo{}, fmt.Errorf("%w: invalid runtime metadata", ErrIdentityMismatch)
	}
	if !explicitLoopbackAddress(info.Address) {
		return RuntimeInfo{}, fmt.Errorf("%w: runtime address is not explicit loopback", ErrIdentityMismatch)
	}
	return info, nil
}

func Attest(ctx context.Context, layout Layout, expectedBuildID, expectedAPIVersion string, client *http.Client) (Identity, error) {
	info, err := ReadRuntime(layout.RuntimePath)
	if err != nil {
		return Identity{}, err
	}
	credential, err := ReadCredential(layout.CredentialPath)
	if err != nil {
		return Identity{}, err
	}
	if info.BuildID != expectedBuildID || info.APIVersion != expectedAPIVersion || info.InstallID != credential.InstallID {
		return Identity{}, ErrIdentityMismatch
	}
	client, err = hardenedLoopbackClient(client)
	if err != nil {
		return Identity{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+info.Address+"/v1/identity", nil)
	if err != nil {
		return Identity{}, err
	}
	req.Header.Set("Authorization", "Bearer "+credential.Token)
	resp, err := client.Do(req)
	if err != nil {
		return Identity{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Identity{}, ErrIdentityMismatch
	}
	var identity Identity
	dec := json.NewDecoder(io.LimitReader(resp.Body, 32<<10))
	if err := dec.Decode(&identity); err != nil {
		return Identity{}, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return Identity{}, ErrIdentityMismatch
	}
	if identity.Product != "KeyDeck" || identity.BuildID != expectedBuildID || identity.APIVersion != expectedAPIVersion || identity.InstallID != credential.InstallID || identity.InstanceID != info.InstanceID {
		return Identity{}, ErrIdentityMismatch
	}
	if strings.Contains(identity.BuildID, credential.Token) {
		return Identity{}, ErrIdentityMismatch
	}
	return identity, nil
}

func explicitLoopbackAddress(address string) bool {
	host, port, err := net.SplitHostPort(address)
	if err != nil || port == "" {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func hardenedLoopbackClient(base *http.Client) (*http.Client, error) {
	var out http.Client
	if base == nil {
		out.Timeout = 5 * time.Second
	} else {
		out = *base
		if out.Timeout <= 0 {
			out.Timeout = 5 * time.Second
		}
	}
	switch tr := out.Transport.(type) {
	case nil:
		clone := http.DefaultTransport.(*http.Transport).Clone()
		clone.Proxy = nil
		out.Transport = clone
	case *http.Transport:
		clone := tr.Clone()
		clone.Proxy = nil
		out.Transport = clone
	default:
		return nil, fmt.Errorf("%w: custom HTTP transport is not allowed for local attestation", ErrIdentityMismatch)
	}
	out.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	return &out, nil
}
