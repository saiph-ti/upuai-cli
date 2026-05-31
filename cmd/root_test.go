package cmd

import (
	"strings"
	"testing"

	"github.com/upuai-cloud/cli/internal/api"
)

func TestMatchProjectRef(t *testing.T) {
	projects := []api.Project{
		{ID: "cmpr4dbk900390md4uojkveq1", Name: "adv-os"},
		{ID: "cmbillingxxxxxxxxxxxxxxxxx", Name: "billing", Slug: "billing-svc"},
	}
	tests := []struct {
		name string
		ref  string
		want string
	}{
		{"exact id", "cmpr4dbk900390md4uojkveq1", "cmpr4dbk900390md4uojkveq1"},
		{"name exact", "adv-os", "cmpr4dbk900390md4uojkveq1"},
		{"name case-insensitive", "ADV-OS", "cmpr4dbk900390md4uojkveq1"},
		{"slug match", "billing-svc", "cmbillingxxxxxxxxxxxxxxxxx"},
		{"unknown id passes through", "cmnotinthislist0000000000x", "cmnotinthislist0000000000x"},
		{"unknown name passes through", "ghost", "ghost"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := matchProjectRef(projects, tc.ref)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("matchProjectRef(%q) = %q, want %q", tc.ref, got, tc.want)
			}
		})
	}
}

func TestMatchProjectRefAmbiguous(t *testing.T) {
	// Nome não é único por tenant — dois projetos "dup" (case-insensitive) → erro com IDs.
	projects := []api.Project{
		{ID: "id-alpha", Name: "dup"},
		{ID: "id-beta", Name: "DUP"},
	}
	_, err := matchProjectRef(projects, "dup")
	if err == nil {
		t.Fatal("expected ambiguity error, got nil")
	}
	for _, want := range []string{"ambiguous", "id-alpha", "id-beta"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err.Error(), want)
		}
	}
}
