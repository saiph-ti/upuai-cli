package api

import (
	"encoding/json"
	"testing"
)

// Regressão: POST .../domains devolve {primary, sibling} (o par apex/www que a
// API cria junto), não um Domain solto — decodificar como Domain deixava todos
// os campos vazios e a CLI imprimia "Domain  added".
func TestCreateDomainResponseDecoding(t *testing.T) {
	payload := `{
		"primary": {"id":"p1","hostname":"petalia.com.br","type":"custom","status":"pending","createdAt":"2026-08-26T00:00:00Z"},
		"sibling": {"id":"s1","hostname":"www.petalia.com.br","type":"custom","status":"pending","createdAt":"2026-08-26T00:00:00Z",
		            "redirectTo":{"domainId":"p1","hostname":"petalia.com.br","status":301}}
	}`
	var got CreateDomainResponse
	if err := json.Unmarshal([]byte(payload), &got); err != nil {
		t.Fatal(err)
	}
	if got.Primary.Domain != "petalia.com.br" || got.Primary.RedirectTo != nil {
		t.Fatalf("primary decodificado errado: %+v", got.Primary)
	}
	if got.Sibling == nil || got.Sibling.Domain != "www.petalia.com.br" {
		t.Fatalf("sibling decodificado errado: %+v", got.Sibling)
	}
	if r := got.Sibling.RedirectTo; r == nil || r.DomainID != "p1" || r.Hostname != "petalia.com.br" || r.Status != 301 {
		t.Fatalf("redirectTo do sibling decodificado errado: %+v", got.Sibling.RedirectTo)
	}

	var noSibling CreateDomainResponse
	if err := json.Unmarshal([]byte(`{"primary":{"id":"p2","hostname":"app.acme.com"},"sibling":null}`), &noSibling); err != nil {
		t.Fatal(err)
	}
	if noSibling.Sibling != nil {
		t.Fatalf("sibling null deve virar nil, veio %+v", noSibling.Sibling)
	}
}

// "redirectTo": null precisa ir explícito no PATCH — é assim que se remove.
func TestUpdateDomainBodyKeepsExplicitNull(t *testing.T) {
	body := map[string]any{"redirectTo": nil}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"redirectTo":null}` {
		t.Fatalf("body = %s", raw)
	}
}
