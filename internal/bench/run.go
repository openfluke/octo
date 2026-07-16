package bench

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/openfluke/octo/internal/catalog"
	"github.com/openfluke/octo/internal/convert"
	"github.com/openfluke/octo/internal/hub"
	"github.com/openfluke/octo/internal/paths"
	"github.com/openfluke/welvet/quant"
	"github.com/openfluke/welvet/simd"
	"github.com/openfluke/welvet/tokenizer"
	"github.com/openfluke/welvet/transformer"
	"github.com/openfluke/welvet/webgpu"
)

var benchMu sync.Mutex // only one benchmark at a time (one model in flight)

// Run executes a template sequentially: one entity loaded at a time, one profile at a time.
func Run(templatePath string, quiet bool) (string, error) {
	benchMu.Lock()
	defer benchMu.Unlock()

	t, err := LoadTemplate(templatePath)
	if err != nil {
		return "", err
	}
	formats, err := t.ResolveQuantize()
	if err != nil {
		return "", err
	}
	profileNames, err := t.ResolveProfiles()
	if err != nil {
		return "", err
	}

	log := ResultLog{
		Template:  t.Name,
		StartedAt: time.Now(),
		Host:      runtime.GOOS,
		Platform:  fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
		Repo:      t.Model.Repo,
		Runs:      []RunRow{},
	}

	if err := os.MkdirAll(paths.LogsDir(), 0o755); err != nil {
		return "", err
	}
	partialPath := partialLogPath(log.StartedAt, t.Name)

	totalPlanned := countPlannedRuns(formats, profileNames, t.Messages)
	done := 0

	if !quiet {
		fmt.Printf("Bench %q — %s (sequential, one model at a time)\n", t.Name, t.Model.Repo)
		fmt.Printf("  planned runs: ~%d (%d formats × %d profiles × %d messages)\n",
			totalPlanned, len(formats), len(profileNames), len(t.Messages))
	}

	snapDir := paths.ManualSnapshotDir(paths.HubRoot(), t.Model.Repo)
	if t.Model.Download {
		snapDir, err = hub.EnsureRepo(t.Model.Repo, quiet)
		if err != nil {
			return "", fmt.Errorf("download: %w", err)
		}
	} else if !hasST(snapDir) {
		return "", fmt.Errorf("no safetensors at %s (set download: true)", snapDir)
	}
	snap := catalog.Snapshot{RepoID: t.Model.Repo, Dir: snapDir}

	tokPath := filepath.Join(snapDir, "tokenizer.json")
	if _, err := os.Stat(tokPath); err != nil {
		return "", fmt.Errorf("tokenizer: %w", err)
	}
	tok, err := tokenizer.LoadTokenizer(tokPath)
	if err != nil {
		return "", err
	}

	encode := func(text string, addSpecial bool) []uint32 { return tok.Encode(text, addSpecial) }
	decode := func(ids []uint32, skip bool) string { return tok.Decode(ids, skip) }

	// Phase 1: ensure all entities exist (convert one format at a time).
	entityPaths := make(map[string]string, len(formats))
	for _, format := range formats {
		fmtName := format.String()
		path, convertSec, convErr := ensureEntity(snap, format, t, quiet)
		if convErr != nil {
			for _, prof := range profileNames {
				for mi, msg := range t.Messages {
					log.Runs = append(log.Runs, RunRow{
						Quantize: fmtName, Profile: prof, MessageIndex: mi, Message: msg,
						Error: fmt.Sprintf("convert: %v", convErr),
					})
					done++
					flushPartial(partialPath, &log)
				}
			}
			continue
		}
		entityPaths[fmtName] = path
		if convertSec > 0 && !quiet {
			mb := 0.0
			if st, err := os.Stat(path); err == nil {
				mb = float64(st.Size()) / (1024 * 1024)
			}
			fmt.Printf("  packed %s (%.1f MB, %.1fs)\n", fmtName, mb, convertSec)
		}
		_ = convertSec
	}
	if !quiet {
		fmt.Printf("  entities ready: %d\n", len(entityPaths))
	}

	// Phase 2: run — load each entity once, sweep profiles, unload before next format.
	for _, format := range formats {
		fmtName := format.String()
		entityPath := entityPaths[fmtName]
		if entityPath == "" {
			continue
		}
		// Skip if every run for this format already failed at convert.
		if allConvertFailed(&log, fmtName, profileNames, t.Messages) {
			continue
		}

		var entityMB float64
		if st, err := os.Stat(entityPath); err == nil {
			entityMB = float64(st.Size()) / (1024 * 1024)
		}

		if !quiet {
			fmt.Printf("  loading %s …\n", fmtName)
		}
		tLoad := time.Now()
		model, err := transformer.LoadEntity(entityPath)
		if err != nil {
			for _, prof := range profileNames {
				for mi, msg := range t.Messages {
					log.Runs = append(log.Runs, runRowErr(fmtName, entityPath, entityMB, prof, mi, msg, "load: "+err.Error()))
					done++
					flushPartial(partialPath, &log)
				}
			}
			continue
		}
		if !quiet {
			fmt.Printf("    loaded in %v\n", time.Since(tLoad).Round(time.Millisecond))
		}

		for _, profName := range profileNames {
			if skipReason := skipProfile(profName, format); skipReason != "" {
				for mi, msg := range t.Messages {
					done++
					log.Runs = append(log.Runs, RunRow{
						Quantize: fmtName, EntityPath: entityPath, EntityMB: entityMB,
						Profile: profName, MessageIndex: mi, Message: msg, Skipped: skipReason,
					})
					flushPartial(partialPath, &log)
					if !quiet {
						fmt.Printf("  [%d/%d] [skip] %s / %s msg%d — %s\n",
							done, totalPlanned, fmtName, profName, mi, skipReason)
					}
				}
				continue
			}

			prof, ok := profileByName(profName)
			if !ok {
				for mi, msg := range t.Messages {
					done++
					log.Runs = append(log.Runs, runRowErr(fmtName, entityPath, entityMB, profName, mi, msg, "unknown profile"))
					flushPartial(partialPath, &log)
				}
				continue
			}
			prof.TileSize = t.TileSize
			if prof.Fused && format != quant.FormatNone {
				prof.PackFormat = format
			}

			model.CloseGPU() // tear down previous profile GPU state before switching
			if err := model.ApplyExec(prof); err != nil {
				for mi, msg := range t.Messages {
					done++
					log.Runs = append(log.Runs, runRowErr(fmtName, entityPath, entityMB, profName, mi, msg, "exec: "+err.Error()))
					flushPartial(partialPath, &log)
				}
				continue
			}

			for mi, msg := range t.Messages {
				done++
				t0 := time.Now()
				model.ResetKV()
				reply, metrics, genErr := model.Generate(
					encode, decode, nil, t.SystemPrompt, msg,
					transformer.GenOptions{MaxTokens: t.MaxTokens, Silent: true, PrintMetrics: false},
				)
				elapsed := time.Since(t0)

				row := RunRow{
					Quantize:     fmtName,
					EntityPath:   entityPath,
					EntityMB:     entityMB,
					Profile:      profName,
					MessageIndex: mi,
					Message:      msg,
					Reply:        strings.TrimSpace(reply),
				}
				if genErr != nil {
					row.Error = genErr.Error()
				} else {
					row.Metrics = metricsToRow(metrics)
				}
				log.Runs = append(log.Runs, row)
				flushPartial(partialPath, &log)

				if !quiet {
					status := "ok"
					if row.Error != "" {
						status = "ERR"
					}
					speed := ""
					if row.Metrics != nil && row.Metrics.GeneratedTokens > 0 {
						speed = fmt.Sprintf(" decode=%.2f tok/s", row.Metrics.DecodeTokPerSec)
					}
					fmt.Printf("  [%d/%d] [%s] %s / %s msg%d (%v)%s\n",
						done, totalPlanned, status, fmtName, profName, mi, elapsed.Round(time.Millisecond), speed)
				}
			}
		}

		model.CloseGPU()
		model = nil
		runtime.GC()
		if !quiet {
			fmt.Printf("  unloaded %s\n", fmtName)
		}
	}

	log.FinishedAt = time.Now()
	summarize(&log)

	outPath, err := writeLog(log)
	if err != nil {
		return "", err
	}
	_ = os.Remove(partialPath)
	if !quiet {
		fmt.Printf("\n✅ Results: %s\n", outPath)
		fmt.Printf("   total=%d ok=%d failed=%d skipped=%d (%.0fs)\n",
			log.Summary.Total, log.Summary.OK, log.Summary.Failed, log.Summary.Skipped,
			log.FinishedAt.Sub(log.StartedAt).Seconds())
	}
	return outPath, nil
}

