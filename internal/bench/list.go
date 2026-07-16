package bench

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// TemplatesDir is OCTO_TEMPLATES or ./templates.
func TemplatesDir() string {
	if v := os.Getenv("OCTO_TEMPLATES"); v != "" {
		return filepath.Clean(v)
	}
	return filepath.Join(".", "templates")
}

// TemplateInfo is one JSON recipe on disk.
type TemplateInfo struct {
	Name string // file stem
	Path string
}

// ListTemplates returns *.json under TemplatesDir (any model — not Smol-specific).
func ListTemplates() []TemplateInfo {
	dir := TemplatesDir()
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []TemplateInfo
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		out = append(out, TemplateInfo{
			Name: name,
			Path: filepath.Join(dir, e.Name()),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ResolveTemplatePath accepts a path, a bare name (looked up in templates/), or "".
func ResolveTemplatePath(arg string) (string, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return "", fmt.Errorf("need a template path or name (see templates/*.json)")
	}
	if st, err := os.Stat(arg); err == nil && !st.IsDir() {
		return arg, nil
	}
	// Bare name → templates/<name>.json
	cand := filepath.Join(TemplatesDir(), arg)
	if !strings.HasSuffix(strings.ToLower(cand), ".json") {
		cand += ".json"
	}
	if st, err := os.Stat(cand); err == nil && !st.IsDir() {
		return cand, nil
	}
	return "", fmt.Errorf("template not found: %s (tried %s)", arg, cand)
}
