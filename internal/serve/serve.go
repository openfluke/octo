package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/openfluke/octo/internal/catalog"
	"github.com/openfluke/octo/internal/paths"
	"github.com/openfluke/welvet/entity"
)

// Options for the entity CDN and optional model host.
type Options struct {
	Addr      string // default :7878
	Model     string // optional .entity path, file name, or repo ID
	QueueSize int    // default 32; only used when Model is set
	Profile   string // cpu_mc, simd_fuse, gpu_fuse; default cpu_mc
}

// Status is the live CDN listener state (FinchKit + CLI).
type Status struct {
	Listening   bool     `json:"listening"`
	Addr        string   `json:"addr,omitempty"`
	Port        int      `json:"port,omitempty"`
	URLs        []string `json:"urls,omitempty"`
	EntitiesDir string   `json:"entities_dir,omitempty"`
	EntityCount int      `json:"entity_count"`
	Model       string   `json:"model,omitempty"`
	Profile     string   `json:"profile,omitempty"`
	QueueDepth  int      `json:"queue_depth,omitempty"`
	QueueSize   int      `json:"queue_size,omitempty"`
	Error       string   `json:"error,omitempty"`
}

var (
	serveMu    sync.Mutex
	httpSrv    *http.Server
	listener   net.Listener
	serveAddr  string
	serveErr   string
	activeHost *modelHost
)

// Start begins serving local .entity files (non-blocking). Idempotent if already up.
func Start(opts Options) error {
	serveMu.Lock()
	defer serveMu.Unlock()
	if listener != nil {
		return nil
	}
	addr := strings.TrimSpace(opts.Addr)
	if addr == "" {
		addr = ":7878"
	}
	var host *modelHost
	var err error
	if strings.TrimSpace(opts.Model) != "" {
		host, err = newModelHost(opts.Model, opts.QueueSize, opts.Profile)
		if err != nil {
			serveErr = err.Error()
			return err
		}
	}
	mux := newMux(addr, host)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		host.close()
		serveErr = err.Error()
		return err
	}
	srv := &http.Server{Handler: mux}
	listener = ln
	httpSrv = srv
	serveAddr = ln.Addr().String()
	serveErr = ""
	activeHost = host
	go func() {
		err := srv.Serve(ln)
		serveMu.Lock()
		defer serveMu.Unlock()
		if err != nil && err != http.ErrServerClosed {
			serveErr = err.Error()
		}
		if listener == ln {
			listener = nil
			httpSrv = nil
			serveAddr = ""
			activeHost = nil
		}
		host.close()
	}()
	return nil
}

// Stop shuts down the CDN listener.
func Stop() error {
	serveMu.Lock()
	srv := httpSrv
	ln := listener
	host := activeHost
	httpSrv = nil
	listener = nil
	serveAddr = ""
	activeHost = nil
	serveMu.Unlock()
	if srv == nil {
		if ln != nil {
			_ = ln.Close()
		}
		host.close()
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := srv.Shutdown(ctx)
	if err != nil {
		_ = srv.Close()
	}
	host.close()
	return err
}

// GetStatus snapshots the CDN listener.
func GetStatus() Status {
	serveMu.Lock()
	listening := listener != nil
	addr := serveAddr
	errMsg := serveErr
	host := activeHost
	serveMu.Unlock()

	st := Status{
		Listening:   listening,
		Addr:        addr,
		EntitiesDir: paths.EntitiesDir(),
		Error:       errMsg,
	}
	if host != nil {
		st.Model = host.entityPath
		st.Profile = host.profile
		st.QueueDepth = len(host.jobs)
		st.QueueSize = cap(host.jobs)
	}
	if matches, err := filepath.Glob(filepath.Join(paths.EntitiesDir(), "*.entity")); err == nil {
		st.EntityCount = len(matches)
	}
	if listening && addr != "" {
		_, portStr, err := net.SplitHostPort(addr)
		if err == nil {
			if p, perr := strconv.Atoi(portStr); perr == nil {
				st.Port = p
			}
		}
		port := st.Port
		if port == 0 {
			port = 7878
		}
		for _, ip := range localIPv4s() {
			st.URLs = append(st.URLs, fmt.Sprintf("http://%s:%d", ip, port))
		}
		if len(st.URLs) == 0 {
			st.URLs = append(st.URLs, fmt.Sprintf("http://127.0.0.1:%d", port))
		}
	}
	return st
}

// StatusJSON is GetStatus encoded for FFI.
func StatusJSON() string {
	b, _ := json.Marshal(GetStatus())
	return string(b)
}

// Run starts an HTTP server hosting local .entity files (blocking; CLI).
func Run(opts Options) error {
	if err := Start(opts); err != nil {
		return err
	}
	st := GetStatus()
	fmt.Printf("Octo entity CDN listening on http://%s\n", st.Addr)
	fmt.Printf("  entities: %s\n", st.EntitiesDir)
	for _, u := range st.URLs {
		fmt.Printf("  LAN: %s  (FinchKit → Find on LAN / ping)\n", u)
	}
	fmt.Println("  GET /v1/health  (also /v1/ping)")
	fmt.Println("  GET /v1/entities")
	fmt.Println("  GET /v1/entities/{id}")
	fmt.Println("  GET /v1/entities/{id}/tokenizer.json")
	if st.Model != "" {
		fmt.Printf("  model: %s (profile=%s, queue=%d)\n", st.Model, st.Profile, st.QueueSize)
		fmt.Println("  POST /v1/generate  {\"prompt\":\"...\",\"max_tokens\":256}")
		fmt.Println("  POST /v1/logits    {\"prompt\":\"...\"}  (float32 + exact bits)")
		fmt.Println("  GET  /v1/queue")
	}
	// Block until process exit (Serve already running in goroutine).
	select {}
}

func newMux(addr string, host *modelHost) *http.ServeMux {
	mux := http.NewServeMux()
	health := func(w http.ResponseWriter, r *http.Request) {
		host, _ := os.Hostname()
		entityCount := 0
		if matches, err := filepath.Glob(filepath.Join(paths.EntitiesDir(), "*.entity")); err == nil {
			entityCount = len(matches)
		}
		writeJSON(w, map[string]any{
			"ok":           true,
			"service":      "octo-serve",
			"hostname":     host,
			"addr":         addr,
			"entities_dir": paths.EntitiesDir(),
			"entity_count": entityCount,
			"time":         time.Now().UTC().Format(time.RFC3339),
		})
	}
	mux.HandleFunc("/v1/health", health)
	mux.HandleFunc("/v1/ping", health)
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
	registerModelRoutes(mux, host)
	return mux
}

func localIPv4s() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			var ip net.IP
			switch v := a.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() || ip.To4() == nil {
				continue
			}
			out = append(out, ip.String())
		}
	}
	return out
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
