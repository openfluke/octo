package stt

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/openfluke/octo/internal/hub"
	"github.com/openfluke/octo/internal/paths"
	"github.com/openfluke/octo/internal/ui"
	"github.com/openfluke/welvet/apps/qwenasr"
	"github.com/openfluke/welvet/model/wav2vec2"
	"github.com/openfluke/welvet/simd"
)

const (
	defaultASRRepo     = "facebook/wav2vec2-base-960h"
	defaultQwenASRRepo = "Qwen/Qwen3-ASR-0.6B"
	qwenASR17BRepo     = "Qwen/Qwen3-ASR-1.7B"
)

var (
	modelMu   sync.Mutex
	cached    *wav2vec2.Model
	cachedDir string

	qwenMu    sync.Mutex
	qwenPipe  *qwenasr.Pipeline
	qwenSnap  string
)

// Menu interactive file / mic transcription (wav2vec2 or Qwen3-ASR).
func Menu(in *bufio.Reader) {
	fmt.Println("\nTranscribe speech")
	fmt.Println("  Engines: wav2vec2 (CTC) · qwen (native Welvet Qwen3-ASR)")
	engine := strings.ToLower(strings.TrimSpace(ui.Ask(in, "Engine [wav2vec2/qwen]: ", "qwen")))
	if engine == "" {
		engine = "qwen"
	}
	switch engine {
	case "qwen", "qwen3", "qwenasr", "asr":
		menuQwen(in)
	default:
		menuWav2Vec(in)
	}
}

func menuWav2Vec(in *bufio.Reader) {
	fmt.Println("\nTranscribe (wav2vec2-base-960h CTC)")
	fmt.Println("  Note: offline CTC — “live” = record a clip, then decode (not streaming tokens).")
	mode := strings.TrimSpace(strings.ToLower(ui.Ask(in, "Mode [file/live]: ", "live")))
	dir, err := ResolveModelDir("")
	if err != nil {
		fmt.Printf("❌ %v\n", err)
		fmt.Println("  Place HF snapshot / .entity under octo_hub, or set OCTO_WAV2VEC2")
		return
	}
	fmt.Printf("  model: %s\n", dir)

	m, err := loadModel(dir)
	if err != nil {
		fmt.Printf("❌ load: %v\n", err)
		return
	}

	switch mode {
	case "f", "file", "wav":
		path := strings.TrimSpace(ui.Ask(in, "WAV path: ", ""))
		if path == "" {
			fmt.Println("Need a WAV path")
			return
		}
		runFile(m, path)
	default:
		secs, _ := strconv.Atoi(ui.Ask(in, "Seconds per clip [5]: ", "5"))
		if secs <= 0 {
			secs = 5
		}
		loopAns := strings.TrimSpace(strings.ToLower(ui.Ask(in, "Loop until Enter/q [Y/n]: ", "y")))
		loop := loopAns != "n" && loopAns != "no" && loopAns != "0"
		if err := RunLive(m, secs, loop, in); err != nil {
			fmt.Printf("❌ %v\n", err)
		}
	}
}

func menuQwen(in *bufio.Reader) {
	fmt.Println("\nQwen3-ASR (native Welvet)")
	fmt.Println("  Presets: 0.6b · 1.7b · or paste Qwen/… repo id")
	choice := strings.TrimSpace(ui.Ask(in, "Model [0.6b]: ", "0.6b"))
	repo := resolveQwenASRRepo(choice)
	dl := strings.TrimSpace(strings.ToLower(ui.Ask(in, "Download if missing [Y/n]: ", "y")))
	snap, err := ensureQwenASRSnapshot(repo, dl != "n" && dl != "no" && dl != "0")
	if err != nil {
		fmt.Printf("❌ %v\n", err)
		return
	}
	fmt.Printf("  repo: %s\n", repo)
	fmt.Printf("  snapshot: %s\n", snap)

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
	maxTok, _ := strconv.Atoi(ui.Ask(in, "Max new tokens [256]: ", "256"))
	if maxTok <= 0 {
		maxTok = 256
	}

	p, err := loadQwen(snap)
	if err != nil {
		fmt.Printf("❌ load: %v\n", err)
		return
	}
	opts := qwenasr.TranscribeOpts{MaxNewTokens: maxTok, FuseSIMD: fuseSIMD}

	mode := strings.TrimSpace(strings.ToLower(ui.Ask(in, "Mode [file/live]: ", "file")))
	switch mode {
	case "l", "live", "mic":
		secs, _ := strconv.Atoi(ui.Ask(in, "Seconds per clip [5]: ", "5"))
		if secs <= 0 {
			secs = 5
		}
		if err := runQwenLive(p, opts, secs, in); err != nil {
			fmt.Printf("❌ %v\n", err)
		}
	default:
		path := strings.TrimSpace(ui.Ask(in, "WAV path: ", ""))
		if path == "" {
			fmt.Println("Need a WAV path")
			return
		}
		if err := runQwenFile(p, path, opts); err != nil {
			fmt.Printf("❌ %v\n", err)
		}
	}
}

