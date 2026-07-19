package catalog

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/openfluke/octo/internal/paths"
	"github.com/openfluke/welvet/model/entity"
)

// Snapshot describes a local HF-style snapshot.
type Snapshot struct {
	RepoID string
	Dir    string
	HasST  bool
	HasGGUF bool
	Config bool
}

// EntityInfo is a converted .entity on disk.
type EntityInfo struct {
	Path   string
	RepoID string
	Bytes  int64
	Meta   map[string]any
}

// ListSnapshots walks OCTO_HUB for models--*/snapshots/manual-download.
func ListSnapshots() []Snapshot {
	root := paths.HubRoot()
	ents, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []Snapshot
	for _, e := range ents {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "models--") {
			continue
		}
		repo := hubNameToRepo(e.Name())
		dir := filepath.Join(root, e.Name(), "snapshots", "manual-download")
		st, err := os.Stat(dir)
		if err != nil || !st.IsDir() {
			continue
		}
		s := Snapshot{RepoID: repo, Dir: dir}
		s.Config = fileExists(filepath.Join(dir, "config.json"))
		s.HasST = globExists(dir, "*.safetensors")
		s.HasGGUF = globExists(dir, "*.gguf")
		out = append(out, s)
	}
	return out
}

func hubNameToRepo(dirName string) string {
	// models--HuggingFaceTB--SmolLM2-135M-Instruct
	s := strings.TrimPrefix(dirName, "models--")
	return strings.ReplaceAll(s, "--", "/")
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func globExists(dir, pattern string) bool {
	m, _ := filepath.Glob(filepath.Join(dir, pattern))
	return len(m) > 0
}

// ListEntities scans OCTO_ENTITIES for *.entity.
func ListEntities() []EntityInfo {
	dir := paths.EntitiesDir()
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []EntityInfo
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".entity") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		info := EntityInfo{Path: p, RepoID: strings.TrimSuffix(strings.ReplaceAll(e.Name(), "--", "/"), ".entity")}
		if st, err := os.Stat(p); err == nil {
			info.Bytes = st.Size()
		}
		info.Meta = readEntityMeta(p)
		out = append(out, info)
	}
	return out
}

func readEntityMeta(path string) map[string]any {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var head [20]byte
	n, err := f.Read(head[:])
	if err != nil || n < 12 {
		return nil
	}
	magic := string(head[:8])
	if magic == entity.Magic {
		if n < 20 {
			return map[string]any{"magic": "ENTITY", "status": "?"}
		}
		headerLen := int(binary.LittleEndian.Uint64(head[12:20]))
		if headerLen <= 0 || headerLen > 16<<20 {
			return map[string]any{"magic": "ENTITY", "status": "?"}
		}
		buf := make([]byte, headerLen)
		if _, err := io.ReadFull(f, buf); err != nil {
			return map[string]any{"magic": "ENTITY", "status": "?"}
		}
		var doc map[string]any
		if err := json.Unmarshal(buf, &doc); err != nil {
			return map[string]any{"magic": "ENTITY", "status": "?"}
		}
		doc["magic"] = "ENTITY"
		if _, ok := doc["status"]; !ok {
			doc["status"] = "packed"
		}
		return doc
	}
	if magic != "OCTOENT1" {
		return map[string]any{"magic": magic}
	}
	jsonLen := int(head[8]) | int(head[9])<<8 | int(head[10])<<16 | int(head[11])<<24
	if jsonLen <= 0 || jsonLen > 16<<20 {
		return nil
	}
	// Already consumed 12 header bytes from the 20-byte peek; keep leftover if any.
	need := jsonLen
	var buf []byte
	if n > 12 {
		extra := n - 12
		if extra > need {
			extra = need
		}
		buf = append(buf, head[12:12+extra]...)
		need -= extra
	}
	if need > 0 {
		rest := make([]byte, need)
		if _, err := io.ReadFull(f, rest); err != nil {
			return nil
		}
		buf = append(buf, rest...)
	}
	var m map[string]any
	if err := json.Unmarshal(buf, &m); err != nil {
		return nil
	}
	return m
}

// PrintAll lists snapshots and entities.
func PrintAll() {
	fmt.Println("\nLocal hub snapshots")
	snaps := ListSnapshots()
	if len(snaps) == 0 {
		fmt.Printf("  (none under %s — use menu [2] Download)\n", paths.HubRoot())
	}
	for i, s := range snaps {
		flags := []string{}
		if s.Config {
			flags = append(flags, "config")
		}
		if s.HasST {
			flags = append(flags, "safetensors")
		}
		if s.HasGGUF {
			flags = append(flags, "gguf")
		}
		fmt.Printf("  [%d] %s  [%s]\n      %s\n", i+1, s.RepoID, strings.Join(flags, ","), s.Dir)
	}
	fmt.Println("\nLocal .entity files")
	ents := ListEntities()
	if len(ents) == 0 {
		fmt.Printf("  (none under %s — use menu [3] Convert)\n", paths.EntitiesDir())
	}
	for i, e := range ents {
		status := "?"
		if e.Meta != nil {
			if s, ok := e.Meta["status"].(string); ok {
				status = s
			}
		}
		fmt.Printf("  [%d] %s  (%d bytes, status=%s)\n      %s\n", i+1, e.RepoID, e.Bytes, status, e.Path)
	}
}
