package contextcompiler

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type Compiler struct {
	Runner Runner
}

func (c *Compiler) Compile(ctx context.Context, opt CompileOptions) (Packet, error) {
	root, err := filepath.Abs(opt.ProjectRoot)
	if err != nil {
		return Packet{}, err
	}
	if strings.TrimSpace(opt.Objective) == "" {
		return Packet{}, fmt.Errorf("objective is required")
	}
	if opt.MaxChars <= 0 {
		opt.MaxChars = 12_000
	}
	if opt.MaxFiles <= 0 {
		opt.MaxFiles = 6
	}
	p := Packet{
		Version:            1,
		CreatedAt:          time.Now().UTC(),
		Objective:          opt.Objective,
		ProjectRoot:        root,
		Keywords:           extractKeywords(opt.Objective, 10),
		Git:                readGitState(ctx, root),
		StructuralProvider: "none",
	}

	var structuralPaths []string
	if c.Runner != nil {
		p.StructuralProvider = c.Runner.Name()
		p.StructuralVersion = c.Runner.Version(ctx)
		indexOut, indexErr := c.callEvidence(ctx, "index_repository", map[string]any{"repo_path": root}, 2500)
		p.StructuralEvidence = append(p.StructuralEvidence, indexOut)
		if indexErr != nil {
			p.Warnings = append(p.Warnings, "structural index failed; exact source scout remained available: "+indexErr.Error())
		} else {
			p.StructuralIndexSucceeded = true
			p.ProjectID = findProjectID([]byte(indexOut.Output))
			projectArg := p.ProjectID
			if projectArg == "" {
				projectArg = root
			}

			arch, _ := c.callEvidence(ctx, "get_architecture", map[string]any{"project": projectArg}, 3200)
			p.StructuralEvidence = append(p.StructuralEvidence, arch)
			structuralPaths = append(structuralPaths, extractPaths([]byte(arch.Output))...)

			searchArgs := map[string]any{
				"project":      projectArg,
				"name_pattern": symbolSearchPattern(p.Keywords),
				"limit":        12,
			}
			search, searchErr := c.callEvidence(ctx, "search_graph", searchArgs, 4200)
			p.StructuralEvidence = append(p.StructuralEvidence, search)
			if searchErr == nil {
				p.StructuralSearchSucceeded = true
			}
			if searchErr != nil {
				// The focused regex is an optimization, not a correctness boundary.
				// Verify the graph query surface with the most portable pattern before
				// giving up on structural evidence entirely.
				fallbackArgs := map[string]any{"project": projectArg, "name_pattern": ".*", "limit": 12}
				fallback, fallbackErr := c.callEvidence(ctx, "search_graph", fallbackArgs, 4200)
				p.StructuralEvidence = append(p.StructuralEvidence, fallback)
				if fallbackErr == nil {
					search = fallback
					p.StructuralSearchSucceeded = true
					p.Warnings = append(p.Warnings, "focused structural symbol search failed; portable broad graph search succeeded")
				} else {
					p.Warnings = append(p.Warnings, "structural graph search failed; exact source scout remained available: "+fallbackErr.Error())
				}
			}
			structuralPaths = append(structuralPaths, extractPaths([]byte(search.Output))...)

			for _, symbol := range extractSymbolNames([]byte(search.Output), 2) {
				trace, traceErr := c.callEvidence(ctx, "trace_path", map[string]any{"project": projectArg, "function_name": symbol, "direction": "both", "depth": 3}, 2600)
				p.StructuralEvidence = append(p.StructuralEvidence, trace)
				if traceErr == nil {
					structuralPaths = append(structuralPaths, extractPaths([]byte(trace.Output))...)
				}
			}
		}
	}

	snippets, omitted, warnings := scoutSource(root, p.Keywords, structuralPaths, opt.MaxFiles)
	p.SourceSnippets = snippets
	p.OmittedEvidenceCount = omitted
	p.Warnings = append(p.Warnings, warnings...)

	// Budget after all evidence is collected so the packet remains auditable: the
	// compiler records exactly what it omitted rather than silently dropping state.
	rendered := p.Render()
	if len(rendered) > opt.MaxChars {
		trimToBudget(&p, opt.MaxChars)
		rendered = p.Render()
	}
	p.RenderedChars = len(rendered)
	return p, nil
}

func (c *Compiler) callEvidence(ctx context.Context, tool string, args map[string]any, max int) (StructuralEvidence, error) {
	argBytes, _ := json.Marshal(args)
	e := StructuralEvidence{Tool: tool, Arguments: string(argBytes)}
	out, err := c.Runner.Call(ctx, tool, args)
	if err != nil {
		e.Error = err.Error()
		e.Output = strings.TrimSpace(string(out))
		if len(e.Output) > max {
			e.Output = e.Output[:max]
			e.Truncated = true
		}
		return e, err
	}
	e.Successful = true
	e.Output = strings.TrimSpace(string(out))
	if len(e.Output) > max {
		e.Output = e.Output[:max]
		e.Truncated = true
	}
	return e, nil
}

