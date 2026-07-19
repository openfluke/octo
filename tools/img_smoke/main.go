package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/openfluke/welvet/apps/flux2"
)

func main() {
	snap := "octo_hub/models--prism-ml--bonsai-image-binary-4B-mlx-1bit/snapshots/manual-download"
	if len(os.Args) > 1 {
		snap = os.Args[1]
	}
	fmt.Println("loading transformer…")
	t0 := time.Now()
	m, err := flux2.LoadTransformerFromMLX(snap)
	if err != nil {
		fmt.Fprintf(os.Stderr, "transformer: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("  transformer ok in %v\n", time.Since(t0).Round(time.Millisecond))

	fmt.Println("loading VAE…")
	t0 = time.Now()
	vae, err := flux2.LoadVAEFromDir(snap)
	if err != nil {
		fmt.Fprintf(os.Stderr, "vae: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("  vae ok in %v (loaded=%v)\n", time.Since(t0).Round(time.Millisecond), vae != nil && vae.Loaded)

	if err := m.SyncGPU(); err != nil {
		fmt.Printf("  SyncGPU: %v (CPU fallback)\n", err)
	}
	defer m.CloseGPU()

	pipe := flux2.NewPipeline(m)
	pipe.VAE = vae

	txtSeq := 32
	joint := m.Cfg.JointAttentionDim
	embeds := make([]float32, txtSeq*joint)
	for i := range embeds {
		embeds[i] = float32((i%17)-8) * 0.01
	}

	outDir := "octo_outputs"
	_ = os.MkdirAll(outDir, 0o755)
	outPath := filepath.Join(outDir, "smoke_gpu.png")
	fmt.Println("generating 128x128 steps=2 (random embeds, GPU if available)…")
	t0 = time.Now()
	png, err := pipe.Generate(embeds, txtSeq, 128, 128, 2, 42)
	if err != nil {
		fmt.Fprintf(os.Stderr, "generate: %v\n", err)
		// still may have png bytes
	}
	if len(png) > 0 {
		if err := os.WriteFile(outPath, png, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "write: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("  wrote %s (%d bytes) in %v\n", outPath, len(png), time.Since(t0).Round(time.Millisecond))
	}
}
