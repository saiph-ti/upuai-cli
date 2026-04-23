package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type OutputFormat string

const (
	FormatTable OutputFormat = "table"
	FormatJSON  OutputFormat = "json"
	FormatText  OutputFormat = "text"
)

func ParseOutputFormat(s string) OutputFormat {
	switch strings.ToLower(s) {
	case "json":
		return FormatJSON
	case "text":
		return FormatText
	default:
		return FormatTable
	}
}

func PrintJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func PrintText(lines ...string) {
	for _, line := range lines {
		fmt.Println(line)
	}
}

func PrintKeyValue(pairs ...string) {
	if len(pairs)%2 != 0 {
		return
	}
	maxLen := 0
	for i := 0; i < len(pairs); i += 2 {
		if len(pairs[i]) > maxLen {
			maxLen = len(pairs[i])
		}
	}
	for i := 0; i < len(pairs); i += 2 {
		key := pairs[i]
		val := pairs[i+1]
		fmt.Printf("  %s  %s\n",
			Label.Render(fmt.Sprintf("%-*s", maxLen, key)),
			Value.Render(val),
		)
	}
}

func PrintSuccess(msg string) {
	fmt.Println(Success.Render("✓") + " " + msg)
}

func PrintError(msg string) {
	fmt.Fprintln(os.Stderr, Error.Render("✗")+" "+msg)
}

func PrintWarning(msg string) {
	fmt.Println(Warning.Render("!") + " " + msg)
}

func PrintInfo(msg string) {
	fmt.Println(Info.Render("ℹ") + " " + msg)
}
