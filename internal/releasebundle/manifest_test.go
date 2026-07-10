package releasebundle

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

const testKeyID = "keydeck-test-release-key"

func TestParseAndVerifyValidManifest(t *testing.T) {
	manifestRaw, envelopeRaw, publicKey := signedFixture(t, validManifest())
	manifest, err := ParseAndVerify(manifestRaw, envelopeRaw, map[string]ed25519.PublicKey{testKeyID: publicKey})
	if err != nil {
		t.Fatal(err)
	}
	if manifest.ReleaseID != "v0.39.0-test" || len(manifest.Files) != 4 {
		t.Fatalf("unexpected manifest: %+v", manifest)
	}
}

func TestParseAndVerifyRejectsTamperingAndUnknownKeys(t *testing.T) {
	manifestRaw, envelopeRaw, publicKey := signedFixture(t, validManifest())
	tampered := bytes.Replace(manifestRaw, []byte("v0.39.0-test"), []byte("v0.39.0-evil"), 1)
	if _, err := ParseAndVerify(tampered, envelopeRaw, map[string]ed25519.PublicKey{testKeyID: publicKey}); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("tamper error=%v", err)
	}
	if _, err := ParseAndVerify(manifestRaw, envelopeRaw, nil); !errors.Is(err, ErrUntrustedKey) {
		t.Fatalf("unknown-key error=%v", err)
	}
}

