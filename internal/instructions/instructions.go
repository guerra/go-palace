// Package instructions provides embedded .md instruction files for CLI output.
package instructions

import (
	"embed"
	"fmt"
	"sort"
	"strings"
)

//go:embed *.md
var content embed.FS

var available = []string{"init", "search", "mine", "help", "status"}

// Get returns the instruction text for the given name.
func Get(name string) (string, error) {
	for _, a := range available {
		if a == name {
			data, err := content.ReadFile(name + ".md")
			if err != nil {
				return "", fmt.Errorf("instructions: read %s: %w", name, err)
			}
			return string(data), nil
		}
	}
	return "", fmt.Errorf("instructions: unknown %q (available: %s)", name, strings.Join(Available(), ", "))
}

// Available returns the sorted list of available instruction names.
func Available() []string {
	out := make([]string, len(available))
	copy(out, available)
	sort.Strings(out)
	return out
}