func resolveQwenASRRepo(choice string) string {
	c := strings.ToLower(strings.TrimSpace(choice))
	switch c {
	case "", "0.6", "0.6b", "06b", "600m":
		return defaultQwenASRRepo
	case "1.7", "1.7b", "17b":
		return qwenASR17BRepo
	default:
		return strings.TrimSpace(choice)
	}
}

func ensureQwenASRSnapshot(repo string, download bool) (string, error) {
	hubRoot := paths.HubRoot()
	snap := paths.ManualSnapshotDir(hubRoot, repo)
	if qwenASRReady(snap) {
		return snap, nil
	}
	if !download {
		return "", fmt.Errorf("Qwen3-ASR snapshot missing at %s (enable download)", snap)
	}
	fmt.Printf("  Downloading %s …\n", repo)
	dir, err := hub.DownloadRepo(repo)
	if err != nil {
		return "", err
	}
	if !qwenASRReady(dir) {
		return "", fmt.Errorf("download finished but ASR files incomplete in %s", dir)
	}
	return dir, nil
}

func qwenASRReady(dir string) bool {
	need := []string{"config.json", "vocab.json", "merges.txt"}
	for _, n := range need {
		if st, err := os.Stat(filepath.Join(dir, n)); err != nil || st.IsDir() {
			return false
		}
	}
	if st, err := os.Stat(filepath.Join(dir, "model.safetensors")); err == nil && !st.IsDir() && st.Size() > 0 {
		return true
	}
	idx := filepath.Join(dir, "model.safetensors.index.json")
	if st, err := os.Stat(idx); err != nil || st.IsDir() {
		return false
	}
	b, err := os.ReadFile(idx)
	if err != nil {
		return false
	}
	// Require at least one listed shard to exist (index alone is not enough).
	type wm struct {
		WeightMap map[string]string `json:"weight_map"`
	}
	var m wm
	if json.Unmarshal(b, &m) != nil || len(m.WeightMap) == 0 {
		return false
	}
	seen := map[string]bool{}
	for _, shard := range m.WeightMap {
		if seen[shard] {
			continue
		}
		seen[shard] = true
		if st, err := os.Stat(filepath.Join(dir, shard)); err != nil || st.IsDir() || st.Size() == 0 {
			return false
		}
		return true
	}
	return false
}

func loadQwen(snap string) (*qwenasr.Pipeline, error) {
	qwenMu.Lock()
	defer qwenMu.Unlock()
	if qwenPipe != nil && qwenSnap == snap {
		return qwenPipe, nil
	}
	fmt.Printf("  Loading Qwen3-ASR from %s …\n", snap)
	t0 := time.Now()
	p, err := qwenasr.LoadPipeline(snap)
	if err != nil {
		return nil, err
	}
	fmt.Printf("  loaded in %v\n", time.Since(t0).Round(time.Millisecond))
	qwenPipe, qwenSnap = p, snap
	return p, nil
}

