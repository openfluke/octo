package hub

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/openfluke/octo/internal/paths"
	"github.com/openfluke/octo/internal/ui"
)

// treeEntry is one Hugging Face /api/models/{repo}/tree/main node.
type treeEntry struct {
	Type string `json:"type"`
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// DownloadMenu prompts for org/name and downloads into OCTO_HUB.
func DownloadMenu(in *bufio.Reader) {
	fmt.Println("\nDownload from Hugging Face")
	fmt.Println("  Paste any repo id, e.g. HuggingFaceTB/SmolLM2-135M-Instruct")
	fmt.Println("  (example only — nothing is pre-selected)")
	repo := normalizeRepo(ui.Ask(in, "Repo [org/name]: ", ""))
	if repo == "" {
		fmt.Println("Need org/name")
		return
	}
	hubRoot := paths.HubRoot()
	if err := os.MkdirAll(hubRoot, 0o755); err != nil {
		fmt.Printf("❌ hub: %v\n", err)
		return
	}
	if os.Getenv("HUGGING_FACE_HUB_TOKEN") == "" {
		fmt.Println("  (Set HUGGING_FACE_HUB_TOKEN if the repo is gated / returns 401.)")
	}
	fmt.Printf("  hub root: %s\n", hubRoot)
	client := &http.Client{Timeout: 0}
	files, err := listRepoFiles(client, repo)
	if err != nil {
		fmt.Printf("❌ list files: %v\n", err)
		return
	}
	want := selectDownloadable(files)
	if len(want) == 0 {
		fmt.Println("❌ No config/tokenizer/weight files found in repo tree")
		return
	}
	fmt.Printf("  will download %d files:\n", len(want))
	for _, f := range want {
		fmt.Printf("    - %s (%s)\n", f.Path, formatBytes(f.Size))
	}
	if !ui.Confirm(in, "Proceed") {
		fmt.Println("Cancelled")
		return
	}
	snap := paths.ManualSnapshotDir(hubRoot, repo)
	if err := os.MkdirAll(snap, 0o755); err != nil {
		fmt.Printf("❌ %v\n", err)
		return
	}
	for i, f := range want {
		dest := filepath.Join(snap, filepath.FromSlash(f.Path))
		if st, err := os.Stat(dest); err == nil && st.Size() > 0 {
			fmt.Printf("  [%d/%d] skip %s\n", i+1, len(want), f.Path)
			continue
		}
		url := resolveURL(repo, f.Path)
		fmt.Printf("  [%d/%d] %s …\n", i+1, len(want), f.Path)
		t0 := time.Now()
		if err := downloadFile(client, url, dest); err != nil {
			fmt.Printf("❌ %s: %v\n", f.Path, err)
			return
		}
		fmt.Printf("        ok (%v)\n", time.Since(t0).Round(time.Millisecond))
	}
	if err := writeRefsMain(hubRoot, repo); err != nil {
		fmt.Printf("⚠️  refs/main: %v\n", err)
	}
	fmt.Printf("\n✅ Snapshot ready:\n   %s\n", snap)
	fmt.Println("Next: menu [3] Convert → .entity, then [1] Run.")
}

func normalizeRepo(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "https://huggingface.co/")
	s = strings.TrimPrefix(s, "http://huggingface.co/")
	s = strings.Trim(s, "/")
	if i := strings.Index(s, "/tree/"); i >= 0 {
		s = s[:i]
	}
	if i := strings.Index(s, "/blob/"); i >= 0 {
		s = s[:i]
	}
	return s
}

func resolveURL(repo, file string) string {
	return fmt.Sprintf("https://huggingface.co/%s/resolve/main/%s", repo, file)
}

func listRepoFiles(client *http.Client, repo string) ([]treeEntry, error) {
	api := fmt.Sprintf("https://huggingface.co/api/models/%s/tree/main?recursive=1", repo)
	req, err := http.NewRequest(http.MethodGet, api, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Octo/0.1 (Welvet)")
	if tok := strings.TrimSpace(os.Getenv("HUGGING_FACE_HUB_TOKEN")); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("HTTP %d from %s: %s", resp.StatusCode, api, strings.TrimSpace(string(body)))
	}
	var entries []treeEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func selectDownloadable(entries []treeEntry) []treeEntry {
	var out []treeEntry
	for _, e := range entries {
		if e.Type != "file" {
			continue
		}
		p := e.Path
		base := filepath.Base(p)
		lower := strings.ToLower(p)
		switch {
		case base == "config.json",
			base == "generation_config.json",
			base == "tokenizer.json",
			base == "tokenizer_config.json",
			base == "special_tokens_map.json",
			base == "vocab.json",
			base == "merges.txt",
			base == "model.safetensors.index.json",
			strings.HasSuffix(lower, ".safetensors"),
			strings.HasSuffix(lower, ".gguf"),
			strings.HasSuffix(lower, ".ggml"):
			out = append(out, e)
		}
	}
	return out
}

func downloadFile(client *http.Client, url, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	part := dest + ".part"
	_ = os.Remove(part)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Octo/0.1 (Welvet)")
	if tok := strings.TrimSpace(os.Getenv("HUGGING_FACE_HUB_TOKEN")); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d %s", resp.StatusCode, url)
	}

	out, err := os.Create(part)
	if err != nil {
		return err
	}
	var written int64
	buf := make([]byte, 1<<20)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				_ = out.Close()
				_ = os.Remove(part)
				return werr
			}
			written += int64(n)
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			_ = out.Close()
			_ = os.Remove(part)
			return rerr
		}
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(part)
		return err
	}
	if written == 0 {
		_ = os.Remove(part)
		return fmt.Errorf("empty download")
	}
	_ = os.Remove(dest)
	return os.Rename(part, dest)
}

func writeRefsMain(hubRoot, repoID string) error {
	refs := filepath.Join(hubRoot, paths.RepoDirName(repoID), "refs")
	if err := os.MkdirAll(refs, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(refs, "main"), []byte("manual-download"), 0o644)
}

func formatBytes(n int64) string {
	if n <= 0 {
		return "?"
	}
	const u = 1024
	if n < u {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(u), 0
	for v := n / u; v >= u; v /= u {
		div *= u
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