func (p Packet) Render() string {
	var b strings.Builder
	fmt.Fprintf(&b, "KEYDECK CONTEXT PACKET v%d\n", p.Version)
	fmt.Fprintf(&b, "Objective: %s\n", p.Objective)
	fmt.Fprintf(&b, "Project: %s\n", p.ProjectRoot)
	if p.ProjectID != "" {
		fmt.Fprintf(&b, "Structural project id: %s\n", p.ProjectID)
	}
	fmt.Fprintf(&b, "Keywords: %s\n", strings.Join(p.Keywords, ", "))
	if p.Git.Head != "" || p.Git.Status != "" {
		fmt.Fprintf(&b, "Git HEAD: %s\nGit status:\n%s\n", p.Git.Head, p.Git.Status)
	}
	fmt.Fprintf(&b, "Structural provider: %s %s\n", p.StructuralProvider, p.StructuralVersion)
	for _, e := range p.StructuralEvidence {
		fmt.Fprintf(&b, "\n[STRUCTURAL %s | success=%v | truncated=%v]\n%s\n", e.Tool, e.Successful, e.Truncated, e.Output)
		if e.Error != "" {
			fmt.Fprintf(&b, "Error: %s\n", e.Error)
		}
	}
	for _, s := range p.SourceSnippets {
		fmt.Fprintf(&b, "\n[SOURCE %s:%d-%d | score=%d]\n%s\n", filepath.ToSlash(s.Path), s.StartLine, s.EndLine, s.Score, s.Content)
	}
	if len(p.Warnings) > 0 {
		fmt.Fprintf(&b, "\n[WARNINGS]\n- %s\n", strings.Join(p.Warnings, "\n- "))
	}
	if p.OmittedEvidenceCount > 0 {
		fmt.Fprintf(&b, "\nOmitted lower-ranked evidence items: %d\n", p.OmittedEvidenceCount)
	}
	return b.String()
}

func trimToBudget(p *Packet, max int) {
	// Structural tool receipts are proof evidence. Never delete them merely to
	// satisfy the context packet budget: compact verbose outputs while keeping
	// tool name, arguments, success/failure, and error state auditable.
	compactStructuralOutputs := func(limit int) {
		for i := range p.StructuralEvidence {
			if len(p.StructuralEvidence[i].Output) > limit {
				p.StructuralEvidence[i].Output = p.StructuralEvidence[i].Output[:limit]
				p.StructuralEvidence[i].Truncated = true
			}
		}
	}

	compactStructuralOutputs(1200)
	for len(p.Render()) > max && len(p.SourceSnippets) > 2 {
		p.SourceSnippets = p.SourceSnippets[:len(p.SourceSnippets)-1]
		p.OmittedEvidenceCount++
	}
	for i := range p.SourceSnippets {
		if len(p.Render()) <= max {
			break
		}
		if len(p.SourceSnippets[i].Content) > 1400 {
			p.SourceSnippets[i].Content = p.SourceSnippets[i].Content[:1400]
		}
	}
	if len(p.Render()) > max {
		compactStructuralOutputs(600)
	}
	if len(p.Render()) > max {
		compactStructuralOutputs(240)
	}
	// Last resort: retain at least one exact source snippet, but keep all
	// structural receipts. The packet can exceed an unrealistically tiny test
	// budget rather than erase evidence and later misreport a successful tool
	// call as if it never happened.
	for len(p.Render()) > max && len(p.SourceSnippets) > 1 {
		p.SourceSnippets = p.SourceSnippets[:len(p.SourceSnippets)-1]
		p.OmittedEvidenceCount++
	}
}

func extractKeywords(text string, limit int) []string {
	stop := map[string]bool{
		"the": true, "and": true, "that": true, "this": true, "with": true, "from": true, "when": true, "into": true, "after": true, "before": true,
		"why": true, "can": true, "could": true, "would": true, "should": true, "both": true, "same": true, "especially": true,
		"request": true, "requests": true, "reported": true, "receive": true, "identify": true, "exact": true, "root": true, "cause": true,
		"files": true, "file": true, "fix": true, "write": true, "report": true, "code": true, "project": true, "source": true, "use": true,
		"two": true, "bug": true, "change": true,
		"changes": true, "diagnose": true, "call": true, "makes": true, "must": true, "path": true, "possible": true,
	}
	freq := map[string]int{}
	first := map[string]int{}
	position := 0
	for _, raw := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' }) {
		word := strings.Trim(raw, "_")
		if len(word) < 3 || stop[word] {
			position++
			continue
		}
		if _, ok := first[word]; !ok {
			first[word] = position
		}
		freq[word]++
		position++
	}
	type kv struct {
		k string
		v int
		p int
	}
	var values []kv
	for k, v := range freq {
		values = append(values, kv{k: k, v: v, p: first[k]})
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].v != values[j].v {
			return values[i].v > values[j].v
		}
		if values[i].p != values[j].p {
			return values[i].p < values[j].p
		}
		return values[i].k < values[j].k
	})
	if len(values) > limit {
		values = values[:limit]
	}
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = v.k
	}
	return out
}

