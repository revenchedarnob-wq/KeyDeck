package contextscout

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var sourceExtensions = map[string]bool{
	".c": true, ".cc": true, ".cpp": true, ".cs": true, ".css": true,
	".go": true, ".graphql": true, ".h": true, ".hpp": true, ".html": true,
	".java": true, ".js": true, ".jsx": true, ".json": true, ".kt": true,
	".kts": true, ".md": true, ".php": true, ".proto": true, ".ps1": true,
	".py": true, ".rb": true, ".rs": true, ".scss": true, ".sh": true,
	".sql": true, ".swift": true, ".toml": true, ".ts": true, ".tsx": true,
	".xml": true, ".yaml": true, ".yml": true,
}

var sourceNames = map[string]bool{
	"Dockerfile": true, "Makefile": true, "go.mod": true, "go.sum": true,
	"package-lock.json": true, "pnpm-lock.yaml": true, "yarn.lock": true,
}

var ignoredDirs = map[string]bool{
	".git": true, ".hg": true, ".svn": true, ".keydeck-lab": true,
	"node_modules": true, "vendor": true, "dist": true, "build": true,
}

func FingerprintProject(projectRoot string) (string, error) {
	root, err := filepath.Abs(projectRoot)
	if err != nil {
		return "", err
	}
	var files []string
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if path != root && ignoredDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || !isSourceRelevant(d.Name()) {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.Clean(rel))
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Slice(files, func(i, j int) bool { return filepath.ToSlash(files[i]) < filepath.ToSlash(files[j]) })
	h := sha256.New()
	for _, rel := range files {
		full := filepath.Join(root, rel)
		info, err := os.Stat(full)
		if err != nil {
			return "", err
		}
		if info.Size() > 4<<20 {
			continue
		}
		if _, err := io.WriteString(h, filepath.ToSlash(rel)); err != nil {
			return "", err
		}
		if _, err := io.WriteString(h, "\x00"); err != nil {
			return "", err
		}
		f, err := os.Open(full)
		if err != nil {
			return "", err
		}
		reader := bufio.NewReader(f)
		if _, err := io.Copy(h, reader); err != nil {
			_ = f.Close()
			return "", err
		}
		if err := f.Close(); err != nil {
			return "", err
		}
		if _, err := io.WriteString(h, "\x00"); err != nil {
			return "", err
		}
	}
	if len(files) == 0 {
		if _, err := io.WriteString(h, fmt.Sprintf("empty-source-root:%s", filepath.Clean(root))); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func isSourceRelevant(name string) bool {
	lower := strings.ToLower(name)
	if lower == ".env" || strings.HasPrefix(lower, ".env.") {
		return false
	}
	if sourceNames[name] {
		return true
	}
	return sourceExtensions[strings.ToLower(filepath.Ext(name))]
}
