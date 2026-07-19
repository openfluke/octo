package imagegen

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/openfluke/octo/internal/paths"
	"github.com/openfluke/octo/internal/ui"
	"github.com/openfluke/welvet/apps/flux2"
	"github.com/openfluke/welvet/webgpu"
)

const defaultImageRepo = "prism-ml/bonsai-image-binary-4B-mlx-1bit"

// Menu runs interactive prompt → PNG generation.
func Menu(in *bufio.Reader) {
	fmt.Println("\nGenerate image (Flux2 / Bonsai Image)")
	snap, err := resolveSnapshot(in)
	if err != nil {
		fmt.Printf("❌ %v\n", err)
		return
	}
	fmt.Printf("  snapshot: %s\n", snap)
	fmt.Printf("  components: %s\n", snapshotStatus(snap))

	prompt := ui.Ask(in, "Prompt: ", "a red bicycle on a sunny street")
	if strings.TrimSpace(prompt) == "" {
		fmt.Println("Need a prompt")
		return
	}

	height, _ := strconv.Atoi(ui.Ask(in, "Height [256]: ", "256"))
	width, _ := strconv.Atoi(ui.Ask(in, "Width [256]: ", "256"))
	steps, _ := strconv.Atoi(ui.Ask(in, "Steps [4]: ", "4"))
	seed, _ := strconv.ParseInt(ui.Ask(in, "Seed [42]: ", "42"), 10, 64)
	if height <= 0 {
		height = 256
	}
	if width <= 0 {
		width = 256
	}
	if steps <= 0 {
		steps = 4
	}

	if !webgpu.Available() {
		err := webgpu.InitError()
		if err == nil {
			err = fmt.Errorf("no adapter")
		}
		fmt.Printf("❌ GPU required for image gen: %v\n", err)
		return
	}
	adapter := webgpu.AdapterName()
	if adapter == "" {
		adapter = "(unknown)"
	}
	fmt.Printf("  GPU adapter: %s\n", adapter)
	ans := strings.TrimSpace(strings.ToLower(ui.Ask(in, "GPU fuse [Y/n]: ", "y")))
	if ans == "n" || ans == "no" || ans == "0" {
		fmt.Println("❌ GPU fuse required — no CPU fallback")
		return
	}

	outPath, err := GenerateFromPrompt(snap, prompt, height, width, steps, seed)
	if err != nil {
		fmt.Printf("❌ %v\n", err)
		return
	}
	fmt.Printf("✅ Wrote %s\n", outPath)
}

// RunCLI is `octo image "prompt"` — non-interactive defaults (256², 4 steps, GPU required).
func RunCLI(prompt string, height, width, steps int, seed int64) error {
	if strings.TrimSpace(prompt) == "" {
		return fmt.Errorf("empty prompt")
	}
	if !webgpu.Available() {
		err := webgpu.InitError()
		if err == nil {
			err = fmt.Errorf("no adapter")
		}
		return fmt.Errorf("GPU required for image gen: %w", err)
	}
	snap := paths.ManualSnapshotDir(paths.HubRoot(), defaultImageRepo)
	if st, err := os.Stat(snap); err != nil || !st.IsDir() {
		return fmt.Errorf("no snapshot at %s — run octo [2]/[7] download %s", snap, defaultImageRepo)
	}
	if height <= 0 {
		height = 256
	}
	if width <= 0 {
		width = 256
	}
	if steps <= 0 {
		steps = 4
	}
	outPath, err := GenerateFromPrompt(snap, prompt, height, width, steps, seed)
	if err != nil {
		return err
	}
	fmt.Printf("✅ Wrote %s\n", outPath)
	return nil
}

