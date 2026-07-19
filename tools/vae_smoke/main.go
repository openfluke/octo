package main

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"time"

	"github.com/openfluke/welvet/apps/flux2"
)

func main() {
	snap := os.Args[1]
	if snap == "" {
		fmt.Fprintln(os.Stderr, "usage: vae_smoke <snapshotDir> [pixelSide]")
		os.Exit(2)
	}
	side := 256
	if len(os.Args) > 2 {
		fmt.Sscanf(os.Args[2], "%d", &side)
	}
	lat := side / 8
	fmt.Printf("Loading VAE from %s (%dpx → lat %d)\n", snap, side, lat)
	v, err := flux2.LoadVAEFromDir(snap)
	if err != nil {
		fatal(err)
	}
	t0 := time.Now()
	if err := v.SyncGPU(side); err != nil {
		fatal(err)
	}
	defer v.CloseGPU()
	fmt.Printf("SyncGPU %v\n", time.Since(t0).Round(time.Millisecond))

	z := make([]float32, 32*lat*lat)
	rng := rand.New(rand.NewSource(1))
	for i := range z {
		z[i] = float32(rng.NormFloat64() * 0.5)
	}
	t0 = time.Now()
	rgb, err := v.Decode(z, lat, lat)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("Decode %v → RGB %d floats\n", time.Since(t0).Round(time.Millisecond), len(rgb))
	out := filepath.Join("octo_outputs", fmt.Sprintf("vae_smoke_%d.png", side))
	_ = os.MkdirAll("octo_outputs", 0o755)
	u8 := flux2.FloatRGBToUint8(rgb)
	f, err := os.Create(out)
	if err != nil {
		fatal(err)
	}
	defer f.Close()
	if err := flux2.EncodePNG(f, u8, side, side); err != nil {
		fatal(err)
	}
	fmt.Println("Wrote", out)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
