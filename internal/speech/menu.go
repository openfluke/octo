package speech

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
	"github.com/openfluke/welvet/mosstts"
	"github.com/openfluke/welvet/simd"
	"github.com/openfluke/welvet/webgpu"
)

const defaultTTSRepo = "OpenMOSS-Team/MOSS-TTS-Nano-100M"

// Menu runs interactive text → WAV generation.
func Menu(in *bufio.Reader) {
	fmt.Println("\nGenerate speech (MOSS-TTS-Nano)")
	snap, err := resolveSnapshot(in)
	if err != nil {
		fmt.Printf("❌ %v\n", err)
		return
	}
	fmt.Printf("  snapshot: %s\n", snap)
	fmt.Printf("  status: %s\n", snapshotStatus(snap))

	text := ui.Ask(in, "Text: ", "Hello from Octo.")
	if strings.TrimSpace(text) == "" {
		fmt.Println("Need text")
		return
	}
	ref := strings.TrimSpace(ui.Ask(in, "Reference WAV (optional, clone): ", ""))
	maxFrames, _ := strconv.Atoi(ui.Ask(in, "Max frames [300]: ", "300"))
	seed, _ := strconv.ParseInt(ui.Ask(in, "Seed [42]: ", "42"), 10, 64)
	sampleAns := strings.TrimSpace(strings.ToLower(ui.Ask(in, "Sample [Y/n]: ", "y")))
	doSample := sampleAns != "n" && sampleAns != "no" && sampleAns != "0"
	if maxFrames <= 0 {
		maxFrames = 300
	}

	simdDef := "y"
	if !simd.Enabled() {
		simdDef = "n"
	}
	simdAns := strings.TrimSpace(strings.ToLower(ui.Ask(in, fmt.Sprintf("SIMD fuse [%s]: ", map[bool]string{true: "Y/n", false: "y/N"}[simd.Enabled()]), simdDef)))
	fuseSIMD := simdAns != "n" && simdAns != "no" && simdAns != "0"
	if fuseSIMD && !simd.Enabled() {
		fmt.Println("  (SIMD kernels unavailable on this GOARCH — using scalar host)")
		fuseSIMD = false
	}

	// Default GPU off: per-GEMV WebGPU submit/readback is far slower than SIMD for
	// decode-step mats; "hang" is usually that path with no progress for a long time.
	gpuAvail := webgpu.Available()
	gpuAns := strings.TrimSpace(strings.ToLower(ui.Ask(in, "GPU fuse [y/N] (slow for TTS): ", "n")))
	fuseGPU := gpuAns == "y" || gpuAns == "yes" || gpuAns == "1"
	if fuseGPU && !gpuAvail {
		err := webgpu.InitError()
		if err == nil {
			err = fmt.Errorf("no adapter")
		}
		fmt.Printf("❌ GPU fuse requested but unavailable: %v\n", err)
		return
	}
	if fuseGPU {
		fmt.Println("  note: GPU fuse = resident GPT-2 decode (one submit/token); LM heads stay on SIMD")
	}

	outPath, err := Generate(snap, text, ref, maxFrames, doSample, seed, fuseSIMD, fuseGPU)
	if err != nil {
		fmt.Printf("❌ %v\n", err)
		return
	}
	fmt.Printf("✅ Wrote %s\n", outPath)
}

// RunCLI is `octo speak "text" [--ref wav] [--frames N] [--seed N] [--greedy] [--simd] [--gpu]`.
func RunCLI(text, ref string, maxFrames int, doSample bool, seed int64, fuseSIMD, fuseGPU bool) error {
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("empty text")
	}
	snap := paths.ManualSnapshotDir(paths.HubRoot(), defaultTTSRepo)
	if st, err := os.Stat(snap); err != nil || !st.IsDir() {
		return fmt.Errorf("no snapshot at %s — convert via octo/tools/moss_convert and place under octo_hub", snap)
	}
	if maxFrames <= 0 {
		maxFrames = 300
	}
	if fuseSIMD && !simd.Enabled() {
		fuseSIMD = false
	}
	if fuseGPU && !webgpu.Available() {
		err := webgpu.InitError()
		if err == nil {
			err = fmt.Errorf("no adapter")
		}
		return fmt.Errorf("GPU fuse: %w", err)
	}
	outPath, err := Generate(snap, text, ref, maxFrames, doSample, seed, fuseSIMD, fuseGPU)
	if err != nil {
		return err
	}
	fmt.Printf("✅ Wrote %s\n", outPath)
	return nil
}

// Generate loads pipeline and writes WAV under octo_outputs/.
func Generate(snap, text, ref string, maxFrames int, doSample bool, seed int64, fuseSIMD, fuseGPU bool) (string, error) {
	fmt.Println("  Loading MOSS-TTS-Nano + audio tokenizer…")
	t0 := time.Now()
	pipe, err := mosstts.LoadPipeline(snap)
	if err != nil {
		return "", err
	}
	fmt.Printf("  loaded in %v\n", time.Since(t0).Round(time.Millisecond))
	fmt.Printf("  fuse: simd=%v gpu=%v\n", fuseSIMD, fuseGPU)

	fmt.Printf("  Generating speech (max_frames=%d sample=%v)…\n", maxFrames, doSample)
	t0 = time.Now()
	outDir := paths.OutputsDir()
	path, err := pipe.SpeakToFile(text, outDir, mosstts.SpeakOpts{
		MaxNewFrames: maxFrames,
		DoSample:     doSample,
		Seed:         seed,
		RefWAV:       ref,
		FuseSIMD:     fuseSIMD,
		FuseGPU:      fuseGPU,
	})
	if err != nil {
		return "", err
	}
	fmt.Printf("  done in %v\n", time.Since(t0).Round(time.Millisecond))
	return path, nil
}

func resolveSnapshot(in *bufio.Reader) (string, error) {
	repo := ui.Ask(in, "Repo [org/name]: ", defaultTTSRepo)
	repo = normalizeRepo(repo)
	if repo == "" {
		return "", fmt.Errorf("need org/name")
	}
	snap := paths.ManualSnapshotDir(paths.HubRoot(), repo)
	if st, err := os.Stat(snap); err != nil || !st.IsDir() {
		return "", fmt.Errorf("no snapshot at %s — download/convert first (see welvet/mosstts/README.md)", snap)
	}
	return snap, nil
}

func snapshotStatus(snap string) string {
	parts := []string{}
	check := func(label, rel string) {
		if _, err := os.Stat(filepath.Join(snap, rel)); err == nil {
			parts = append(parts, label+"=ok")
		} else {
			parts = append(parts, label+"=missing")
		}
	}
	check("model", "model.safetensors")
	check("tokenizer", "tokenizer.model")
	check("config", "config.json")
	if d, err := mosstts.FindAudioTokenizerDir(snap); err == nil {
		parts = append(parts, "audio_tokenizer=ok:"+filepath.Base(filepath.Dir(filepath.Dir(d))))
	} else {
		parts = append(parts, "audio_tokenizer=missing")
	}
	return strings.Join(parts, ", ")
}

func normalizeRepo(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "https://huggingface.co/")
	s = strings.Trim(s, "/")
	return s
}
