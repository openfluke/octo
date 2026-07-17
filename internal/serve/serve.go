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
		id := strings.TrimPrefix(r.URL.Path, "/v1/entities/")
		id = filepath.Base(id)
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
