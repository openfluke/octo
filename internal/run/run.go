package run

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/openfluke/octo/internal/catalog"
	"github.com/openfluke/octo/internal/ui"
	"github.com/openfluke/welvet/entity"
	"github.com/openfluke/welvet/quant"
	"github.com/openfluke/welvet/simd"
	"github.com/openfluke/welvet/tokenizer"
	"github.com/openfluke/welvet/transformer"
	"github.com/openfluke/welvet/webgpu"
)

// Menu picks a .entity and runs generate/chat.
func Menu(in *bufio.Reader) {
	fmt.Println("\nRun .entity model")
	ents := catalog.ListEntities()
	if len(ents) == 0 {
		fmt.Println("No .entity files. Flow: [2] Download → [3] Convert → [1] Run.")
		fmt.Println("Example paste for download: HuggingFaceTB/SmolLM2-135M-Instruct")
		return
	}
	for i, e := range ents {
		status := "?"
		pack := ""
		if e.Meta != nil {
			if s, ok := e.Meta["status"].(string); ok {
				status = s
			}
			if tr, ok := e.Meta["transformer"].(map[string]any); ok {
				if pf, ok := tr["pack_format"].(string); ok && pf != "" {
					pack = "  quant=" + pf
				}
			}
		}
		fmt.Printf("  [%d] %s  (status=%s, %d bytes%s)\n", i+1, e.RepoID, status, e.Bytes, pack)
	}
	choice := ui.Ask(in, "Choice: ", "1")
	idx, err := strconv.Atoi(choice)
	if err != nil || idx < 1 || idx > len(ents) {
		fmt.Println("Invalid")
		return
	}
	e := ents[idx-1]
	fmt.Printf("\nSelected: %s\n  path: %s\n", e.RepoID, e.Path)

	magic, err := entity.PeekMagic(e.Path)
	if err != nil || magic != entity.Magic {
		if e.Meta != nil {
			if st, ok := e.Meta["status"].(string); ok && st == "envelope" {
				fmt.Println("\n🚧 This file is an Octo envelope (metadata only).")
				fmt.Println("   Re-run menu [3] Convert with FormatNone (0) to pack weights.")
				return
			}
		}
		fmt.Printf("❌ Not a packed Welvet ENTITY file (magic=%q)\n", magic)
		return
	}

	model, err := transformer.LoadEntity(e.Path)
	if err != nil {
		fmt.Printf("❌ Load entity: %v\n", err)
		return
	}

	prof, ok := askExecProfile(in, model)
	if !ok {
		return
	}
	if err := model.ApplyExec(prof); err != nil {
		fmt.Printf("❌ Exec profile: %v\n", err)
		return
	}
	fmt.Printf("  exec: %s\n", prof.String())
	if note := prof.GPUHybridNote(); note != "" {
		fmt.Printf("  note: %s\n", note)
	}
	if note := prof.FusedNote(); note != "" {
		fmt.Printf("  note: %s\n", note)
	}

	tokPath := model.TokenizerPath
	if tokPath == "" && model.Snapshot != "" {
		tokPath = filepath.Join(model.Snapshot, "tokenizer.json")
	}
	if tokPath == "" {
		fmt.Println("❌ No tokenizer.json path in entity header or snapshot")
		return
	}
	tok, err := tokenizer.LoadTokenizer(tokPath)
	if err != nil {
		fmt.Printf("❌ Tokenizer: %v\n", err)
		return
	}

	system := ui.Ask(in, "System prompt (blank=helpful assistant): ", "")
	if strings.TrimSpace(system) == "" {
		system = "You are a helpful assistant."
	}

	fmt.Println("Chat started (blank line to quit). Streams tokens + tok/s like Lucy.")
	var turns []transformer.Turn
	encode := func(text string, addSpecial bool) []uint32 { return tok.Encode(text, addSpecial) }
	decode := func(ids []uint32, skipSpecial bool) string { return tok.Decode(ids, skipSpecial) }

	for {
		user := ui.Ask(in, "You: ", "")
		if strings.TrimSpace(user) == "" {
			fmt.Println("Bye.")
			break
		}
		fmt.Print("Assistant: ")
		reply, _, err := model.Generate(
			encode, decode, turns, system, user,
			transformer.GenOptions{MaxTokens: 128},
		)
		if err != nil {
			fmt.Printf("\n❌ Generate: %v\n", err)
			continue
		}
		turns = append(turns, transformer.Turn{User: user, Assistant: reply})
		_ = os.Stdout.Sync()
	}
}

