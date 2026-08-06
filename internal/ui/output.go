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
)

func ParseOutputFormat(s string) OutputFormat {
	switch strings.ToLower(s) {
	case "json":
		return FormatJSON
	default:
		return FormatTable
	}
}

func PrintJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
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

// PrintNotice reports something the CLI did on the user's behalf (e.g. switching
// the active workspace to match the linked project).
//
// It writes to STDERR on purpose: a notice is diagnostics, not payload. Emitting
// it on stdout would corrupt `upuai <cmd> -o json | jq` and every script that
// pipes CLI output — the same reason PrintError uses stderr. Anything a machine
// consumes belongs on stdout; anything a human reads about how the command ran
// belongs here.
func PrintNotice(msg string) {
	fmt.Fprintln(os.Stderr, Muted.Render("→ "+msg))
}
