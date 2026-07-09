package mcpbridge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var (
	ErrServerIdentityInvalid = errors.New("MCP server identity is invalid")
	sha256Pattern            = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// ServerIdentity binds a local MCP process to immutable package evidence.
// KeyDeck records this identity above the transport so changing adapters cannot
// silently change the third-party package that produced tool evidence.
type ServerIdentity struct {
	Name             string `json:"name"`
	Version          string `json:"version"`
	Registry         string `json:"registry"`
	Package          string `json:"package"`
	PackageIntegrity string `json:"package_integrity"`
	PackageSHA256    string `json:"package_sha256"`
	EntryPoint       string `json:"entry_point"`
}

func (id ServerIdentity) Validate() error {
	fields := map[string]string{
		"name":              id.Name,
		"version":           id.Version,
		"registry":          id.Registry,
		"package":           id.Package,
		"package_integrity": id.PackageIntegrity,
		"package_sha256":    id.PackageSHA256,
		"entry_point":       id.EntryPoint,
	}
	for name, value := range fields {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: %s is required", ErrServerIdentityInvalid, name)
		}
	}
	if id.Name != strings.TrimSpace(id.Name) || id.Version != strings.TrimSpace(id.Version) || id.Package != strings.TrimSpace(id.Package) {
		return fmt.Errorf("%w: name, version and package must not contain surrounding whitespace", ErrServerIdentityInvalid)
	}
	if !sha256Pattern.MatchString(id.PackageSHA256) {
		return fmt.Errorf("%w: package_sha256 must be 64 lowercase hexadecimal characters", ErrServerIdentityInvalid)
	}
	if strings.ContainsAny(id.Name, "\r\n#") || strings.ContainsAny(id.Version, "\r\n#") || strings.ContainsAny(id.Package, "\r\n#") {
		return fmt.Errorf("%w: name, version and package contain forbidden separators", ErrServerIdentityInvalid)
	}
	return nil
}

func (id ServerIdentity) CanonicalRef() string {
	if id.Registry == "npm" {
		return "npm:" + id.Package + "@" + id.Version
	}
	return id.Registry + ":" + id.Package + "@" + id.Version
}

func (id ServerIdentity) Hash() (string, error) {
	if err := id.Validate(); err != nil {
		return "", err
	}
	raw, err := json.Marshal(id)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
