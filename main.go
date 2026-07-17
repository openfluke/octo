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
	"github.com/openfluke/octo/internal/tested"
	"github.com/openfluke/octo/internal/ui"
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
			text, ref, frames, seed, doSample, fuseSIMD, fuseGPU := parseSpeakArgs(os.Args[2:])
			if text == "" {
				fmt.Fprintln(os.Stderr, `usage: octo speak "text" [--ref wav] [--frames 300] [--seed 42] [--greedy] [--simd] [--gpu]`)
				os.Exit(2)
			}
			if err := speech.RunCLI(text, ref, frames, doSample, seed, fuseSIMD, fuseGPU); err != nil {
				fmt.Fprintf(os.Stderr, "speak: %v\n", err)
				os.Exit(1)
			}
			return
		case "serve":
			addr := ":7878"
			for i := 2; i < len(os.Args); i++ {
				a := os.Args[i]
				if (a == "--addr" || a == "-a") && i+1 < len(os.Args) {
					i++
					addr = os.Args[i]
				} else if strings.HasPrefix(a, ":") || strings.Contains(a, ".") {
					addr = a
				}
			}
			if err := serve.Run(serve.Options{Addr: addr}); err != nil {
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

func parseSpeakArgs(args []string) (text, ref string, frames int, seed int64, doSample, fuseSIMD, fuseGPU bool) {
	frames, seed, doSample, fuseSIMD = 300, 42, true, true
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--ref":
			i++
			if i < len(args) {
				ref = args[i]
			}
		case a == "--frames":
			i++
			if i < len(args) {
				frames, _ = strconv.Atoi(args[i])
			}
		case a == "--seed":
			i++
			if i < len(args) {
				seed, _ = strconv.ParseInt(args[i], 10, 64)
			}
		case a == "--greedy":
			doSample = false
		case a == "--simd":
			fuseSIMD = true
		case a == "--no-simd":
			fuseSIMD = false
		case a == "--gpu":
			fuseGPU = true
		case a == "--no-gpu":
			fuseGPU = false
		case strings.HasPrefix(a, "-"):
			// ignore
		default:
			if text == "" {
				text = a
			} else {
				text += " " + a
			}
		}
	}
	return text, ref, frames, seed, doSample, fuseSIMD, fuseGPU
}

func printHelp() {
	fmt.Println("Octo — Welvet model shell")
	fmt.Println("  ./octo                         interactive menu")
	fmt.Println("  ./octo image \"prompt\" [opts]    generate PNG → octo_outputs/ (GPU required)")
	fmt.Println("      -h/-w/-s N  --seed N")
	fmt.Println("  ./octo speak \"text\" [opts]      generate WAV → octo_outputs/")
	fmt.Println("      --ref wav  --frames N  --seed N  --greedy  --simd/--no-simd  --gpu")
	fmt.Println("  ./octo serve [--addr :7878]    host octo_entities for FinchKit phones")
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
		fmt.Println("  [9] Generate speech (MOSS-TTS-Nano)")
		fmt.Println("  [0] Serve .entity CDN (FinchKit phones)")
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
		case "0", "s", "serve":
			addr := ui.Ask(in, "Listen addr [:7878]: ", ":7878")
			if err := serve.Run(serve.Options{Addr: addr}); err != nil {
				fmt.Printf("❌ %v\n", err)
			}
		case "q", "quit", "exit":
			fmt.Println("bye")
			return
		default:
			fmt.Println("Invalid choice")
		}
		fmt.Println()
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
