package main

import (
	"bufio"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/openfluke/octo/internal/bench"
	"github.com/openfluke/octo/internal/catalog"
	"github.com/openfluke/octo/internal/convert"
	"github.com/openfluke/octo/internal/hub"
	"github.com/openfluke/octo/internal/imagegen"
	"github.com/openfluke/octo/internal/paths"
	"github.com/openfluke/octo/internal/run"
	"github.com/openfluke/octo/internal/serve"
	"github.com/openfluke/octo/internal/speech"
	"github.com/openfluke/octo/internal/stt"
	"github.com/openfluke/octo/internal/tested"
	"github.com/openfluke/octo/internal/ui"
	"github.com/openfluke/welvet/model/transformer"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "bench":
			if len(os.Args) < 3 {
				fmt.Fprintln(os.Stderr, "usage: octo bench <template.json|name>")
				printTemplateList(os.Stderr)
				os.Exit(2)
			}
			path, err := bench.ResolveTemplatePath(os.Args[2])
			if err != nil {
				fmt.Fprintf(os.Stderr, "bench: %v\n", err)
				printTemplateList(os.Stderr)
				os.Exit(1)
			}
			out, err := bench.Run(path, false)
			if err != nil {
				fmt.Fprintf(os.Stderr, "bench: %v\n", err)
				os.Exit(1)
			}
			fmt.Println(out)
			return
		case "image":
			// octo image "a red bicycle" [-h 256] [-w 256] [-s 4] [--seed 42]
			prompt, h, w, steps, seed := parseImageArgs(os.Args[2:])
			if prompt == "" {
				fmt.Fprintln(os.Stderr, `usage: octo image "prompt" [-h 256] [-w 256] [-s 4] [--seed 42]`)
				os.Exit(2)
			}
			if err := imagegen.RunCLI(prompt, h, w, steps, seed); err != nil {
				fmt.Fprintf(os.Stderr, "image: %v\n", err)
				os.Exit(1)
			}
			return
		case "speak":
			o := parseSpeakArgs(os.Args[2:])
			if o.Text == "" {
				fmt.Fprintln(os.Stderr, `usage: octo speak "text" [--engine moss|qwen] [--model 0.6b|1.7b|repo]`)
				fmt.Fprintln(os.Stderr, `                 [--speaker Ryan] [--language English] [--download]`)
				fmt.Fprintln(os.Stderr, `                 [--ref wav] [--frames 300] [--seed 42] [--greedy] [--simd] [--gpu]`)
				os.Exit(2)
			}
			if err := speech.RunCLI(o); err != nil {
				fmt.Fprintf(os.Stderr, "speak: %v\n", err)
				os.Exit(1)
			}
			return
		case "transcribe", "stt":
			if err := stt.RunCLI(os.Args[2:]); err != nil {
				fmt.Fprintf(os.Stderr, "transcribe: %v\n", err)
				fmt.Fprintln(os.Stderr, `usage: octo transcribe <audio.wav> [--engine wav2vec2|qwen] [--model DIR|0.6b|1.7b]`)
				fmt.Fprintln(os.Stderr, `       octo transcribe --live [--secs 5] [--loop] [--engine …] [--model …]`)
				fmt.Fprintln(os.Stderr, `       octo transcribe --engine qwen --model 0.6b --download <wav>`)
				os.Exit(1)
			}
			return
		case "serve", "host":
			addr := ":7878"
			model := ""
			queueSize := 32
			profile := "cpu_mc"
			tileSize := 32
			for i := 2; i < len(os.Args); i++ {
				a := os.Args[i]
				if (a == "--addr" || a == "-a") && i+1 < len(os.Args) {
					i++
					addr = os.Args[i]
				} else if (a == "--model" || a == "-m") && i+1 < len(os.Args) {
					i++
					model = os.Args[i]
				} else if a == "--queue" && i+1 < len(os.Args) {
					i++
					queueSize, _ = strconv.Atoi(os.Args[i])
				} else if a == "--profile" && i+1 < len(os.Args) {
					i++
					profile = os.Args[i]
				} else if a == "--tile" && i+1 < len(os.Args) {
					i++
					tileSize, _ = strconv.Atoi(os.Args[i])
				} else if !strings.HasPrefix(a, "-") && strings.HasSuffix(strings.ToLower(a), ".entity") {
					model = a
				} else if strings.HasPrefix(a, ":") {
					addr = a
				} else if !strings.HasPrefix(a, "-") && os.Args[1] == "host" {
					model = a
				} else if strings.Contains(a, ".") {
					addr = a
				}
			}
			if os.Args[1] == "host" && model == "" {
				fmt.Fprintln(os.Stderr, "usage: octo host <model.entity|entity-id> [--addr :7878] [--queue 32]")
				os.Exit(2)
			}
			if err := serve.Run(serve.Options{
				Addr: addr, Model: model, QueueSize: queueSize,
				Profile: profile, TileSize: tileSize,
			}); err != nil {
				fmt.Fprintf(os.Stderr, "serve: %v\n", err)
				os.Exit(1)
			}
			return
		case "help", "-h", "--help":
			printHelp()
			return
		}
	}
	interactive()
}

