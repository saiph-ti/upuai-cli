package cmd

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/upuai-cloud/cli/internal/api"
)

func TestDBVersionShowsPendingUpdate(t *testing.T) {
	newDBOpsAPI(t, map[string]dbOpsRoute{
		"GET " + dbOpsBase + "/instance/version": {200, `{"engine":"postgresql","version":"16","updateAvailable":true}`},
	})

	var err error
	out := captureStdout(t, func() { err = dbVersionCmd.RunE(dbVersionCmd, nil) })
	if err != nil {
		t.Fatalf("db version: %v", err)
	}
	for _, want := range []string{"postgresql", "16", "available", "upuai db update"} {
		if !strings.Contains(out, want) {
			t.Errorf("saída sem %q:\n%s", want, out)
		}
	}
}

func TestDBVersionJSON(t *testing.T) {
	newDBOpsAPI(t, map[string]dbOpsRoute{
		"GET " + dbOpsBase + "/instance/version": {200, `{"engine":"postgresql","version":"18","updateAvailable":false}`},
	})
	flagOutput = "json"

	var err error
	out := captureStdout(t, func() { err = dbVersionCmd.RunE(dbVersionCmd, nil) })
	if err != nil {
		t.Fatalf("db version -o json: %v", err)
	}
	var got api.DatabaseVersion
	if jerr := json.Unmarshal([]byte(out), &got); jerr != nil || got.Version != "18" || got.UpdateAvailable || got.Engine != "postgresql" {
		t.Fatalf("JSON = %q (%v)", out, jerr)
	}
}

// Sem atualização pendente não há restart a confirmar nem POST a fazer —
// scripts idempotentes (`db update --yes`) saem 0.
func TestDBUpdateNoopWhenUpToDate(t *testing.T) {
	requests := newDBOpsAPI(t, map[string]dbOpsRoute{
		"GET " + dbOpsBase + "/instance/version": {200, `{"engine":"postgresql","version":"16","updateAvailable":false}`},
	})

	var err error
	out := captureStdout(t, func() { err = dbUpdateCmd.RunE(dbUpdateCmd, nil) })
	if err != nil {
		t.Fatalf("db update: %v", err)
	}
	if !strings.Contains(out, "already up to date") {
		t.Fatalf("saída = %q", out)
	}
	for _, r := range *requests {
		if r.Method == http.MethodPost {
			t.Fatalf("POST não deveria sair: %+v", *requests)
		}
	}
}

func TestDBUpdateNeedsConfirmationWithoutTTY(t *testing.T) {
	requests := newDBOpsAPI(t, map[string]dbOpsRoute{
		"GET " + dbOpsBase + "/instance/version": {200, `{"engine":"postgresql","version":"16","updateAvailable":true}`},
	})

	err := dbUpdateCmd.RunE(dbUpdateCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("erro = %v, quero pedido de --yes", err)
	}
	for _, r := range *requests {
		if r.Method == http.MethodPost {
			t.Fatalf("o restart não pode sair sem confirmação: %+v", *requests)
		}
	}
}

func TestDBUpdateAcceptedWithoutWait(t *testing.T) {
	requests := newDBOpsAPI(t, map[string]dbOpsRoute{
		"GET " + dbOpsBase + "/instance/version":      {200, `{"engine":"postgresql","version":"16","updateAvailable":true}`},
		"POST " + dbOpsBase + "/instance/maintenance": {202, `{"status":"accepted","deploymentId":"dep-1","version":"16"}`},
	})
	flagYes = true

	var err error
	out := captureStdout(t, func() { err = dbUpdateCmd.RunE(dbUpdateCmd, nil) })
	if err != nil {
		t.Fatalf("db update: %v", err)
	}
	if !strings.Contains(out, "dep-1") || !strings.Contains(out, "--wait") {
		t.Fatalf("saída = %q", out)
	}
	want := []dbOpsRequest{
		{Method: http.MethodGet, Path: dbOpsBase + "/instance/version"},
		{Method: http.MethodPost, Path: dbOpsBase + "/instance/maintenance"},
	}
	if len(*requests) != len(want) {
		t.Fatalf("requests = %+v, quero %+v", *requests, want)
	}
	for i := range want {
		if (*requests)[i] != want[i] {
			t.Fatalf("request[%d] = %+v, quero %+v", i, (*requests)[i], want[i])
		}
	}
}

