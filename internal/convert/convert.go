package convert

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/openfluke/octo/internal/catalog"
	"github.com/openfluke/octo/internal/paths"
	"github.com/openfluke/octo/internal/ui"
	"github.com/openfluke/welvet/entity"
	"github.com/openfluke/welvet/hf"
	"github.com/openfluke/welvet/quant"
)

// Menu converts a local snapshot → .entity envelope.
func Menu(in *bufio.Reader) {
	fmt.Println("\nConvert snapshot → .entity")
	snaps := catalog.ListSnapshots()
	if len(snaps) == 0 {
		fmt.Println("No local snapshots. Download first (menu [2]).")
		repo := ui.Ask(in, "Or paste org/name if you already downloaded elsewhere: ", "")
		if repo == "" {
			return
		}
		dir := paths.ManualSnapshotDir(paths.HubRoot(), normalizeRepo(repo))
		if _, err := os.Stat(dir); err != nil {
			fmt.Printf("❌ No snapshot at %s\n", dir)
			return
		}
		snaps = []catalog.Snapshot{{RepoID: normalizeRepo(repo), Dir: dir}}
	} else {
		for i, s := range snaps {
			fmt.Printf("  [%d] %s\n", i+1, s.RepoID)
		}
		fmt.Printf("  [p] Paste org/name\n")
	}
	choice := ui.Ask(in, "Choice: ", "1")
	var snap catalog.Snapshot
	if strings.EqualFold(choice, "p") || len(snaps) == 0 {
		repo := normalizeRepo(ui.Ask(in, "Repo org/name: ", ""))
		if repo == "" {
			return
		}
		snap = catalog.Snapshot{RepoID: repo, Dir: paths.ManualSnapshotDir(paths.HubRoot(), repo)}
	} else {
		idx, err := strconv.Atoi(choice)
		if err != nil || idx < 1 || idx > len(snaps) {
			fmt.Println("Invalid")
			return
		}
		snap = snaps[idx-1]
	}
	if _, err := os.Stat(snap.Dir); err != nil {
		fmt.Printf("❌ Missing snapshot dir: %s\n", snap.Dir)
		return
	}

	fmt.Println("\nSource format")
	fmt.Println("  [1] Safetensors (HF)  ← SmolLM2 / Qwen / Bonsai MLX 1-bit")
	fmt.Println("  [2] GGUF")
	fmt.Println("  (press Enter = Safetensors)")
	src := ui.Ask(in, "Choice: ", "1")
	if src == "?" || strings.EqualFold(src, "h") || strings.EqualFold(src, "help") {
		fmt.Println("Safetensors = model.safetensors from HF (incl. Bonsai mlx-1bit). GGUF = .gguf file.")
		src = "1"
	}

	// Detect MLX 1-bit early — pack format is forced to BinaryPacked g128.
	forceBinary := false
	if cfgPath := filepath.Join(snap.Dir, "config.json"); fileExistsLocal(cfgPath) {
		if cfg, err := loadJSONMap(cfgPath); err == nil {
			if hf.IsQwen35Hybrid(cfg) {
				forceBinary = true
				fmt.Println("\nDetected Qwen3.5 / Bonsai hybrid (MLX 1-bit) — packing BinaryPacked g128 (text-only).")
			} else if bits, group, ok := hf.QuantBitsGroup(cfg); ok && bits == 1 && group == 128 {
				forceBinary = true
				fmt.Println("\nDetected dense Qwen3 / Bonsai-8B (MLX 1-bit) — packing BinaryPacked g128.")
			}
		}
	}

	formats := quant.AllFormats
	var format quant.Format
	if forceBinary {
		format = quant.FormatBinaryPacked
	} else {
		fmt.Println("\nTarget pack format")
		fmt.Println("  [0] none     (full precision FP32)")
		fmt.Println("  [2] Q4_0     ← recommended for chat (Lucy-style; Enter)")
		fmt.Println("  [l] list all formats (k-quants / IQ / BitNet)")
		fi := ui.Ask(in, "Format: ", "2")
		if fi == "l" || fi == "L" || fi == "?" {
			for i, f := range formats {
				fmt.Printf("  [%d] %s\n", i, f.String())
			}
			fi = ui.Ask(in, "Format index [0]: ", "0")
		}
		fidx, err := strconv.Atoi(fi)
		if err != nil || fidx < 0 || fidx >= len(formats) {
			fmt.Println("Invalid format — use 0 for none")
			return
		}
		format = formats[fidx]
	}

	switch src {
	case "2":
		if err := convertGGUF(snap, format); err != nil {
			fmt.Printf("❌ %v\n", err)
		}
	default:
		if err := convertSafetensors(snap, format); err != nil {
			fmt.Printf("❌ %v\n", err)
		}
	}
}