func askExecProfile(in *bufio.Reader, model *transformer.Model) (transformer.ExecProfile, bool) {
	profiles := transformer.NamedProfiles()
	fmt.Println("\nRun settings")
	simdOK := simd.Enabled()
	for i, p := range profiles {
		note := ""
		switch p.Name {
		case "simd_sc", "simd_mc":
			if !simdOK {
				note = "  (unavailable on this GOARCH)"
			} else if p.Name == "simd_mc" {
				note = "  ← default"
			}
		case "simd_fuse":
			if !simdOK {
				note = "  (unavailable on this GOARCH)"
			} else {
				note = "  packed quant + SIMD fused (Lucy CPU path)"
			}
		case "gpu":
			if webgpu.Available() {
				note = "  (hybrid: dense + LM head GPU)"
			} else {
				note = "  (needs Vulkan/DX12/Metal adapter)"
			}
		case "gpu_fuse":
			if webgpu.Available() {
				note = "  packed quant dense + LM head on GPU"
			} else {
				note = "  (needs Vulkan/DX12/Metal adapter)"
			}
		}
		cores := "single-core"
		if p.MultiCore {
			cores = "multicore"
		}
		fmt.Printf("  [%d] %-8s  %s, %s%s\n", i+1, p.Name, p.Backend.String(), cores, note)
	}
	fmt.Println("  tile: Dense MatVec tile size (Enter = 32)")

	defaultIdx := "4" // simd_mc
	if !simdOK {
		defaultIdx = "2" // cpu_mc fallback when SIMD kernels missing
		fmt.Println("  (SIMD off → defaulting to cpu_mc)")
	}
	choice := ui.Ask(in, "Profile: ", defaultIdx)
	idx, err := strconv.Atoi(choice)
	if err != nil || idx < 1 || idx > len(profiles) {
		fmt.Println("Invalid profile")
		return transformer.ExecProfile{}, false
	}
	prof := profiles[idx-1]

	tileStr := ui.Ask(in, "Tile size [32]: ", "32")
	tile, err := strconv.Atoi(tileStr)
	if err != nil || tile < 0 {
		fmt.Println("Invalid tile size")
		return transformer.ExecProfile{}, false
	}
	if tile == 0 {
		tile = 32
	}
	prof.TileSize = tile

	if prof.Fused {
		if model.FusedPack {
			prof.PackFormat = model.PackFormat
			fmt.Printf("  using entity baked quant: %s\n", model.PackFormat.String())
		} else {
			format, ok := askPackFormat(in)
			if !ok {
				return transformer.ExecProfile{}, false
			}
			prof.PackFormat = format
		}
	}
	return prof, true
}

func askPackFormat(in *bufio.Reader) (quant.Format, bool) {
	formats := quant.AllFormats
	fmt.Println("\nFused pack format (all k-quants / IQ / BitNet)")
	fmt.Println("  [2] Q4_0  ← default (Lucy-style)")
	fmt.Println("  [l] list all formats")
	fi := ui.Ask(in, "Format: ", "2")
	if fi == "l" || fi == "L" || fi == "?" {
		for i, f := range formats {
			if f == quant.FormatNone {
				continue
			}
			fmt.Printf("  [%d] %s\n", i, f.String())
		}
		fi = ui.Ask(in, "Format index [2]: ", "2")
	}
	fidx, err := strconv.Atoi(fi)
	if err != nil || fidx < 0 || fidx >= len(formats) {
		fmt.Println("Invalid format")
		return quant.FormatNone, false
	}
	format := formats[fidx]
	if format == quant.FormatNone {
		fmt.Println("Fused path needs a quant format (not none)")
		return quant.FormatNone, false
	}
	if !quant.Supported(format) {
		fmt.Printf("Format %s not supported yet\n", format)
		return quant.FormatNone, false
	}
	return format, true
}
