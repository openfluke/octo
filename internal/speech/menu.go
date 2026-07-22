package speech

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/openfluke/octo/internal/hub"
	"github.com/openfluke/octo/internal/paths"
	"github.com/openfluke/octo/internal/ui"
	"github.com/openfluke/welvet/apps/mosstts"
	"github.com/openfluke/welvet/apps/qwentts"
	"github.com/openfluke/welvet/simd"
	"github.com/openfluke/welvet/webgpu"
)

const (
	defaultMossRepo = "OpenMOSS-Team/MOSS-TTS-Nano-100M"
	defaultQwenRepo = "Qwen/Qwen3-TTS-12Hz-0.6B-CustomVoice"
	qwen17BRepo     = "Qwen/Qwen3-TTS-12Hz-1.7B-CustomVoice"
)

// Menu runs interactive text → WAV generation (MOSS or Qwen3-TTS).
func Menu(in *bufio.Reader) {
	fmt.Println("\nGenerate speech")
	fmt.Println("  Engines: moss (native Welvet) · qwen (native Welvet Qwen3-TTS)")
	engine := strings.ToLower(strings.TrimSpace(ui.Ask(in, "Engine [moss/qwen]: ", "qwen")))
	if engine == "" {
		engine = "qwen"
	}
	switch engine {
	case "qwen", "qwen3", "qwentts":
		menuQwen(in)
	default:
		menuMoss(in)
	}
}

func menuMoss(in *bufio.Reader) {
	fmt.Println("\nMOSS-TTS-Nano (native)")
	snap, err := resolveMossSnapshot(in)
	if err != nil {
		fmt.Printf("❌ %v\n", err)
		return
	}
	fmt.Printf("  snapshot: %s\n", snap)
	fmt.Printf("  status: %s\n", mossSnapshotStatus(snap))

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

	outPath, err := GenerateMoss(snap, text, ref, maxFrames, doSample, seed, fuseSIMD, fuseGPU)
	if err != nil {
		fmt.Printf("❌ %v\n", err)
		return
	}
	fmt.Printf("✅ Wrote %s\n", outPath)
}

