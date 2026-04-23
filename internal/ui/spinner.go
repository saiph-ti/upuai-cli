package ui

import (
	"fmt"
	"os"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type SpinnerModel struct {
	spinner spinner.Model
	message string
	done    bool
	err     error
}

func NewSpinner(message string) SpinnerModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(Carmesim)
	return SpinnerModel{spinner: s, message: message}
}

func (m SpinnerModel) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m SpinnerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "q" || msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case SpinnerDoneMsg:
		m.done = true
		m.err = msg.Err
		return m, tea.Quit
	}
	return m, nil
}

func (m SpinnerModel) View() string {
	if m.done {
		if m.err != nil {
			return Error.Render("✗") + " " + m.message + "\n"
		}
		return Success.Render("✓") + " " + m.message + "\n"
	}
	return m.spinner.View() + " " + m.message + "\n"
}

type SpinnerDoneMsg struct {
	Err error
}

func isTTY() bool {
	fi, err := os.Stderr.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

func RunWithSpinner(message string, fn func() error) error {
	if !isTTY() {
		fmt.Fprintln(os.Stderr, "  "+message)
		return fn()
	}

	m := NewSpinner(message)
	p := tea.NewProgram(m, tea.WithOutput(os.Stderr))

	errCh := make(chan error, 1)
	go func() {
		err := fn()
		errCh <- err
		p.Send(SpinnerDoneMsg{Err: err})
	}()

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("spinner error: %w", err)
	}

	return <-errCh
}
