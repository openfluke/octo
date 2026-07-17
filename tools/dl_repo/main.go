package main

import (
	"fmt"
	"os"

	"github.com/openfluke/octo/internal/hub"
)

func main() {
	repo := "prism-ml/bonsai-image-binary-4B-mlx-1bit"
	if len(os.Args) > 1 {
		repo = os.Args[1]
	}
	fmt.Printf("Downloading %s …\n", repo)
	snap, err := hub.DownloadRepo(repo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "download: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("OK", snap)
}