func menuQwen(in *bufio.Reader) {
	fmt.Println("\nQwen3-TTS-12Hz CustomVoice (native Welvet)")
	fmt.Println("  Presets: 0.6b · 1.7b · or paste Qwen/… repo id")
	choice := strings.TrimSpace(ui.Ask(in, "Model [0.6b]: ", "0.6b"))
	repo := resolveQwenRepo(choice)
	dl := strings.TrimSpace(strings.ToLower(ui.Ask(in, "Download if missing [Y/n]: ", "y")))
	snap, err := ensureQwenSnapshot(repo, dl != "n" && dl != "no" && dl != "0")
	if err != nil {
		fmt.Printf("❌ %v\n", err)
		return
	}
	fmt.Printf("  repo: %s\n", repo)
	fmt.Printf("  snapshot: %s\n", snap)

	text := ui.Ask(in, "Text: ", "Hello from Octo.")
	if strings.TrimSpace(text) == "" {
		fmt.Println("Need text")
		return
	}
	fmt.Println("  Speakers: Ryan · Aiden · Vivian · Serena · Uncle_Fu · Dylan · Eric · Ono_Anna · Sohee")
	speaker := ui.Ask(in, "Speaker [Ryan]: ", "Ryan")
	language := ui.Ask(in, "Language [English]: ", "English")
	instruct := strings.TrimSpace(ui.Ask(in, "Instruct (optional, 1.7B): ", ""))
	fmt.Println("  Tip: ~12 frames ≈ 1s audio. Short paragraph ≈ 200–300 frames; Max 50 ≈ first few words only.")
	maxFrames, _ := strconv.Atoi(ui.Ask(in, "Max frames [2048]: ", "2048"))
	seed, _ := strconv.ParseInt(ui.Ask(in, "Seed [42]: ", "42"), 10, 64)
	sampleAns := strings.TrimSpace(strings.ToLower(ui.Ask(in, "Sample [Y/n]: ", "y")))
	doSample := sampleAns != "n" && sampleAns != "no" && sampleAns != "0"
	if maxFrames <= 0 {
		maxFrames = 2048
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

	gpuAvail := webgpu.Available()
	gpuAns := strings.TrimSpace(strings.ToLower(ui.Ask(in, "GPU fuse [y/N]: ", "n")))
	fuseGPU := gpuAns == "y" || gpuAns == "yes" || gpuAns == "1"
	if fuseGPU && !gpuAvail {
		err := webgpu.InitError()
		if err == nil {
			err = fmt.Errorf("no adapter")
		}
		fmt.Printf("❌ GPU fuse requested but unavailable: %v\n", err)
		return
	}

	outPath, err := GenerateQwen(snap, text, speaker, language, instruct, maxFrames, doSample, seed, fuseSIMD, fuseGPU)
	if err != nil {
		fmt.Printf("❌ %v\n", err)
		return
	}
	fmt.Printf("✅ Wrote %s\n", outPath)
}

// SpeakCLIOpts is the non-interactive speak surface.
type SpeakCLIOpts struct {
	Engine   string // moss | qwen
	Model    string // repo alias or org/name (qwen)
	Text     string
	Ref      string
	Speaker  string
	Language string
	Instruct string
	Frames   int
	Seed     int64
	DoSample bool
	FuseSIMD bool
	FuseGPU  bool
	Download bool
}

// RunCLI is `octo speak "text" [--engine moss|qwen] …`.
func RunCLI(o SpeakCLIOpts) error {
	if strings.TrimSpace(o.Text) == "" {
		return fmt.Errorf("empty text")
	}
	engine := strings.ToLower(strings.TrimSpace(o.Engine))
	if engine == "" {
		engine = "moss"
	}
	if o.Frames <= 0 {
		o.Frames = 300
	}

	switch engine {
	case "qwen", "qwen3", "qwentts":
		repo := resolveQwenRepo(o.Model)
		snap, err := ensureQwenSnapshot(repo, o.Download)
		if err != nil {
			return err
		}
		speaker := o.Speaker
		if speaker == "" {
			speaker = "Ryan"
		}
		language := o.Language
		if language == "" {
			language = "English"
		}
		outPath, err := GenerateQwen(snap, o.Text, speaker, language, o.Instruct, o.Frames, o.DoSample, o.Seed, o.FuseSIMD, o.FuseGPU)
		if err != nil {
			return err
		}
		fmt.Printf("✅ Wrote %s\n", outPath)
		return nil

	default:
		snap := paths.ManualSnapshotDir(paths.HubRoot(), defaultMossRepo)
		if st, err := os.Stat(snap); err != nil || !st.IsDir() {
			return fmt.Errorf("no snapshot at %s — download/convert MOSS first", snap)
		}
		if o.FuseSIMD && !simd.Enabled() {
			o.FuseSIMD = false
		}
		if o.FuseGPU && !webgpu.Available() {
			err := webgpu.InitError()
			if err == nil {
				err = fmt.Errorf("no adapter")
			}
			return fmt.Errorf("GPU fuse: %w", err)
		}
		outPath, err := GenerateMoss(snap, o.Text, o.Ref, o.Frames, o.DoSample, o.Seed, o.FuseSIMD, o.FuseGPU)
		if err != nil {
			return err
		}
		fmt.Printf("✅ Wrote %s\n", outPath)
		return nil
	}
}

// GenerateMoss loads MOSS pipeline and writes WAV under octo_outputs/.
func GenerateMoss(snap, text, ref string, maxFrames int, doSample bool, seed int64, fuseSIMD, fuseGPU bool) (string, error) {
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

// GenerateQwen loads native qwentts and writes WAV under octo_outputs/.
func GenerateQwen(snap, text, speaker, language, instruct string, maxFrames int, doSample bool, seed int64, fuseSIMD, fuseGPU bool) (string, error) {
	fmt.Println("  Loading Qwen3-TTS (native Welvet)…")
	t0 := time.Now()
	pipe, err := qwentts.LoadPipeline(snap)
	if err != nil {
		return "", err
	}
	fmt.Printf("  loaded in %v\n", time.Since(t0).Round(time.Millisecond))
	fmt.Printf("  fuse: simd=%v gpu=%v\n", fuseSIMD, fuseGPU)

	fmt.Printf("  Generating (speaker=%s language=%s frames=%d)…\n", speaker, language, maxFrames)
	t0 = time.Now()
	outDir := paths.OutputsDir()
	path, err := pipe.SpeakToFile(text, outDir, qwentts.SpeakOpts{
		MaxNewFrames: maxFrames,
		DoSample:     doSample,
		Seed:         seed,
		Speaker:      speaker,
		Language:     language,
		Instruct:     instruct,
		FuseSIMD:     fuseSIMD,
		FuseGPU:      fuseGPU,
	})
	if err != nil {
		return "", err
	}
	fmt.Printf("  done in %v\n", time.Since(t0).Round(time.Millisecond))
	return path, nil
}

func resolveMossSnapshot(in *bufio.Reader) (string, error) {
	repo := ui.Ask(in, "Repo [org/name]: ", defaultMossRepo)
	repo = normalizeRepo(repo)
	if repo == "" {
		return "", fmt.Errorf("need org/name")
	}
	snap := paths.ManualSnapshotDir(paths.HubRoot(), repo)
	if st, err := os.Stat(snap); err != nil || !st.IsDir() {
		return "", fmt.Errorf("no snapshot at %s — download/convert first (see welvet/apps/mosstts/README.md)", snap)
	}
	return snap, nil
}

func resolveQwenRepo(s string) string {
	s = normalizeRepo(s)
	switch strings.ToLower(s) {
	case "", "0.6b", "0.6", "06b", "qwen-0.6b":
		return defaultQwenRepo
	case "1.7b", "1.7", "17b", "qwen-1.7b":
		return qwen17BRepo
	default:
		if strings.Contains(s, "/") {
			return s
		}
		return defaultQwenRepo
	}
}

func ensureQwenSnapshot(repo string, download bool) (string, error) {
	snap := paths.ManualSnapshotDir(paths.HubRoot(), repo)
	if qwenSnapReady(snap) {
		return snap, nil
	}
	if !download {
		return "", fmt.Errorf("no Qwen3-TTS snapshot at %s — re-run with --download or menu download", snap)
	}
	fmt.Printf("  downloading %s into OCTO_HUB…\n", repo)
	if _, err := hub.DownloadRepo(repo); err != nil {
		return "", err
	}
	if !qwenSnapReady(snap) {
		return "", fmt.Errorf("download finished but snapshot incomplete: %s (need config.json + model.safetensors + speech_tokenizer/)", snap)
	}
	return snap, nil
}

func qwenSnapReady(snap string) bool {
	need := []string{
		"config.json",
		"model.safetensors",
		"vocab.json",
		"merges.txt",
		filepath.Join("speech_tokenizer", "config.json"),
		filepath.Join("speech_tokenizer", "model.safetensors"),
	}
	for _, rel := range need {
		if _, err := os.Stat(filepath.Join(snap, rel)); err != nil {
			return false
		}
	}
	return true
}

func mossSnapshotStatus(snap string) string {
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
