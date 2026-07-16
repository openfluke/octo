package main

import (
	"bufio"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/openfluke/octo/internal/catalog"
	"github.com/openfluke/octo/internal/convert"
	"github.com/openfluke/octo/internal/hub"
	"github.com/openfluke/octo/internal/paths"
	"github.com/openfluke/octo/internal/run"
	"github.com/openfluke/octo/internal/ui"
)

func main() {
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
		case "q", "quit", "exit":
			fmt.Println("bye")
			return
		default:
			fmt.Println("Invalid choice")
		}
		fmt.Println()
	}
}
