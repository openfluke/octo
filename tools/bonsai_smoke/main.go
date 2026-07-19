package main

import (
	"fmt"
	"os"
	"time"

	"github.com/openfluke/welvet/model/tokenizer"
	"github.com/openfluke/welvet/model/transformer"
	"github.com/openfluke/welvet/webgpu"
)

func main() {
	path := "octo_entities/prism-ml--Bonsai-27B-mlx-1bit--binarypacked.entity"
	profile := "gpu_fuse"
	if len(os.Args) > 1 {
		path = os.Args[1]
	}
	if len(os.Args) > 2 {
		profile = os.Args[2]
	}
	fmt.Println("loading", path)
	t0 := time.Now()
	m, err := transformer.LoadEntity(path)
	if err != nil {
		fmt.Println("LOAD ERR:", err)
		os.Exit(1)
	}
	fmt.Printf("loaded in %v arch=%s layers=%d vocab=%d hidden=%d hybrid=%v webgpu=%v\n",
		time.Since(t0).Round(time.Millisecond), m.Architecture, len(m.Blocks), m.VocabSize, m.HiddenSize,
		m.IsHybrid(), webgpu.Available())

	var prof transformer.ExecProfile
	found := false
	for _, p := range transformer.NamedProfiles() {
		if p.Name == profile {
			prof = p
			found = true
			break
		}
	}
	if !found {
		fmt.Println("unknown profile:", profile)
		os.Exit(1)
	}
	if m.FusedPack {
		prof.PackFormat = m.PackFormat
	}
	if err := m.ApplyExec(prof); err != nil {
		fmt.Println("ApplyExec ERR:", err)
		os.Exit(1)
	}
	fmt.Printf("exec %s fused=%v hybridGPU=%v\n", prof.String(), m.Fused, m.HybridGPUFuse())

	tok, err := tokenizer.LoadTokenizer(m.TokenizerPath)
	if err != nil {
		fmt.Println("tok ERR:", err)
		os.Exit(1)
	}
	ids := tok.Encode("Hi", true)
	n := len(ids)
	if n > 8 {
		n = 8
	}
	fmt.Println("ids", ids[:n], "n=", len(ids))
	m.ResetKV()
	t1 := time.Now()
	logits, err := m.ForwardTokens(ids)
	if err != nil {
		fmt.Println("FWD ERR:", err)
		os.Exit(1)
	}
	fmt.Printf("forward ok in %v logits=%d\n", time.Since(t1).Round(time.Millisecond), len(logits))
	best := 0
	for i := 1; i < len(logits); i++ {
		if logits[i] > logits[best] {
			best = i
		}
	}
	fmt.Println("argmax", best, tok.Decode([]uint32{uint32(best)}, false))
}
