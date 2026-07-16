package hub

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/openfluke/octo/internal/paths"
)

// EnsureRepo downloads HF weights into the hub if safetensors are missing.
// Returns the snapshot directory.
func EnsureRepo(repoID string, quiet bool) (string, error) {
	repoID = normalizeRepo(repoID)
	if repoID == "" {
		return "", fmt.Errorf("empty repo id")
	}
	hubRoot := paths.HubRoot()
	snap := paths.ManualSnapshotDir(hubRoot, repoID)
	if hasSafetensors(snap) {
		return snap, nil
	}
	if !quiet {
		fmt.Printf("  downloading %s …\n", repoID)
	}
	if err := os.MkdirAll(hubRoot, 0o755); err != nil {
		return "", err
	}
	client := &http.Client{Timeout: 0}
	files, err := listRepoFiles(client, repoID)
	if err != nil {
		return "", err
	}
	want := selectDownloadable(files)
	if len(want) == 0 {
		return "", fmt.Errorf("no downloadable files in %s", repoID)
	}
	if err := os.MkdirAll(snap, 0o755); err != nil {
		return "", err
	}
	for i, f := range want {
		dest := filepath.Join(snap, filepath.FromSlash(f.Path))
		if st, err := os.Stat(dest); err == nil && st.Size() > 0 {
			continue
		}
		url := resolveURL(repoID, f.Path)
		if !quiet {
			fmt.Printf("  [%d/%d] %s …\n", i+1, len(want), f.Path)
		}
		t0 := time.Now()
		if err := downloadFile(client, url, dest); err != nil {
			return "", fmt.Errorf("%s: %w", f.Path, err)
		}
		if !quiet {
			fmt.Printf("        ok (%v)\n", time.Since(t0).Round(time.Millisecond))
		}
	}
	if err := writeRefsMain(hubRoot, repoID); err != nil {
		return "", err
	}
	if !hasSafetensors(snap) {
		return "", fmt.Errorf("download finished but no safetensors in %s", snap)
	}
	return snap, nil
}

func hasSafetensors(dir string) bool {
	m, _ := filepath.Glob(filepath.Join(dir, "*.safetensors"))
	return len(m) > 0
}