// QuantizeMenu re-packs an existing .entity (stub until welvet/entity packer).
func QuantizeMenu(in *bufio.Reader) {
	fmt.Println("\nQuantize / re-pack .entity")
	ents := catalog.ListEntities()
	if len(ents) == 0 {
		fmt.Println("No .entity files yet. Convert first (menu [3]).")
		return
	}
	for i, e := range ents {
		fmt.Printf("  [%d] %s\n", i+1, e.Path)
	}
	choice := ui.Ask(in, "Choice: ", "1")
	idx, err := strconv.Atoi(choice)
	if err != nil || idx < 1 || idx > len(ents) {
		fmt.Println("Invalid")
		return
	}
	formats := quant.AllFormats
	for i, f := range formats {
		fmt.Printf("  [%d] %s\n", i, f.String())
	}
	fi := ui.Ask(in, "Target format index: ", "0")
	fidx, _ := strconv.Atoi(fi)
	if fidx < 0 || fidx >= len(formats) {
		fmt.Println("Invalid format")
		return
	}
	fmt.Printf("🚧 Re-pack to %s not wired yet (awaits welvet/entity Pack).\n", formats[fidx])
	fmt.Printf("   Selected: %s\n", ents[idx-1].Path)
}

func convertSafetensors(snap catalog.Snapshot, format quant.Format) error {
	cfg := filepath.Join(snap.Dir, "config.json")
	if _, err := os.Stat(cfg); err != nil {
		return fmt.Errorf("need config.json in %s", snap.Dir)
	}
	sts, _ := filepath.Glob(filepath.Join(snap.Dir, "*.safetensors"))
	if len(sts) == 0 {
		return fmt.Errorf("no *.safetensors in %s", snap.Dir)
	}
	out := paths.EntityPathForFormat(snap.RepoID, format.String())
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}

	if format != quant.FormatNone && !quant.Supported(format) {
		return fmt.Errorf("unsupported pack format %s", format.String())
	}

	label := format.String()
	if format == quant.FormatNone {
		label = "FormatNone (FP32)"
	}
	fmt.Printf("  Packing Safetensors → Welvet ENTITY (%s)…\n", label)
	err := entity.PackFromHF(snap.Dir, out, entity.PackOptions{
		Repo:   snap.RepoID,
		Format: format,
		Progress: func(block, total int, detail string) {
			if block == 0 {
				fmt.Printf("    %s\n", detail)
			} else if block == total || block%5 == 0 || block == 1 {
				fmt.Printf("    [%d/%d] %s\n", block, total, detail)
			}
		},
	})
	if err != nil {
		return err
	}
	st, _ := os.Stat(out)
	fmt.Printf("✅ Wrote packed .entity:\n   %s\n", out)
	if st != nil {
		fmt.Printf("   size: %.1f MB  status=packed\n", float64(st.Size())/(1024*1024))
	}
	fmt.Println("   Next: menu [1] Run to chat.")
	return nil
}

// DetectPackFormat chooses BinaryPacked for MLX 1-bit, else Q4_0.
func DetectPackFormat(snapDir string) quant.Format {
	cfgPath := filepath.Join(snapDir, "config.json")
	if cfg, err := loadJSONMap(cfgPath); err == nil {
		if hf.IsQwen35Hybrid(cfg) {
			return quant.FormatBinaryPacked
		}
		if bits, group, ok := hf.QuantBitsGroup(cfg); ok && bits == 1 && group == 128 {
			return quant.FormatBinaryPacked
		}
	}
	return quant.FormatQ4_0
}

// PackRepo converts a local hub snapshot for repoID using auto-detected format.
func PackRepo(repoID string) (entityPath string, err error) {
	repoID = normalizeRepo(repoID)
	dir := paths.ManualSnapshotDir(paths.HubRoot(), repoID)
	if _, err := os.Stat(dir); err != nil {
		return "", fmt.Errorf("no snapshot at %s (download first)", dir)
	}
	format := DetectPackFormat(dir)
	snap := catalog.Snapshot{RepoID: repoID, Dir: dir}
	if err := convertSafetensors(snap, format); err != nil {
		return "", err
	}
	return paths.EntityPathForFormat(repoID, format.String()), nil
}

func convertGGUF(snap catalog.Snapshot, format quant.Format) error {
	ggs, _ := filepath.Glob(filepath.Join(snap.Dir, "*.gguf"))
	if len(ggs) == 0 {
		return fmt.Errorf("no *.gguf in %s (download a GGUF repo or place files in the snapshot)", snap.Dir)
	}
	meta := map[string]any{
		"magic":    "OCTOENT1",
		"version":  1,
		"source":   "gguf",
		"repo":     snap.RepoID,
		"snapshot": snap.Dir,
		"format":   format.String(),
		"weights":  ggs,
		"status":   "envelope",
		"note":     "GGUF→ENTITY unpack not wired yet (welvet/entity).",
		"engine":   "welvet",
		"octo":     "0.1",
	}
	out := paths.EntityPath(snap.RepoID)
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	if err := writeEnvelope(out, meta); err != nil {
		return err
	}
	fmt.Printf("✅ Wrote .entity envelope (GGUF):\n   %s\n", out)
	return nil
}

func writeEnvelope(path string, meta map[string]any) error {
	js, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write([]byte("OCTOENT1")); err != nil {
		return err
	}
	var lenBuf [4]byte
	binary.LittleEndian.PutUint32(lenBuf[:], uint32(len(js)))
	if _, err := f.Write(lenBuf[:]); err != nil {
		return err
	}
	_, err = f.Write(js)
	return err
}

func fileOK(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func fileExistsLocal(p string) bool { return fileOK(p) }

func loadJSONMap(path string) (map[string]any, error) {
	return hf.LoadConfigJSON(path)
}

func normalizeRepo(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "https://huggingface.co/")
	s = strings.Trim(s, "/")
	return s
}
