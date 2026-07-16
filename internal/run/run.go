package run

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/openfluke/octo/internal/catalog"
	"github.com/openfluke/octo/internal/ui"
	"github.com/openfluke/welvet/entity"
	"github.com/openfluke/welvet/tokenizer"
	"github.com/openfluke/welvet/transformer"
)

// Menu picks a .entity and runs generate/chat.
func Menu(in *bufio.Reader) {
	fmt.Println("\nRun .entity model")
	ents := catalog.ListEntities()
	if len(ents) == 0 {
		fmt.Println("No .entity files. Flow: [2] Download → [3] Convert → [1] Run.")
		fmt.Println("Example paste for download: HuggingFaceTB/SmolLM2-135M-Instruct")
		return
	}
	for i, e := range ents {
		status := "?"
		if e.Meta != nil {
			if s, ok := e.Meta["status"].(string); ok {
				status = s
			}
		}
		fmt.Printf("  [%d] %s  (status=%s, %d bytes)\n", i+1, e.RepoID, status, e.Bytes)
	}
	choice := ui.Ask(in, "Choice: ", "1")
	idx, err := strconv.Atoi(choice)
	if err != nil || idx < 1 || idx > len(ents) {
		fmt.Println("Invalid")
		return
	}
	e := ents[idx-1]
	fmt.Printf("\nSelected: %s\n  path: %s\n", e.RepoID, e.Path)

	magic, err := entity.PeekMagic(e.Path)
	if err != nil || magic != entity.Magic {
		if e.Meta != nil {
			if st, ok := e.Meta["status"].(string); ok && st == "envelope" {
				fmt.Println("\n🚧 This file is an Octo envelope (metadata only).")
				fmt.Println("   Re-run menu [3] Convert with FormatNone (0) to pack weights.")
				return
			}
		}
		fmt.Printf("❌ Not a packed Welvet ENTITY file (magic=%q)\n", magic)
		return
	}

	model, err := transformer.LoadEntity(e.Path)
	if err != nil {
		fmt.Printf("❌ Load entity: %v\n", err)
		return
	}

	tokPath := model.TokenizerPath
	if tokPath == "" && model.Snapshot != "" {
		tokPath = filepath.Join(model.Snapshot, "tokenizer.json")
	}
	if tokPath == "" {
		fmt.Println("❌ No tokenizer.json path in entity header or snapshot")
		return
	}
	tok, err := tokenizer.LoadTokenizer(tokPath)
	if err != nil {
		fmt.Printf("❌ Tokenizer: %v\n", err)
		return
	}

	system := ui.Ask(in, "System prompt (blank=helpful assistant): ", "")
	if strings.TrimSpace(system) == "" {
		system = "You are a helpful assistant."
	}

	fmt.Println("Chat started (blank line to quit). Streams tokens + tok/s like Lucy.")
	var turns []transformer.Turn
	encode := func(text string, addSpecial bool) []uint32 { return tok.Encode(text, addSpecial) }
	decode := func(ids []uint32, skipSpecial bool) string { return tok.Decode(ids, skipSpecial) }

	for {
		user := ui.Ask(in, "You: ", "")
		if strings.TrimSpace(user) == "" {
			fmt.Println("Bye.")
			break
		}
		fmt.Print("Assistant: ")
		reply, _, err := model.Generate(
			encode, decode, turns, system, user,
			transformer.GenOptions{MaxTokens: 128},
		)
		if err != nil {
			fmt.Printf("\n❌ Generate: %v\n", err)
			continue
		}
		turns = append(turns, transformer.Turn{User: user, Assistant: reply})
		_ = os.Stdout.Sync()
	}
}
