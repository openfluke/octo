package bench

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/openfluke/welvet/quant"
)

// Template is a JSON benchmark recipe (see templates/*.json).
type Template struct {
	Name string `json:"name"`

	Model struct {
		Repo     string `json:"repo"`
		Download bool   `json:"download"`
	} `json:"model"`

	// Quantize is "all" or a list of format names (none, Q4_0, Q4_K, …).
	Quantize json.RawMessage `json:"quantize"`

	SystemPrompt string   `json:"system_prompt"`
	Messages     []string `json:"messages"`
	MaxTokens    int      `json:"max_tokens"`

	// Profiles is "all" or a list of run profile names (simd_mc, gpu_fuse, …).
	Profiles json.RawMessage `json:"profiles"`

	TileSize int `json:"tile_size"`

	// SkipConvertIfExists skips packing when the .entity file is already present.
	SkipConvertIfExists bool `json:"skip_convert_if_exists"`
	// ForceConvert re-packs even when the entity exists.
	ForceConvert bool `json:"force_convert"`
}

// ResultLog is written to octo/logs/<timestamp>_<name>.json.
type ResultLog struct {
	Template   string    `json:"template"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	Host       string    `json:"host"`
	Platform   string    `json:"platform"`
	Repo       string    `json:"repo"`
	Runs       []RunRow  `json:"runs"`
	Summary    Summary   `json:"summary"`
}

// RunRow is one quant × profile × message measurement.
type RunRow struct {
	Quantize       string  `json:"quantize"`
	EntityPath     string  `json:"entity_path"`
	EntityMB       float64 `json:"entity_mb"`
	Profile        string  `json:"profile"`
	MessageIndex   int     `json:"message_index"`
	Message        string  `json:"message"`
	Reply          string  `json:"reply,omitempty"`
	Error          string  `json:"error,omitempty"`
	Skipped        string  `json:"skipped,omitempty"`
	ConvertSeconds float64 `json:"convert_seconds,omitempty"`
	Metrics        *MetricsRow `json:"metrics,omitempty"`
}

// MetricsRow mirrors transformer.GenMetrics for JSON export.
type MetricsRow struct {
	PrefillTokPerSec float64 `json:"prefill_tok_per_sec"`
	DecodeTokPerSec  float64 `json:"decode_tok_per_sec"`
	TotalTokPerSec   float64 `json:"total_tok_per_sec"`
	PrefillTokens    int     `json:"prefill_tokens"`
	GeneratedTokens  int     `json:"generated_tokens"`
	PrefillMS        int64   `json:"prefill_ms"`
	DecodeMS         int64   `json:"decode_ms"`
}

// Summary counts outcomes.
type Summary struct {
	Total   int `json:"total"`
	OK      int `json:"ok"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
}

// LoadTemplate reads and validates a JSON template file.
func LoadTemplate(path string) (*Template, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var t Template
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, err
	}
	if strings.TrimSpace(t.Name) == "" {
		t.Name = strings.TrimSuffix(filepathBase(path), ".json")
	}
	if strings.TrimSpace(t.Model.Repo) == "" {
		return nil, fmt.Errorf("template: model.repo required")
	}
	if len(t.Messages) == 0 {
		return nil, fmt.Errorf("template: messages required")
	}
	if t.MaxTokens <= 0 {
		t.MaxTokens = 32
	}
	if t.TileSize <= 0 {
		t.TileSize = 32
	}
	if strings.TrimSpace(t.SystemPrompt) == "" {
		t.SystemPrompt = "You are a helpful assistant."
	}
	return &t, nil
}

func filepathBase(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// ResolveQuantize parses quantize field.
func (t *Template) ResolveQuantize() ([]quant.Format, error) {
	if len(t.Quantize) == 0 {
		return []quant.Format{quant.FormatQ4_0}, nil
	}
	var s string
	if err := json.Unmarshal(t.Quantize, &s); err == nil {
		if strings.EqualFold(s, "all") {
			return quant.AllFormats, nil
		}
		f := quant.ParseFormatName(s)
		return []quant.Format{f}, nil
	}
	var names []string
	if err := json.Unmarshal(t.Quantize, &names); err != nil {
		return nil, fmt.Errorf("quantize: want \"all\" or [\"Q4_0\", ...]")
	}
	out := make([]quant.Format, 0, len(names))
	for _, n := range names {
		out = append(out, quant.ParseFormatName(n))
	}
	return out, nil
}

// ResolveProfiles parses profiles field.
func (t *Template) ResolveProfiles() ([]string, error) {
	if len(t.Profiles) == 0 {
		return []string{"simd_mc"}, nil
	}
	var s string
	if err := json.Unmarshal(t.Profiles, &s); err == nil {
		if strings.EqualFold(s, "all") {
			return allProfileNames(), nil
		}
		return []string{s}, nil
	}
	var names []string
	if err := json.Unmarshal(t.Profiles, &names); err != nil {
		return nil, fmt.Errorf("profiles: want \"all\" or [\"simd_mc\", ...]")
	}
	return names, nil
}

func allProfileNames() []string {
	return []string{
		"cpu_sc", "cpu_mc", "simd_sc", "simd_mc", "gpu", "simd_fuse", "gpu_fuse",
	}
}
