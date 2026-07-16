package ui

import (
	"bufio"
	"fmt"
	"strings"
)

// Ask prints prompt and returns trimmed line; empty → def.
func Ask(in *bufio.Reader, prompt, def string) string {
	fmt.Print(prompt)
	line, err := in.ReadString('\n')
	if err != nil {
		return def
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return def
	}
	return line
}

// Confirm asks y/N.
func Confirm(in *bufio.Reader, prompt string) bool {
	a := strings.ToLower(Ask(in, prompt+" [y/N]: ", "n"))
	return a == "y" || a == "yes"
}
