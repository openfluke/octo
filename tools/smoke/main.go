// Smoke pack + one-shot generate for SmolLM2 (manual: go run ./tools/smoke from octo/).
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/openfluke/welvet/core"
	"github.com/openfluke/welvet/entity"
	"github.com/openfluke/welvet/quant"
	"github.com/openfluke/welvet/tokenizer"
	"github.com/openfluke/welvet/transformer"
)

func main() {
	snap := "octo_hub/models--HuggingFaceTB--SmolLM2-135M-Instruct/snapshots/manual-download"
	out := "octo_entities/HuggingFaceTB--SmolLM2-135M-Instruct.entity"
	if len(os.Args) > 1 && os.Args[1] == "chat" {
		runChat(out, snap)
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "pack-q4" {
		outQ4 := strings.TrimSuffix(out, ".entity") + "-q4.entity"
		fmt.Println("Packing Q4_0 baked entity…")
		if err := entity.PackFromHF(snap, outQ4, entity.PackOptions{
			Repo:   "HuggingFaceTB/SmolLM2-135M-Instruct",
			Format: quant.FormatQ4_0,
			Progress: func(b, t int, d string) {
				if b == 0 || b == t || b%10 == 0 {
					fmt.Printf("  [%d/%d] %s\n", b, t, d)
				}
			},
		}); err != nil {
			panic(err)
		}
		st, _ := os.Stat(outQ4)
		fmt.Printf("packed %s (%.1f MB)\n", outQ4, float64(st.Size())/(1024*1024))
		return
	}
	fmt.Println("Packing…")
	if err := entity.PackFromHF(snap, out, entity.PackOptions{
		Repo:   "HuggingFaceTB/SmolLM2-135M-Instruct",
		Format: quant.FormatNone,
		Progress: func(b, t int, d string) {
			if b == 0 || b == t || b%10 == 0 {
				fmt.Printf("  [%d/%d] %s\n", b, t, d)
			}
		},
	}); err != nil {
		fmt.Fprintf(os.Stderr, "pack: %v\n", err)
		os.Exit(1)
	}
	st, _ := os.Stat(out)
	fmt.Printf("packed %s (%.1f MB)\n", out, float64(st.Size())/(1024*1024))
}

func runChat(entityPath, snap string) {
	if len(os.Args) > 3 {
		entityPath = os.Args[3]
	}
	m, err := transformer.LoadEntity(entityPath)
	if err != nil {
		panic(err)
	}
	tok, err := tokenizer.LoadTokenizer(filepath.Join(snap, "tokenizer.json"))
	if err != nil {
		panic(err)
	}
	prof := transformer.ProfileSIMDMultiCore()
	if len(os.Args) > 2 {
		switch os.Args[2] {
		case "gpu":
			prof = transformer.ExecProfile{Name: "gpu", Backend: core.BackendWebGPU, MultiCore: true, TileSize: 32}
		case "fuse", "simd_fuse":
			prof = transformer.ExecProfile{Name: "simd_fuse", Backend: core.BackendSIMD, MultiCore: true, TileSize: 32, Fused: true, PackFormat: quant.FormatQ4_0}
		case "gpu_fuse":
			prof = transformer.ExecProfile{Name: "gpu_fuse", Backend: core.BackendWebGPU, MultiCore: true, TileSize: 32, Fused: true, PackFormat: quant.FormatQ4_0}
		}
	}
	if err := m.ApplyExec(prof); err != nil {
		panic(err)
	}
	fmt.Printf("exec: %s\n", prof.String())
	if note := prof.GPUHybridNote(); note != "" {
		fmt.Printf("note: %s\n", note)
	}
	if note := prof.FusedNote(); note != "" {
		fmt.Printf("note: %s\n", note)
	}
	fmt.Print("Assistant: ")
	reply, metrics, err := m.Generate(
		func(text string, addSpecial bool) []uint32 { return tok.Encode(text, addSpecial) },
		func(ids []uint32, skip bool) string { return tok.Decode(ids, skip) },
		nil, "You are a helpful assistant.", "Say hi in one short sentence.",
		transformer.GenOptions{MaxTokens: 32},
	)
	if err != nil {
		panic(err)
	}
	_ = reply
	_ = metrics
}