func parseImageArgs(args []string) (prompt string, h, w, steps int, seed int64) {
	h, w, steps, seed = 256, 256, 4, 42
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-h" || a == "--height":
			i++
			if i < len(args) {
				h, _ = strconv.Atoi(args[i])
			}
		case a == "-w" || a == "--width":
			i++
			if i < len(args) {
				w, _ = strconv.Atoi(args[i])
			}
		case a == "-s" || a == "--steps":
			i++
			if i < len(args) {
				steps, _ = strconv.Atoi(args[i])
			}
		case a == "--seed":
			i++
			if i < len(args) {
				seed, _ = strconv.ParseInt(args[i], 10, 64)
			}
		case strings.HasPrefix(a, "-"):
			// ignore unknown flags
		default:
			if prompt == "" {
				prompt = a
			} else {
				prompt += " " + a
			}
		}
	}
	return prompt, h, w, steps, seed
}

func parseSpeakArgs(args []string) speech.SpeakCLIOpts {
	o := speech.SpeakCLIOpts{
		Engine:   "moss",
		Frames:   300,
		Seed:     42,
		DoSample: true,
		FuseSIMD: true,
		Speaker:  "Ryan",
		Language: "English",
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--engine":
			i++
			if i < len(args) {
				o.Engine = args[i]
			}
		case a == "--model":
			i++
			if i < len(args) {
				o.Model = args[i]
			}
		case a == "--speaker":
			i++
			if i < len(args) {
				o.Speaker = args[i]
			}
		case a == "--language":
			i++
			if i < len(args) {
				o.Language = args[i]
			}
		case a == "--instruct":
			i++
			if i < len(args) {
				o.Instruct = args[i]
			}
		case a == "--download":
			o.Download = true
		case a == "--ref":
			i++
			if i < len(args) {
				o.Ref = args[i]
			}
		case a == "--frames":
			i++
			if i < len(args) {
				o.Frames, _ = strconv.Atoi(args[i])
			}
		case a == "--seed":
			i++
			if i < len(args) {
				o.Seed, _ = strconv.ParseInt(args[i], 10, 64)
			}
		case a == "--greedy":
			o.DoSample = false
		case a == "--simd":
			o.FuseSIMD = true
		case a == "--no-simd":
			o.FuseSIMD = false
		case a == "--gpu":
			o.FuseGPU = true
		case a == "--no-gpu":
			o.FuseGPU = false
		case a == "--qwen":
			o.Engine = "qwen"
		case strings.HasPrefix(a, "-"):
			// ignore
		default:
			if o.Text == "" {
				o.Text = a
			} else {
				o.Text += " " + a
			}
		}
	}
	return o
}

