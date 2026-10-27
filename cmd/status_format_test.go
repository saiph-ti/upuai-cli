package cmd

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/upuai-cloud/cli/internal/ui"
)

// unhealthy (serviço web no ar que parou de responder) é tão quebrado quanto um
// crash para quem usa o serviço: mesma cor. Antes caía no default cinza.
func TestFormatServiceStatus_BrokenStates(t *testing.T) {
	// Sem TTY o lipgloss não emite cor e todos os estilos viram o mesmo texto —
	// o teste não distinguiria nada. Força um perfil de cor.
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	if ui.StatusStopped.Render("x") == ui.Muted.Render("x") {
		t.Fatal("broken and muted styles render the same: the test cannot tell them apart")
	}

	for _, s := range []string{"failed", "crashed", "unhealthy"} {
		if got, want := formatServiceStatus(s), ui.StatusStopped.Render("● "+s); got != want {
			t.Errorf("%s: got %q, want the broken style %q", s, got, want)
		}
	}
	if got, want := formatServiceStatus("something-new"), ui.Muted.Render("● something-new"); got != want {
		t.Errorf("unknown status must stay muted: got %q", got)
	}
}