func countPlannedRuns(formats []quant.Format, profiles []string, messages []string) int {
	return len(formats) * len(profiles) * len(messages)
}

func allConvertFailed(log *ResultLog, fmtName string, profiles []string, messages []string) bool {
	want := len(profiles) * len(messages)
	got := 0
	for _, r := range log.Runs {
		if r.Quantize == fmtName && strings.HasPrefix(r.Error, "convert:") {
			got++
		}
	}
	return got >= want && want > 0
}

func runRowErr(fmtName, entityPath string, entityMB float64, prof string, mi int, msg, err string) RunRow {
	return RunRow{
		Quantize: fmtName, EntityPath: entityPath, EntityMB: entityMB,
		Profile: prof, MessageIndex: mi, Message: msg, Error: err,
	}
}

func metricsToRow(m transformer.GenMetrics) *MetricsRow {
	return &MetricsRow{
		PrefillTokPerSec: m.PrefillTokPerSec,
		DecodeTokPerSec:  m.DecodeTokPerSec,
		TotalTokPerSec:   m.TotalTokPerSec,
		PrefillTokens:    m.PrefillTokens,
		GeneratedTokens:  m.GeneratedTokens,
		PrefillMS:        m.PrefillTime.Milliseconds(),
		DecodeMS:         m.DecodeTime.Milliseconds(),
	}
}

