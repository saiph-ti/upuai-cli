package ui

import (
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

func Confirm(message string) (bool, error) {
	var confirmed bool
	theme := huh.ThemeCharm()
	theme.Focused.Title = theme.Focused.Title.Foreground(Carmesim)
	theme.Focused.FocusedButton = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(Carmesim).
		Padding(0, 1)

	err := huh.NewConfirm().
		Title(message).
		Affirmative("Yes").
		Negative("No").
		Value(&confirmed).
		WithTheme(theme).
		Run()
	if err != nil {
		return false, err
	}
	return confirmed, nil
}

func SelectOne(title string, options []string) (string, error) {
	var selected string
	opts := make([]huh.Option[string], len(options))
	for i, o := range options {
		opts[i] = huh.NewOption(o, o)
	}

	theme := huh.ThemeCharm()
	theme.Focused.Title = theme.Focused.Title.Foreground(Carmesim)

	err := huh.NewSelect[string]().
		Title(title).
		Options(opts...).
		Value(&selected).
		WithTheme(theme).
		Run()
	if err != nil {
		return "", err
	}
	return selected, nil
}

func InputText(title, placeholder string) (string, error) {
	var value string

	theme := huh.ThemeCharm()
	theme.Focused.Title = theme.Focused.Title.Foreground(Carmesim)

	err := huh.NewInput().
		Title(title).
		Placeholder(placeholder).
		Value(&value).
		WithTheme(theme).
		Run()
	if err != nil {
		return "", err
	}
	return value, nil
}

func PrintBanner() {
	banner := lipgloss.NewStyle().
		Foreground(Carmesim).
		Bold(true).
		Render("Upuai Cloud")

	tagline := Dim.Render("Smart deploy. Brazilian infrastructure.")

	fmt.Printf("\n  %s  %s\n\n", banner, tagline)
}