func printHelp() {
	fmt.Println("Octo — Welvet model shell")
	fmt.Println("  ./octo                         interactive menu")
	fmt.Println("  ./octo image \"prompt\" [opts]    generate PNG → octo_outputs/ (GPU required)")
	fmt.Println("      -h/-w/-s N  --seed N")
	fmt.Println("  ./octo speak \"text\" [opts]      generate WAV → octo_outputs/")
	fmt.Println("      --engine moss|qwen  --model 0.6b|1.7b|repo  --download")
	fmt.Println("      --speaker Ryan  --language English")
	fmt.Println("      --ref wav  --frames N  --seed N  --greedy  --simd/--no-simd  --gpu")
	fmt.Println("  ./octo transcribe <wav>         ASR (wav2vec2 or Qwen3-ASR)")
	fmt.Println("  ./octo transcribe --live [opts] record mic clip → transcript")
	fmt.Println("      --engine wav2vec2|qwen  --model DIR|0.6b|1.7b  --download")
	fmt.Println("      --secs N  --loop  --simd/--no-simd  --max-tokens N")
	fmt.Println("  ./octo serve [--addr :7878]    host octo_entities for FinchKit phones")
	fmt.Println("  ./octo host <model.entity>      HTTP inference with a bounded request queue")
	fmt.Println("      --addr :7878  --queue 32  --profile NAME  --tile 32")
	fmt.Println("  ./octo bench <tpl.json|name>   run JSON benchmark → logs/")
	fmt.Println("  ./octo help                    this message")
	fmt.Println()
	fmt.Println("Templates are generic JSON recipes (any HF repo). See templates/.")
	printTemplateList(os.Stdout)
}

func printTemplateList(w *os.File) {
	tpls := bench.ListTemplates()
	if len(tpls) == 0 {
		fmt.Fprintf(w, "  (no templates in %s — drop any *.json there)\n", bench.TemplatesDir())
		return
	}
	fmt.Fprintf(w, "  templates in %s:\n", bench.TemplatesDir())
	for _, t := range tpls {
		fmt.Fprintf(w, "    - %s\n", t.Name)
	}
}

