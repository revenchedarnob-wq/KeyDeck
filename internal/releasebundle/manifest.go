package releasebundle

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"path"
	"regexp"
	"strings"
)

const (
	ManifestSchemaVersion  = 1
	SignatureSchemaVersion = 1
	ProductName            = "KeyDeck"
	TargetOS               = "windows"
	TargetArch             = "amd64"
	SignatureAlgorithm     = "ed25519"
	MaxManifestFiles       = 4096
)

var (
	ErrMalformedEnvelope = errors.New("malformed bundle signature envelope")
	ErrUntrustedKey      = errors.New("untrusted bundle signing key")
	ErrInvalidSignature  = errors.New("invalid bundle manifest signature")
	ErrInvalidManifest   = errors.New("invalid bundle manifest")

	hexSHA256       = regexp.MustCompile(`^[0-9a-f]{64}$`)
	hexCommit       = regexp.MustCompile(`^[0-9a-f]{40}$`)
	releaseIdentity = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
)

type Manifest struct {
	SchemaVersion   int         `json:"schema_version"`
	Product         string      `json:"product"`
	ReleaseID       string      `json:"release_id"`
	ReleaseSequence uint64      `json:"release_sequence"`
	SourceCommit    string      `json:"source_commit"`
	BuildID         string      `json:"build_id"`
	OS              string      `json:"os"`
	Arch            string      `json:"arch"`
	SigningKeyID    string      `json:"signing_key_id"`
	Files           []FileEntry `json:"files"`
}

type FileEntry struct {
	Path   string `json:"path"`
	Role   string `json:"role"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type SignatureEnvelope struct {
	SchemaVersion  int    `json:"schema_version"`
	Algorithm      string `json:"algorithm"`
	KeyID          string `json:"key_id"`
	ManifestSHA256 string `json:"manifest_sha256"`
	Signature      string `json:"signature"`
}

func ParseAndVerify(manifestRaw, envelopeRaw []byte, trustedKeys map[string]ed25519.PublicKey) (Manifest, error) {
	var envelope SignatureEnvelope
	if err := strictDecode(envelopeRaw, &envelope); err != nil {
		return Manifest{}, fmt.Errorf("%w: %v", ErrMalformedEnvelope, err)
	}
	if envelope.SchemaVersion != SignatureSchemaVersion ||
		envelope.Algorithm != SignatureAlgorithm ||
		!releaseIdentity.MatchString(envelope.KeyID) ||
		!hexSHA256.MatchString(envelope.ManifestSHA256) {
		return Manifest{}, ErrMalformedEnvelope
	}
	publicKey, ok := trustedKeys[envelope.KeyID]
	if !ok || len(publicKey) != ed25519.PublicKeySize {
		return Manifest{}, ErrUntrustedKey
	}
	signature, err := base64.StdEncoding.Strict().DecodeString(envelope.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return Manifest{}, ErrMalformedEnvelope
	}
	manifestSum := sha256.Sum256(manifestRaw)
	if hex.EncodeToString(manifestSum[:]) != envelope.ManifestSHA256 ||
		!ed25519.Verify(publicKey, manifestRaw, signature) {
		return Manifest{}, ErrInvalidSignature
	}

	var manifest Manifest
	if err := strictDecode(manifestRaw, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("%w: %v", ErrInvalidManifest, err)
	}
	if err := validateManifest(manifest, envelope.KeyID); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func validateManifest(manifest Manifest, envelopeKeyID string) error {
	if manifest.SchemaVersion != ManifestSchemaVersion ||
		manifest.Product != ProductName ||
		!releaseIdentity.MatchString(manifest.ReleaseID) ||
		manifest.ReleaseSequence == 0 ||
		!hexCommit.MatchString(manifest.SourceCommit) ||
		!releaseIdentity.MatchString(manifest.BuildID) ||
		manifest.OS != TargetOS ||
		manifest.Arch != TargetArch ||
		manifest.SigningKeyID != envelopeKeyID ||
		len(manifest.Files) == 0 ||
		len(manifest.Files) > MaxManifestFiles {
		return ErrInvalidManifest
	}

	allowedRoles := map[string]bool{
		"bootstrap": true,
		"core":      true,
		"desktop":   true,
		"metadata":  true,
		"renderer":  true,
		"resource":  true,
		"runtime":   true,
	}
	requiredRoles := map[string]int{
		"bootstrap": 0,
		"core":      0,
		"desktop":   0,
		"renderer":  0,
	}
	paths := make(map[string]struct{}, len(manifest.Files))
	var totalSize int64
	for _, file := range manifest.Files {
		if err := validateBundlePath(file.Path); err != nil ||
			!allowedRoles[file.Role] ||
			file.Size < 0 ||
			!hexSHA256.MatchString(file.SHA256) {
			return ErrInvalidManifest
		}
		folded := strings.ToUpper(file.Path)
		if _, exists := paths[folded]; exists {
			return ErrInvalidManifest
		}
		paths[folded] = struct{}{}
		if _, required := requiredRoles[file.Role]; required {
			requiredRoles[file.Role]++
			if file.Size == 0 || !strings.HasSuffix(strings.ToLower(file.Path), ".exe") {
				return ErrInvalidManifest
			}
		}
		if file.Size > math.MaxInt64-totalSize {
			return ErrInvalidManifest
		}
		totalSize += file.Size
	}
	for _, count := range requiredRoles {
		if count != 1 {
			return ErrInvalidManifest
		}
	}
	return nil
}

func validateBundlePath(name string) error {
	if name == "" ||
		strings.HasPrefix(name, "/") ||
		strings.Contains(name, "\\") ||
		strings.Contains(name, ":") ||
		strings.ContainsRune(name, '\x00') ||
		path.Clean(name) != name {
		return ErrInvalidManifest
	}
	for _, component := range strings.Split(name, "/") {
		if component == "" || strings.TrimRight(component, " .") != component {
			return ErrInvalidManifest
		}
		for _, r := range component {
			if r < 0x20 {
				return ErrInvalidManifest
			}
		}
		base := component
		if i := strings.IndexByte(base, '.'); i >= 0 {
			base = base[:i]
		}
		if isReservedWindowsName(base) {
			return ErrInvalidManifest
		}
	}
	return nil
}

func isReservedWindowsName(name string) bool {
	switch strings.ToUpper(name) {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return true
	default:
		return false
	}
}

func strictDecode(raw []byte, value any) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return errors.New("empty JSON")
	}
	if err := rejectDuplicateFields(raw); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func rejectDuplicateFields(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := consumeJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func consumeJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("object key is not a string")
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("duplicate JSON field %q", key)
			}
			seen[key] = struct{}{}
			if err := consumeJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim('}') {
			return errors.New("unterminated JSON object")
		}
	case '[':
		for decoder.More() {
			if err := consumeJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim(']') {
			return errors.New("unterminated JSON array")
		}
	default:
		return errors.New("unexpected JSON delimiter")
	}
	return nil
}
