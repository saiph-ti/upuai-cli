package cmd

import (
	"encoding/json"
	"testing"

	"github.com/upuai-cloud/cli/internal/api"
)

// `scheduler create` exige exatamente um entre --schedule e --once: job sem
// nenhum dos dois não tem quando rodar, e com os dois a intenção é ambígua.
func TestValidateSchedulerCreate(t *testing.T) {
	cases := []struct {
		name                   string
		job, command, schedule string
		once                   bool
		wantErr                bool
	}{
		{name: "agendado", job: "nightly", command: "rails db:cleanup", schedule: "0 3 * * *"},
		{name: "uma vez", job: "import", command: "php artisan ibge:import", once: true},
		{name: "sem schedule nem once", job: "import", command: "php artisan ibge:import", wantErr: true},
		{name: "schedule e once juntos", job: "import", command: "x", schedule: "@daily", once: true, wantErr: true},
		{name: "sem nome", command: "x", once: true, wantErr: true},
		{name: "sem comando", job: "import", once: true, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateSchedulerCreate(tc.job, tc.command, tc.schedule, tc.once)
			if (err != nil) != tc.wantErr {
				t.Errorf("validateSchedulerCreate = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

// A API devolve schedule null para job sob demanda.
func TestScheduledJob_OnDemandOnTheWire(t *testing.T) {
	cases := []struct {
		name, payload, wantLabel string
		wantOnDemand             bool
	}{
		{"sob demanda", `{"name":"import","schedule":null}`, "on demand", true},
		{"agendado", `{"name":"nightly","schedule":"0 3 * * *"}`, "0 3 * * *", false},
		{"campo ausente", `{"name":"import"}`, "on demand", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var job api.ScheduledJob
			if err := json.Unmarshal([]byte(tc.payload), &job); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if job.OnDemand() != tc.wantOnDemand {
				t.Errorf("OnDemand = %v, want %v", job.OnDemand(), tc.wantOnDemand)
			}
			if got := scheduleLabel(&job); got != tc.wantLabel {
				t.Errorf("scheduleLabel = %q, want %q", got, tc.wantLabel)
			}
		})
	}
}

// --once não manda schedule: é a ausência do campo que cria o job sob demanda.
func TestCreateScheduledJobRequest_OmitsEmptySchedule(t *testing.T) {
	got, err := json.Marshal(api.CreateScheduledJobRequest{Name: "import", Command: "php artisan ibge:import"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if want := `{"name":"import","command":"php artisan ibge:import"}`; string(got) != want {
		t.Errorf("payload = %s, want %s", got, want)
	}
}