func interactive() {
	fmt.Println("Octo — Welvet model shell (Lucy successor)")
	fmt.Printf("  platform %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Printf("  hub      %s\n", paths.HubRoot())
	fmt.Printf("  entities %s\n", paths.EntitiesDir())
	fmt.Printf("  templates %s\n", bench.TemplatesDir())
	fmt.Println()

	in := bufio.NewReader(os.Stdin)
	for {
		fmt.Println("Octo")
		fmt.Println("  [1] Run .entity model")
		fmt.Println("  [2] Download model (Hugging Face — paste org/name)")
		fmt.Println("  [3] Convert snapshot → .entity (Safetensors / GGUF)")
		fmt.Println("  [4] List local snapshots + entities")
		fmt.Println("  [5] Quantize / re-pack existing .entity")
		fmt.Println("  [6] Run benchmark template (JSON → logs/)")
		fmt.Println("  [7] Tested models (download + convert)")
		fmt.Println("  [8] Generate image (Flux2 / Bonsai Image)")
		fmt.Println("  [9] Generate speech (MOSS / Qwen3-TTS)")
		fmt.Println("  [t] Transcribe speech (wav2vec2 / Qwen3-ASR)")
		fmt.Println("  [0] Serve .entity CDN (FinchKit phones)")
		fmt.Println("  [h] Host a mounted .entity model over HTTP")
		fmt.Println("  [q] Quit")
		choice := ui.Ask(in, "Choice: ", "")
		switch strings.ToLower(strings.TrimSpace(choice)) {
		case "1":
			run.Menu(in)
		case "2":
			hub.DownloadMenu(in)
		case "3":
			convert.Menu(in)
		case "4":
			catalog.PrintAll()
		case "5":
			convert.QuantizeMenu(in)
		case "6":
			benchMenu(in)
		case "7":
			tested.Menu(in)
		case "8":
			imagegen.Menu(in)
		case "9":
			speech.Menu(in)
		case "t", "transcribe", "stt":
			stt.Menu(in)
		case "0", "s", "serve":
			addr := ui.Ask(in, "Listen addr [:7878]: ", ":7878")
			if err := serve.Run(serve.Options{Addr: addr}); err != nil {
				fmt.Printf("❌ %v\n", err)
			}
		case "h", "host":
			hostMenu(in)
		case "q", "quit", "exit":
			fmt.Println("bye")
			return
		default:
			fmt.Println("Invalid choice")
		}
		fmt.Println()
	}
}

func hostMenu(in *bufio.Reader) {
	fmt.Println("\nHost .entity model over HTTP")
	ents := catalog.ListEntities()
	if len(ents) == 0 {
		fmt.Println("No .entity files. Download and convert a model first.")
		return
	}
	for i, e := range ents {
		fmt.Printf("  [%d] %s  (%d bytes)\n", i+1, e.RepoID, e.Bytes)
	}
	choice := ui.Ask(in, "Model: ", "1")
	idx, err := strconv.Atoi(strings.TrimSpace(choice))
	if err != nil || idx < 1 || idx > len(ents) {
		fmt.Println("Invalid model")
		return
	}

	fmt.Println("\nExecution profile")
	profiles := transformer.NamedProfiles()
	for i, candidate := range profiles {
		cores := "single-core"
		if candidate.MultiCore {
			cores = "multicore"
		}
		note := ""
		switch candidate.Name {
		case "gpu":
			note = "  host attention + WebGPU matrix operations"
		case "simd_fuse":
			note = "  fused quantized CPU"
		case "gpu_fuse":
			note = "  full fused model on GPU"
		}
		fmt.Printf("  [%d] %-10s %s, %s%s\n",
			i+1, candidate.Name, candidate.Backend.String(), cores, note)
	}
	defaultProfile := "4" // simd_mc for general models
	if tr, ok := ents[idx-1].Meta["transformer"].(map[string]any); ok {
		arch, _ := tr["architecture"].(string)
		if arch == "qwen35_hybrid" || arch == "qwen3_dense" {
			defaultProfile = "7" // gpu_fuse for Bonsai/Qwen3 BinaryG128
		}
	}
	profileChoice := ui.Ask(in, "Profile: ", defaultProfile)
	profileIdx, err := strconv.Atoi(strings.TrimSpace(profileChoice))
	if err != nil || profileIdx < 1 || profileIdx > len(profiles) {
		fmt.Println("Invalid profile")
		return
	}
	profile := profiles[profileIdx-1].Name

	tileText := ui.Ask(in, "Dense MatVec tile size [32]: ", "32")
	tileSize, err := strconv.Atoi(strings.TrimSpace(tileText))
	if err != nil || tileSize <= 0 {
		fmt.Println("Invalid tile size")
		return
	}

	addr := ui.Ask(in, "Listen addr [:7878]: ", ":7878")
	queueText := ui.Ask(in, "Queue size [32]: ", "32")
	queueSize, err := strconv.Atoi(strings.TrimSpace(queueText))
	if err != nil || queueSize <= 0 {
		fmt.Println("Invalid queue size")
		return
	}
	if err := serve.Run(serve.Options{
		Addr: addr, Model: ents[idx-1].Path, QueueSize: queueSize,
		Profile: profile, TileSize: tileSize,
	}); err != nil {
		fmt.Printf("❌ %v\n", err)
	}
}

func benchMenu(in *bufio.Reader) {
	fmt.Println("\nBenchmark template (JSON)")
	fmt.Println("  Any HF model — drop recipes in templates/*.json")
	tpls := bench.ListTemplates()
	if len(tpls) == 0 {
		fmt.Printf("  (none yet under %s)\n", bench.TemplatesDir())
		path := ui.Ask(in, "Template path: ", "")
		if path == "" {
			return
		}
		runBench(path)
		return
	}
	for i, t := range tpls {
		fmt.Printf("  [%d] %s\n", i+1, t.Name)
	}
	fmt.Println("  [p] Paste path / name")
	choice := ui.Ask(in, "Choice: ", "1")
	var path string
	if strings.EqualFold(choice, "p") {
		path = ui.Ask(in, "Template path or name: ", "")
	} else {
		idx, err := strconv.Atoi(choice)
		if err != nil || idx < 1 || idx > len(tpls) {
			fmt.Println("Invalid")
			return
		}
		path = tpls[idx-1].Path
	}
	if path == "" {
		return
	}
	runBench(path)
}

func runBench(arg string) {
	path, err := bench.ResolveTemplatePath(arg)
	if err != nil {
		fmt.Printf("❌ %v\n", err)
		return
	}
	out, err := bench.Run(path, false)
	if err != nil {
		fmt.Printf("❌ %v\n", err)
		return
	}
	fmt.Printf("✅ Wrote %s\n", out)
}