func summarize(log *ResultLog) {
	log.Summary = Summary{}
	for _, r := range log.Runs {
		log.Summary.Total++
		switch {
		case r.Skipped != "":
			log.Summary.Skipped++
		case r.Error != "":
			log.Summary.Failed++
		default:
			log.Summary.OK++
		}
	}
}

func partialLogPath(start time.Time, name string) string {
	stamp := start.UTC().Format("20060102-150405")
	safe := sanitizeName(name)
	return filepath.Join(paths.LogsDir(), fmt.Sprintf("%s_%s.partial.json", stamp, safe))
}

func flushPartial(path string, log *ResultLog) {
	summarize(log)
	f, err := os.Create(path)
	if err != nil {
		return
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	_ = enc.Encode(log)
	_ = f.Close()
}

func ensureEntity(snap catalog.Snapshot, format quant.Format, t *Template, quiet bool) (string, float64, error) {
	out := paths.EntityPathForFormat(snap.RepoID, format.String())
	if t.SkipConvertIfExists && !t.ForceConvert {
		if st, err := os.Stat(out); err == nil && st.Size() > 0 {
			return out, 0, nil
		}
		if format == quant.FormatNone {
			legacy := paths.EntityPath(snap.RepoID)
			if st, err := os.Stat(legacy); err == nil && st.Size() > 0 {
				return legacy, 0, nil
			}
		}
		if format == quant.FormatQ4_0 {
			legacy := paths.EntityPathLegacyQ4(snap.RepoID)
			if st, err := os.Stat(legacy); err == nil && st.Size() > 0 {
				return legacy, 0, nil
			}
		}
	}
	if !quiet {
		fmt.Printf("  converting → %s …\n", format.String())
	}
	t0 := time.Now()
	force := t.ForceConvert
	_, err := convert.EnsureEntity(snap, format, force, quiet)
	sec := time.Since(t0).Seconds()
	return out, sec, err
}

func hasST(dir string) bool {
	m, _ := filepath.Glob(filepath.Join(dir, "*.safetensors"))
	return len(m) > 0
}

func profileByName(name string) (transformer.ExecProfile, bool) {
	for _, p := range transformer.NamedProfiles() {
		if p.Name == name {
			return p, true
		}
	}
	return transformer.ExecProfile{}, false
}

func skipProfile(name string, format quant.Format) string {
	switch {
	case strings.HasPrefix(name, "simd") && name != "simd_fuse" && !simd.Enabled():
		return "SIMD unavailable on this GOARCH"
	case name == "simd_fuse" && !simd.Enabled():
		return "SIMD unavailable on this GOARCH"
	case strings.HasPrefix(name, "gpu") && !webgpu.Available():
		return "WebGPU unavailable"
	case (name == "simd_fuse" || name == "gpu_fuse") && format == quant.FormatNone:
		return "fused profiles need a quant format (not none)"
	case name == "gpu_fuse" && format != quant.FormatQ4_0:
		// Full on-device decoder (fusedgpu) is Q4_0-only. Other formats used to
		// silently fall through to per-GEMV WebGPU (~3–5 tok/s) — not a real fuse.
		return "full fused GPU is Q4_0-only (hybrid per-GEMV ~3–5 tok/s; use simd_fuse)"
	}
	p, ok := profileByName(name)
	if !ok {
		return "unknown profile"
	}
	if err := p.Validate(); err != nil {
		return err.Error()
	}
	return ""
}

func sanitizeName(name string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, name)
}

func writeLog(log ResultLog) (string, error) {
	dir := paths.LogsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	stamp := log.StartedAt.UTC().Format("20060102-150405")
	out := filepath.Join(dir, fmt.Sprintf("%s_%s.json", stamp, sanitizeName(log.Template)))
	f, err := os.Create(out)
	if err != nil {
		return "", err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(log); err != nil {
		_ = f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return out, nil
}
