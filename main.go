package main

import (
	"bufio"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/openfluke/octo/internal/bench"
	"github.com/openfluke/octo/internal/catalog"
	"github.com/openfluke/octo/internal/convert"
	"github.com/openfluke/octo/internal/hub"
	"github.com/openfluke/octo/internal/paths"
	"github.com/openfluke/octo/internal/run"
	"github.com/openfluke/octo/internal/ui"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "bench":
			path := "templates/smol2_135m_all.json"
			if len(os.Args) > 2 {
				path = os.Args[2]
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
	fmt.Println("  ./octo              interactive menu")
	fmt.Println("  ./octo bench [tpl]  run JSON benchmark template → logs/")
	fmt.Println("  ./octo help         this message")
}

func interactive() {
	fmt.Println("Octo — Welvet model shell (Lucy successor)")
	fmt.Printf("  platform %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Printf("  hub      %s\n", paths.HubRoot())
	fmt.Printf("  entities %s\n", paths.EntitiesDir())
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
	fmt.Println("  [1] templates/smol2_135m_all.json  (all quants × all profiles)")
	fmt.Println("  [p] Paste path to template JSON")
	choice := ui.Ask(in, "Choice: ", "1")
	path := "templates/smol2_135m_all.json"
	if strings.EqualFold(choice, "p") {
		path = ui.Ask(in, "Template path: ", path)
	}
	if path == "" {
		return
	}
	out, err := bench.Run(path, false)
	if err != nil {
		fmt.Printf("❌ %v\n", err)
		return
	}
	fmt.Printf("✅ Wrote %s\n", out)
}
