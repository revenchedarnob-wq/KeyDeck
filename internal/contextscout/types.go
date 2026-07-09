package contextscout

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"keydeck.local/feasibilitylab/internal/contextcompiler"
	"keydeck.local/feasibilitylab/internal/proofreceipt"
)

var (
	ErrArtifactTampered = errors.New("context packet artifact failed integrity verification")
	ErrStoreState       = errors.New("invalid context packet store state")
	ErrHygiene          = errors.New("context packet failed hygiene validation")
	ErrProviderMismatch = errors.New("context provider identity or schema does not match configured provider")
)

var DefaultProviderTools = []string{"get_architecture", "index_repository", "search_graph", "trace_path"}

type BuildOptions struct {
	ProjectRoot          string
	Objective            string
	MaxChars             int
	MaxFiles             int
	ProviderServerID     string
	ProviderSchemaSHA256 string
	ProviderTools        []string
	ForbiddenExactValues []string
}

type CacheKeyInput struct {
	ProviderServerID     string `json:"provider_server_id"`
	ProviderSchemaSHA256 string `json:"provider_schema_sha256"`
	ProjectRoot          string `json:"project_root"`
	Objective            string `json:"objective"`
	MaxChars             int    `json:"max_chars"`
	MaxFiles             int    `json:"max_files"`
}

func (in CacheKeyInput) Hash() string {
	raw, _ := json.Marshal(in)
	return digest(raw)
}

type ProviderEvidence struct {
	ProviderServerID     string   `json:"provider_server_id"`
	ProviderSchemaSHA256 string   `json:"provider_schema_sha256"`
	ProviderTools        []string `json:"provider_tools"`
	CacheKey             string   `json:"cache_key"`
}

type Record struct {
	Sequence               uint64    `json:"sequence"`
	CreatedAt              time.Time `json:"created_at"`
	PacketID               string    `json:"packet_id"`
	CacheKey               string    `json:"cache_key"`
	ProjectFingerprint     string    `json:"project_fingerprint"`
	PacketSHA256           string    `json:"packet_sha256"`
	ProviderServerID       string    `json:"provider_server_id"`
	ProviderSchemaSHA256   string    `json:"provider_schema_sha256"`
	ProjectRoot            string    `json:"project_root"`
	Objective              string    `json:"objective"`
	MaxChars               int       `json:"max_chars"`
	MaxFiles               int       `json:"max_files"`
	PacketJSONPath         string    `json:"packet_json_path"`
	PacketJSONSHA256       string    `json:"packet_json_sha256"`
	RenderedTextPath       string    `json:"rendered_text_path"`
	RenderedTextSHA256     string    `json:"rendered_text_sha256"`
	ProviderEvidencePath   string    `json:"provider_evidence_path"`
	ProviderEvidenceSHA256 string    `json:"provider_evidence_sha256"`
	PreviousRecordSHA256   string    `json:"previous_record_sha256,omitempty"`
	RecordSHA256           string    `json:"record_sha256"`
}

type SaveInput struct {
	CacheKey             string
	ProjectFingerprint   string
	ProviderServerID     string
	ProviderSchemaSHA256 string
	ProjectRoot          string
	Objective            string
	MaxChars             int
	MaxFiles             int
	Packet               contextcompiler.Packet
	ProviderEvidence     ProviderEvidence
}

type HygieneReport struct {
	RenderedChars        int `json:"rendered_chars"`
	SourceSnippetCount   int `json:"source_snippet_count"`
	StructuralReceipts   int `json:"structural_receipts"`
	OmittedEvidenceCount int `json:"omitted_evidence_count"`
}

type Output struct {
	Packet             contextcompiler.Packet `json:"packet"`
	Record             Record                 `json:"record"`
	Hygiene            HygieneReport          `json:"hygiene"`
	Reused             bool                   `json:"reused"`
	ProviderCallCount  int                    `json:"provider_call_count"`
	ProjectFingerprint string                 `json:"project_fingerprint"`
	CacheKey           string                 `json:"cache_key"`
}

func (o Output) ReceiptArtifacts(store *Store) ([]proofreceipt.Artifact, error) {
	if store == nil {
		return nil, errors.New("context store is required")
	}
	return store.ReceiptArtifacts(o.Record)
}

func normalizeTools(values []string) []string {
	if len(values) == 0 {
		values = append([]string(nil), DefaultProviderTools...)
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sortStrings(out)
	return out
}

func digest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}