func runQwenFile(p *qwenasr.Pipeline, path string, opts qwenasr.TranscribeOpts) error {
	fmt.Printf("  Transcribing %s …\n", path)
	t0 := time.Now()
	text, err := p.TranscribeFile(path, opts)
	if err != nil {
		return err
	}
	fmt.Printf("  done in %v\n", time.Since(t0).Round(time.Millisecond))
	fmt.Println(text)
	return nil
}

func runQwenLive(p *qwenasr.Pipeline, opts qwenasr.TranscribeOpts, secs int, in *bufio.Reader) error {
	if _, err := exec.LookPath("arecord"); err != nil {
		return fmt.Errorf("arecord not found (install alsa-utils for mic capture)")
	}
	outDir := paths.OutputsDir()
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	wav := filepath.Join(outDir, fmt.Sprintf("stt_qwen_%s.wav", time.Now().Format("20060102_150405")))
	fmt.Printf("  Recording %ds → %s …\n", secs, wav)
	cmd := exec.Command("arecord", "-q", "-f", "S16_LE", "-r", "16000", "-c", "1", "-d", strconv.Itoa(secs), wav)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("arecord: %w", err)
	}
	return runQwenFile(p, wav, opts)
}

// RunCLI handles `octo transcribe <wav>|--live [opts]`.
func RunCLI(args []string) error {
	live := false
	loop := false
	secs := 5
	modelDir := ""
	wavPath := ""
	engine := "wav2vec2"
	download := false
	fuseSIMD := simd.Enabled()
	maxTok := 256
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--live" || a == "-l":
			live = true
		case a == "--loop":
			loop = true
		case a == "--download":
			download = true
		case (a == "--secs" || a == "-s") && i+1 < len(args):
			i++
			secs, _ = strconv.Atoi(args[i])
		case (a == "--model" || a == "-m") && i+1 < len(args):
			i++
			modelDir = args[i]
		case (a == "--engine" || a == "-e") && i+1 < len(args):
			i++
			engine = strings.ToLower(args[i])
		case a == "--simd":
			fuseSIMD = true
		case a == "--no-simd":
			fuseSIMD = false
		case (a == "--max-tokens") && i+1 < len(args):
			i++
			maxTok, _ = strconv.Atoi(args[i])
		case strings.HasPrefix(a, "-"):
			return fmt.Errorf("unknown flag %s", a)
		default:
			if wavPath == "" {
				wavPath = a
			}
		}
	}
	if secs <= 0 {
		secs = 5
	}
	if engine == "qwen" || engine == "qwen3" || engine == "qwenasr" {
		var snap string
		var err error
		if modelDir != "" && (strings.HasPrefix(modelDir, "/") || strings.HasSuffix(modelDir, "manual-download") || qwenASRReady(modelDir)) {
			snap = modelDir
			if !qwenASRReady(snap) {
				return fmt.Errorf("incomplete Qwen3-ASR snapshot: %s", snap)
			}
		} else {
			snap, err = ensureQwenASRSnapshot(resolveQwenASRRepo(modelDir), download)
			if err != nil {
				return err
			}
		}
		p, err := loadQwen(snap)
		if err != nil {
			return err
		}
		opts := qwenasr.TranscribeOpts{MaxNewTokens: maxTok, FuseSIMD: fuseSIMD}
		if live || wavPath == "" {
			return runQwenLive(p, opts, secs, bufio.NewReader(os.Stdin))
		}
		return runQwenFile(p, wavPath, opts)
	}

	dir, err := ResolveModelDir(modelDir)
	if err != nil {
		return err
	}
	m, err := loadModel(dir)
	if err != nil {
		return err
	}
	if live || wavPath == "" {
		if wavPath != "" && !live {
			return runFileErr(m, wavPath)
		}
		return RunLive(m, secs, loop, bufio.NewReader(os.Stdin))
	}
	return runFileErr(m, wavPath)
}

func runFile(m *wav2vec2.Model, path string) {
	if err := runFileErr(m, path); err != nil {
		fmt.Printf("❌ %v\n", err)
	}
}

