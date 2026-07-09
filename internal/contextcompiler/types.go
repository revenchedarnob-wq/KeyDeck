package contextcompiler

import "time"

type StructuralEvidence struct {
	Tool       string `json:"tool"`
	Arguments  string `json:"arguments"`
	Output     string `json:"output"`
	Truncated  bool   `json:"truncated"`
	Successful bool   `json:"successful"`
	Error      string `json:"error,omitempty"`
}

type SourceSnippet struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Score     int    `json:"score"`
	Content   string `json:"content"`
}

type GitState struct {
	Head   string `json:"head,omitempty"`
	Status string `json:"status,omitempty"`
}

type Packet struct {
	Version                   int                  `json:"version"`
	CreatedAt                 time.Time            `json:"created_at"`
	Objective                 string               `json:"objective"`
	ProjectRoot               string               `json:"project_root"`
	ProjectID                 string               `json:"project_id,omitempty"`
	Keywords                  []string             `json:"keywords"`
	Git                       GitState             `json:"git"`
	StructuralProvider        string               `json:"structural_provider"`
	StructuralVersion         string               `json:"structural_version,omitempty"`
	StructuralEvidence        []StructuralEvidence `json:"structural_evidence"`
	StructuralIndexSucceeded  bool                 `json:"structural_index_succeeded"`
	StructuralSearchSucceeded bool                 `json:"structural_search_succeeded"`
	SourceSnippets            []SourceSnippet      `json:"source_snippets"`
	Warnings                  []string             `json:"warnings,omitempty"`
	OmittedEvidenceCount      int                  `json:"omitted_evidence_count"`
	RenderedChars             int                  `json:"rendered_chars"`
}

type CompileOptions struct {
	ProjectRoot string
	Objective   string
	MaxChars    int
	MaxFiles    int
}