func readGitState(ctx context.Context, root string) GitState {
	var out GitState
	cmd := exec.CommandContext(ctx, "git", "-C", root, "rev-parse", "HEAD")
	if b, err := cmd.Output(); err == nil {
		out.Head = strings.TrimSpace(string(b))
	}
	cmd = exec.CommandContext(ctx, "git", "-C", root, "status", "--short")
	if b, err := cmd.Output(); err == nil {
		out.Status = boundGitStatus(strings.TrimSpace(string(b)), 80, 4000)
	}
	return out
}

func boundGitStatus(status string, maxLines, maxChars int) string {
	if status == "" {
		return ""
	}
	lines := strings.Split(status, "\n")
	omitted := 0
	if maxLines > 0 && len(lines) > maxLines {
		omitted = len(lines) - maxLines
		lines = lines[:maxLines]
	}
	out := strings.Join(lines, "\n")
	if maxChars > 0 && len(out) > maxChars {
		out = out[:maxChars]
		out = strings.TrimRight(out, "\r\n")
		if omitted == 0 {
			omitted = len(lines)
		}
	}
	if omitted > 0 {
		out += fmt.Sprintf("\n... %d additional git status entries omitted", omitted)
	}
	return out
}

func symbolSearchPattern(keywords []string) string {
	// codebase-memory-mcp v0.8.1 uses a C regex path where inline flags such
	// as (?i) are not portable. Expand a small set of case variants instead
	// of relying on engine-specific regex extensions.
	var terms []string
	seen := map[string]bool{}
	for _, kw := range keywords {
		kw = strings.TrimSpace(kw)
		if len(kw) < 3 {
			continue
		}
		variants := []string{kw, titleASCII(kw), strings.ToUpper(kw)}
		for _, v := range variants {
			if seen[v] {
				continue
			}
			seen[v] = true
			terms = append(terms, regexp.QuoteMeta(v))
		}
		if len(terms) >= 18 {
			break
		}
	}
	if len(terms) == 0 {
		return ".*"
	}
	return ".*(" + strings.Join(terms, "|") + ").*"
}

func titleASCII(s string) string {
	if s == "" {
		return s
	}
	b := []byte(s)
	if b[0] >= 'a' && b[0] <= 'z' {
		b[0] -= 'a' - 'A'
	}
	return string(b)
}

func findProjectID(raw []byte) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return ""
	}
	return findStringByKeys(v, "project", "project_id", "projectId")
}

func findStringByKeys(v any, keys ...string) string {
	switch x := v.(type) {
	case map[string]any:
		for _, key := range keys {
			if s, ok := x[key].(string); ok && s != "" {
				return s
			}
		}
		for _, child := range x {
			if s := findStringByKeys(child, keys...); s != "" {
				return s
			}
		}
	case []any:
		for _, child := range x {
			if s := findStringByKeys(child, keys...); s != "" {
				return s
			}
		}
	}
	return ""
}

func extractPaths(raw []byte) []string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	var walk func(any)
	walk = func(value any) {
		switch x := value.(type) {
		case map[string]any:
			for k, child := range x {
				lk := strings.ToLower(k)
				if s, ok := child.(string); ok && (strings.Contains(lk, "path") || lk == "file") && looksLikeSourcePath(s) {
					n := filepath.Clean(filepath.FromSlash(s))
					if !seen[n] {
						seen[n] = true
						out = append(out, n)
					}
				}
				walk(child)
			}
		case []any:
			for _, child := range x {
				walk(child)
			}
		}
	}
	walk(v)
	return out
}

func extractSymbolNames(raw []byte, limit int) []string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	var walk func(any)
	walk = func(value any) {
		if len(out) >= limit {
			return
		}
		switch x := value.(type) {
		case map[string]any:
			for _, k := range []string{"name", "function_name", "symbol"} {
				if s, ok := x[k].(string); ok && validSymbol(s) && !seen[s] {
					seen[s] = true
					out = append(out, s)
					if len(out) >= limit {
						return
					}
				}
			}
			for _, child := range x {
				walk(child)
			}
		case []any:
			for _, child := range x {
				walk(child)
			}
		}
	}
	walk(v)
	return out
}