func runFileErr(m *wav2vec2.Model, path string) error {
	fmt.Printf("  Transcribing %s …\n", path)
	t0 := time.Now()
	text, err := m.TranscribeFile(path)
	if err != nil {
		return err
	}
	fmt.Printf("  done in %v\n", time.Since(t0).Round(time.Millisecond))
	fmt.Println(text)
	return nil
}

// RunLive records from the default mic via arecord, then CTC-decodes.
func RunLive(m *wav2vec2.Model, secs int, loop bool, in *bufio.Reader) error {
	if _, err := exec.LookPath("arecord"); err != nil {
		return fmt.Errorf("arecord not found (install alsa-utils for mic capture)")
	}
	outDir := paths.OutputsDir()
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	for {
		wav := filepath.Join(outDir, fmt.Sprintf("stt_%s.wav", time.Now().Format("20060102_150405")))
		fmt.Printf("  Recording %ds → %s (Ctrl+C to abort)…\n", secs, wav)
		cmd := exec.Command("arecord", "-q", "-f", "S16_LE", "-r", "16000", "-c", "1", "-d", strconv.Itoa(secs), wav)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("arecord: %w", err)
		}
		if err := runFileErr(m, wav); err != nil {
			return err
		}
		if !loop {
			return nil
		}
		fmt.Print("  [Enter]=again  q=quit: ")
		line, _ := in.ReadString('\n')
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "q") {
			return nil
		}
	}
}

func loadModel(path string) (*wav2vec2.Model, error) {
	modelMu.Lock()
	defer modelMu.Unlock()
	if cached != nil && cachedDir == path {
		return cached, nil
	}
	fmt.Printf("  Loading wav2vec2 from %s …\n", path)
	t0 := time.Now()
	m, err := wav2vec2.LoadAuto(path)
	if err != nil {
		return nil, err
	}
	fmt.Printf("  loaded in %v\n", time.Since(t0).Round(time.Millisecond))
	cached, cachedDir = m, path
	return m, nil
}

// ResolveModelDir finds a wav2vec2 .entity or HF snapshot directory.
func ResolveModelDir(explicit string) (string, error) {
	if explicit != "" {
		if hasModelFiles(explicit) || isEntity(explicit) {
			return filepath.Clean(explicit), nil
		}
		return "", fmt.Errorf("not a wav2vec2 dir/entity: %s", explicit)
	}
	candidates := []string{}
	if v := os.Getenv("OCTO_WAV2VEC2"); v != "" {
		candidates = append(candidates, v)
	}
	if v := os.Getenv("WELVET_WAV2VEC2_DIR"); v != "" {
		candidates = append(candidates, v)
	}
	candidates = append(candidates,
		paths.EntityPathForFormat(defaultASRRepo, "none"),
		paths.EntityPath(defaultASRRepo),
	)
	hubRoot := paths.HubRoot()
	candidates = append(candidates,
		paths.ManualSnapshotDir(hubRoot, defaultASRRepo),
		filepath.Join(hubRoot, paths.RepoDirName(defaultASRRepo), "snapshots", "main"),
	)
	repoRoot := filepath.Join(hubRoot, paths.RepoDirName(defaultASRRepo), "snapshots")
	if entries, err := os.ReadDir(repoRoot); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				candidates = append(candidates, filepath.Join(repoRoot, e.Name()))
			}
		}
	}
	candidates = append(candidates,
		filepath.Join("..", "..", ".cache", "wav2vec2-base-960h"),
		filepath.Join(".", ".cache", "wav2vec2-base-960h"),
	)
	for _, c := range candidates {
		if isEntity(c) || hasModelFiles(c) {
			return filepath.Clean(c), nil
		}
	}
	return "", fmt.Errorf("wav2vec2-base-960h not found — use menu [7] Tested models, or set OCTO_WAV2VEC2")
}

func isEntity(path string) bool {
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		return false
	}
	return strings.HasSuffix(strings.ToLower(path), ".entity")
}

func hasModelFiles(dir string) bool {
	need := []string{"config.json", "vocab.json", "model.safetensors"}
	for _, n := range need {
		st, err := os.Stat(filepath.Join(dir, n))
		if err != nil || st.IsDir() {
			return false
		}
	}
	return true
}
