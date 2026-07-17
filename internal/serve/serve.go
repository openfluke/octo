package serve

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/openfluke/octo/internal/catalog"
	"github.com/openfluke/octo/internal/paths"
	"github.com/openfluke/welvet/entity"
)

// Options for the entity CDN.
type Options struct {
	Addr string // default :7878
}

// Run starts an HTTP server hosting local .entity files for FinchKit phones.
func Run(opts Options) error {
	addr := strings.TrimSpace(opts.Addr)
	if addr == "" {
		addr = ":7878"
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"ok":       true,
			"service":  "octo-serve",
			"entities": paths.EntitiesDir(),
			"time":     time.Now().UTC().Format(time.RFC3339),
		})
	})
	mux.HandleFunc("/v1/entities", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, listEntities())
	})
	mux.HandleFunc("/v1/entities/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		rest := strings.TrimPrefix(r.URL.Path, "/v1/entities/")
		rest = strings.Trim(rest, "/")
		if rest == "" {
			http.NotFound(w, r)
			return
		}
		if strings.HasSuffix(rest, "/tokenizer.json") {
			id := filepath.Base(strings.TrimSuffix(rest, "/tokenizer.json"))
			serveTokenizer(w, id)
			return
		}
		id := filepath.Base(rest)
		if id == "" || id == "." || id == ".." {
			http.NotFound(w, r)
			return
		}
		path, err := resolveEntityFile(id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		f, err := os.Open(path)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		st, _ := f.Stat()
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filepath.Base(path)))
		if st != nil {
			w.Header().Set("Content-Length", fmt.Sprintf("%d", st.Size()))
		}
		_, _ = io.Copy(w, f)
	})

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	fmt.Printf("Octo entity CDN listening on http://%s\n", ln.Addr().String())
	fmt.Printf("  entities: %s\n", paths.EntitiesDir())
	fmt.Println("  GET /v1/health")
	fmt.Println("  GET /v1/entities")
	fmt.Println("  GET /v1/entities/{id}")
	fmt.Println("  GET /v1/entities/{id}/tokenizer.json")
	return http.Serve(ln, mux)
}

type entityRow struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256,omitempty"`
	Quant  string `json:"quant,omitempty"`
	Arch   string `json:"arch,omitempty"`
	RepoID string `json:"repo_id,omitempty"`
}

func listEntities() []entityRow {
	ents := catalog.ListEntities()
	out := make([]entityRow, 0, len(ents))
	for _, e := range ents {
		id := filepath.Base(e.Path)
		quant, arch := "", ""
		if e.Meta != nil {
			if tr, ok := e.Meta["transformer"].(map[string]any); ok {
				if pf, ok := tr["pack_format"].(string); ok {
					quant = pf
				}
				if a, ok := tr["architecture"].(string); ok {
					arch = a
				}
			}
		}
		// Do not SHA256 full .entity files here — catalog can be multi‑GB and
		// hashing on every GET /v1/entities freezes FinchKit Browse.
		out = append(out, entityRow{
			ID: id, Name: e.RepoID, Path: e.Path, Size: e.Bytes,
			Quant: quant, Arch: arch, RepoID: e.RepoID,
		})
	}
	return out
}

func serveTokenizer(w http.ResponseWriter, id string) {
	data, name, err := resolveTokenizerBytes(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, name))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
	_, _ = w.Write(data)
}

func resolveTokenizerBytes(id string) ([]byte, string, error) {
	entPath, err := resolveEntityFile(id)
	if err != nil {
		return nil, "", err
	}
	if ef, err := entity.Open(entPath); err == nil {
		if data, err := ef.LoadTokenizerJSON(); err == nil && len(data) > 0 {
			_ = ef.Close()
			return data, "tokenizer.json", nil
		}
		_ = ef.Close()
	}
	for _, p := range tokenizerCandidates(entPath) {
		data, err := os.ReadFile(p)
		if err == nil && len(data) > 0 {
			return data, "tokenizer.json", nil
		}
	}
	return nil, "", fmt.Errorf("tokenizer.json not found for %s (need hub snapshot or re-convert)", id)
}

func tokenizerCandidates(entPath string) []string {
	var out []string
	ef, err := entity.Open(entPath)
	if err != nil {
		return out
	}
	defer ef.Close()
	hdr := ef.Header()
	if hdr == nil || hdr.Transformer == nil {
		return out
	}
	tr := hdr.Transformer
	if tr.Tokenizer != "" {
		out = append(out, tr.Tokenizer)
	}
	if tr.Snapshot != "" {
		out = append(out, filepath.Join(tr.Snapshot, "tokenizer.json"))
	}
	repo := tr.Repo
	if repo == "" {
		// legacy filename → org/name guess
		base := strings.TrimSuffix(filepath.Base(entPath), ".entity")
		base = strings.Split(base, "--q")[0]
		base = strings.Split(base, "-q")[0]
		base = strings.Split(base, "--iq")[0]
		base = strings.Split(base, "--binary")[0]
		base = strings.Split(base, "--ternary")[0]
		if strings.Contains(base, "--") {
			repo = strings.Replace(base, "--", "/", 1)
		}
	}
	if repo != "" {
		out = append(out, filepath.Join(paths.ManualSnapshotDir(paths.HubRoot(), repo), "tokenizer.json"))
	}
	return out
}

func resolveEntityFile(id string) (string, error) {
	id = filepath.Base(id)
	direct := filepath.Join(paths.EntitiesDir(), id)
	if st, err := os.Stat(direct); err == nil && !st.IsDir() {
		return direct, nil
	}
	if !strings.HasSuffix(strings.ToLower(id), ".entity") {
		p := direct + ".entity"
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	for _, e := range catalog.ListEntities() {
		if filepath.Base(e.Path) == id || e.RepoID == id {
			return e.Path, nil
		}
	}
	return "", fmt.Errorf("not found")
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}