// GenerateFromPrompt encodes the prompt, loads transformer+VAE on GPU, denoises, writes PNG.
func GenerateFromPrompt(snap, prompt string, height, width, steps int, seed int64) (string, error) {
	if !webgpu.Available() {
		err := webgpu.InitError()
		if err == nil {
			err = fmt.Errorf("no adapter")
		}
		return "", fmt.Errorf("GPU required: %w", err)
	}
	fmt.Printf("  GPU fuse on (%s)\n", webgpu.AdapterName())

	fmt.Println("  Encoding prompt (Qwen3 Affine4 → GPU)…")
	t0 := time.Now()
	embeds, txtSeq, err := flux2.EncodePrompt(snap, prompt, 0)
	if err != nil {
		return "", fmt.Errorf("encode: %w", err)
	}
	fmt.Printf("  embeds seq=%d dim=%d in %v\n", txtSeq, len(embeds)/txtSeq, time.Since(t0).Round(time.Millisecond))

	fmt.Println("  Loading Flux2 transformer…")
	t0 = time.Now()
	model, err := flux2.LoadTransformerFromMLX(snap)
	if err != nil {
		return "", fmt.Errorf("transformer: %w", err)
	}
	defer model.CloseGPU()
	fmt.Printf("  transformer ok in %v\n", time.Since(t0).Round(time.Millisecond))

	if err := model.SyncGPU(); err != nil {
		return "", fmt.Errorf("transformer SyncGPU: %w", err)
	}

	pipe := flux2.NewPipeline(model)
	fmt.Println("  Loading VAE…")
	if err := pipe.LoadVAE(snap); err != nil {
		return "", fmt.Errorf("vae: %w", err)
	}
	side := height
	if width > side {
		side = width
	}
	if err := pipe.VAE.SyncGPU(side); err != nil {
		return "", fmt.Errorf("VAE SyncGPU: %w", err)
	}
	defer pipe.VAE.CloseGPU()

	fmt.Printf("  Generating %dx%d steps=%d seed=%d…\n", width, height, steps, seed)
	t0 = time.Now()
	pngBytes, err := pipe.Generate(embeds, txtSeq, height, width, steps, seed)
	if err != nil {
		return "", fmt.Errorf("generate: %w", err)
	}
	fmt.Printf("  denoise+VAE done in %v\n", time.Since(t0).Round(time.Millisecond))

	outDir := paths.OutputsDir()
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", err
	}
	name := fmt.Sprintf("bonsai_%s.png", time.Now().Format("20060102-150405"))
	outPath := filepath.Join(outDir, name)
	if err := os.WriteFile(outPath, pngBytes, 0o644); err != nil {
		return "", err
	}
	_ = os.WriteFile(outPath+".txt", []byte(prompt+"\n"), 0o644)
	return outPath, nil
}

func resolveSnapshot(in *bufio.Reader) (string, error) {
	repo := ui.Ask(in, "Repo [org/name]: ", defaultImageRepo)
	repo = normalizeRepo(repo)
	if repo == "" {
		return "", fmt.Errorf("need org/name")
	}
	snap := paths.ManualSnapshotDir(paths.HubRoot(), repo)
	if st, err := os.Stat(snap); err != nil || !st.IsDir() {
		return "", fmt.Errorf("no snapshot at %s — download via menu [2] or [7]", snap)
	}
	return snap, nil
}

func snapshotStatus(snap string) string {
	parts := []string{}
	if flux2.IsFlux2KleinPipeline(snap) {
		parts = append(parts, "Flux2Klein")
	}
	check := func(label, rel string) {
		if _, err := os.Stat(filepath.Join(snap, rel)); err == nil {
			parts = append(parts, label+"=ok")
		} else {
			parts = append(parts, label+"=missing")
		}
	}
	check("transformer", "transformer-packed-mflux/diffusion_pytorch_model.safetensors")
	check("vae", "vae/diffusion_pytorch_model.safetensors")
	if te, err := flux2.FindTextEncoderDir(snap); err == nil {
		if st, e := os.Stat(filepath.Join(te, "model.safetensors")); e == nil && st.Size() > 0 {
			parts = append(parts, "text_encoder=ok")
		} else {
			parts = append(parts, "text_encoder=incomplete")
		}
	} else {
		parts = append(parts, "text_encoder=missing")
	}
	return strings.Join(parts, ", ")
}

func normalizeRepo(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "https://huggingface.co/")
	s = strings.Trim(s, "/")
	return s
}
