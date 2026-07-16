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
	"github.com/openfluke/octo/internal/paths"
	"github.com/openfluke/octo/internal/run"
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
		case "help", "-h", "--help":
			printHelp()
			return
		}
	}
	interactive()
}

func printHelp() {
	fmt.Println("Octo — Welvet model shell")
	fmt.Println("  ./octo                         interactive menu")
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