func TestDBUpdateWaitSuccess(t *testing.T) {
	requests := newDBOpsAPI(t, map[string]dbOpsRoute{
		"GET " + dbOpsBase + "/instance/version":      {200, `{"engine":"postgresql","version":"16","updateAvailable":true}`},
		"POST " + dbOpsBase + "/instance/maintenance": {202, `{"status":"accepted","deploymentId":"dep-1","version":"16"}`},
		"GET /deployments/dep-1":                      {200, `{"id":"dep-1","status":"SUCCESS","trigger":"DB_VERSION_CHANGE"}`},
	})
	flagYes, dbUpdateWait = true, true

	var err error
	out := captureStdout(t, func() { err = dbUpdateCmd.RunE(dbUpdateCmd, nil) })
	if err != nil {
		t.Fatalf("db update --wait: %v", err)
	}
	if !strings.Contains(out, "Database updated") {
		t.Fatalf("saída = %q", out)
	}
	last := (*requests)[len(*requests)-1]
	if last.Method != http.MethodGet || last.Path != "/deployments/dep-1" {
		t.Fatalf("o --wait deveria acompanhar o deployment devolvido: %+v", *requests)
	}
}

// Falha terminal sai não-zero (CI/agentes ramificam pelo exit code).
func TestDBUpdateWaitFailureIsNonZero(t *testing.T) {
	newDBOpsAPI(t, map[string]dbOpsRoute{
		"GET " + dbOpsBase + "/instance/version":      {200, `{"engine":"postgresql","version":"16","updateAvailable":true}`},
		"POST " + dbOpsBase + "/instance/maintenance": {202, `{"status":"accepted","deploymentId":"dep-1","version":"16"}`},
		"GET /deployments/dep-1":                      {200, `{"id":"dep-1","status":"FAILED","errorMessage":"version change did not converge"}`},
	})
	flagYes, dbUpdateWait = true, true

	var err error
	_ = captureStdout(t, func() { err = dbUpdateCmd.RunE(dbUpdateCmd, nil) })
	if err == nil || !strings.Contains(err.Error(), "FAILED") {
		t.Fatalf("erro = %v, quero falha não-zero", err)
	}
}

// Outro cliente aplicou entre o GET e o POST: a API responde noop (200).
func TestDBUpdateRaceNoop(t *testing.T) {
	newDBOpsAPI(t, map[string]dbOpsRoute{
		"GET " + dbOpsBase + "/instance/version":      {200, `{"engine":"postgresql","version":"16","updateAvailable":true}`},
		"POST " + dbOpsBase + "/instance/maintenance": {200, `{"status":"noop","version":"16"}`},
	})
	flagYes, dbUpdateWait = true, true

	var err error
	out := captureStdout(t, func() { err = dbUpdateCmd.RunE(dbUpdateCmd, nil) })
	if err != nil || !strings.Contains(out, "already up to date") {
		t.Fatalf("saída = %q, erro = %v", out, err)
	}
}

func TestDBUpdateCollationRiskGuidesToSupport(t *testing.T) {
	newDBOpsAPI(t, map[string]dbOpsRoute{
		"GET " + dbOpsBase + "/instance/version": {200, `{"engine":"postgresql","version":"16","updateAvailable":true}`},
		"POST " + dbOpsBase + "/instance/maintenance": {409, `{"statusCode":409,"error":"DB_MAINTENANCE_COLLATION_RISK","message":"collation risk",
			"details":{"databases":"app","actionable":true,"retryable":false}}`},
	})
	flagYes = true

	err := dbUpdateCmd.RunE(dbUpdateCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "contact support") || !strings.Contains(err.Error(), "app") {
		t.Fatalf("erro = %v", err)
	}
	if api.ErrorCode(err) != "DB_MAINTENANCE_COLLATION_RISK" {
		t.Fatalf("code perdido: %q", api.ErrorCode(err))
	}
}
