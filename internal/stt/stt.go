package stt

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/openfluke/octo/internal/paths"
	"github.com/openfluke/octo/internal/ui"
	"github.com/openfluke/welvet/model/wav2vec2"
)

const defaultASRRepo = "facebook/wav2vec2-base-960h"

var (
	modelMu sync.Mutex
	cached  *wav2vec2.Model
	cachedDir string
)

// Menu interactive file / mic transcription.
func Menu(in *bufio.Reader) {
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

// RunCLI handles `octo transcribe <wav>|--live [opts]`.
func RunCLI(args []string) error {
	live := false
	loop := false
	secs := 5
	modelDir := ""
	wavPath := ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--live" || a == "-l":
			live = true
		case a == "--loop":
			loop = true
		case (a == "--secs" || a == "-s") && i+1 < len(args):
			i++
			secs, _ = strconv.Atoi(args[i])
		case (a == "--model" || a == "-m") && i+1 < len(args):
			i++
			modelDir = args[i]
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
	// Prefer packed ENTITY from tested/convert flow.
	candidates = append(candidates,
		paths.EntityPathForFormat(defaultASRRepo, "none"),
		paths.EntityPath(defaultASRRepo),
	)
	hub := paths.HubRoot()
	candidates = append(candidates,
		paths.ManualSnapshotDir(hub, defaultASRRepo),
		filepath.Join(hub, paths.RepoDirName(defaultASRRepo), "snapshots", "main"),
	)
	repoRoot := filepath.Join(hub, paths.RepoDirName(defaultASRRepo), "snapshots")
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