func TestParseAndVerifyRejectsMalformedSignedJSON(t *testing.T) {
	manifestRaw, publicKey, privateKey := fixtureKeys(t, validManifest())
	tests := map[string][]byte{
		"duplicate field": bytes.Replace(manifestRaw, []byte(`"product":"KeyDeck"`), []byte(`"product":"KeyDeck","product":"Other"`), 1),
		"unknown field":   bytes.Replace(manifestRaw, []byte(`"product":"KeyDeck"`), []byte(`"product":"KeyDeck","surprise":true`), 1),
		"trailing value":  append(append([]byte{}, manifestRaw...), []byte("\n{}")...),
	}
	for name, malformed := range tests {
		t.Run(name, func(t *testing.T) {
			envelopeRaw := signEnvelope(t, malformed, privateKey)
			if _, err := ParseAndVerify(malformed, envelopeRaw, map[string]ed25519.PublicKey{testKeyID: publicKey}); !errors.Is(err, ErrInvalidManifest) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestParseAndVerifyRejectsMalformedEnvelope(t *testing.T) {
	manifestRaw, envelopeRaw, publicKey := signedFixture(t, validManifest())
	tests := map[string][]byte{
		"duplicate field": bytes.Replace(envelopeRaw, []byte(`"algorithm":"ed25519"`), []byte(`"algorithm":"ed25519","algorithm":"other"`), 1),
		"unknown field":   bytes.Replace(envelopeRaw, []byte(`"algorithm":"ed25519"`), []byte(`"algorithm":"ed25519","surprise":true`), 1),
		"trailing value":  append(append([]byte{}, envelopeRaw...), []byte("\n{}")...),
		"bad signature":   bytes.Replace(envelopeRaw, []byte(`"signature":"`), []byte(`"signature":"!!!`), 1),
	}
	for name, malformed := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseAndVerify(manifestRaw, malformed, map[string]ed25519.PublicKey{testKeyID: publicKey}); !errors.Is(err, ErrMalformedEnvelope) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestParseAndVerifyRejectsUnsafeOrAmbiguousPaths(t *testing.T) {
	unsafePaths := []string{
		"../keydeck-core.exe",
		"/keydeck-core.exe",
		`bin\keydeck-core.exe`,
		"bin/keydeck-core.exe:payload",
		"bin/CON.exe",
		"bin/keydeck-core.exe.",
		"bin//keydeck-core.exe",
	}
	for _, unsafe := range unsafePaths {
		t.Run(unsafe, func(t *testing.T) {
			manifest := validManifest()
			manifest.Files[1].Path = unsafe
			assertInvalidSignedManifest(t, manifest)
		})
	}

	manifest := validManifest()
	manifest.Files[3].Path = strings.ToUpper(manifest.Files[2].Path)
	assertInvalidSignedManifest(t, manifest)
}

func TestParseAndVerifyRejectsMissingOrDuplicateRequiredRoles(t *testing.T) {
	missing := validManifest()
	missing.Files = missing.Files[:3]
	assertInvalidSignedManifest(t, missing)

	duplicate := validManifest()
	duplicate.Files[3].Role = "desktop"
	assertInvalidSignedManifest(t, duplicate)
}

func TestParseAndVerifyRejectsInvalidIdentityAndFileMetadata(t *testing.T) {
	tests := []Manifest{
		func() Manifest { m := validManifest(); m.ReleaseSequence = 0; return m }(),
		func() Manifest { m := validManifest(); m.SourceCommit = "not-a-commit"; return m }(),
		func() Manifest { m := validManifest(); m.SigningKeyID = "different-key"; return m }(),
		func() Manifest { m := validManifest(); m.Files[0].SHA256 = strings.ToUpper(m.Files[0].SHA256); return m }(),
		func() Manifest { m := validManifest(); m.Files[0].Size = 0; return m }(),
		func() Manifest { m := validManifest(); m.Files[0].Path = "bin/keydeck-bootstrap.dll"; return m }(),
	}
	for i, manifest := range tests {
		t.Run(string(rune('A'+i)), func(t *testing.T) { assertInvalidSignedManifest(t, manifest) })
	}
}

func assertInvalidSignedManifest(t *testing.T, manifest Manifest) {
	t.Helper()
	manifestRaw, envelopeRaw, publicKey := signedFixture(t, manifest)
	if _, err := ParseAndVerify(manifestRaw, envelopeRaw, map[string]ed25519.PublicKey{testKeyID: publicKey}); !errors.Is(err, ErrInvalidManifest) {
		t.Fatalf("error=%v", err)
	}
}

func validManifest() Manifest {
	return Manifest{
		SchemaVersion:   ManifestSchemaVersion,
		Product:         ProductName,
		ReleaseID:       "v0.39.0-test",
		ReleaseSequence: 39,
		SourceCommit:    strings.Repeat("a", 40),
		BuildID:         "keydeck-v0.39.0-test",
		OS:              TargetOS,
		Arch:            TargetArch,
		SigningKeyID:    testKeyID,
		Files: []FileEntry{
			{Path: "bin/keydeck-bootstrap.exe", Role: "bootstrap", Size: 11, SHA256: strings.Repeat("1", 64)},
			{Path: "bin/keydeck-core.exe", Role: "core", Size: 12, SHA256: strings.Repeat("2", 64)},
			{Path: "bin/keydeck-desktop.exe", Role: "desktop", Size: 13, SHA256: strings.Repeat("3", 64)},
			{Path: "bin/keydeck-desktop-ui.exe", Role: "renderer", Size: 14, SHA256: strings.Repeat("4", 64)},
		},
	}
}

func signedFixture(t *testing.T, manifest Manifest) ([]byte, []byte, ed25519.PublicKey) {
	t.Helper()
	manifestRaw, publicKey, privateKey := fixtureKeys(t, manifest)
	return manifestRaw, signEnvelope(t, manifestRaw, privateKey), publicKey
}

func fixtureKeys(t *testing.T, manifest Manifest) ([]byte, ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	seed := bytes.Repeat([]byte{0x39}, ed25519.SeedSize)
	privateKey := ed25519.NewKeyFromSeed(seed)
	publicKey := privateKey.Public().(ed25519.PublicKey)
	manifestRaw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	return manifestRaw, publicKey, privateKey
}

func signEnvelope(t *testing.T, manifestRaw []byte, privateKey ed25519.PrivateKey) []byte {
	t.Helper()
	sum := sha256.Sum256(manifestRaw)
	envelope := SignatureEnvelope{
		SchemaVersion:  SignatureSchemaVersion,
		Algorithm:      SignatureAlgorithm,
		KeyID:          testKeyID,
		ManifestSHA256: hex.EncodeToString(sum[:]),
		Signature:      base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, manifestRaw)),
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