func validSymbol(s string) bool {
	if len(s) < 3 || len(s) > 100 || strings.ContainsAny(s, "\\/\n\r\t ") {
		return false
	}
	for _, r := range s {
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == ':' || r == '.') {
			return false
		}
	}
	return true
}

func looksLikeSourcePath(s string) bool {
	ext := strings.ToLower(filepath.Ext(s))
	switch ext {
	case ".go", ".ts", ".tsx", ".js", ".jsx", ".py", ".rs", ".java", ".kt", ".cs", ".cpp", ".c", ".h", ".hpp", ".php", ".rb", ".swift", ".sql", ".yaml", ".yml", ".toml":
		return true
	}
	return false
}

type scoredFile struct {
	path       string
	score      int
	lines      []string
	matchLines []int
}

func scoutSource(root string, keywords, structuralPaths []string, maxFiles int) ([]SourceSnippet, int, []string) {
	files, warning := listSourceFiles(root)
	var warnings []string
	if warning != "" {
		warnings = append(warnings, warning)
	}
	structuralBoost := map[string]int{}
	for _, p := range structuralPaths {
		rel := cleanRelative(root, p)
		if rel != "" {
			structuralBoost[strings.ToLower(filepath.ToSlash(rel))] += 8
		}
	}
	var scored []scoredFile
	for _, rel := range files {
		full := filepath.Join(root, rel)
		info, err := os.Stat(full)
		if err != nil || info.Size() > 512<<10 {
			continue
		}
		b, err := os.ReadFile(full)
		if err != nil || bytes.IndexByte(b, 0) >= 0 {
			continue
		}
		lines := strings.Split(string(b), "\n")
		lower := strings.ToLower(string(b))
		score := structuralBoost[strings.ToLower(filepath.ToSlash(rel))]
		var matchLines []int
		for _, kw := range keywords {
			count := strings.Count(lower, strings.ToLower(kw))
			if count > 0 {
				score += min(count, 5)
				for i, line := range lines {
					if strings.Contains(strings.ToLower(line), strings.ToLower(kw)) {
						matchLines = append(matchLines, i+1)
						if len(matchLines) >= 12 {
							break
						}
					}
				}
			}
		}
		if score > 0 {
			scored = append(scored, scoredFile{path: rel, score: score, lines: lines, matchLines: uniqueInts(matchLines)})
		}
	}
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].path < scored[j].path
	})
	omitted := 0
	if len(scored) > maxFiles {
		omitted = len(scored) - maxFiles
		scored = scored[:maxFiles]
	}
	var snippets []SourceSnippet
	for _, f := range scored {
		center := 1
		if len(f.matchLines) > 0 {
			center = f.matchLines[0]
		}
		start := max(1, center-8)
		end := min(len(f.lines), center+18)
		var b strings.Builder
		for i := start; i <= end; i++ {
			fmt.Fprintf(&b, "%4d | %s\n", i, f.lines[i-1])
		}
		snippets = append(snippets, SourceSnippet{Path: f.path, StartLine: start, EndLine: end, Score: f.score, Content: strings.TrimRight(b.String(), "\n")})
	}
	return snippets, omitted, warnings
}

func listSourceFiles(root string) ([]string, string) {
	if _, err := exec.LookPath("git"); err == nil {
		cmd := exec.Command("git", "-C", root, "ls-files", "--cached", "--others", "--exclude-standard")
		if out, err := cmd.Output(); err == nil {
			var files []string
			s := bufio.NewScanner(bytes.NewReader(out))
			for s.Scan() {
				rel := filepath.Clean(filepath.FromSlash(strings.TrimSpace(s.Text())))
				if looksLikeSourcePath(rel) {
					files = append(files, rel)
				}
			}
			return files, ""
		}
	}
	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == ".keydeck-lab" || d.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err == nil && looksLikeSourcePath(rel) {
			files = append(files, rel)
		}
		return nil
	})
	if err != nil {
		return files, err.Error()
	}
	return files, "git ls-files unavailable; used filesystem walk"
}

func cleanRelative(root, candidate string) string {
	c := filepath.Clean(filepath.FromSlash(candidate))
	if !filepath.IsAbs(c) {
		return c
	}
	rel, err := filepath.Rel(root, c)
	if err != nil || strings.HasPrefix(rel, "..") {
		return ""
	}
	return rel
}

func uniqueInts(in []int) []int {
	seen := map[int]bool{}
	var out []int
	for _, v := range in {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Ints(out)
	return out
}

var windowsDrivePrefix = regexp.MustCompile(`^[A-Za-z]:[\\/]`)

func normalizePathForJSON(s string) string {
	if windowsDrivePrefix.MatchString(s) {
		return filepath.ToSlash(s)
	}
	return s
}

func parseInt(s string) int { n, _ := strconv.Atoi(s); return n }
