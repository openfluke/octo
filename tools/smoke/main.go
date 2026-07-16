// Smoke pack + one-shot generate for SmolLM2 (manual: go run ./tools/smoke from octo/).
package main

import (
	"fmt"
	"os"
	"path/filepath"

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
	m, err := transformer.LoadEntity(entityPath)
	if err != nil {
		panic(err)
	}
	tok, err := tokenizer.LoadTokenizer(filepath.Join(snap, "tokenizer.json"))
	if err != nil {
		panic(err)
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
