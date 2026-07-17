package tested

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/openfluke/octo/internal/convert"
	"github.com/openfluke/octo/internal/hub"
	"github.com/openfluke/octo/internal/paths"
	"github.com/openfluke/octo/internal/ui"
)

// Menu lists known-good models; download + convert one, several, or all.
func Menu(in *bufio.Reader) {
	fmt.Println("\nTested models (download + convert → .entity)")
	fmt.Println("  Known-good Welvet/Octo paths — pick one, comma list, or all.")
	for i, m := range Models {
		status := entityStatus(m.Repo)
		fmt.Printf("  [%d] %-22s  %s\n", i+1, m.Title, m.Note)
		fmt.Printf("       %s  %s\n", m.Repo, status)
	}
	fmt.Println("  [a] All")
	fmt.Println("  [q] Back")
	choice := ui.Ask(in, "Choice (e.g. 1, 2,4 or a): ", "")
	choice = strings.TrimSpace(strings.ToLower(choice))
	if choice == "" || choice == "q" || choice == "quit" {
		return
	}

	var picks []Model
	if choice == "a" || choice == "all" {
		picks = append(picks, Models...)
	} else {
		for _, part := range strings.Split(choice, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			idx, err := strconv.Atoi(part)
			if err != nil || idx < 1 || idx > len(Models) {
				fmt.Printf("  skip invalid: %q\n", part)
				continue
			}
			picks = append(picks, Models[idx-1])
		}
	}
	if len(picks) == 0 {
		fmt.Println("Nothing selected")
		return
	}

	fmt.Printf("\nWill prepare %d model(s):\n", len(picks))
	for _, m := range picks {
		fmt.Printf("  - %s (%s)\n", m.Title, m.Repo)
	}
	if !ui.Confirm(in, "Download (if needed) + convert") {
		fmt.Println("Cancelled")
		return
	}

	ok, fail := 0, 0
	for i, m := range picks {
		fmt.Printf("\n—— [%d/%d] %s ——\n", i+1, len(picks), m.Title)
		if err := prepareOne(m); err != nil {
			fmt.Printf("❌ %s: %v\n", m.Repo, err)
			fail++
			continue
		}
		ok++
	}
	fmt.Printf("\nDone: %d ok, %d failed. Menu [1] Run to chat.\n", ok, fail)
}

func prepareOne(m Model) error {
	if path := findExistingEntity(m); path != "" {
		fmt.Printf("  already have entity: %s (skip download/convert)\n", path)
		return nil
	}
	if _, err := hub.DownloadRepo(m.Repo); err != nil {
		return fmt.Errorf("download: %w", err)
	}
	out, err := convert.PackRepo(m.Repo)
	if err != nil {
		return fmt.Errorf("convert: %w", err)
	}
	fmt.Printf("  ready: %s\n", out)
	return nil
}

func findExistingEntity(m Model) string {
	var candidates []string
	if m.FormatHint != "" {
		candidates = append(candidates, paths.EntityPathForFormat(m.Repo, m.FormatHint))
		if m.FormatHint == "q4_0" {
			candidates = append(candidates, paths.EntityPathLegacyQ4(m.Repo))
		}
	} else {
		candidates = []string{
			paths.EntityPathForFormat(m.Repo, "binarypacked"),
			paths.EntityPathForFormat(m.Repo, "q4_0"),
			paths.EntityPath(m.Repo),
			paths.EntityPathLegacyQ4(m.Repo),
		}
	}
	for _, p := range candidates {
		if st, err := os.Stat(p); err == nil && st.Size() > 1024 {
			return p
		}
	}
	return ""
}

func entityStatus(repo string) string {
	for _, m := range Models {
		if m.Repo == repo {
			if p := findExistingEntity(m); p != "" {
				return "[entity ready]"
			}
			break
		}
	}
	snap := paths.ManualSnapshotDir(paths.HubRoot(), repo)
	if st, err := os.Stat(snap); err == nil && st.IsDir() {
		return "[snapshot only]"
	}
	return "[not local]"
}
