package cmd

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/upuai-cloud/cli/internal/api"
)

const credentialsRotatedJSON = `{"rotated":true,"repaired":true,"affectedServices":[{"id":"svc-web","name":"web"},{"id":"svc-worker","name":"worker"}]}`

func TestDBCredentialsRepairWire(t *testing.T) {
	requests := newDBOpsAPI(t, map[string]dbOpsRoute{
		"POST " + dbOpsBase + "/database/credentials/repair": {200, `{"rotated":false,"repaired":false,"affectedServices":[]}`},
	})

	var err error
	out := captureStdout(t, func() { err = dbCredentialsRepairCmd.RunE(dbCredentialsRepairCmd, nil) })
	if err != nil {
		t.Fatalf("repair: %v", err)
	}
	if !strings.Contains(out, "credentials are working") {
		t.Fatalf("saída = %q", out)
	}
	want := dbOpsRequest{Method: http.MethodPost, Path: dbOpsBase + "/database/credentials/repair", Body: ""}
	if len(*requests) != 1 || (*requests)[0] != want {
		t.Fatalf("requests = %+v, quero %+v", *requests, want)
	}
}

func TestDBCredentialsRepairReportsReappliedAccount(t *testing.T) {
	newDBOpsAPI(t, map[string]dbOpsRoute{
		"POST " + dbOpsBase + "/database/credentials/repair": {200, `{"rotated":false,"repaired":true,"affectedServices":[]}`},
	})

	var err error
	out := captureStdout(t, func() { err = dbCredentialsRepairCmd.RunE(dbCredentialsRepairCmd, nil) })
	if err != nil {
		t.Fatalf("repair: %v", err)
	}
	// A conta voltou com a MESMA senha: ninguém precisa de redeploy.
	for _, want := range []string{"account re-applied", "no redeploy needed"} {
		if !strings.Contains(out, want) {
			t.Fatalf("saída sem %q: %q", want, out)
		}
	}
}

// As variáveis gravadas estavam defasadas do cluster: o valor que os consumidores
// carregam mudou, e eles precisam de redeploy.
func TestDBCredentialsRepairListsServicesWhenValuesChanged(t *testing.T) {
	newDBOpsAPI(t, map[string]dbOpsRoute{
		"POST " + dbOpsBase + "/database/credentials/repair": {200, `{"rotated":false,"repaired":false,"affectedServices":[{"id":"svc-web","name":"web"}]}`},
	})

	var err error
	out := captureStdout(t, func() { err = dbCredentialsRepairCmd.RunE(dbCredentialsRepairCmd, nil) })
	if err != nil {
		t.Fatalf("repair: %v", err)
	}
	for _, want := range []string{"brought up to date", "redeploy", "web"} {
		if !strings.Contains(out, want) {
			t.Fatalf("saída sem %q: %q", want, out)
		}
	}
}

// Rotação derruba o acesso de quem ainda usa a senha antiga: sem TTY exige --yes
// e nenhuma chamada sai.
func TestDBCredentialsRotateNeedsConfirmationWithoutTTY(t *testing.T) {
	requests := newDBOpsAPI(t, map[string]dbOpsRoute{
		"POST " + dbOpsBase + "/database/credentials/rotate": {200, credentialsRotatedJSON},
	})

	err := dbCredentialsRotateCmd.RunE(dbCredentialsRotateCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("erro = %v, quero pedido de --yes (stdin não é TTY nos testes)", err)
	}
	if len(*requests) != 0 {
		t.Fatalf("nenhuma chamada deveria sair sem confirmação, houve %+v", *requests)
	}
}

func TestDBCredentialsRotateWithYes(t *testing.T) {
	requests := newDBOpsAPI(t, map[string]dbOpsRoute{
		"POST " + dbOpsBase + "/database/credentials/rotate": {200, credentialsRotatedJSON},
	})
	flagYes = true

	var err error
	out := captureStdout(t, func() { err = dbCredentialsRotateCmd.RunE(dbCredentialsRotateCmd, nil) })
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	for _, want := range []string{"password rotated", "web, worker", "upuai variables list"} {
		if !strings.Contains(out, want) {
			t.Fatalf("saída sem %q: %q", want, out)
		}
	}
	want := dbOpsRequest{Method: http.MethodPost, Path: dbOpsBase + "/database/credentials/rotate", Body: ""}
	if len(*requests) != 1 || (*requests)[0] != want {
		t.Fatalf("requests = %+v, quero %+v", *requests, want)
	}
}

func TestDBCredentialsRotateJSON(t *testing.T) {
	newDBOpsAPI(t, map[string]dbOpsRoute{
		"POST " + dbOpsBase + "/database/credentials/rotate": {200, credentialsRotatedJSON},
	})
	flagYes = true
	flagOutput = "json"

	var err error
	out := captureStdout(t, func() { err = dbCredentialsRotateCmd.RunE(dbCredentialsRotateCmd, nil) })
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	var got api.DatabaseCredentialsResult
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("saída não é JSON: %v — %q", err, out)
	}
	if !got.Rotated || len(got.AffectedServices) != 2 || got.AffectedServices[0].Name != "web" {
		t.Fatalf("resultado = %+v", got)
	}
	// A senha nunca trafega por este comando.
	if strings.Contains(strings.ToLower(out), "password") {
		t.Fatalf("a saída JSON não pode carregar senha: %q", out)
	}
}

func TestDBCredentialsErrorsAreActionable(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"engine sem rotação", 409, `{"code":"OPERATION_NOT_ALLOWED","message":"x","details":{"reason":"mysqlOnly"}}`, "only available on managed MySQL"},
		{"banco fora do ar", 409, `{"error":"DB_NOT_READY","code":"DB_NOT_READY","message":"x"}`, "not up yet"},
		{"outra operação", 409, `{"error":"DB_OPERATION_IN_PROGRESS","code":"DB_OPERATION_IN_PROGRESS","message":"x"}`, "another operation"},
		{"deploy em andamento", 409, `{"code":"OPERATION_NOT_ALLOWED","message":"x","details":{"reason":"deploymentInProgress"}}`, "deployment is in progress"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			newDBOpsAPI(t, map[string]dbOpsRoute{
				"POST " + dbOpsBase + "/database/credentials/repair": {tc.status, tc.body},
			})
			err := dbCredentialsRepairCmd.RunE(dbCredentialsRepairCmd, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("erro = %v, quero conter %q", err, tc.want)
			}
		})
	}
}
