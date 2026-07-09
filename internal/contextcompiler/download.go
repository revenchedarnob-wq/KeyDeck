package contextcompiler

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	PinnedCBMVersion         = "v0.8.1"
	PinnedChecksumsSHA256    = "142399e4e552fb559ede866b2549dbacc942d56f1c8718b52bc701b21f3f94c6"
	PinnedWindowsArchiveName = "codebase-memory-mcp-windows-amd64.zip"
)

type ToolReceipt struct {
	Name                string `json:"name"`
	Version             string `json:"version"`
	Binary              string `json:"binary"`
	ArchiveURL          string `json:"archive_url"`
	ChecksumsURL        string `json:"checksums_url"`
	ChecksumsSHA256     string `json:"checksums_sha256"`
	ArchiveSHA256       string `json:"archive_sha256"`
	ExpectedArchiveHash string `json:"expected_archive_sha256"`
	BinarySHA256        string `json:"binary_sha256"`
	Downloaded          bool   `json:"downloaded"`
}

func EnsurePinnedCBM(ctx context.Context, root string) (ToolReceipt, error) {
	if runtime.GOOS != "windows" {
		return ToolReceipt{}, fmt.Errorf("pinned automatic CBM bootstrap is currently scoped to Windows x64")
	}
	base := "https://github.com/DeusData/codebase-memory-mcp/releases/download/" + PinnedCBMVersion
	receipt := ToolReceipt{Name: "codebase-memory-mcp", Version: PinnedCBMVersion, ArchiveURL: base + "/" + PinnedWindowsArchiveName, ChecksumsURL: base + "/checksums.txt"}
	toolDir := filepath.Join(root, "codebase-memory-mcp", PinnedCBMVersion)
	binary := filepath.Join(toolDir, "codebase-memory-mcp.exe")
	if fileExists(binary) {
		receipt.Binary = binary
		receipt.BinarySHA256, _ = hashFile(binary)
		return receipt, nil
	}
	if err := os.MkdirAll(toolDir, 0o755); err != nil {
		return receipt, err
	}
	tmpDir, err := os.MkdirTemp("", "keydeck-cbm-bootstrap-")
	if err != nil {
		return receipt, err
	}
	defer os.RemoveAll(tmpDir)

	checksumsPath := filepath.Join(tmpDir, "checksums.txt")
	if err := download(ctx, receipt.ChecksumsURL, checksumsPath); err != nil {
		return receipt, err
	}
	receipt.ChecksumsSHA256, err = hashFile(checksumsPath)
	if err != nil {
		return receipt, err
	}
	if !strings.EqualFold(receipt.ChecksumsSHA256, PinnedChecksumsSHA256) {
		return receipt, fmt.Errorf("checksums.txt hash mismatch: got %s want %s", receipt.ChecksumsSHA256, PinnedChecksumsSHA256)
	}
	content, err := os.ReadFile(checksumsPath)
	if err != nil {
		return receipt, err
	}
	receipt.ExpectedArchiveHash = checksumFor(string(content), PinnedWindowsArchiveName)
	if receipt.ExpectedArchiveHash == "" {
		return receipt, fmt.Errorf("pinned archive missing from verified checksums.txt")
	}

	archivePath := filepath.Join(tmpDir, PinnedWindowsArchiveName)
	if err := download(ctx, receipt.ArchiveURL, archivePath); err != nil {
		return receipt, err
	}
	receipt.ArchiveSHA256, err = hashFile(archivePath)
	if err != nil {
		return receipt, err
	}
	if !strings.EqualFold(receipt.ArchiveSHA256, receipt.ExpectedArchiveHash) {
		return receipt, fmt.Errorf("archive hash mismatch: got %s want %s", receipt.ArchiveSHA256, receipt.ExpectedArchiveHash)
	}
	if err := extractZipSafe(archivePath, toolDir); err != nil {
		return receipt, err
	}
	found, err := findBinary(toolDir, "codebase-memory-mcp.exe")
	if err != nil {
		return receipt, err
	}
	if found != binary {
		if err := copyFile(found, binary); err != nil {
			return receipt, err
		}
	}
	receipt.Binary = binary
	receipt.BinarySHA256, err = hashFile(binary)
	if err != nil {
		return receipt, err
	}
	receipt.Downloaded = true
	return receipt, nil
}

func download(ctx context.Context, url, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: HTTP %s", url, resp.Status)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(f, io.LimitReader(resp.Body, 256<<20))
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func checksumFor(content, filename string) string {
	for _, line := range strings.Split(content, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) >= 2 && strings.TrimPrefix(fields[len(fields)-1], "*") == filename {
			if len(fields[0]) == 64 {
				return strings.ToLower(fields[0])
			}
		}
	}
	return ""
}

func extractZipSafe(archive, dest string) error {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer zr.Close()
	base, _ := filepath.Abs(dest)
	for _, f := range zr.File {
		target := filepath.Join(dest, filepath.Clean(filepath.FromSlash(f.Name)))
		abs, err := filepath.Abs(target)
		if err != nil {
			return err
		}
		if abs != base && !strings.HasPrefix(abs, base+string(os.PathSeparator)) {
			return fmt.Errorf("unsafe zip path %q", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(abs, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(abs, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			rc.Close()
			return err
		}
		_, copyErr := io.Copy(out, io.LimitReader(rc, 256<<20))
		closeOut := out.Close()
		closeIn := rc.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeOut != nil {
			return closeOut
		}
		if closeIn != nil {
			return closeIn
		}
	}
	return nil
}

func findBinary(root, name string) (string, error) {
	var found string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.EqualFold(d.Name(), name) {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if found == "" {
		return "", fmt.Errorf("%s not found after extraction", name)
	}
	return found, nil
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func copyFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	_, e := io.Copy(out, in)
	ce := out.Close()
	if e != nil {
		return e
	}
	return ce
}
func fileExists(path string) bool { st, err := os.Stat(path); return err == nil && !st.IsDir() }
