package ui

import "github.com/charmbracelet/lipgloss"

var (
	Bold      = lipgloss.NewStyle().Bold(true)
	Dim       = lipgloss.NewStyle().Foreground(SilverMist)
	Success   = lipgloss.NewStyle().Foreground(Green)
	Error     = lipgloss.NewStyle().Foreground(Red)
	Warning   = lipgloss.NewStyle().Foreground(Yellow)
	Info      = lipgloss.NewStyle().Foreground(Petroleo)
	Accent    = lipgloss.NewStyle().Foreground(Carmesim)
	Secondary = lipgloss.NewStyle().Foreground(Petroleo)
	Muted     = lipgloss.NewStyle().Foreground(SilverMist)

	Title = lipgloss.NewStyle().
		Bold(true).
		Foreground(Carmesim)

	Subtitle = lipgloss.NewStyle().
			Foreground(Petroleo)

	Label = lipgloss.NewStyle().
		Bold(true).
		Foreground(Pearl)

	Value = lipgloss.NewStyle().
		Foreground(White)

	URL = lipgloss.NewStyle().
		Foreground(Petroleo).
		Underline(true)

	StatusRunning  = lipgloss.NewStyle().Foreground(Green).Bold(true)
	StatusStopped  = lipgloss.NewStyle().Foreground(Red).Bold(true)
	StatusBuilding = lipgloss.NewStyle().Foreground(Yellow).Bold(true)
)
